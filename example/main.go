package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/daqnext/downloadmgr"
	"github.com/universe-30/LogrusULog"
	"github.com/universe-30/ULog"
)

func main() {
	logger, _ := LogrusULog.New("./logs", 2, 20, 30)
	logger.SetLevel(ULog.DebugLevel)

	dm := downloadmgr.NewDownloadMgr()
	dm.SetLogger(logger)

	//set some ignore header
	dm.SetIgnoreHeader([]string{})

	targetUrl := [4]string{}
	targetUrl[0] = "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	targetUrl[1] = "https://assets.meson.network:10443/static/js/core.min.js"
	targetUrl[2] = "https://image.baidu.com/search/down?tn=download&word=download&ie=utf8&fr=detail&url=https%3A%2F%2Fgimg2.baidu.com%2Fimage_search%2Fsrc%3Dhttp%253A%252F%252Fadfa.org.au%252Fwp-content%252Fuploads%252F2016%252F08%252Fwhereisasbestos-1024x889.png%26refer%3Dhttp%253A%252F%252Fadfa.org.au%26app%3D2002%26size%3Df9999%2C10000%26q%3Da80%26n%3D0%26g%3D0n%26fmt%3Djpeg%3Fsec%3D1632884767%26t%3Dbf86afc7d333af922f306f47fe41a272&thumburl=https%3A%2F%2Fimg1.baidu.com%2Fit%2Fu%3D281490118%2C363118662%26fm%3D15%26fmt%3Dauto%26gp%3D0.jpg"
	targetUrl[3] = "https://coldcdn.com/api/cdn/wr1cs5/video/spacex2.mp4"

	for i := 0; i < 10; i++ {
		saveFile := [4]string{}
		saveFile[0] = fmt.Sprintf("./downloadFile/1/go1.17.2.darwin-amd64-%d.pkg", i)
		saveFile[1] = fmt.Sprintf("./downloadFile/2/core.min-%d.js", i)
		saveFile[2] = fmt.Sprintf("./downloadFile/3/a-%d.jpg", i)
		saveFile[3] = fmt.Sprintf("./downloadFile/4/spacex2-%d.mp4", i)

		needEncrypt := false
		if rand.Intn(2) == 1 {
			needEncrypt = true
		}

		index := rand.Intn(4)

		task, err := dm.AddNormalDownloadTask(
			fmt.Sprintf("nameHash-%d", i),
			12345,
			saveFile[index],
			targetUrl[index],
			needEncrypt,
			0,
			func(task *downloadmgr.Task) {
				info, _ := os.Stat(task.SavePath)
				logger.Infoln("success", task.Id, "targetUrl", targetUrl[index], "filePath", task.SavePath, "size", info.Size())
			},
			func(task *downloadmgr.Task) {
				logger.Infoln("fail", task.Id, "fail reason:", task.FailReason)
			},
			func(task *downloadmgr.Task) {
				logger.Infoln("cancel", task.Id)
			},
			func(task *downloadmgr.Task) {
				logger.Debugln("downloading", task.Id, task.Response.Progress(), task.Response.BytesPerSecond())
			},
			func(task *downloadmgr.Task) {
				logger.Debugln("speedSlow", task.Id, task.Response.Progress(), task.Response.BytesPerSecond())
			})

		if err != nil {
			logger.Errorln("task error", err)
		}

		logger.Debugln(task)
	}

	time.Sleep(600 * time.Second)
}
