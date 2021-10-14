package downloadmgr

import (
	"log"
	"testing"
)

func Test_download(t *testing.T) {
	dm := NewDownloadMgr()

	//targetUrl := "https://coldcdn.com/api/cdn/wr1cs5/video/spacex2.mp4"
	targetUrl := "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	saveFile := "./downloadFile/go1.17.2.darwin-amd64.pkg"

	header, isSupportRange, err := PreHandleOrigin(targetUrl)
	if err != nil {
		log.Println(err)
	}

	if header != nil {
		dm.SaveHeader(saveFile, header)
	}

	transferTimeoutSec := 150
	if !isSupportRange {
		transferTimeoutSec = 0
	}

	Download(saveFile, targetUrl, transferTimeoutSec)
}
