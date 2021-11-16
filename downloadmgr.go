package downloadmgr

import (
	"bytes"
	"context"
	"errors"
	localLog "github.com/daqnext/LocalLog/log"
	"github.com/daqnext/go-smart-routine/sr"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type DownloadMgr struct {
	//global id
	currentId uint64
	idLock    sync.Mutex

	//channel to receive task
	//globalDownloadTaskChan chan *Task
	preHandleChannel *downloadChannel

	//downloadChannel channel
	downloadChannel map[string]*downloadChannel

	//
	ignoreHeaderMap     map[string]struct{}
	ignoreHeaderMapLock sync.RWMutex

	taskMap sync.Map

	//logger
	llog *localLog.LocalLog
}

//NewDownloadMgr new instance of Download manager
func NewDownloadMgr(logger *localLog.LocalLog) *DownloadMgr {
	if logger == nil {
		panic("logger is nil")
	}

	dm := &DownloadMgr{
		currentId: 0,
		llog:      logger,
	}

	dm.preHandleChannel = initPreHandleChannel(dm)

	dm.genDownloadChannel()

	//loop go routine
	dm.classifyNewTaskLoop()
	dm.startDownloadLoop()

	return dm
}

func (dm *DownloadMgr) AddQuickDownloadTask(savePath string, targetUrl string, expireTime int64,
	onSuccess func(task *Task),
	onFail func(task *Task),
	onCancel func(task *Task),
	onDownloading func(task *Task)) (*Task, error) {
	return dm.addDownloadTask(savePath, targetUrl, quickTask, expireTime, onSuccess, onFail, onCancel, onDownloading)
}

func (dm *DownloadMgr) AddNormalDownloadTask(savePath string, targetUrl string,
	onSuccess func(task *Task),
	onFail func(task *Task),
	onCancel func(task *Task),
	onDownloading func(task *Task)) (*Task, error) {
	return dm.addDownloadTask(savePath, targetUrl, randomTask, 0, onSuccess, onFail, onCancel, onDownloading)
}

func (dm *DownloadMgr) GetTaskInfo(id uint64) *Task {
	v, exist := dm.taskMap.Load(id)
	if !exist {
		return nil
	}
	return v.(*Task)
}

func (dm *DownloadMgr) GetIdleTaskSize() (map[string]int, int) {
	totalSize := 0
	channelIdelSizeMap := map[string]int{}
	for k, v := range dm.downloadChannel {
		size := v.idleSize()
		channelIdelSizeMap[k] = size
		totalSize += size
	}
	return channelIdelSizeMap, totalSize
}

//todo no error return
func (dm *DownloadMgr) addDownloadTask(
	savePath string,
	targetUrl string,
	taskType TaskType,
	expireTime int64,
	onSuccess func(task *Task),
	onFail func(task *Task),
	onCancel func(task *Task),
	onDownloading func(task *Task),
) (*Task, error) {
	//check savePath
	savePath = strings.Trim(savePath, " ")
	//check targetUrl
	targetUrl = strings.Trim(targetUrl, " ")

	//gen id
	dm.idLock.Lock()
	if dm.currentId >= math.MaxUint64 {
		dm.currentId = 0
	}
	dm.currentId++
	taskId := dm.currentId
	dm.idLock.Unlock()

	//new task
	task := newTask(taskId, savePath, targetUrl, taskType, expireTime, onSuccess, onFail, onCancel, onDownloading)
	task.dm = dm

	//into map
	dm.taskMap.Store(task.Id, task)

	//into channel
	if taskType == quickTask {
		//if quickTask, push into quickChannel
		dm.downloadChannel[quickChannel].pushTaskToIdleList(task)
	} else {
		dm.preHandleChannel.pushTaskToIdleList(task)
	}
	return task, nil
}

//init download channel
func (dm *DownloadMgr) genDownloadChannel() {
	dm.downloadChannel = map[string]*downloadChannel{}
	//quickChannel
	dm.downloadChannel[quickChannel] = initQuickChannel(dm)

	//randomChannel
	dm.downloadChannel[randomChannel] = initRandomPauseChannel(dm)

	//unpauseFastChannel
	dm.downloadChannel[unpauseFastChannel] = initUnpauseFastChannel(dm, unpauseFastChannelSpeedLimit)

	//unpauseSlowChannel
	dm.downloadChannel[unpauseSlowChannel] = initUnpauseSlowChannel(dm)
}

//classifyNewTask loop classify task to different channel
func (dm *DownloadMgr) classifyNewTaskLoop() {
	channel := dm.preHandleChannel
	for i := 0; i < channel.downloadingCountLimit; i++ {
		sr.New_Panic_Redo(func() {
			for {
				var task *Task
				task = channel.popTaskFormIdleList()
				if task == nil {
					time.Sleep(500 * time.Millisecond)
					continue
				}

				if task.cancelFlag {
					channel.dm.llog.Debugln("task cancel id", task.Id)
					task.taskCancel()
					continue
				}

				//classify task
				err := dm.classify(task)
				if err != nil {
					//if fail when classify
					//try again or fail
					dm.llog.Debugln("classify error", task)
					task.failTimes++
					task.allowStartTime = time.Now().UnixMilli() + 5000
					if task.failTimes > 1 {
						//fail
						task.taskFail()
					} else {
						//try again
						channel.pushTaskToIdleList(task)
					}
				}
			}
		}, channel.dm.llog).Start()
	}
}

//preHandleOrigin check is url support range get or not, and get origin header at same time
func preHandleOrigin(targetUrl string) (http.Header, bool, error) {
	var req = &http.Request{}
	var err error
	req.Method = "GET"
	req.Close = true
	req.URL, err = url.Parse(targetUrl)
	if err != nil {
		return nil, false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	header := http.Header{}
	header.Set("Range", "bytes=0-0")
	req.Header = header
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, false, errors.New("response status code error")
	}
	if resp.Body == nil {
		return resp.Header, false, nil
	}
	defer resp.Body.Close()
	result, exist := resp.Header["Accept-Ranges"]
	if exist {
		for _, v := range result {
			if v == "bytes" {
				//log.Println("support request range")
				return resp.Header, true, nil
			}
		}
	}

	buf := &bytes.Buffer{}
	written, err := io.CopyN(buf, resp.Body, 2)
	if err == io.EOF && written == 1 {
		return resp.Header, true, nil
	}
	if err != nil {
		return resp.Header, false, nil
	}
	return resp.Header, false, nil
}

//classify check header and distribute to different channel
func (dm *DownloadMgr) classify(task *Task) error {

	// download header and check download is resumable or not
	header, canResume, err := preHandleOrigin(task.TargetUrl)
	if err != nil {
		//task failed
		task.Status = Fail
		return err
	}

	if header != nil {
		//save header
		err := dm.SaveHeader(task.SavePath, header)
		if err != nil {
			return err
		}
	}

	if canResume {
		task.resumable = true
		dm.downloadChannel[randomChannel].pushTaskToIdleList(task)
		//for test
		//dm.downloadChannel[unpauseFastChannel].pushTaskToIdleList(task)
	} else {
		dm.downloadChannel[unpauseFastChannel].pushTaskToIdleList(task)
	}
	return nil
}

func (dm *DownloadMgr) startDownloadLoop() {
	for _, v := range dm.downloadChannel {
		v.run()
	}
}
