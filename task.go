package downloadmgr

import (
	"context"
	"github.com/daqnext/downloadmgr/grab"
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
	broken_cancel
	broken_other
)

type Task struct {
	//init with input param
	Id         uint64
	TargetUrl  string
	SavePath   string
	TaskType   TaskType
	ExpireTime int64 //for quick download, cancel task if expiry

	//modify when downloading
	failTimes         int
	lastFailTimeStamp int64
	response          *grab.Response
	cancel            context.CancelFunc
	Status            TaskStatus
	resumable         bool

	//channel and dm pointer
	channel *downloadChannel
	dm      *DownloadMgr

	//callback
	onSuccess     func(task *Task)
	onFail        func(task *Task)
	onDownloading func(task *Task)

	//for cancel
	cancelFlag bool
}

func newTask(
	id uint64,
	savePath string,
	targetUrl string,
	taskType TaskType,
	expireTime int64,
	onSuccess func(task *Task),
	onFail func(task *Task),
	onDownloading func(task *Task),
) *Task {
	task := &Task{
		Id:            id,
		SavePath:      savePath,
		TargetUrl:     targetUrl,
		TaskType:      taskType,
		ExpireTime:    expireTime,
		Status:        New,
		onSuccess:     onSuccess,
		onFail:        onFail,
		onDownloading: onDownloading,
	}
	return task
}

func (t *Task) CancelDownload() {
	t.cancelFlag = true
}

func timeoutDialer(cTimeout time.Duration, rwTimeout time.Duration) func(ctx context.Context, net, addr string) (c net.Conn, err error) {
	return func(ctx context.Context, netw, addr string) (net.Conn, error) {
		conn, err := net.DialTimeout(netw, addr, cTimeout)
		if err != nil {
			return nil, err
		}
		if rwTimeout > 0 {
			err := conn.SetDeadline(time.Now().Add(rwTimeout))
			if err != nil {
				return nil, err
			}
		}
		return conn, nil
	}
}

func (t *Task) startDownload() {
	//downloading file temp name
	tempSaveFile := t.SavePath + ".download"

	//use grab to download
	client := grab.NewClient()
	client.CanResume = t.resumable

	//cancel context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	//new request
	connectTimeout := 7 * time.Second
	c := &http.Client{Transport: &http.Transport{
		Proxy:       http.ProxyFromEnvironment, //use system proxy
		DialContext: timeoutDialer(connectTimeout, 0),
	}}
	client.HTTPClient = c
	req, err := grab.NewRequest(tempSaveFile, t.TargetUrl)
	if err != nil {
		t.taskBreakOff()
		t.dm.llog.Debugln("download fail", "err:", err)
		return
	}
	req = req.WithContext(ctx)

	if t.cancelFlag {
		return
	}

	// start download
	//llog.Debugf("Downloading %s,form %v...", t.savePath, req.URL())
	resp := client.Do(req)
	if resp == nil {
		t.taskBreakOff()
		t.dm.llog.Debugln("download fail", "err:", err)
		return
	}

	t.response = resp
	t.cancel = cancel

	//if quickTask,save header
	if t.TaskType == quickTask && resp.HTTPResponse != nil && t.dm != nil {
		err := t.dm.SaveHeader(t.SavePath, resp.HTTPResponse.Header)
		if err != nil {
			t.taskBreakOff()
			return
		}
	}

	t.Status = Downloading
	// start monitor loop, every 2s
	ticker := time.NewTicker(2000 * time.Millisecond)
	defer ticker.Stop()
	var needBroken bool
	var brokenType BrokenType = broken_other
Loop:
	for {
		select {
		case <-ticker.C:
			//llog.Debugf("%s transferred %v / %v bytes (%.2f%%)",
			//	t.savePath,
			//	resp.BytesComplete(),
			//	resp.Size(),
			//	100*resp.Progress())
			if t.onDownloading != nil {
				t.onDownloading(t)
			}
			var bType BrokenType
			needBroken, bType = t.channel.checkDownloadingStateFunc(t)
			if needBroken {
				if t.response.IsComplete() {
					break Loop
				}
				cancel()
				brokenType = bType

				t.dm.llog.Debugln("broken task", t.SavePath)
				//t.channel.handleBrokenTask(t, brokenType)
			}

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}

	//if err!=nil means download not finish
	if err := resp.Err(); err != nil {
		t.dm.llog.Debugf("Download broken: %v,file:%v", err, t.SavePath)
		t.channel.handleBrokenTaskFunc(t, brokenType)
		return
	}

	t.dm.llog.Debugln("transferSize:", resp.BytesComplete())
	info, _ := os.Stat(tempSaveFile)
	t.dm.llog.Debugln("fileSize:", info.Size())
	t.dm.llog.Debugf("Download saved to %v ", resp.Filename)

	//download success
	err = os.Rename(resp.Filename, t.SavePath)
	if err != nil {
		_ = os.Remove(resp.Filename)
		_ = os.Remove(t.SavePath)
		_ = os.Remove(t.SavePath + ".header")
		if t.onFail != nil {
			t.onFail(t)
		}
		return
	}
	t.Status = Success
	if t.onSuccess != nil {
		t.onSuccess(t)
	}
}

func (t *Task) taskBreakOff() {
	if t.cancelFlag {
		_ = os.Remove(t.SavePath + ".download")
		_ = os.Remove(t.SavePath + ".header")
		return
	}

	if t.failTimes > t.channel.retryTimesLimit {
		t.fail()
		return
	}
	//try again
	t.failTimes++
	t.lastFailTimeStamp = time.Now().Unix()
	t.channel.pushTaskToIdleList(t)
}

func (t *Task) fail() {
	t.Status = Fail
	if t.onFail != nil {
		t.onFail(t)
	}
	_ = os.Remove(t.SavePath + ".download")
	_ = os.Remove(t.SavePath + ".header")
}
