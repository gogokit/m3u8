package m3u8

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"github.com/gogokit/util"
	"os"
)

func GenResult(status Status, withBar bool) (ret *Result) {
	ret = &Result{}
	var bar util.Bar
	if withBar {
		bar = util.NewBar(uint64(status.TsTotal()))
	}
	for {
		select {
		case <-status.Done():
			return
		case v := <-status.Event():
			if v.Segment != nil {
				ret.Segments = append(ret.Segments, *v.Segment)
				if withBar {
					bar.Update(uint64(status.TsComplete()))
				}
				continue
			}

			if v.Merged != nil {
				ret.Merged = *v.Merged
				ret.MergeErr = v.MergeErr
				ret.MergedFilePath = v.MergedFilePath
				continue
			}

			if v.ConvToMP4 != nil {
				ret.ConvToMP4 = *v.ConvToMP4
				ret.ConvToMP4Err = v.ConvToMP4Err
				ret.MP4FilePath = v.MP4FilePath
			}
		}
	}
}

func decryptByAES128(encrypted, key, iv []byte) ([]byte, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher error, %w", err)
	}

	if len(iv) == 0 {
		iv = key
	}

	origData := make([]byte, len(encrypted))
	cipher.NewCBCDecrypter(b, iv[:b.BlockSize()]).CryptBlocks(origData, encrypted)

	l := len(origData)
	if l == 0 {
		return origData, nil
	}
	return origData[:(l - int(origData[l-1]))], nil
}

func createIfNotExists(dir string) error {
	f, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if err = os.Mkdir(dir, os.ModePerm); err != nil {
				return fmt.Errorf("os.Mkdir error, %w", err)
			}
			return nil
		}
		return fmt.Errorf("os.Stat error, err:%w", err)
	}
	if !f.IsDir() {
		return errors.New(dir + " is exists, but is not directory")
	}
	return nil
}
