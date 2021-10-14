package downloadmgr

import (
	"context"
	"fmt"
	"github.com/daqnext/downloadmgr/grab"
	"log"
	"net/http"
	"os"
	"time"
)

type TaskStatus int

const (
	New TaskStatus = iota
	Idle
	Downloading
	Success
	Fail
)

type TaskType int

const (
	quickTask TaskType = iota
	randomTask
)

type Task struct {
	id             uint64
	originUrlArray []string
	targetUrl      string
	savePath       string
	response       *grab.Response
	status         TaskStatus
	taskType       TaskType
	resumable      bool
	expireTime     int64 //for quick download, cancel task if expiry
	channel        *DownloadChannel

	//callback
	onStart       func()
	onSuccess     func()
	onFail        func()
	onPause       func()
	onDownloading func()
}

func (t *Task) StartDownload() {
	tempSaveFile := t.savePath + ".download"
	client := grab.NewClient()
	client.CanResume = t.resumable
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connectTimeout := 5 * time.Second
	//readWriteTimeout := time.Duration(transferTimeoutSec) * time.Second
	c := &http.Client{Transport: &http.Transport{
		Proxy:       http.ProxyFromEnvironment, //use system proxy
		DialContext: TimeoutDialer(connectTimeout, 0),
	}}
	client.HTTPClient = c
	req, err := grab.NewRequest(tempSaveFile, t.targetUrl)
	req.WithContext(ctx)
	if err != nil {
		log.Panicln(err)
	}

	// start download
	log.Printf("Downloading %v...\n", req.URL())
	resp := client.Do(req)
	//log.Println(resp)
	if resp == nil {
		log.Println("download fail", "err:", err)
		return
	}

	// start UI loop
	ticker := time.NewTicker(2000 * time.Millisecond)
	defer ticker.Stop()

Loop:
	for {
		select {
		case <-ticker.C:
			fmt.Printf("  transferred %v / %v bytes (%.2f%%)\n",
				resp.BytesComplete(),
				resp.Size(),
				100*resp.Progress())

			needBroken := t.channel.checkDownloadingState(t)
			if needBroken {
				t.channel.handleBrokenTask(t)
				cancel()
			}

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}

	if err := resp.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		//
		//os.Exit(1)
		return
	}

	log.Println("transferSize:", resp.BytesComplete())
	log.Println(resp.BytesPerSecond())
	info, _ := os.Stat(tempSaveFile)
	log.Println("fileSize:", info.Size())
	fmt.Printf("Download saved to %v \n", resp.Filename)

	//download success
	os.Rename(resp.Filename, t.savePath)

	// Output:
	// Downloading http://www.golang-book.com/public/pdf/gobook.pdf...
	//   200 OK
	//   transferred 42970 / 2893557 bytes (1.49%)
	//   transferred 1207474 / 2893557 bytes (41.73%)
	//   transferred 2758210 / 2893557 bytes (95.32%)
	// Download saved to ./gobook.pdf
}
