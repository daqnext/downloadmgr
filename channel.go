package downloadmgr

import (
	"math/rand"
	"sync"
	"time"

	"github.com/emirpasic/gods/lists/arraylist"
)

const (
	preHandleChannel = "preHandleChannel"

	quickChannel       = "quickChannel"
	randomChannel      = "randomChannel"
	unpauseFastChannel = "unpauseFastChannel"
	unpauseSlowChannel = "unpauseSlowChannel"
)

const randomDownloadingTimeSec = 300            //todo set to 300 in production
const unpauseFastChannelSpeedLimit = 500 * 1024 //500 KB/s

const slowAlertTime = 200  // second
const slowAlertSpeed = 100 // 100 KB/s

const (
	preHandleChannelCountLimit = 3

	quickChannelCountLimit       = 6
	randomChannelCountLimit      = 6
	unpauseFastChannelCountLimit = 6
	unpauseSlowChannelCountLimit = 6
)

const (
	preHandleChannelRetryTime = 1

	quickChannelRetryTime       = 1
	randomChannelRetryTime      = 3
	unpauseFastChannelRetryTime = 1
	unpauseSlowChannelRetryTime = 3
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
		task.FailReason = Fail_Expire
		task.taskFail()
	case broken_sizeLimit:
		task.FailReason = Fail_SizeLimit
		task.taskFail()
	case broken_noSpeed:
		task.FailReason = Fail_RequestError
		task.taskBreakOff()
	case broken_lowSpeed:
		//to slow channel
		task.failTimes = 0
		task.Status = New
		if dc.dm.logger != nil {
			dc.dm.logger.Debugln("to slow channel", task.SavePath)
		}
		dc.dm.downloadChannel[unpauseSlowChannel].pushTaskToIdleList(task)
	case broken_cancel:
		task.taskCancel()
	default:
		task.FailReason = Fail_Other
		task.taskBreakOff()
	}
}

func (dc *downloadChannel) getChannelName() string {
	return dc.name
}

func (dc *downloadChannel) run() {
	for i := 0; i < dc.downloadingCountLimit; i++ {
		safeInfiLoop(func() {
			for {
				var task *Task
				task = dc.popTaskFormIdleList()
				if task == nil {
					time.Sleep(200 * time.Millisecond)
					continue
				}

				if task.cancelFlag {
					if dc.dm.logger != nil {
						dc.dm.logger.Debugln("task cancel id", task.Id)
					}
					task.taskCancel()
					continue
				}

				if task.TaskType == QuickTask {
					if task.ExpireTime <= time.Now().Unix() {
						task.FailReason = Fail_Expire
						task.taskFail()
						continue
					}
				}

				task.startDownload()
			}
		}, nil, 0, 10)
	}
}

func initChannel(dm *DownloadMgr, downloadingCountLimit int, retryTimesLimit int) *downloadChannel {
	if downloadingCountLimit < 1 || downloadingCountLimit > 15 {
		panic("channel downloadingCountLimit should between 1 and 15")
	}
	if retryTimesLimit < 0 || retryTimesLimit > 5 {
		panic("channel retryTimesLimit should between 0 and 5")
	}
	qdc := &downloadChannel{}
	qdc.dm = dm
	//qdc.speedLimitBPS = speedLimitBPS
	qdc.downloadingCountLimit = downloadingCountLimit
	qdc.retryTimesLimit = retryTimesLimit
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

		if task.SizeLimit > 0 && task.Response.BytesComplete() > task.SizeLimit {
			//size limit
			return true, broken_sizeLimit
		}

		if time.Now().Unix() > task.ExpireTime {
			//task expire
			return true, broken_expire
		}

		if task.channel.idleSize() == 0 {
			return false, no_broken
		}

		if task.Response.Duration().Seconds() > 5 && task.Response.BytesPerSecond() < 1 {
			//on speed and new task is waiting
			return true, broken_noSpeed

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

		if task.SizeLimit > 0 && task.Response.BytesComplete() > task.SizeLimit {
			//size limit
			return true, broken_sizeLimit
		}

		if task.slowSpeedCallback != nil &&
			task.Response.Duration().Seconds() > slowAlertTime &&
			task.Response.BytesPerSecond() < slowAlertSpeed {

			task.slowSpeedCallback(task)
		}

		if task.channel.idleSize() == 0 {
			return false, no_broken
		}

		if task.Response.Duration().Seconds() > 5 && task.Response.BytesPerSecond() < 1 {
			//no speed
			return true, broken_noSpeed
		}

		if task.Response.Duration().Seconds() > randomDownloadingTimeSec {
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

		if task.SizeLimit > 0 && task.Response.BytesComplete() > task.SizeLimit {
			//size limit
			return true, broken_sizeLimit
		}

		speed := task.Response.BytesPerSecond()
		if task.Response.Duration().Seconds() > 5 && speed < 1 {
			//fail
			return true, broken_lowSpeed
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

		if task.SizeLimit > 0 && task.Response.BytesComplete() > task.SizeLimit {
			//size limit
			return true, broken_sizeLimit
		}

		if task.slowSpeedCallback != nil &&
			task.Response.Duration().Seconds() > slowAlertTime &&
			task.Response.BytesPerSecond() < slowAlertSpeed {

			task.slowSpeedCallback(task)
		}

		if task.channel.idleSize() == 0 {
			return false, no_broken
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
