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

type downloadChannel struct {
	dm *DownloadMgr

	speedLimitBPS         float64
	downloadingCountLimit int
	retryTimesLimit       int

	idleListLock sync.RWMutex
	idleList     *arraylist.List

	//func
	//checkDownloadingStateFun check state to decide is continue or stop
	checkDownloadingStateFunc func(task *Task) (needBroken bool, brokenType BrokenType)
	//handleBrokenTaskFun how to handle if task broken
	handleBrokenTaskFunc func(task *Task, brokenType BrokenType)
	//get task from idle list
	popTaskFormIdleList func() *Task

	//for debug
	name string
}

func (dc *downloadChannel) idleSize() int {
	dc.idleListLock.Lock()
	defer dc.idleListLock.Unlock()

	return dc.idleList.Size()
}

func (dc *downloadChannel) pushTaskToIdleList(task *Task) {
	dc.idleListLock.Lock()
	defer dc.idleListLock.Unlock()

	task.channel = dc
	dc.idleList.Add(task)
}

func (dc *downloadChannel) getChannelName() string {
	return dc.name
}

func (dc *downloadChannel) run() {
	for i := 0; i < dc.downloadingCountLimit; i++ {
		sr.New_Panic_Redo(func() {
		Outloop:
			for {
				var task *Task
				for {
					task = dc.popTaskFormIdleList()
					if task != nil {
						break
					}
					//log.Println("no task sleep")
					time.Sleep(500 * time.Millisecond)
				}

				if task.cancelFlag {
					dc.dm.llog.Debugln("task cancel id", task.Id)
					continue
				}

				startTime := task.lastFailTimeStamp + 5
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

					if dc.idleSize() == 0 {
						time.Sleep(1 * time.Second)
						continue
					}

					time.Sleep(100 * time.Millisecond)
					dc.pushTaskToIdleList(task)
					continue Outloop
				}

				//start download task
				task.startDownload()
			}
		}, dc.dm.llog).Start()
	}
}

func initChannel(dm *DownloadMgr, speedLimitBPS float64, downloadingCountLimit int, retryTimesLimt int) *downloadChannel {
	qdc := &downloadChannel{}
	qdc.dm = dm
	qdc.speedLimitBPS = speedLimitBPS
	qdc.downloadingCountLimit = downloadingCountLimit
	qdc.retryTimesLimit = retryTimesLimt
	qdc.idleList = arraylist.New()
	return qdc
}

func popTask(dc *downloadChannel, isRandom bool) *Task {
	dc.idleListLock.Lock()
	defer dc.idleListLock.Unlock()

	size := dc.idleList.Size()
	if size <= 0 {
		return nil
	}

	index := 0
	if isRandom {
		index = rand.Intn(size)
	}

	task, exist := dc.idleList.Get(index)
	if !exist {
		return nil
	}

	dc.idleList.Remove(index)
	return task.(*Task)
}

//quickChannel
func initQuickChannel(dm *DownloadMgr) *downloadChannel {
	qc := initChannel(dm, 1, quickChannelCountLimit, quickChannelRetryTime)
	qc.popTaskFormIdleList = func() *Task {
		return popTask(qc, false)
	}
	qc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		if task.cancelFlag {
			return true, broken_cancel
		}
		if task.channel.idleSize() > 0 {
			if task.response.Duration().Seconds() > 10 && task.response.BytesPerSecond() < qc.speedLimitBPS {
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
			task.taskBreakOff()
		case broken_lowSpeed:
		case broken_cancel:
			task.taskBreakOff()
		default:
			task.taskBreakOff()
		}
	}
	//for debug
	qc.name = quickChannel
	return qc
}

//randomChannel
func initRandomPauseChannel(dm *DownloadMgr) *downloadChannel {
	rc := initChannel(dm, 1, randomChannelCountLimit, randomChannelRetryTime)

	rc.popTaskFormIdleList = func() *Task {
		return popTask(rc, true)
	}

	rc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		if task.cancelFlag {
			return true, broken_cancel
		}
		if task.response.Duration().Seconds() > 15 && task.response.BytesPerSecond() < 1 {
			//no speed
			return true, broken_noSpeed
		}
		// if new task is waiting

		if task.response.Duration().Seconds() > 15 && task.response.BytesPerSecond() < rc.speedLimitBPS {
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
			task.taskBreakOff()
		case broken_lowSpeed:
		case broken_cancel:
			task.taskBreakOff()
		default:
			task.taskBreakOff()
		}
	}
	//for debug
	rc.name = randomChannel

	return rc
}

//UnpauseFastChannel
func initUnpauseFastChannel(dm *DownloadMgr) *downloadChannel {
	ufc := initChannel(dm, unpauseFastChannelSpeedLimit, unpauseFastChannelCountLimit, unpauseFastChannelRetryTime)
	ufc.popTaskFormIdleList = func() *Task {
		return popTask(ufc, false)
	}

	ufc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		if task.cancelFlag {
			return true, broken_cancel
		}
		speed := task.response.BytesPerSecond()
		if task.response.Duration().Seconds() > 15 && speed == 0 {
			//fail
			return true, broken_noSpeed
		}
		if task.response.Duration().Seconds() > 15 && speed < ufc.speedLimitBPS {
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
			task.taskBreakOff()
		case broken_lowSpeed:
			//to slow channel
			task.failTimes = 0
			task.Status = Idle
			task.cancel = nil
			dm.llog.Infoln("to slow channel", task.SavePath)
			dm.downloadChannel[unpauseSlowChannel].pushTaskToIdleList(task)
		case broken_cancel:
			task.taskBreakOff()
		default:
			task.taskBreakOff()
		}
	}
	//for debug
	ufc.name = unpauseFastChannel
	return ufc
}

//UnpauseSlowChannel
func initUnpauseSlowChannel(dm *DownloadMgr) *downloadChannel {
	usdc := initChannel(dm, 1, unpauseSlowChannelCountLimit, unpauseSlowChannelRetryTime)
	usdc.popTaskFormIdleList = func() *Task {
		return popTask(usdc, false)
	}

	usdc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		if task.cancelFlag {
			return true, broken_cancel
		}
		if task.response.Duration().Seconds() > 15 &&
			task.response.BytesPerSecond() < usdc.speedLimitBPS {
			//speed is too low
			return true, broken_noSpeed
		}
		return false, no_broken
	}
	usdc.handleBrokenTaskFunc = func(task *Task, brokenType BrokenType) {
		switch brokenType {
		case no_broken:
			return
		case broken_pause:
		case broken_expire:
		case broken_noSpeed:
			//retry or delete
			task.taskBreakOff()
		case broken_lowSpeed:
		case broken_cancel:
			task.taskBreakOff()
		default:
			task.taskBreakOff()
		}
	}
	//for debug
	usdc.name = unpauseSlowChannel
	return usdc
}
