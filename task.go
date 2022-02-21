package downloadmgr

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/daqnext/downloadmgr/grab"
)

type TaskStatus int

const (
	New TaskStatus = iota
	Pause
	Downloading
	Success
	Fail
)

type FailReasonType int

const (
	NoFail FailReasonType = iota
	Fail_NoSpace
	Fail_Expire
	Fail_SizeLimit
	Fail_RequestError
	Fail_Other
)

type TaskType int

const (
	QuickTask TaskType = iota
	NormalTask
)

type BrokenType int

const (
	no_broken BrokenType = iota
	broken_expire
	broken_sizeLimit
	broken_noSpeed
	broken_pause
	broken_lowSpeed
	broken_cancel
)

type Task struct {
	//init with input param
	Id         uint64
	NameHash   string
	FolderId   uint32
	TargetUrl  string
	SavePath   string
	TaskType   TaskType
	ExpireTime int64 //for quick download, cancel task if expiry,if set 0 never expire
	Encrypt    bool
	SizeLimit  int64 //if >0 means file has size limit, if download data > size limit, task fail

	//modify when downloading
	Response       *grab.Response
	Status         TaskStatus
	FailReason     FailReasonType
	failTimes      int
	allowStartTime int64
	canResume      bool

	//channel and dm pointer
	channel *downloadChannel
	dm      *DownloadMgr

	//callback
	onSuccess     func(task *Task)
	onFail        func(task *Task)
	onCancel      func(task *Task)
	onDownloading func(task *Task)
	onSlowSpeed   func(task *Task)

	//for cancel
	cancelFlag bool
}

func newTask(
	id uint64,
	nameHash string,
	folderId uint32,
	savePath string,
	targetUrl string,
	taskType TaskType,
	expireTime int64,
	encrypt bool,
	sizeLimit int64,
	onSuccess func(task *Task),
	onFail func(task *Task),
	onCancel func(task *Task),
	onDownloading func(task *Task),
	onSlowSpeed func(task *Task),
) *Task {
	task := &Task{
		Id:             id,
		NameHash:       nameHash,
		FolderId:       folderId,
		SavePath:       savePath,
		TargetUrl:      targetUrl,
		TaskType:       taskType,
		ExpireTime:     expireTime,
		Encrypt:        encrypt,
		SizeLimit:      sizeLimit,
		allowStartTime: time.Now().UnixMilli(),
		Status:         New,
		onSuccess:      onSuccess,
		onFail:         onFail,
		onCancel:       onCancel,
		onDownloading:  onDownloading,
		onSlowSpeed:    onSlowSpeed,
	}
	return task
}

//CancelDownload cancel download task by manual
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
	client.NeedEncrypt = t.Encrypt

	//new request
	connectTimeout := 7 * time.Second
	c := &http.Client{Transport: &http.Transport{
		Proxy:       http.ProxyFromEnvironment, //use system proxy
		DialContext: timeoutDialer(connectTimeout, 0),
	}}
	client.HTTPClient = c
	req, err := grab.NewRequest(t.SavePath, t.TargetUrl)
	if err != nil {
		t.FailReason = Fail_RequestError
		t.taskBreakOff()
		if t.dm.logger != nil {
			t.dm.logger.Debugln("download fail", "err:", err, " task:", t.ToString())
		}
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
		t.FailReason = Fail_RequestError
		t.taskBreakOff()
		return
	}

	if err := resp.Err(); err != nil {
		t.FailReason = Fail_Other
		if t.dm.logger != nil {
			t.dm.logger.Debugln("download fail", "err:", err, " task:", t.ToString())
		}
		t.taskBreakOff()
		return
	}

	t.Response = resp

	//check size
	if resp.HTTPResponse.ContentLength != -1 && t.SizeLimit > 0 && resp.HTTPResponse.ContentLength > t.SizeLimit {
		t.FailReason = Fail_SizeLimit
		t.taskFail()
		return
	}

	//save header
	//add originFileName in header
	originFileName, _ := grab.GuessFilename(resp.HTTPResponse)
	err = t.dm.saveHeader(t.SavePath+".header", resp.HTTPResponse.Header, originFileName)
	if err != nil {

		t.FailReason = Fail_RequestError
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
			if t.onDownloading != nil {
				t.onDownloading(t)
			}
			needBroken, bType := t.channel.checkDownloadingStateFunc(t)
			if needBroken && !t.Response.IsComplete() {
				t.channel.handleBrokenTaskFunc(t, bType)
				if t.dm.logger != nil {
					t.dm.logger.Debugln("broken task", t.SavePath)
				}
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
		if t.dm.logger != nil {
			t.dm.logger.Debugln("Download broken:", err, " file:", t.SavePath)
		}
		if strings.Contains(err.Error(), "no space") {
			t.FailReason = Fail_NoSpace
			t.taskFail()
		} else {
			t.taskBreakOff()
		}

		return
	}

	//check file stat
	f, err := os.Stat(t.SavePath)
	if err != nil || f.Size() == 0 {
		t.FailReason = Fail_Other
		t.taskBreakOff()
		return
	}

	//download success
	t.taskSuccess()
}

func (t *Task) taskSuccess() {
	t.Status = Success
	if t.onSuccess != nil {
		t.onSuccess(t)
	}
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
	t.FailReason = NoFail
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
	s := fmt.Sprintf("{\"Id\":%d,\"TargetUrl\":%s,\"SavePath\":%s,\"TaskType\":%d,\"ExpireTime\":%d,\"NeedEncrypt\":%v,\"Status\":%d}", t.Id, t.TargetUrl, t.SavePath, t.TaskType, t.ExpireTime, t.Encrypt, t.Status)
	return s
}
