package downloadmgr

import (
	"fmt"
	"github.com/daqnext/LocalLog/log"
	"testing"
	"time"
)

func Test_Channel(t *testing.T) {
	logger, _ := log.New("logs", 2, 20, 30)
	logger.SetLevel(log.LLEVEL_DEBUG)
	logger.SetReportCaller(true)

	dm := NewDownloadMgr(logger)

	//targetUrl := "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	//saveFile := "./downloadFile/go1.17.2.darwin-amd64.pkg"
	//dm.AddQuickDownloadTask(saveFile,[]string{targetUrl},0)
	//
	//targetUrl = "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	//saveFile = "./downloadFile/go1.17.2.darwin-amd64-2.pkg"
	//dm.AddQuickDownloadTask(saveFile,[]string{targetUrl},0)

	targetUrl := "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	//targetUrl := "https://image.baidu.com/search/down?tn=download&word=download&ie=utf8&fr=detail&url=https%3A%2F%2Fgimg2.baidu.com%2Fimage_search%2Fsrc%3Dhttp%253A%252F%252Fadfa.org.au%252Fwp-content%252Fuploads%252F2016%252F08%252Fwhereisasbestos-1024x889.png%26refer%3Dhttp%253A%252F%252Fadfa.org.au%26app%3D2002%26size%3Df9999%2C10000%26q%3Da80%26n%3D0%26g%3D0n%26fmt%3Djpeg%3Fsec%3D1632884767%26t%3Dbf86afc7d333af922f306f47fe41a272&thumburl=https%3A%2F%2Fimg1.baidu.com%2Fit%2Fu%3D281490118%2C363118662%26fm%3D15%26fmt%3Dauto%26gp%3D0.jpg"

	for i := 0; i < 9; i++ {
		saveFile := fmt.Sprintf("./downloadFile/go1.17.2.darwin-amd64-%d.pkg", i)
		//saveFile := fmt.Sprintf("./downloadFile/a-%d.jpg", i)
		task, _ := dm.AddNormalDownloadTask(
			saveFile,
			targetUrl,
			//time.Now().Unix()+1000,
			func(task *Task) {
				llog.Debugln("success", task.Id)
			},
			func(task *Task) {
				llog.Debugln("fail", task.Id)
			},
			func(task *Task) {
				llog.Debugln("downloading", task.Id)
			})

		llog.Debugln(task)
	}
	//saveFile := "./downloadFile/spacex2.mp4"
	//dm.AddNormalDownloadTask(saveFile,[]string{targetUrl})
	time.Sleep(600 * time.Second)
}
