package test

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/daqnext/downloadmgr"
	"github.com/universe-30/LogrusULog"
	"github.com/universe-30/ULog"
)

func printMemStats(logger ULog.Logger) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Infoln(fmt.Sprintf("Alloc = %v KB, TotalAlloc = %v KB, Sys = %v KB,Lookups = %v NumGC = %v\n", m.Alloc/1024, m.TotalAlloc/1024, m.Sys/1024, m.Lookups, m.NumGC))
}

func Test_Channel(t *testing.T) {
	logger, _ := LogrusULog.New("./logs", 2, 20, 30)
	logger.SetLevel(ULog.DebugLevel)

	dm := downloadmgr.NewDownloadMgr()
	dm.SetLogger(logger)

	//targetUrl := "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	//saveFile := "./downloadFile/go1.17.2.darwin-amd64.pkg"
	//dm.AddQuickDownloadTask(saveFile,[]string{targetUrl},0)
	//
	//targetUrl = "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	//saveFile = "./downloadFile/go1.17.2.darwin-amd64-2.pkg"
	//dm.AddQuickDownloadTask(saveFile,[]string{targetUrl},0)

	targetUrl := "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	//targetUrl := "https://assets.meson.network:10443/static/js/core.min.js"
	//targetUrl := "https://image.baidu.com/search/down?tn=download&word=download&ie=utf8&fr=detail&url=https%3A%2F%2Fgimg2.baidu.com%2Fimage_search%2Fsrc%3Dhttp%253A%252F%252Fadfa.org.au%252Fwp-content%252Fuploads%252F2016%252F08%252Fwhereisasbestos-1024x889.png%26refer%3Dhttp%253A%252F%252Fadfa.org.au%26app%3D2002%26size%3Df9999%2C10000%26q%3Da80%26n%3D0%26g%3D0n%26fmt%3Djpeg%3Fsec%3D1632884767%26t%3Dbf86afc7d333af922f306f47fe41a272&thumburl=https%3A%2F%2Fimg1.baidu.com%2Fit%2Fu%3D281490118%2C363118662%26fm%3D15%26fmt%3Dauto%26gp%3D0.jpg"
	//targetUrl := "https://wr1cs5-bdhkbiekdhkibxx.shoppynext.com:19091/video/spacex2-redirecter456gt.mp4"
	//targetUrl := "https://coldcdn.com/api/cdn/wr1cs5/video/AcceleratedByUsingMesonNetwork.mp4"

	var tt *downloadmgr.Task
	for i := 0; i < 1; i++ {
		saveFile := fmt.Sprintf("./downloadFile/go1.17.2.darwin-amd64-%d.pkg", i)
		//saveFile := fmt.Sprintf("./downloadFile/core.min-%d.js", i)
		//saveFile := fmt.Sprintf("./downloadFile/a-%d.jpg", i)
		//saveFile := fmt.Sprintf("./downloadFile/AcceleratedByUsingMesonNetwork-%d.mp4", i)
		needEncrypt := false
		if i%2 == 0 {
			//needEncrypt=true
		}
		task, err := dm.AddNormalDownloadTask(
			"abc",
			"1",
			saveFile,
			targetUrl,
			//time.Now().Unix()+20,
			needEncrypt,
			0,
			func(task *downloadmgr.Task) {
				logger.Infoln("success", task.Id, "targetUrl", targetUrl)
				info, _ := os.Stat(task.SavePath)
				logger.Infoln("filePath", task.SavePath)
				logger.Infoln("size", info.Size())
				logger.Infoln("needEncrypt", task.NeedEncrypt)
			},
			func(task *downloadmgr.Task) {
				logger.Debugln("fail", task.Id)
			},
			func(task *downloadmgr.Task) {
				logger.Infoln("cancel", task.Id)
			},
			func(task *downloadmgr.Task) {
				logger.Debugln("downloading", task.Id, task.Response.Progress(), task.Response.BytesPerSecond())
			},
			nil)

		if err != nil {
			logger.Errorln("task error", err)
		}

		logger.Debugln(task.ToString())
		tt = task
	}

	go func() {
		for {
			f, e := os.Stat(tt.SavePath)
			if e != nil {
				logger.Debugln("no file")
			} else {
				logger.Debugln(f.Size())
			}

			time.Sleep(time.Second * 1)
		}
	}()

	//saveFile := "./downloadFile/spacex2.mp4"
	//dm.AddNormalDownloadTask(saveFile,[]string{targetUrl})

	//go func() {
	//	for {
	//		logger.Debugln("task info")
	//		tm := dm.GetTaskMap()
	//		tm.Range(func(key, value interface{}) bool {
	//			logger.Debugln(value.(*downloadmgr.Task).Id, value.(*downloadmgr.Task).ToString())
	//
	//			return true
	//		})
	//		time.Sleep(time.Second * 1)
	//	}
	//}()

	go func() {
		for {
			printMemStats(logger)
			time.Sleep(time.Second * 5)
		}
	}()

	time.Sleep(1200 * time.Second)
}

func Test_MultiChannel(t *testing.T) {
	logger, _ := LogrusULog.New("./logs", 2, 20, 30)
	logger.SetLevel(ULog.DebugLevel)

	dm := downloadmgr.NewDownloadMgr()
	dm.SetLogger(logger)

	targetUrl := [4]string{}
	targetUrl[0] = "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	targetUrl[1] = "https://assets.meson.network:10443/static/js/core.min.js"
	targetUrl[2] = "https://image.baidu.com/search/down?tn=download&word=download&ie=utf8&fr=detail&url=https%3A%2F%2Fgimg2.baidu.com%2Fimage_search%2Fsrc%3Dhttp%253A%252F%252Fadfa.org.au%252Fwp-content%252Fuploads%252F2016%252F08%252Fwhereisasbestos-1024x889.png%26refer%3Dhttp%253A%252F%252Fadfa.org.au%26app%3D2002%26size%3Df9999%2C10000%26q%3Da80%26n%3D0%26g%3D0n%26fmt%3Djpeg%3Fsec%3D1632884767%26t%3Dbf86afc7d333af922f306f47fe41a272&thumburl=https%3A%2F%2Fimg1.baidu.com%2Fit%2Fu%3D281490118%2C363118662%26fm%3D15%26fmt%3Dauto%26gp%3D0.jpg"
	targetUrl[3] = "https://coldcdn.com/api/cdn/wr1cs5/video/spacex2.mp4"

	for i := 0; i < 10; i++ {
		saveFile := [4]string{}
		saveFile[0] = fmt.Sprintf("./downloadFile/go1.17.2.darwin-amd64-%d.pkg", i)
		saveFile[1] = fmt.Sprintf("./downloadFile/core.min-%d.js", i)
		saveFile[2] = fmt.Sprintf("./downloadFile/a-%d.jpg", i)
		saveFile[3] = fmt.Sprintf("./downloadFile/spacex2-%d.mp4", i)
		needEncrypt := false

		index := rand.Intn(4)

		task, err := dm.AddNormalDownloadTask(
			fmt.Sprintf("nameHash-%d", i),
			"1",
			saveFile[index],
			targetUrl[index],
			//time.Now().Unix()+20,
			needEncrypt,
			0,
			func(task *downloadmgr.Task) {
				info, _ := os.Stat(task.SavePath)
				logger.Infoln("success", task.Id, "targetUrl", targetUrl[index], "filePath", task.SavePath, "size", info.Size())
			},
			func(task *downloadmgr.Task) {
				logger.Debugln("fail", task.Id)
			},
			func(task *downloadmgr.Task) {
				logger.Infoln("cancel", task.Id)
			},
			func(task *downloadmgr.Task) {
				logger.Debugln("downloading", task.Id, task.Response.Progress(), task.Response.BytesPerSecond())
			},
			nil)

		if err != nil {
			logger.Errorln("task error", err)
		}

		logger.Debugln(task)
	}
	time.Sleep(600 * time.Second)
}

func someFunc() (value bool) {
	defer func() {
		if value == false {
			log.Println(1)
		}
	}()

	return true
}

func Test_func(t *testing.T) {
	log.Println(someFunc())
}
