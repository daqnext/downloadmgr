package downloadmgr

import (
	"github.com/daqnext/go-smart-routine/sr"
	"github.com/emirpasic/gods/lists/arraylist"
	"math/rand"
	"sync"
	"time"
)

const (
	quickChannel       = "quickChannel"
	randomChannel      = "randomChannel"
	unpauseFastChannel = "unpauseFastChannel"
	unpauseSlowChannel = "unpauseSlowChannel"
)

//DownloadChannel interface for DownloadChannel
type DownloadChannel interface {
	idleSize() int
	pushTaskToIdleList(task ...interface{})
	pushTaskToDownloading(task ...interface{})
	popTaskFormIdleList() *Task
	run()
	checkDownloadingState(task *Task) (needBroken bool, brokenType BrokenType)
	handleBrokenTask(task *Task, brokenType BrokenType)
	getRetryTimesLimit() int

	//for debug
	getChannelName() string
}

type DownloadChannelBase struct {
	SpeedLimitBPS         float64
	DownloadingCountLimit int
	RetryTimesLimit       int
	DownloadingListLock   sync.Mutex
	DownloadingList       *arraylist.List

	//checkDownloadingStateFun check state to decide is continue or stop
	checkDownloadingStateFunc func(task *Task) (needBroken bool, brokenType BrokenType)

	//handleBrokenTaskFun how to handle if task broken
	handleBrokenTaskFunc func(task *Task, brokenType BrokenType)

	//for debug
	name string
}

func (dc *DownloadChannelBase) pushTaskToDownloading(task ...interface{}) {
	dc.DownloadingListLock.Lock()
	defer dc.DownloadingListLock.Unlock()

	dc.DownloadingList.Add(task...)
}

func (dc *DownloadChannelBase) handleBrokenTask(task *Task, brokenType BrokenType) {
	if dc.handleBrokenTaskFunc != nil {
		dc.handleBrokenTaskFunc(task, brokenType)
	}
}

func (dc *DownloadChannelBase) checkDownloadingState(task *Task) (needBroken bool, brokenType BrokenType) {
	if dc.checkDownloadingStateFunc != nil {
		return dc.checkDownloadingStateFunc(task)
	}
	return false, no_broken
}

func (dc *DownloadChannelBase) getChannelName() string {
	return dc.name
}

func (dc *DownloadChannelBase) getRetryTimesLimit() int {
	return dc.RetryTimesLimit
}

//QueueDownloadChannel FIFO start the download task
type QueueDownloadChannel struct {
	DownloadChannelBase
	IdleChannel chan *Task
}

func initQueueChannel(speedLimitBPS float64, downloadingCountLimit int, retryTimesLimt int) *QueueDownloadChannel {
	qdc := &QueueDownloadChannel{}
	qdc.SpeedLimitBPS = speedLimitBPS
	qdc.DownloadingCountLimit = downloadingCountLimit
	qdc.RetryTimesLimit = retryTimesLimt
	qdc.DownloadingList = arraylist.New()
	qdc.IdleChannel = make(chan *Task, 1024*10)
	return qdc
}

func (qdc *QueueDownloadChannel) idleSize() int {
	return len(qdc.IdleChannel)
}

func (qdc *QueueDownloadChannel) pushTaskToIdleList(task ...interface{}) {
	for _, v := range task {
		t := v.(*Task)
		t.channel = qdc
		qdc.IdleChannel <- t
	}
}

func (qdc *QueueDownloadChannel) popTaskFormIdleList() *Task {
	return <-qdc.IdleChannel
}

func (qdc *QueueDownloadChannel) run() {
	for i := 0; i < qdc.DownloadingCountLimit; i++ {
		sr.New_Panic_Redo(func() {
			for {
				task := qdc.popTaskFormIdleList()
				//llog.Println(task)

				//if it's retry task,wait at least 15s from last failed,exclude quickTask
				startTime := task.lastFailTimeStamp + 15
				if task.taskType == quickTask {
					startTime = 0
				}
				for {
					if startTime <= time.Now().Unix() {
						break
					}

					if qdc.idleSize() == 0 {
						time.Sleep(1 * time.Second)
						continue
					}

					qdc.pushTaskToIdleList(task)
					return
				}

				qdc.pushTaskToDownloading(task)

				//start download task
				task.StartDownload()
			}
		}, llog).Start()
	}
}

//RandomDownloadChannel random run the download task
type RandomDownloadChannel struct {
	DownloadChannelBase
	IdleListLock sync.RWMutex
	IdleList     *arraylist.List
}

func initRandomChannel(speedLimitBPS float64, downloadingCountLimit int, retryTimesLimit int) *RandomDownloadChannel {
	rdc := &RandomDownloadChannel{}
	rdc.SpeedLimitBPS = speedLimitBPS
	rdc.DownloadingCountLimit = downloadingCountLimit
	rdc.RetryTimesLimit = retryTimesLimit
	rdc.DownloadingList = arraylist.New()
	rdc.IdleList = arraylist.New()

	return rdc
}

func (rdc *RandomDownloadChannel) idleSize() int {
	rdc.IdleListLock.Lock()
	defer rdc.IdleListLock.Unlock()

	return rdc.IdleList.Size()
}

func (rdc *RandomDownloadChannel) pushTaskToIdleList(task ...interface{}) {
	rdc.IdleListLock.Lock()
	defer rdc.IdleListLock.Unlock()

	for _, v := range task {
		t := v.(*Task)
		t.channel = rdc
	}

	rdc.IdleList.Add(task...)
}

func (rdc *RandomDownloadChannel) popTaskFormIdleList() *Task {
	rdc.IdleListLock.Lock()
	defer rdc.IdleListLock.Unlock()

	size := rdc.IdleList.Size()
	if size <= 0 {
		return nil
	}
	index := rand.Intn(size)
	task, exist := rdc.IdleList.Get(index)
	if !exist {
		return nil
	}

	rdc.IdleList.Remove(index)
	return task.(*Task)
}

func (rdc *RandomDownloadChannel) run() {
	for i := 0; i < rdc.DownloadingCountLimit; i++ {
		sr.New_Panic_Redo(func() {
			for {
				var task *Task
				for {
					task = rdc.popTaskFormIdleList()
					if task != nil {
						break
					}
					//log.Println("no task sleep")
					time.Sleep(500 * time.Millisecond)
				}

				//if it's retry task,wait at least 15s from last failed,exclude quickTask
				startTime := task.lastFailTimeStamp + 15
				for {
					if startTime <= time.Now().Unix() {
						break
					}

					if rdc.idleSize() == 0 {
						time.Sleep(1 * time.Second)
						continue
					}

					rdc.pushTaskToIdleList(task)
					return
				}

				rdc.pushTaskToDownloading(task)

				//start download task
				task.StartDownload()
			}
		}, llog).Start()
	}
}
