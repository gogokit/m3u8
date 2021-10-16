package m3u8

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gogokit/util"
)

type M3u8 struct {
	Segments     []Segment
	MastPlayList []PlayInfo
	PlayListType string
	EndList      bool
}

func (m *M3u8) Copy() *M3u8 {
	if m == nil {
		return nil
	}
	ret := &M3u8{
		PlayListType: m.PlayListType,
		EndList:      m.EndList,
	}
	if m.Segments != nil {
		ret.Segments = make([]Segment, len(m.Segments), len(m.Segments))
		copy(ret.Segments, m.Segments)
	}
	if m.MastPlayList != nil {
		ret.MastPlayList = make([]PlayInfo, len(m.MastPlayList), len(m.MastPlayList))
		copy(ret.MastPlayList, m.MastPlayList)
	}
	return ret
}

type PlayInfo struct {
	M3u8Url    string
	ProgramId  int64
	BandWidth  int64
	Resolution Resolution
}

type Resolution struct {
	Width int64
	High  int64
}

type Segment struct {
	Idx         int
	Url         string
	Duration    time.Duration
	Sequence    int64
	EncryptMeta EncryptMeta
	ErrMsg      string
}

func (s Segment) IsEncrypted() bool {
	return s.EncryptMeta.Method == CryptMethodAES
}

type EncryptMeta struct {
	SecretKeyUrl string
	IV           string
	Method       string
	SecretKey    string
}

const (
	CryptMethodAES  = "AES-128"
	CryptMethodNONE = "NONE"
)

var attrReg = regexp.MustCompile(`([A-Z-]+)=("[^"\n\r]+"|[^",\s]+)`)

// 注意Parse不会填充SecretKey
func Parse(content []byte, m3u8Url string) (*M3u8, error) {
	urlStruct, err := url.Parse(m3u8Url)
	if err != nil {
		return nil, fmt.Errorf("m3u8 url illegal, %w", err)
	}

	if !urlStruct.IsAbs() {
		return nil, fmt.Errorf("m3u8 url %s is not absolute url", m3u8Url)
	}

	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(content))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	if !(len(lines) >= 1 && strings.TrimSpace(lines[0]) == "#EXTM3U") {
		return nil, errors.New("line:0, not begin with #EXTM3U")
	}

	var (
		ret         = &M3u8{}
		encryptMeta EncryptMeta
		seq         int64
		duration    time.Duration
	)

	for i := 1; i < len(lines); i++ {
		line := util.TrimWhite(lines[i])
		switch {
		case line == "":
		case !strings.HasPrefix(line, "#"):
			u, err := toUrl(line, urlStruct)
			if err != nil {
				return nil, fmt.Errorf("line:%d, ts file url %s is illegal, %w", i, line, err)
			}
			ret.Segments = append(ret.Segments, Segment{
				Idx:         len(ret.Segments),
				Url:         u,
				Duration:    duration,
				Sequence:    seq + int64(len(ret.Segments)),
				EncryptMeta: encryptMeta,
			})
		case !strings.HasPrefix(line, "#EXT"):
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			play := PlayInfo{}
			params := toParam(line)
			if v, ok := params["PROGRAM-ID"]; ok {
				pid, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("line:%d, PROGRAM-ID %s is not a number, %w", i, v, err)
				}
				play.ProgramId = pid
			}
			if v, ok := params["BANDWIDTH"]; ok {
				bandWidth, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("line:%d, BANDWIDTH %s is not a number, %w", i, v, err)
				}
				play.BandWidth = bandWidth
			}
			if v, ok := params["RESOLUTION"]; ok {
				arr := strings.Split(v, "x")
				if len(arr) != 2 {
					return nil, fmt.Errorf("line:%d, RESOLUTION %s is illegal", i, v)
				}
				width, err := strconv.ParseInt(arr[0], 10, 64)
				if err != nil {
					return nil, fmt.Errorf("line:%d, RESOLUTION %s is illegal, %w", i, v, err)
				}
				high, err := strconv.ParseInt(arr[1], 10, 64)
				if err != nil {
					return nil, fmt.Errorf("line:%d, RESOLUTION %s is illegal, %w", i, v, err)
				}
				play.Resolution.Width = width
				play.Resolution.High = high
			}

			i++
			line = util.TrimWhite(lines[i])
			u, err := toUrl(line, urlStruct)
			if err != nil {
				return nil, fmt.Errorf("line:%d, sub m3u8 url %s is illegal, %w", i, line, err)
			}
			play.M3u8Url = u
			ret.MastPlayList = append(ret.MastPlayList, play)
		case strings.HasPrefix(line, "#EXT-X-KEY"):
			encryptMeta = EncryptMeta{}
			params := toParam(line)
			if v, ok := params["METHOD"]; ok {
				if v != CryptMethodAES && v != CryptMethodNONE {
					return nil, fmt.Errorf("line:%d, unknown encrypt method %s", i, v)
				}
				encryptMeta.Method = v
			}
			if v, ok := params["URI"]; ok {
				u, err := toUrl(v, urlStruct)
				if err != nil {
					return nil, fmt.Errorf("line:%d, URI %s is illegal, %w", i, v, err)
				}
				encryptMeta.SecretKeyUrl = u
			}
			if v, ok := params["IV"]; ok {
				encryptMeta.IV = v
			}
		case strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE"):
			pos := strings.Index(line, ":")
			if pos < 0 {
				return nil, fmt.Errorf("line:%d, EXT-X-PLAYLIST-TYPE %s is illegal", i, line)
			}
			line = line[pos+1:]
			if line != "VOD" && line != "EVENT" {
				return nil, fmt.Errorf("line:%d, EXT-X-PLAYLIST-TYPE %s is illegal", i, line)
			}
			ret.PlayListType = line
		case strings.HasPrefix(line, "#EXTINF"):
			pos := strings.Index(line, ":")
			if pos < 0 {
				return nil, fmt.Errorf("line:%d, EXTINF %s is illegal", i, line)
			}
			line = line[pos+1:]
			pos = strings.Index(line, ",")
			if pos >= 0 {
				line = line[:pos]
			}
			d, err := strconv.ParseFloat(line, 64)
			if err != nil {
				return nil, fmt.Errorf("line:%d, EXTINF %s is illegal, %w", i, line, err)
			}
			duration = time.Duration(d * float64(time.Second))
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE"):
			pos := strings.Index(line, ":")
			if pos < 0 {
				return nil, fmt.Errorf("line:%d, EXT-X-MEDIA-SEQUENCE %s is illegal", i, line)
			}
			line = line[pos+1:]
			seq, err = strconv.ParseInt(line, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("line:%d, EXT-X-MEDIA-SEQUENCE %s is illegal", i, line)
			}
		case strings.HasPrefix(line, "#EXT-X-ENDLIST"):
			if line != "#EXT-X-ENDLIST" {
				return nil, fmt.Errorf("line:%d, EXT-X-ENDLIST %s is illegal", i, line)
			}
			ret.EndList = true
		}
	}
	return ret, nil
}

func toParam(l string) map[string]string {
	r := attrReg.FindAllStringSubmatch(l, -1)
	ret := make(map[string]string)
	for _, v := range r {
		ret[v[1]] = strings.Trim(v[2], "\"")
	}
	return ret
}

func toUrl(uri string, m3u8UrlStruct *url.URL) (ret string, err error) {
	defer func() {
		if _, err = url.Parse(uri); err != nil {
			err = fmt.Errorf("uri %s is illegal, %w", uri, err)
			return
		}
	}()

	if strings.HasPrefix(uri, "https://") || strings.HasPrefix(uri, "http://") {
		return uri, nil
	}

	m3u8Url := m3u8UrlStruct.String()
	if uri[0] == '/' {
		return m3u8UrlStruct.Scheme + "://" + m3u8UrlStruct.Host + uri, nil
	}

	if m3u8UrlStruct.Path == "" {
		return m3u8UrlStruct.Host + "/" + uri, nil
	}

	return m3u8Url[0:strings.LastIndex(m3u8Url, "/")] + "/" + uri, nil
}
