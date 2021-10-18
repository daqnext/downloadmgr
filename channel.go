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

const channelIdleTaskLimit = 1024 * 20

const randomDownloadingTimeSec = 20
const unpauseFastChannelSpeedLimit = 800 * 1024

const (
	quickChannelCountLimit       = 6
	randomChannelCountLimit      = 6
	unpauseFastChannelCountLimit = 6
	unpauseSlowChannelCountLimit = 6
)

const (
	quickChannelRetryTime       = 1
	randomChannelRetryTime      = 1
	unpauseFastChannelRetryTime = 1
	unpauseSlowChannelRetryTime = 1
)

//DownloadChannel interface for DownloadChannel
type DownloadChannel interface {
	idleSize() int
	pushTaskToIdleList(task *Task)
	//pushTaskToDownloading(task ...interface{})
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
	//DownloadingListLock   sync.Mutex
	//DownloadingList       *arraylist.List

	//checkDownloadingStateFun check state to decide is continue or stop
	checkDownloadingStateFunc func(task *Task) (needBroken bool, brokenType BrokenType)

	//handleBrokenTaskFun how to handle if task broken
	handleBrokenTaskFunc func(task *Task, brokenType BrokenType)

	//for debug
	name string
}

//func (dc *DownloadChannelBase) pushTaskToDownloading(task ...interface{}) {
//	dc.DownloadingListLock.Lock()
//	defer dc.DownloadingListLock.Unlock()
//
//	dc.DownloadingList.Add(task...)
//}

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
	//qdc.DownloadingList = arraylist.New()
	qdc.IdleChannel = make(chan *Task, channelIdleTaskLimit)
	return qdc
}

func (qdc *QueueDownloadChannel) idleSize() int {
	return len(qdc.IdleChannel)
}

func (qdc *QueueDownloadChannel) pushTaskToIdleList(task *Task) {
	task.channel = qdc
	qdc.IdleChannel <- task
}

func (qdc *QueueDownloadChannel) popTaskFormIdleList() *Task {
	return <-qdc.IdleChannel
}

func (qdc *QueueDownloadChannel) run() {
	for i := 0; i < qdc.DownloadingCountLimit; i++ {
		sr.New_Panic_Redo(func() {
		Outloop:
			for {
				task := qdc.popTaskFormIdleList()

				if task.cancelFlag {
					llog.Debugln("task cancel id", task.Id)
					continue
				}

				startTime := task.lastFailTimeStamp + 15
				if task.TaskType == quickTask {
					if task.ExpireTime <= time.Now().Unix() {
						task.fail()
						continue
					}
					startTime = 0
				}

				//if it's retry task,wait at least 15s from last failed,exclude quickTask
				for {
					if startTime <= time.Now().Unix() {
						break
					}

					if qdc.idleSize() == 0 {
						time.Sleep(1 * time.Second)
						continue
					}

					time.Sleep(100 * time.Millisecond)
					qdc.pushTaskToIdleList(task)
					continue Outloop
				}

				//qdc.pushTaskToDownloading(task)

				//start download task
				task.startDownload()
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
	//rdc.DownloadingList = arraylist.New()
	rdc.IdleList = arraylist.New()

	return rdc
}

func (rdc *RandomDownloadChannel) idleSize() int {
	rdc.IdleListLock.Lock()
	defer rdc.IdleListLock.Unlock()

	return rdc.IdleList.Size()
}

func (rdc *RandomDownloadChannel) pushTaskToIdleList(task *Task) {
	rdc.IdleListLock.Lock()
	defer rdc.IdleListLock.Unlock()

	task.channel = rdc
	rdc.IdleList.Add(task)
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
		Outloop:
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

				if task.cancelFlag {
					llog.Debugln("task cancel id", task.Id)
					continue
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

					time.Sleep(100 * time.Millisecond)
					rdc.pushTaskToIdleList(task)
					continue Outloop
				}

				//rdc.pushTaskToDownloading(task)

				//start download task
				task.startDownload()
			}
		}, llog).Start()
	}
}

//quickChannel
func initQuickChannel() *QueueDownloadChannel {
	qc := initQueueChannel(1, quickChannelCountLimit, quickChannelRetryTime)
	qc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		if task.channel.idleSize() > 0 {
			if task.response.Duration().Seconds() > 10 && task.response.BytesPerSecond() < qc.SpeedLimitBPS {
				//on speed and new task is waiting
				return true, broken_noSpeed

			}
		}

		if time.Now().Unix() > task.ExpireTime {
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
			task.fail()
		case broken_noSpeed:
			task.tryAgainOrFail()
		case broken_lowSpeed:
		default:
			task.tryAgainOrFail()
		}
	}
	//for debug
	qc.name = quickChannel
	return qc
}

//randomChannel
func initRandomPauseChannel() *RandomDownloadChannel {
	rc := initRandomChannel(1, randomChannelCountLimit, randomChannelRetryTime)
	rc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		if task.response.Duration().Seconds() > 15 && task.response.BytesPerSecond() < 1 {
			//no speed
			return true, broken_noSpeed
		}
		// if new task is waiting

		if task.response.Duration().Seconds() > 15 && task.response.BytesPerSecond() < rc.SpeedLimitBPS {
			//no speed
			return true, broken_noSpeed
		}

		if task.channel.idleSize() > 0 && task.response.Duration().Seconds() > randomDownloadingTimeSec {
			//pause
			return true, broken_pause
		}

		return false, no_broken
	}
	rc.handleBrokenTaskFunc = func(task *Task, brokenType BrokenType) {
		switch brokenType {
		case no_broken:
			return
		case broken_pause:
			//back to idle array
			task.Status = Pause
			rc.pushTaskToIdleList(task)

		case broken_expire:
		case broken_noSpeed:
			task.tryAgainOrFail()
		case broken_lowSpeed:
		default:
			task.tryAgainOrFail()
		}
	}
	//for debug
	rc.name = randomChannel

	return rc
}

//UnpauseFastChannel
func initUnpauseFastChannel(dm *DownloadMgr) *QueueDownloadChannel {
	ufc := initQueueChannel(unpauseFastChannelSpeedLimit, unpauseFastChannelCountLimit, unpauseFastChannelRetryTime)
	ufc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {

		speed := task.response.BytesPerSecond()
		if task.response.Duration().Seconds() > 15 && speed == 0 {
			//fail
			return true, broken_noSpeed
		}
		if task.response.Duration().Seconds() > 15 && speed < ufc.SpeedLimitBPS {
			//to slow channel
			return true, broken_lowSpeed
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
			//retry or delete
			task.tryAgainOrFail()
		case broken_lowSpeed:
			//to slow channel
			task.failTimes = 0
			task.Status = Idle
			llog.Infoln("to slow channel", task.SavePath)
			dm.downloadChannel[unpauseSlowChannel].pushTaskToIdleList(task)
		default:
			task.tryAgainOrFail()
		}
	}
	//for debug
	ufc.name = unpauseFastChannel
	return ufc
}

//UnpauseSlowChannel
func initUnpauseSlowChannel() *QueueDownloadChannel {
	usc := initQueueChannel(1, unpauseSlowChannelCountLimit, unpauseSlowChannelRetryTime)
	usc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {

		if task.response.Duration().Seconds() > 15 &&
			task.response.BytesPerSecond() < usc.SpeedLimitBPS {
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
			//retry or delete
			task.tryAgainOrFail()
		case broken_lowSpeed:
		default:
			task.tryAgainOrFail()
		}
	}
	//for debug
	usc.name = unpauseSlowChannel
	return usc
}
