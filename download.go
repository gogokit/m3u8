package m3u8

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gogokit/gpool"
	"github.com/gogokit/util"
	"golang.org/x/time/rate"
)

type Model int

const (
	ModelMerged       = 0
	ModelConvertToMP4 = 1
)

func Download(ctx context.Context, m3u8Url string, model Model, fileDir string, tsFilePrefix string, workerCnt int, withBar bool) (*Result, error) {
	status, err := DownloadWithOpt(ctx, NewDefaultOption(m3u8Url, model, fileDir, tsFilePrefix, workerCnt))
	if err != nil {
		return nil, err
	}
	return GenResult(status, withBar), nil
}

func NewDefaultOption(m3u8Url string, model Model, fileDir string, tsFilePrefix string, workerCnt int) Option {
	return Option{
		M3u8Url:   m3u8Url,
		Model:     model,
		Qps:       2 * workerCnt,
		WorkerCnt: workerCnt,
		ChooseStream: func(infos []PlayInfo) PlayInfo {
			idx := 0
			val := infos[0].Resolution.Width * infos[0].Resolution.High
			for i := 1; i < len(infos); i++ {
				v := infos[i].Resolution.Width * infos[i].Resolution.High
				if v <= val {
					continue
				}
				idx = i
				val = v
			}
			return infos[idx]
		},
		RemoveSubTs:  true,
		FileDir:      fileDir,
		TsFilePrefix: tsFilePrefix,
	}
}

type Option struct {
	M3u8Url      string
	Model        Model
	Qps          int
	WorkerCnt    int
	ChooseStream func(infos []PlayInfo) PlayInfo
	RemoveSubTs  bool
	FileDir      string
	TsFilePrefix string
}

func DownloadWithOpt(ctx context.Context, opt Option) (Status, error) {
	return (&m3u8Downloader{
		ChooseStream: opt.ChooseStream,
		gp:           gpool.NewDefaultPool(opt.WorkerCnt),
		qpsLimit: func() *rate.Limiter {
			if opt.Qps <= 0 {
				return nil
			}
			return rate.NewLimiter(rate.Limit(opt.Qps), opt.Qps)
		}(),
		doMerge:        opt.Model >= ModelMerged,
		convToMP4:      opt.Model >= ModelConvertToMP4,
		ffmpeg:         "ffmpeg",
		removeSubTs:    opt.RemoveSubTs,
		fileDir:        opt.FileDir,
		tsFilePrefix:   opt.TsFilePrefix,
		allDone:        make(chan struct{}),
		stopSignalChan: make(chan struct{}),
	}).Download(ctx, opt.M3u8Url)
}

// Segment, Merger和ConvToMP4中有且只有一个为非nil
type Event struct {
	*Segment
	Merged         *bool
	MergedFilePath string
	MergeErr       string // 仅在Merged不为nil且*Merged为false时不为nil
	ConvToMP4      *bool
	ConvToMP4Err   string // 仅在ConvToMP4不为nil且*ConvToMP4为false时不为nil
	MP4FilePath    string
}

type m3u8Downloader struct {
	ChooseStream   func([]PlayInfo) PlayInfo
	gp             *gpool.Pool
	wg             sync.WaitGroup
	qpsLimit       *rate.Limiter
	fileDir        string
	tsFilePrefix   string
	doMerge        bool
	convToMP4      bool
	ffmpeg         string
	err            sync.Map
	removeSubTs    bool
	m3u8           *M3u8
	m3u8Copy       AllM3u8
	allDone        chan struct{}
	doneCnt        int32
	eventChan      chan Event
	stopSignalChan chan struct{}
}

type Result struct {
	Segments       []Segment
	Merged         bool
	MergeErr       string
	MergedFilePath string
	ConvToMP4      bool
	ConvToMP4Err   string
	MP4FilePath    string
}

type AllM3u8 struct {
	MastPlay *M3u8
	Common   *M3u8
}

type Status interface {
	TsTotal() int    // 任务总数
	TsComplete() int // 已经完成的任务数(包含失败和成功的任务)
	Done() <-chan struct{}
	M3u8() AllM3u8
	Event() <-chan Event // 每完成一个任务向此chan中写入
	Shutdown()           // 强制终止下载
}

type status struct {
	md *m3u8Downloader
}

func (s *status) TsTotal() int {
	return len(s.md.m3u8.Segments)
}

func (s *status) TsComplete() int {
	return int(atomic.LoadInt32(&s.md.doneCnt))
}

func (s *status) Done() <-chan struct{} {
	return s.md.allDone
}

func (s *status) M3u8() AllM3u8 {
	return AllM3u8{
		Common:   s.md.m3u8Copy.Common.Copy(),
		MastPlay: s.md.m3u8Copy.MastPlay.Copy(),
	}
}

func (s *status) Event() <-chan Event {
	return s.md.eventChan
}

func (s *status) Shutdown() {
	defer func() {
		if err := recover(); err != nil {
		}
	}()
	close(s.md.stopSignalChan)
}

func (md *m3u8Downloader) Download(ctx context.Context, m3u8Url string) (ret Status, err error) {
	if err = md.pre(ctx, m3u8Url); err != nil {
		return nil, err
	}

	util.Async(ctx, func() {
		defer func() {
			close(md.allDone)
		}()
		if err = md.startDownload(); err != nil {
			return
		}
		md.succ(ctx)
	})

	return &status{
		md: md,
	}, nil
}

// 预处理
func (md *m3u8Downloader) pre(ctx context.Context, m3u8Url string) (err error) {
	if md.m3u8, err = md.Parse(ctx, m3u8Url); err != nil {
		return fmt.Errorf("parse m3u8 error, %w", err)
	}

	md.m3u8Copy.Common = md.m3u8.Copy()

	if md.convToMP4 {
		if !md.doMerge {
			return errors.New("convert to mp4 need set merge be true")
		}

		if md.ffmpeg, err = exec.LookPath("ffmpeg"); err != nil {
			return fmt.Errorf("set to mp4, but look ffmpeg error, %w", err)
		}
	}

	now := time.Now().UnixNano()
	md.fileDir = strings.TrimSpace(md.fileDir)
	if md.fileDir == "" {
		md.fileDir = fmt.Sprintf("m3u8_download_%d", now)
	}

	md.tsFilePrefix = strings.TrimSpace(md.tsFilePrefix)
	if md.tsFilePrefix == "" {
		md.tsFilePrefix = fmt.Sprintf("ts_%d", now)
	}

	if err = createIfNotExists(md.fileDir); err != nil {
		return err
	}

	return nil
}

func (md *m3u8Downloader) needStop() bool {
	select {
	case <-md.stopSignalChan:
		return true
	default:
		return false
	}
}

func (md *m3u8Downloader) startDownload() error {
	var wg sync.WaitGroup
	for i := range md.m3u8.Segments {
		if md.needStop() {
			return nil
		}

		idx := i
		wg.Add(1)
		if _, err := md.gp.AddTask(func() {
			var (
				body []byte
				err  error
			)
			defer func() {
				if err != nil {
					md.m3u8.Segments[idx].ErrMsg = err.Error()
				}

				atomic.AddInt32(&md.doneCnt, 1)
				md.eventChan <- Event{
					Segment: &md.m3u8.Segments[idx],
				}

				wg.Done()
			}()

			if body, err = md.downloadAndDecryptOneTs(idx); err != nil {
				return
			}
			err = md.save(idx, body)
		}, nil, true); err != nil {
			return err
		}
	}
	wg.Wait()
	return nil
}

func (md *m3u8Downloader) succ(_ context.Context) {
	if md.needStop() || !md.doMerge {
		return
	}

	// 合并文件
	var fs []string
	for idx, v := range md.m3u8.Segments {
		if v.ErrMsg != "" {
			continue
		}
		fs = append(fs, md.fullPath(md.tsName(idx)))
	}

	mergedPath := md.tsFilePrefix + ".ts"
	if err := md.merge(fs, mergedPath); err != nil {
		md.eventChan <- Event{
			Merged: func() *bool {
				t := false
				return &t
			}(),
			MergeErr: err.Error(),
		}
		return
	}

	md.eventChan <- Event{
		Merged: func() *bool {
			t := true
			return &t
		}(),
		MergedFilePath: mergedPath,
	}

	if md.needStop() || !md.convToMP4 {
		return
	}

	mp4FilePath := md.tsFilePrefix + ".mp4"
	if err := md.toMP4(mergedPath, mp4FilePath); err != nil {
		md.eventChan <- Event{
			ConvToMP4: func() *bool {
				t := false
				return &t
			}(),
			ConvToMP4Err: err.Error(),
		}
		return
	}

	if md.removeSubTs {
		_ = os.Remove(mergedPath)
		_ = os.RemoveAll(md.fileDir)
	}

	md.eventChan <- Event{
		ConvToMP4: func() *bool {
			t := true
			return &t
		}(),
		MP4FilePath: mp4FilePath,
	}

	return
}

// merge 合并文件主函数
func (md *m3u8Downloader) merge(subTsFilePaths []string, mergedTsFilePath string) error {
	mergedTsFile, err := os.OpenFile(mergedTsFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, os.ModePerm)
	if err != nil {
		return fmt.Errorf("os.OpenFile %s error, %w", mergedTsFilePath, err)
	}
	defer func() {
		_ = mergedTsFile.Close()
	}()

	for _, v := range subTsFilePaths {
		if err = func() error {
			subTsFile, err := os.OpenFile(v, os.O_CREATE|os.O_RDONLY, os.ModePerm)
			if err != nil {
				return fmt.Errorf("os.OpenFile %s error, %w", v, err)
			}
			defer func() {
				_ = subTsFile.Close()
			}()

			body, err := ioutil.ReadAll(subTsFile)
			if err != nil {
				return fmt.Errorf("ioutil.ReadAll with %s error, %w", v, err)
			}

			if _, err = mergedTsFile.Write(body); err != nil {
				return fmt.Errorf("write to merged file error, %w", err)
			}

			if md.removeSubTs {
				if err = os.Remove(v); err != nil {
					return fmt.Errorf("os.Remove %s error, %w", v, err)
				}
			}
			return nil
		}(); err != nil {
			return err
		}
	}
	return nil
}

func (md *m3u8Downloader) toMP4(tsPath string, mp4Path string) error {
	// ffmpeg -i ${tsPath} -c:v copy -c:a aac -strict experimental -b:a 128k ${mp4Path}
	_, err := exec.Command(md.ffmpeg, []string{"-i", tsPath, "-c:v", "copy", "-c:a", "aac", "-strict", "experimental", "-b:a", "128k", mp4Path}...).Output()
	return err
}

func (md *m3u8Downloader) downloadAndDecryptOneTs(idx int) (body []byte, err error) {
	seg := md.m3u8.Segments[idx]
	util.Retry(func(sn int) (end bool) {
		body, err = md.httpGet(seg.Url)
		return err == nil
	}, 10, time.Second*10)

	if err != nil {
		return nil, err
	}

	if !seg.IsEncrypted() {
		return body, nil
	}

	body, err = decryptByAES128(body, []byte(seg.EncryptMeta.SecretKey), []byte(seg.EncryptMeta.IV))
	if err != nil {
		return nil, err
	}

	for i, v := range body {
		if v == uint8(71) {
			body = body[i:]
			break
		}
	}

	return body, nil
}

func (md *m3u8Downloader) tsName(idx int) string {
	return md.tsFilePrefix + fmt.Sprintf("_%d.ts", idx)
}

func (md *m3u8Downloader) fullPath(tsName string) string {
	return md.fileDir + "/" + tsName
}

// 保存索引为idx的文件
func (md *m3u8Downloader) save(idx int, body []byte) error {
	path := md.fullPath(md.tsName(idx))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, os.ModePerm)
	if err != nil {
		return fmt.Errorf("error in os.OpenFile, %w", err)
	}
	defer func() {
		_ = f.Close()
	}()
	if _, err = f.Write(body); err != nil {
		return fmt.Errorf("write to file error, %w", err)
	}
	return nil
}

func (md *m3u8Downloader) httpGet(u string) ([]byte, error) {
	if md.qpsLimit != nil {
		if err := md.qpsLimit.Wait(context.Background()); err != nil {
			return nil, fmt.Errorf("wait on limiter error, %w", err)
		}
	}

	resp, err := (&http.Client{
		Timeout: 30 * time.Second,
	}).Get(u)
	if err != nil {
		return nil, fmt.Errorf("http get %s error, %w", u, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("response StatusCode is %d, Status is %s", resp.StatusCode, resp.Status)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("io.ReadAll error, %w", err)
	}
	return body, nil
}

func (md *m3u8Downloader) Parse(ctx context.Context, link string) (ret *M3u8, err error) {
	var body []byte
	util.Retry(func(sn int) (end bool) {
		body, err = md.httpGet(link)
		return err == nil
	}, 10, time.Second*10)
	if err != nil {
		return nil, fmt.Errorf("http request[%s] fail, %v", link, err)
	}

	//解析请求体内容，m3u8中的内容
	m3u8, err := Parse(body, link)
	if err != nil {
		return nil, err
	}

	if len(m3u8.MastPlayList) > 0 {
		md.m3u8Copy.MastPlay = m3u8
		if len(m3u8.MastPlayList) == 1 {
			return md.Parse(ctx, m3u8.MastPlayList[0].M3u8Url)
		}
		if md.ChooseStream != nil {
			return md.Parse(ctx, md.ChooseStream(m3u8.MastPlayList).M3u8Url)
		}
		return nil, fmt.Errorf("link(%s) is master play list and has more than 1 stream, but ChooseStream not set", link)
	}

	if len(m3u8.Segments) == 0 {
		return nil, errors.New("ts files list is empty")
	}

	secretKeys := make(map[string]*string)
	for _, v := range m3u8.Segments {
		if !v.IsEncrypted() {
			continue
		}

		skUrl := v.EncryptMeta.SecretKeyUrl
		if _, ok := secretKeys[skUrl]; ok {
			continue
		}
		secretKeys[skUrl] = new(string)
	}

	var errMap sync.Map

	// 并发获取解密秘钥
	var wg sync.WaitGroup
	for k, v := range secretKeys {
		secretUrl := k
		secretValue := v

		wg.Add(1)
		if _, err := md.gp.AddTask(func() {
			defer wg.Done()
			var (
				body []byte
				err  error
			)
			util.Retry(func(sn int) (end bool) {
				body, err = md.httpGet(secretUrl)
				return err == nil
			}, 10, 10*time.Second)

			if err == nil {
				*secretValue = string(body)
			} else {
				errMap.Store(secretUrl, err)
			}
		}, nil, true); err != nil {
			_ = md.gp.Stop()
			return nil, fmt.Errorf("add task to gpool error, %w", err)
		}
	}
	wg.Wait()

	md.eventChan = make(chan Event, len(m3u8.Segments)+10)

	for i := range m3u8.Segments {
		if !m3u8.Segments[i].IsEncrypted() {
			continue
		}

		skUrl := m3u8.Segments[i].EncryptMeta.SecretKeyUrl
		if err, ok := errMap.Load(skUrl); ok {
			m3u8.Segments[i].ErrMsg = err.(error).Error()
			md.doneCnt++
			md.eventChan <- Event{
				Segment: &md.m3u8.Segments[i],
			}
			continue
		}

		m3u8.Segments[i].EncryptMeta.SecretKey = *secretKeys[skUrl]
	}

	return m3u8, nil
}
