package main

import (
	"fmt"
	"time"

	"github.com/gogokit/util"

	"github.com/gogokit/logs"
	"github.com/gogokit/m3u8"
)

func main() {
	ctx := logs.NewCtxWithLogId()
	const m3u8Url = "http://devimages.apple.com/iphone/samples/bipbop/bipbopall.m3u8"
	res, err := m3u8.Download(ctx, m3u8Url, m3u8.ModelConvertToMP4, "m3u8_download_files", time.Now().Local().String(), 10, true)
	if err != nil {
		panic(err)
	}
	fmt.Printf("[AllInfo]=%v\n", util.Stringer(res))
}
