package downloadmgr

import (
	"fmt"
	"github.com/daqnext/LocalLog/log"
	"testing"
	"time"
)

func Test_Channel(t *testing.T) {
	logger, _ := log.New("logs", 2, 20, 30)
	dm := NewDownloadMgr(logger)

	//targetUrl := "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	//saveFile := "./downloadFile/go1.17.2.darwin-amd64.pkg"
	//dm.AddQuickDownloadTask(saveFile,[]string{targetUrl},0)
	//
	//targetUrl = "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	//saveFile = "./downloadFile/go1.17.2.darwin-amd64-2.pkg"
	//dm.AddQuickDownloadTask(saveFile,[]string{targetUrl},0)

	targetUrl := "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	for i := 0; i < 9; i++ {
		saveFile := fmt.Sprintf("./downloadFile/go1.17.2.darwin-amd64-%d.pkg", i)
		dm.AddNormalDownloadTask(saveFile, targetUrl)
	}
	//saveFile := "./downloadFile/spacex2.mp4"
	//dm.AddNormalDownloadTask(saveFile,[]string{targetUrl})

	time.Sleep(600 * time.Second)
}
