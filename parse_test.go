package m3u8

import (
	"github.com/gogokit/tostr"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestParse(t *testing.T) {
	Convey("TestParse", t, func() {
		Convey("Master Playlist", func() {
			const m3u8Content = `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=150000,RESOLUTION=416x234,CODECS="avc1.42e00a,mp4a.40.2"
low/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=240000,RESOLUTION=416x234,CODECS="avc1.42e00a,mp4a.40.2"
https://example.com/lo_mid/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=440000,RESOLUTION=416x234,CODECS="avc1.42e00a,mp4a.40.2"
http://example.com/hi_mid/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=640000,RESOLUTION=640x360,CODECS="avc1.42e00a,mp4a.40.2"
http://example.com/high/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=64000,CODECS="mp4a.40.5"
http://example.com/audio/index.m3u8`
			m3u8, err := Parse([]byte(m3u8Content), "http://example.com/")
			So(err, ShouldEqual, nil)
			So(tostr.String(m3u8), ShouldEqual, `{Segments:nil, MastPlayList:[{M3u8Url:"http://example.com/low/index.m3u8", ProgramId:0, BandWidth:150000, Resolution:{Width:416, High:234}}, {M3u8Url:"https://example.com/lo_mid/index.m3u8", ProgramId:0, BandWidth:240000, Resolution:{Width:416, High:234}}, {M3u8Url:"http://example.com/hi_mid/index.m3u8", ProgramId:0, BandWidth:440000, Resolution:{Width:416, High:234}}, {M3u8Url:"http://example.com/high/index.m3u8", ProgramId:0, BandWidth:640000, Resolution:{Width:640, High:360}}, {M3u8Url:"http://example.com/audio/index.m3u8", ProgramId:0, BandWidth:64000, Resolution:{Width:0, High:0}}], PlayListType:"", EndList:false}`)
		})

		Convey("Meida Playlist", func() {
			const m3u8Content = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:6
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-MEDIA-SEQUENCE:250
#EXT-X-KEY:METHOD=AES-128,URI="http://example.com/20201008/ojroOJOt/1000kb/hls/key.key"
#EXTINF:3,
20201008/ojroOJOt/1000kb/hls/nfTcXY3x.ts
#EXTINF:1.52,
/20201008/ojroOJOt/1000kb/hls/VtMpEYqz.ts
#EXTINF:3,
http://example.com/20201008/ojroOJOt/1000kb/hls/uqvfZRwE.ts
#EXT-X-ENDLIST
`
			m3u8, err := Parse([]byte(m3u8Content), "http://example.com/")
			So(err, ShouldEqual, nil)
			So(tostr.String(m3u8), ShouldEqual, `{Segments:[{Idx:0, Url:"http://example.com/20201008/ojroOJOt/1000kb/hls/nfTcXY3x.ts", Duration:3000000000, Sequence:250, EncryptMeta:{SecretKeyUrl:"http://example.com/20201008/ojroOJOt/1000kb/hls/key.key", IV:"", Method:"AES-128", SecretKey:""}, ErrMsg:""}, {Idx:1, Url:"http://example.com/20201008/ojroOJOt/1000kb/hls/VtMpEYqz.ts", Duration:1520000000, Sequence:251, EncryptMeta:{SecretKeyUrl:"http://example.com/20201008/ojroOJOt/1000kb/hls/key.key", IV:"", Method:"AES-128", SecretKey:""}, ErrMsg:""}, {Idx:2, Url:"http://example.com/20201008/ojroOJOt/1000kb/hls/uqvfZRwE.ts", Duration:3000000000, Sequence:252, EncryptMeta:{SecretKeyUrl:"http://example.com/20201008/ojroOJOt/1000kb/hls/key.key", IV:"", Method:"AES-128", SecretKey:""}, ErrMsg:""}], MastPlayList:nil, PlayListType:"VOD", EndList:true}`)
		})
	})
}
