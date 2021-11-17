package downloadmgr

import (
	"github.com/daqnext/go-smart-routine/sr"
	"github.com/emirpasic/gods/lists/arraylist"
	"math/rand"
	"sync"
	"time"
)

const (
	preHandleChannel = "preHandleChannel"

	quickChannel       = "quickChannel"
	randomChannel      = "randomChannel"
	unpauseFastChannel = "unpauseFastChannel"
	unpauseSlowChannel = "unpauseSlowChannel"
)

const randomDownloadingTimeSec = 300 //todo set to 300 in production
const unpauseFastChannelSpeedLimit = 800 * 1024

const (
	preHandleChannelCountLimit = 2

	quickChannelCountLimit       = 6
	randomChannelCountLimit      = 6
	unpauseFastChannelCountLimit = 6
	unpauseSlowChannelCountLimit = 6
)

const (
	preHandleChannelRetryTime = 1

	quickChannelRetryTime       = 1
	randomChannelRetryTime      = 1
	unpauseFastChannelRetryTime = 1
	unpauseSlowChannelRetryTime = 1
)

type downloadChannel struct {
	dm *DownloadMgr

	downloadingCountLimit int
	retryTimesLimit       int

	idleListLock sync.RWMutex
	idleList     *arraylist.List

	//func
	//checkDownloadingStateFun check state to decide is continue or stop
	checkDownloadingStateFunc func(task *Task) (needBroken bool, brokenType BrokenType)
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
	//sorted by allowStartTime
	it := dc.idleList.Iterator()
	insertPos := -1
	for it.End(); it.Prev(); {
		index, value := it.Index(), it.Value()
		t := value.(*Task)
		if t.allowStartTime <= task.allowStartTime {
			insertPos = index
			break
		}
	}

	dc.idleList.Insert(insertPos+1, task)
}

func (dc *downloadChannel) handleBrokenTaskFunc(task *Task, brokenType BrokenType) {
	switch brokenType {
	case no_broken:
		return
	case broken_pause:
		//back to idle array
		task.Status = Pause
		dc.pushTaskToIdleList(task)
	case broken_expire:
		//cancel and delete task
		task.taskExpire()
	case broken_noSpeed:
		task.taskBreakOff()
	case broken_lowSpeed:
		//to slow channel
		task.failTimes = 0
		task.Status = New
		dc.dm.llog.Debugln("to slow channel", task.SavePath)
		dc.dm.downloadChannel[unpauseSlowChannel].pushTaskToIdleList(task)
	case broken_cancel:
		task.taskCancel()
	default:
		task.taskBreakOff()
	}
}

func (dc *downloadChannel) getChannelName() string {
	return dc.name
}

func (dc *downloadChannel) run() {
	for i := 0; i < dc.downloadingCountLimit; i++ {
		sr.New_Panic_Redo(func() {
			for {
				var task *Task
				task = dc.popTaskFormIdleList()
				if task == nil {
					time.Sleep(200 * time.Millisecond)
					continue
				}

				if task.cancelFlag {
					dc.dm.llog.Debugln("task cancel id", task.Id)
					task.taskCancel()
					continue
				}

				if task.TaskType == quickTask {
					if task.ExpireTime <= time.Now().Unix() {
						task.taskExpire()
						continue
					}
				}

				task.startDownload()
			}
		}, dc.dm.llog).Start()
	}
}

func initChannel(dm *DownloadMgr, downloadingCountLimit int, retryTimesLimiit int) *downloadChannel {
	if downloadingCountLimit < 1 || downloadingCountLimit > 15 {
		panic("channel downloadingCountLimit should between 1 and 15")
	}
	if retryTimesLimiit < 0 || retryTimesLimiit > 5 {
		panic("channel retryTimesLimt should between 0 and 5")
	}
	qdc := &downloadChannel{}
	qdc.dm = dm
	//qdc.speedLimitBPS = speedLimitBPS
	qdc.downloadingCountLimit = downloadingCountLimit
	qdc.retryTimesLimit = retryTimesLimiit
	qdc.idleList = arraylist.New()
	return qdc
}

func popRandomTask(dc *downloadChannel) *Task {
	dc.idleListLock.Lock()
	defer dc.idleListLock.Unlock()

	size := dc.idleList.Size()
	if size <= 0 {
		return nil
	}

	//random
	nowTime := time.Now().UnixMilli()
	it := dc.idleList.Iterator()
	maxIndex := -1
	for it.Next() {
		index, value := it.Index(), it.Value()
		if value.(*Task).allowStartTime > nowTime {
			break
		}
		maxIndex = index
	}

	if maxIndex == -1 {
		return nil
	}

	index := rand.Intn(maxIndex + 1)

	task, exist := dc.idleList.Get(index)
	if !exist {
		return nil
	}

	dc.idleList.Remove(index)
	return task.(*Task)
}

func popQueueTask(dc *downloadChannel) *Task {
	dc.idleListLock.Lock()
	defer dc.idleListLock.Unlock()

	size := dc.idleList.Size()
	if size <= 0 {
		return nil
	}

	//queue
	t, exist := dc.idleList.Get(0)
	if !exist {
		return nil
	}

	task := t.(*Task)
	if task.allowStartTime > time.Now().UnixMilli() {
		return nil
	}

	dc.idleList.Remove(0)
	return task
}

func initPreHandleChannel(dm *DownloadMgr) *downloadChannel {
	ic := initChannel(dm, preHandleChannelCountLimit, preHandleChannelRetryTime)
	ic.popTaskFormIdleList = func() *Task {
		return popQueueTask(ic)
	}

	ic.name = preHandleChannel
	return ic
}

//quickChannel
func initQuickChannel(dm *DownloadMgr) *downloadChannel {
	qc := initChannel(dm, quickChannelCountLimit, quickChannelRetryTime)
	qc.popTaskFormIdleList = func() *Task {
		return popQueueTask(qc)
	}
	qc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		if task.cancelFlag {
			return true, broken_cancel
		}
		if task.channel.idleSize() > 0 && task.Response.Duration().Seconds() > 10 && task.Response.BytesPerSecond() < 1 {
			//on speed and new task is waiting
			return true, broken_noSpeed

		}

		if time.Now().Unix() > task.ExpireTime {
			//task expire
			return true, broken_expire
		}

		return false, no_broken
	}
	//for debug
	qc.name = quickChannel
	return qc
}

//randomChannel
func initRandomPauseChannel(dm *DownloadMgr) *downloadChannel {
	rc := initChannel(dm, randomChannelCountLimit, randomChannelRetryTime)

	rc.popTaskFormIdleList = func() *Task {
		return popRandomTask(rc)
	}

	rc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		if task.cancelFlag {
			return true, broken_cancel
		}

		//todo don't break if idlelist size==0
		if task.Response.Duration().Seconds() > 15 && task.Response.BytesPerSecond() < 1 {
			//no speed
			return true, broken_noSpeed
		}

		if task.channel.idleSize() > 0 && task.Response.Duration().Seconds() > randomDownloadingTimeSec {
			//pause
			return true, broken_pause
		}

		return false, no_broken
	}
	//for debug
	rc.name = randomChannel

	return rc
}

//UnpauseFastChannel
func initUnpauseFastChannel(dm *DownloadMgr, speedLimitBPS float64) *downloadChannel {
	ufc := initChannel(dm, unpauseFastChannelCountLimit, unpauseFastChannelRetryTime)
	ufc.popTaskFormIdleList = func() *Task {
		return popQueueTask(ufc)
	}

	ufc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		if task.cancelFlag {
			return true, broken_cancel
		}
		speed := task.Response.BytesPerSecond()
		if task.Response.Duration().Seconds() > 15 && speed < 1 {
			//fail
			return true, broken_noSpeed
		}
		if task.Response.Duration().Seconds() > 15 && speed < speedLimitBPS {
			//to slow channel
			return true, broken_lowSpeed
		}

		return false, no_broken
	}
	//for debug
	ufc.name = unpauseFastChannel
	return ufc
}

//UnpauseSlowChannel
func initUnpauseSlowChannel(dm *DownloadMgr) *downloadChannel {
	usdc := initChannel(dm, unpauseSlowChannelCountLimit, unpauseSlowChannelRetryTime)
	usdc.popTaskFormIdleList = func() *Task {
		return popQueueTask(usdc)
	}

	usdc.checkDownloadingStateFunc = func(task *Task) (needBroken bool, brokenType BrokenType) {
		if task.cancelFlag {
			return true, broken_cancel
		}
		if task.Response.Duration().Seconds() > 15 &&
			task.Response.BytesPerSecond() < 1 {
			//speed too low
			return true, broken_noSpeed
		}
		return false, no_broken
	}
	//for debug
	usdc.name = unpauseSlowChannel
	return usdc
}
