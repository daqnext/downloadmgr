package downloadmgr

import (
	"bytes"
	"context"
	"github.com/daqnext/LocalLog/log"
	"github.com/daqnext/go-smart-routine/sr"
	"io"
	"math"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const randomDownloadingTimeSec = 7

type DownloadMgr struct {
	//channel to receive task
	globalDownloadTaskChan chan *Task

	//global id
	currentId uint64
	idLock    sync.Mutex

	//different channel
	downloadChannel map[string]DownloadChannel

	//
	//ignoreHeaderMap        sync.Map
	ignoreHeaderMap     map[string]struct{}
	ignoreHeaderMapLock sync.RWMutex

	taskMap sync.Map
}

//NewDownloadMgr new instance of Download manager
func NewDownloadMgr(logger *log.LocalLog) *DownloadMgr {
	if logger == nil {
		panic("logger is nil")
	}
	llog = logger

	dm := &DownloadMgr{
		globalDownloadTaskChan: make(chan *Task, 1024*10),
		currentId:              0,
	}
	dm.genDownloadChannel()

	//loop go routine
	dm.classifyNewTaskLoop()
	dm.StartDownloadLoop()

	return dm
}

//init download channel
func (dm *DownloadMgr) genDownloadChannel() {
	dm.downloadChannel = map[string]DownloadChannel{}
	//quickChannel
	qc := initQueueChannel(1, 6, 1)
	qc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		if task.channel.idleSize() > 0 {
			if task.response.Duration().Seconds() > 10 && task.response.BytesPerSecond() < 1 {
				//on speed and new task is waiting
				return true, broken_noSpeed

			}
		}

		if time.Now().Unix() > task.expireTime {
			//task expire
			return true, broken_expire
		}

		return false, no_broken
	}
	qc.handleBrokenTaskFunc = func(task *Task, brokenType BrokenType) {
		switch brokenType {
		case no_broken:
			return
		case broken_pause:
		case broken_expire:
			//cancel and delete task
			task.cancel()
			task.tryAgainOrFail()
		case broken_noSpeed:
			task.cancel()
			task.tryAgainOrFail()
		case broken_lowSpeed:
		}
	}
	//for debug
	qc.name = quickChannel
	dm.downloadChannel[quickChannel] = qc

	//randomChannel
	rc := initRandomChannel(1, 6, 2)
	rc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		// if new task is waiting
		if task.channel.idleSize() > 0 {
			if task.response.Duration().Seconds() > 15 && task.response.BytesPerSecond() < 1 {
				//no speed
				return true, broken_noSpeed
			}

			llog.Println(task.savePath, task.response.Start.Unix())
			if task.response.Duration().Seconds() > randomDownloadingTimeSec {
				//pause
				return true, broken_pause
			}
		}
		return false, no_broken
	}
	rc.handleBrokenTaskFunc = func(task *Task, brokenType BrokenType) {
		switch brokenType {
		case no_broken:
			return
		case broken_pause:
			task.cancel()
			//back to idle array
			rc.pushTaskToIdleList(task)

		case broken_expire:
		case broken_noSpeed:
			task.cancel()
			task.tryAgainOrFail()
		case broken_lowSpeed:
		}
	}
	//for debug
	rc.name = randomChannel
	dm.downloadChannel[randomChannel] = rc

	//unpauseFastChannel
	ufc := initQueueChannel(500*1024, 6, 1)
	ufc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		if task.channel.idleSize() > 0 && task.response.Duration().Seconds() > 15 {
			speed := task.response.BytesPerSecond()
			if speed == 0 {
				//fail
				return true, broken_noSpeed
			}
			if speed < ufc.SpeedLimitBPS {
				//to slow channel
				return true, broken_lowSpeed
			}
		}

		return false, no_broken
	}
	ufc.handleBrokenTaskFunc = func(task *Task, brokenType BrokenType) {
		switch brokenType {
		case no_broken:
			return
		case broken_pause:
		case broken_expire:
		case broken_noSpeed:
			task.cancel()
			//retry or delete
			task.tryAgainOrFail()
		case broken_lowSpeed:
			task.cancel()
			//to slow channel
		}
	}
	//for debug
	ufc.name = unpauseFastChannel
	dm.downloadChannel[unpauseFastChannel] = ufc

	//unpauseSlowChannel
	usc := initQueueChannel(1, 6, 1)
	usc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		if task.channel.idleSize() > 0 &&
			task.response.Duration().Seconds() > 15 &&
			task.response.BytesPerSecond() < 1 {
			//speed is too low
			return true, broken_noSpeed
		}
		return false, no_broken
	}
	usc.handleBrokenTaskFunc = func(task *Task, brokenType BrokenType) {
		switch brokenType {
		case no_broken:
			return
		case broken_pause:
		case broken_expire:
		case broken_noSpeed:
			task.cancel()
			//retry or delete
			task.tryAgainOrFail()
		case broken_lowSpeed:
		}
	}
	//for debug
	usc.name = unpauseSlowChannel
	dm.downloadChannel[unpauseSlowChannel] = usc

}

func (dm *DownloadMgr) AddQuickDownloadTask(savePath string, targetUrl string, expireTime int64) uint64 {
	return dm.addDownloadTask(0, savePath, targetUrl, quickTask, expireTime)
}

func (dm *DownloadMgr) AddNormalDownloadTask(savePath string, targetUrl string) uint64 {
	return dm.addDownloadTask(0, savePath, targetUrl, randomTask, 0)
}

func (dm *DownloadMgr) addDownloadTask(id uint64, savePath string, targetUrl string, taskType TaskType, expireTime int64) uint64 {
	taskId := id
	if taskId == 0 {
		//gen id
		dm.idLock.Lock()
		if dm.currentId >= math.MaxUint64 {
			dm.currentId = 0
		}
		dm.currentId++
		dm.idLock.Unlock()
		taskId = dm.currentId
	}

	//new task
	task := newTask(taskId, savePath, targetUrl, taskType, expireTime)
	task.dm = dm

	//into map
	dm.taskMap.Store(task.id, task)

	//into channel
	dm.globalDownloadTaskChan <- task
	return task.id
}

//classifyNewTask loop classify task to different channel
func (dm *DownloadMgr) classifyNewTaskLoop() {
	sr.New_Panic_Redo(func() {
		for {
			select {
			case task := <-dm.globalDownloadTaskChan:
				//classify task
				err := dm.classify(task)
				if err != nil {
					// Todo how to handle error
					//log error
					//llog.Println("classify task error",err)

					//on failed
					if task.onFail != nil {
						task.onFail(task)
					}
				}
			}
		}
	}, llog).Start()
}

//PreHandleOrigin check is url support range get or not, and get origin header at same time
func PreHandleOrigin(targetUrl string) (http.Header, bool, error) {
	var req http.Request
	var err error
	req.Method = "GET"
	req.Close = true
	req.URL, err = url.Parse(targetUrl)
	if err != nil {
		return nil, false, err
	}
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	req.WithContext(ctx)
	header := http.Header{}
	header.Set("Range", "bytes=0-0")
	req.Header = header
	resp, err := http.DefaultClient.Do(&req)
	if err != nil {
		return nil, false, err
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

	task.status = Idle
	//if quickTask, push into quickChannel
	if task.taskType == quickTask {
		dm.downloadChannel[quickChannel].pushTaskToIdleList(task)
		return nil
	}

	// download header and check download is resumable or not
	header, canResume, err := PreHandleOrigin(task.targetUrl)
	if err != nil {
		//task failed
		task.status = Fail
		return err
	}

	if header != nil {
		//save header
		err := dm.SaveHeader(task.savePath, header)
		if err != nil {
			return err
		}
	}

	if canResume {
		task.resumable = true
		dm.downloadChannel[randomChannel].pushTaskToIdleList(task)
	} else {
		dm.downloadChannel[unpauseFastChannel].pushTaskToIdleList(task)
	}
	return nil
}

func (dm *DownloadMgr) StartDownloadLoop() {
	for _, v := range dm.downloadChannel {
		v.run()
	}
}
