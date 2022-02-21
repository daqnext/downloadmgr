# downloadmgr

Download manager is package for downloading files from the given url.

### Channel

DownloadMgr has 1 Prehandlee channel and 4 Download channel to handle the download

* Prehandle channel: This channel will classify tasks and assign tasks to different channels according to whether the download can be resumed
* QuickDownload channel: For quick task download, do download task by FIFO
* RandomDownload channel: For normal task, task will be assigned to this channel if resumable. do download task randomly
* UnpauseDownload channel: For normal task, unresumable task will be assigned to this channel, do download task by FIFO
* UnpauseSlowDownload channel: For normal task, if task is unresumable and download speed is low, it will be pushed into this channel do download task by FIFO

There are two different download task: QuickTask and NormalTask

QuickTask: It needs to be downloaded as soon as possible within a certain period of time. This task goes directly into QuickDownload channel.

NormalTask: General download tasks without timelimit. It will first enter the prehandle channel and be classified according to whether it can resume or not.

Each channel has a idle list. Tasks are queued based on the start time.Channel will start several goroutines, cyclically check whether there are tasks in the idlelist, and do download tasks.

### Callback

* onDownloadSuccess: run when download success
* onDownloadFailed: run when download failed(after retried several times)
* onDownloading: run every 2 seconds when downloading
* onDownloadCanceled: run when task canceled
* onDownloadSlowSpeed: run when the download speed low

### Download process

The download status of the task will be detected every 2 seconds when downloading. OnDownloading callback will trigger here.

The download task will be interrupted if the download task is abnormal. The failed task will get a chance to retry unless no sapce or task canceled

## Example

```go
func main() {
	//logger
	logger, _ := LogrusULog.New("./logs", 2, 20, 30)
	logger.SetLevel(ULog.DebugLevel)

	dm := downloadmgr.NewDownloadMgr()
	dm.SetLogger(logger)

	//set some ignore header
	dm.SetIgnoreHeader([]string{})

    //example url
	targetUrl := [4]string{}
	targetUrl[0] = "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	targetUrl[1] = "https://assets.meson.network:10443/static/js/core.min.js"
	targetUrl[2] = "https://coldcdn.com/api/cdn/wr1cs5/video/spacex2.mp4"

	//start 10 download task
	for i := 0; i < 10; i++ {
		saveFile := [3]string{}
		saveFile[0] = fmt.Sprintf("./downloadFile/1/go1.17.2.darwin-amd64-%d.pkg", i)
		saveFile[1] = fmt.Sprintf("./downloadFile/2/core.min-%d.js", i)
		saveFile[2] = fmt.Sprintf("./downloadFile/4/spacex2-%d.mp4", i)

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
```
