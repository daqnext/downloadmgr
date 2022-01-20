package downloadmgr

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/universe-30/ULog"
)

type DownloadMgr struct {
	//global id
	currentId uint64
	idLock    sync.Mutex

	//channel to classify task
	preHandleChannel *downloadChannel

	//downloadChannel channel
	downloadChannel map[string]*downloadChannel

	//
	ignoreHeaderMap     map[string]struct{}
	ignoreHeaderMapLock sync.RWMutex

	taskMap sync.Map

	//logger
	logger ULog.Logger

	folderHandleLock *sync.Mutex
}

//NewDownloadMgr new instance of Download manager
func NewDownloadMgr(folderLock *sync.Mutex) *DownloadMgr {
	dm := &DownloadMgr{
		currentId:        0,
		logger:           nil,
		folderHandleLock: folderLock,
	}

	dm.preHandleChannel = initPreHandleChannel(dm)

	dm.genDownloadChannel()

	//loop go routine
	dm.classifyNewTaskLoop()
	dm.startDownloadLoop()

	return dm
}

func (dm *DownloadMgr) SetLogger(logger ULog.Logger) {
	dm.logger = logger
}

func (dm *DownloadMgr) GetLogger() ULog.Logger {
	return dm.logger
}

func (dm *DownloadMgr) AddQuickDownloadTask(nameHash string, savePath string, targetUrl string, expireTime int64, needEncrypt bool, sizeLimit int64,
	onSuccess func(task *Task),
	onFail func(task *Task),
	onCancel func(task *Task),
	onDownloading func(task *Task)) (*Task, error) {
	return dm.addDownloadTask(nameHash, savePath, targetUrl, QuickTask, expireTime, needEncrypt, sizeLimit, onSuccess, onFail, onCancel, onDownloading, nil)
}

func (dm *DownloadMgr) AddNormalDownloadTask(nameHash string, savePath string, targetUrl string, needEncrypt bool, sizeLimit int64,
	onSuccess func(task *Task),
	onFail func(task *Task),
	onCancel func(task *Task),
	onDownloading func(task *Task),
	slowSpeedCallback func(task *Task)) (*Task, error) {
	return dm.addDownloadTask(nameHash, savePath, targetUrl, RandomTask, 0, needEncrypt, sizeLimit, onSuccess, onFail, onCancel, onDownloading, slowSpeedCallback)
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

//GetTaskMap for debug
func (dm *DownloadMgr) GetTaskMap() *sync.Map {
	return &dm.taskMap
}

func (dm *DownloadMgr) addDownloadTask(
	nameHash string,
	savePath string,
	targetUrl string,
	taskType TaskType,
	expireTime int64,
	needEncrypt bool,
	sizeLimit int64,
	onSuccess func(task *Task),
	onFail func(task *Task),
	onCancel func(task *Task),
	onDownloading func(task *Task),
	slowSpeedCallback func(task *Task),
) (*Task, error) {
	//check savePath
	savePath = strings.Trim(savePath, " ")
	//check targetUrl
	targetUrl = strings.Trim(targetUrl, " ")
	testUrl := strings.ToLower(targetUrl)
	if !(strings.HasPrefix(testUrl, "http://") || strings.HasPrefix(testUrl, "https://")) {
		return nil, ErrOriginUrlProtocol
	}

	//gen id
	dm.idLock.Lock()
	if dm.currentId >= math.MaxUint64 {
		dm.currentId = 0
	}
	dm.currentId++
	taskId := dm.currentId
	dm.idLock.Unlock()

	//new task
	task := newTask(taskId, nameHash, savePath, targetUrl, taskType, expireTime, needEncrypt, sizeLimit, onSuccess, onFail, onCancel, onDownloading, slowSpeedCallback)
	task.dm = dm

	//into map
	dm.taskMap.Store(task.Id, task)

	//into channel
	if taskType == QuickTask {
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
		safeInfiLoop(func() {
			for {
				var task *Task
				task = channel.popTaskFormIdleList()
				if task == nil {
					time.Sleep(200 * time.Millisecond)
					continue
				}

				if task.cancelFlag {
					if channel.dm.logger != nil {
						channel.dm.logger.Debugln("task cancel id", task.Id)
					}
					task.taskCancel()
					continue
				}

				//classify task
				err := dm.classify(task)
				if err != nil {
					//if fail when classify
					//try again or fail
					if channel.dm.logger != nil {
						dm.logger.Debugln("classify error", err, task)
					}
					task.FailReason = Fail_RequestError
					task.taskBreakOff()
				}
			}
		}, nil, 0, 10)
	}
}

//checkOriginResumeSupport check is url support range get or not
func checkOriginResumeSupport(targetUrl string) (bool, error) {
	c := &http.Client{Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment, //use system proxy
	}}
	var req = &http.Request{}
	var err error
	req.Method = "GET"
	req.Close = true
	req.URL, err = url.Parse(targetUrl)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	header := http.Header{}
	header.Set("Range", "bytes=0-0")
	req.Header = header
	resp, err := c.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return false, errors.New("response status code error")
	}
	result, exist := resp.Header["Accept-Ranges"]
	if exist {
		for _, v := range result {
			if v == "bytes" {
				//log.Println("support request range")
				return true, nil
			}
		}
	}

	buf := &bytes.Buffer{}
	written, err := io.CopyN(buf, resp.Body, 2)
	if err == io.EOF && written == 1 {
		return true, nil
	}
	if err != nil {
		return false, nil
	}
	return false, nil
}

//classify check header and distribute to different channel
func (dm *DownloadMgr) classify(task *Task) error {
	// download header and check download is resumable or not
	canResume, err := checkOriginResumeSupport(task.TargetUrl)
	if err != nil {
		//task failed
		return err
	}

	if canResume {
		task.canResume = true
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

func safeInfiLoop(todo func(), onPanic func(err interface{}), interval int, redoDelaySec int) {
	runChannel := make(chan struct{})
	go func() {
		for {
			<-runChannel
			go func() {
				defer func() {
					if err := recover(); err != nil {
						if onPanic != nil {
							onPanic(err)
						}
						time.Sleep(time.Duration(redoDelaySec) * time.Second)
						runChannel <- struct{}{}
					}
				}()
				for {
					todo()
					time.Sleep(time.Duration(interval) * time.Second)
				}
			}()
		}
	}()
	runChannel <- struct{}{}
}
