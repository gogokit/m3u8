package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gogokit/logs"
	"github.com/gogokit/m3u8"
)

func main() {
	// m3u8 ${m3u8Url} ${taskCnt} ${fileName}
	const (
		m3u8UrlIdx  = 1
		taskCntIdx  = 2
		fileNameIdx = 3
	)
	if len(os.Args) <= 1 {
		fmt.Printf("请输入命令 形如: m3u8 ${m3u8Url} ${taskCnt} ${fileName}\n")
		return
	}

	m3u8Url := os.Args[m3u8UrlIdx]

	var taskCnt int64
	if len(os.Args) > taskCntIdx+1 {
		var err error
		if taskCnt, err = strconv.ParseInt(os.Args[taskCntIdx], 10, 64); err != nil || taskCnt <= 0 {
			fmt.Printf("请输入正确的并发任务数!\n")
			return
		}
	}

	if taskCnt <= 0 {
		taskCnt = 10
	}

	var fileName string
	if len(os.Args) >= fileNameIdx+1 {
		fileName = os.Args[fileNameIdx]
	} else {
		fileName = time.Now().Format("20060102150405")
	}

	fmt.Printf("m3u8文件url:[%s]\n", m3u8Url)
	fmt.Printf("并发任务数:[%d]\n", taskCnt)
	fmt.Printf("保存文件名:[%s]\n", fileName)

	res, err := m3u8.Download(logs.NewCtxWithLogId(), m3u8Url, m3u8.ModelConvertToMP4, "m3u8_download_files", fileName, int(taskCnt), true)
	if err != nil {
		fmt.Printf("任务执行出错! 错误信息:%v\n", err)
		return
	}
	if res.MergeErr != "" {
		fmt.Printf("文件合并错误:%s\n", res.MergeErr)
	}
	if res.ConvToMP4Err != "" {
		fmt.Printf("转码为MP4错误:%s\n", res.ConvToMP4Err)
	}
}
