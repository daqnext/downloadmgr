package downloadmgr

import (
	"context"
	"fmt"
	"github.com/daqnext/downloadmgr/grab"
	"net"
	"net/http"
	"os"
	"time"
)

type TaskStatus int

const (
	New TaskStatus = iota
	Pause
	Downloading
	Success
	Fail
	Expire
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
)

type Task struct {
	//init with input param
	Id          uint64
	TargetUrl   string
	SavePath    string
	TaskType    TaskType
	ExpireTime  int64 //for quick download, cancel task if expiry,if set 0 never expire
	NeedEncrypt bool

	//modify when downloadin
	Response        *grab.Response
	Status          TaskStatus
	failTimes       int
	allowStartTime  int64
	canResume       bool
	slowSpeedCalled bool

	//channel and dm pointer
	channel *downloadChannel
	dm      *DownloadMgr

	//callback
	onSuccess         func(task *Task)
	onFail            func(task *Task)
	onCancel          func(task *Task)
	onDownloading     func(task *Task)
	slowSpeedCallback func(task *Task)

	//for cancel
	cancelFlag bool
}

func newTask(
	id uint64,
	savePath string,
	targetUrl string,
	taskType TaskType,
	expireTime int64,
	needEncrypt bool,
	onSuccess func(task *Task),
	onFail func(task *Task),
	onCancel func(task *Task),
	onDownloading func(task *Task),
	slowSpeedCallback func(task *Task),
) *Task {
	task := &Task{
		Id:                id,
		SavePath:          savePath,
		TargetUrl:         targetUrl,
		TaskType:          taskType,
		ExpireTime:        expireTime,
		NeedEncrypt:       needEncrypt,
		allowStartTime:    time.Now().UnixMilli(),
		Status:            New,
		onSuccess:         onSuccess,
		onFail:            onFail,
		onCancel:          onCancel,
		onDownloading:     onDownloading,
		slowSpeedCallback: slowSpeedCallback,
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
	//use grab to download
	client := grab.NewClient()
	client.CanResume = t.canResume
	client.NeedEncrypt = t.NeedEncrypt

	//new request
	connectTimeout := 7 * time.Second
	c := &http.Client{Transport: &http.Transport{
		Proxy:       http.ProxyFromEnvironment, //use system proxy
		DialContext: timeoutDialer(connectTimeout, 0),
	}}
	client.HTTPClient = c
	req, err := grab.NewRequest(t.SavePath, t.TargetUrl)
	if err != nil {
		t.taskBreakOff()
		t.dm.llog.Debugln("download fail", "err:", err)
		return
	}
	//cancel context
	reqCtx, reqCancel := context.WithCancel(context.Background())
	defer reqCancel()
	req = req.WithContext(reqCtx)

	if t.cancelFlag {
		t.taskCancel()
		return
	}

	// start download
	//llog.Debugf("Downloading %s,form %v...", t.savePath, req.URL())
	resp := client.Do(req)
	if resp == nil || resp.HTTPResponse == nil {
		t.taskBreakOff()
		return
	}

	t.Response = resp

	//if quickTask,save header
	err = t.dm.saveHeader(t.SavePath+".header", resp.HTTPResponse.Header)
	if err != nil {
		t.taskBreakOff()
		return
	}

	t.Status = Downloading
	// start monitor loop, every 2s
	ticker := time.NewTicker(2000 * time.Millisecond)
	defer ticker.Stop()
loop:
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
			needBroken, bType := t.channel.checkDownloadingStateFunc(t)
			if needBroken && !t.Response.IsComplete() {
				t.channel.handleBrokenTaskFunc(t, bType)
				t.dm.llog.Debugln("broken task", t.SavePath)
				return
			}

		case <-resp.Done:
			// request error || completed
			break loop
		}
	}

	//if err!=nil means download not success
	// failed || or break by system
	if err := resp.Err(); err != nil {
		t.dm.llog.Debugf("Download broken: %v,file:%v", err, t.SavePath)
		t.taskBreakOff()
		return
	}

	//check file stat
	_, err = os.Stat(t.SavePath)
	if err != nil {
		t.taskBreakOff()
		return
	}

	//download success
	//t.dm.llog.Debugln("transferSize:", resp.BytesComplete())
	//t.dm.llog.Debugln("fileSize:", info.Size())
	//t.dm.llog.Debugf("Download saved to %v ", resp.Filename)
	t.taskSuccess()
}

func (t *Task) taskSuccess() {
	t.Status = Success
	if t.onSuccess != nil {
		t.onSuccess(t)
	}
	t.dm.taskMap.Delete(t.Id)
}

func (t *Task) taskExpire() {
	t.Status = Expire
	if t.onFail != nil {
		t.onFail(t)
	}
	_ = os.Remove(t.SavePath)
	_ = os.Remove(t.SavePath + ".header")
	t.dm.taskMap.Delete(t.Id)
}

func (t *Task) taskCancel() {
	if t.onCancel != nil {
		t.onCancel(t)
	}
	_ = os.Remove(t.SavePath)
	_ = os.Remove(t.SavePath + ".header")
	t.dm.taskMap.Delete(t.Id)
}

func (t *Task) taskBreakOff() {
	t.Status = Fail
	if t.failTimes >= t.channel.retryTimesLimit {
		t.taskFail()
		return
	}
	//try again
	t.failTimes++
	t.allowStartTime = time.Now().UnixMilli() + 5000
	t.channel.pushTaskToIdleList(t)
}

func (t *Task) taskFail() {
	t.Status = Fail
	if t.onFail != nil {
		t.onFail(t)
	}
	_ = os.Remove(t.SavePath)
	_ = os.Remove(t.SavePath + ".header")
	t.dm.taskMap.Delete(t.Id)
}

//ToString for debug
func (t *Task) ToString() string {
	s := fmt.Sprintf("{\"Id\":%d,\"TargetUrl\":%s,\"SavePath\":%s,\"TaskType\":%d,\"ExpireTime\":%d,\"NeedEncrypt\":%v,\"Status\":%d}", t.Id, t.TargetUrl, t.SavePath, t.TaskType, t.ExpireTime, t.NeedEncrypt, t.Status)
	return s
}
