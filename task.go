package downloadmgr

import (
	"context"
	"github.com/daqnext/downloadmgr/grab"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

type TaskStatus int

const (
	New TaskStatus = iota
	Idle
	Pause
	Downloading
	Success
	Fail
)

type TaskType int

const (
	quickTask TaskType = iota
	randomTask
)

type BrokenType int

const (
	no_broken BrokenType = iota
	broken_expire
	broken_noSpeed
	broken_pause
	broken_lowSpeed
)

type Task struct {
	//init with input param
	id         uint64
	targetUrl  string
	savePath   string
	taskType   TaskType
	expireTime int64 //for quick download, cancel task if expiry

	//modify when downloading
	failTimes         int
	lastFailTimeStamp int64
	response          *grab.Response
	cancel            context.CancelFunc
	status            TaskStatus
	resumable         bool
	channel           DownloadChannel
	dm                *DownloadMgr

	//callback
	onStart       func(task *Task)
	onSuccess     func(task *Task)
	onFail        func(task *Task)
	onDownloading func(task *Task)
}

func newTask(id uint64, savePath string, targetUrl string, taskType TaskType, expireTime int64) *Task {
	task := &Task{
		id:         id,
		savePath:   savePath,
		targetUrl:  targetUrl,
		taskType:   taskType,
		expireTime: expireTime,
		status:     New,
	}

	task.onStart = func(task *Task) {
		llog.Println("onStart", "id", task.id, "savePath", task.savePath)
	}

	task.onSuccess = func(task *Task) {
		llog.Println("onSuccess", "id", task.id, "savePath", task.savePath)
	}

	task.onFail = func(task *Task) {
		llog.Println("onFail", "id", task.id, "savePath", task.savePath)
	}
	task.onDownloading = func(task *Task) {
		llog.Println("onDownloading", "id", task.id, "savePath", task.savePath)
	}

	return task
}

func TimeoutDialer(cTimeout time.Duration, rwTimeout time.Duration) func(ctx context.Context, net, addr string) (c net.Conn, err error) {
	return func(ctx context.Context, netw, addr string) (net.Conn, error) {
		conn, err := net.DialTimeout(netw, addr, cTimeout)
		if err != nil {
			return nil, err
		}
		if rwTimeout > 0 {
			conn.SetDeadline(time.Now().Add(rwTimeout))
		}
		return conn, nil
	}
}

func (t *Task) StartDownload() {
	tempSaveFile := t.savePath + ".download"
	client := grab.NewClient()
	client.CanResume = t.resumable
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	defer cancel()
	connectTimeout := 5 * time.Second
	//readWriteTimeout := time.Duration(transferTimeoutSec) * time.Second
	c := &http.Client{Transport: &http.Transport{
		Proxy:       http.ProxyFromEnvironment, //use system proxy
		DialContext: TimeoutDialer(connectTimeout, 0),
	}}
	client.HTTPClient = c
	req, err := grab.NewRequest(tempSaveFile, t.targetUrl)
	req = req.WithContext(ctx)
	if err != nil {
		log.Panicln(err)
	}

	// start download
	llog.Printf("Downloading %s,form %v...\n", t.savePath, req.URL())
	resp := client.Do(req)
	if resp == nil {
		llog.Println("download fail", "err:", err)
		return
	}

	t.response = resp

	if t.taskType == quickTask && resp.HTTPResponse != nil && t.dm != nil {
		t.dm.SaveHeader(t.savePath, resp.HTTPResponse.Header)
	}

	// start monitor loop
	ticker := time.NewTicker(2000 * time.Millisecond)
	defer ticker.Stop()
	var needBroken bool
	var brokenType BrokenType
Loop:
	for {
		select {
		case <-ticker.C:
			llog.Printf("%s transferred %v / %v bytes (%.2f%%)",
				t.savePath,
				resp.BytesComplete(),
				resp.Size(),
				100*resp.Progress())

			needBroken, brokenType = t.channel.checkDownloadingState(t)
			if needBroken {
				if t.response.IsComplete() {
					break Loop
				}
				llog.Println("broken task", t.savePath)
				cancel()
			}

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}

	if err := resp.Err(); err != nil {
		llog.Errorf("Download failed: %v\n", err)
		t.channel.handleBrokenTask(t, brokenType)
		return
	}

	llog.Println("transferSize:", resp.BytesComplete())
	llog.Println(resp.BytesPerSecond())
	info, _ := os.Stat(tempSaveFile)
	llog.Println("fileSize:", info.Size())
	llog.Printf("Download saved to %v \n", resp.Filename)

	//download success
	os.Rename(resp.Filename, t.savePath)
	if t.onSuccess != nil {
		t.onSuccess(t)
	}
}

func (t *Task) tryAgainOrFail() {
	//
	if t.failTimes > t.channel.getRetryTimesLimit() {
		if t.onFail != nil {
			t.onFail(t)
		}
		return
	}
	//try again
	t.failTimes++
	t.lastFailTimeStamp = time.Now().Unix()
	t.channel.pushTaskToIdleList(t)
}
