package downloadmgr

import (
	"github.com/daqnext/go-smart-routine/sr"
	"github.com/emirpasic/gods/lists/arraylist"
	"math/rand"
	"sync"
	"time"
)

//DownloadChannel interface for DownloadChannel
type DownloadChannel interface {
	idleSize() int
	pushTaskToIdleList(task ...interface{})
	pushTaskToDownloading(task ...interface{})
	popTaskFormIdleList() *Task
	run()
}

type DownloadChannelBase struct {
	SpeedLimitBPS           float64
	RunningCountControlChan chan bool
	DownloadingListLock     sync.Mutex
	DownloadingList         *arraylist.List

	//checkDownloadingState check state to decide is continue or stop
	checkDownloadingState func(task *Task) bool

	//handleBrokenTask how to handle if task broken
	handleBrokenTask func(task *Task)
}

func (dc *DownloadChannelBase) pushTaskToDownloading(task ...interface{}) {
	dc.DownloadingListLock.Lock()
	defer dc.DownloadingListLock.Unlock()

	dc.DownloadingList.Add(task...)
}

//QueueDownloadChannel FIFO start the download task
type QueueDownloadChannel struct {
	DownloadChannelBase
	IdleChannel chan *Task
}

func initQueueChannel(speedLimitBPS float64, downloadingCountLimit int) *QueueDownloadChannel {
	qdc := &QueueDownloadChannel{}
	qdc.SpeedLimitBPS = speedLimitBPS
	qdc.RunningCountControlChan = make(chan bool, downloadingCountLimit)
	qdc.DownloadingList = arraylist.New()
	qdc.IdleChannel = make(chan *Task, 1024*10)

	for i := 0; i < downloadingCountLimit; i++ {
		qdc.RunningCountControlChan <- true
	}
	return qdc
}

func (qdc *QueueDownloadChannel) idleSize() int {
	return len(qdc.IdleChannel)
}

func (qdc *QueueDownloadChannel) pushTaskToIdleList(task ...interface{}) {
	for _, v := range task {
		qdc.IdleChannel <- v.(*Task)
	}
}

func (qdc *QueueDownloadChannel) popTaskFormIdleList() *Task {
	return <-qdc.IdleChannel
}

func (qdc *QueueDownloadChannel) run() {
	sr.New_Panic_Redo(func() {
		for true {
			//get token
			<-qdc.RunningCountControlChan
			select {
			case task := <-qdc.IdleChannel:
				//smart-routine
				sr.New_Panic_Return(func() {
					defer func() {
						// token back
						qdc.RunningCountControlChan <- true
					}()
					qdc.pushTaskToIdleList(task)

					//start download task
					_ = task

				}).Start()
			}
		}
	})
}

//RandomDownloadChannel random run the download task
type RandomDownloadChannel struct {
	DownloadChannelBase
	IdleListLock sync.RWMutex
	IdleList     *arraylist.List
}

func initRandomChannel(speedLimitBPS float64, downloadingCountLimit int) *RandomDownloadChannel {
	qdc := &RandomDownloadChannel{}
	qdc.SpeedLimitBPS = speedLimitBPS
	qdc.RunningCountControlChan = make(chan bool, downloadingCountLimit)
	qdc.DownloadingList = arraylist.New()
	qdc.IdleList = arraylist.New()

	for i := 0; i < downloadingCountLimit; i++ {
		qdc.RunningCountControlChan <- true
	}
	return qdc
}

func (rdc *RandomDownloadChannel) idleSize() int {
	rdc.IdleListLock.Lock()
	defer rdc.IdleListLock.Unlock()

	return rdc.IdleList.Size()
}

func (rdc *RandomDownloadChannel) pushTaskToIdleList(task ...interface{}) {
	rdc.IdleListLock.Lock()
	defer rdc.IdleListLock.Unlock()

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
	sr.New_Panic_Redo(func() {
		for true {
			//get token
			<-rdc.RunningCountControlChan

			for {
				if rdc.idleSize() > 0 {
					break
				}
				time.Sleep(500 * time.Millisecond)
			}

			//get task form idleList
			task := rdc.popTaskFormIdleList()
			//smart-routine
			sr.New_Panic_Return(func() {
				defer func() {
					// token back
					rdc.RunningCountControlChan <- true
				}()
				rdc.pushTaskToDownloading(task)

				//start download task
				_ = task

			}).Start()

		}
	})
}

//type DownloadChannel struct {
//	SpeedLimitBPS         float64
//	DownloadingCountLimit int
//
//	DownloadingListLock sync.RWMutex
//	DownloadingList     *arraylist.List
//
//	IdleListLock sync.RWMutex
//	IdleList     *arraylist.List
//
//	IdleChannel chan *Task
//
//	//checkDownloadingState check state to decide is continue or stop
//	checkDownloadingState func(task *Task) bool
//
//	//handleBrokenTask how to handle if task broken
//	handleBrokenTask func(task *Task)
//
//	RunningCountControlChan chan bool
//}
//
//const (
//	quickChannel       = "quickChannel"
//	randomChannel      = "randomChannel"
//	unpauseFastChannel = "unpauseFastChannel"
//	unpauseSlowChannel = "unpauseSlowChannel"
//)
//
//func initChannel(speedLimitBPS float64, downloadingCountLimit int) *DownloadChannel {
//	dc := &DownloadChannel{
//		SpeedLimitBPS:           speedLimitBPS,
//		DownloadingCountLimit:   downloadingCountLimit,
//		RunningCountControlChan: make(chan bool, downloadingCountLimit),
//		DownloadingList:         arraylist.New(),
//		pauseList:                arraylist.New(),
//		IdleChannel: make(chan *Task,1024*10),
//	}
//	for i := 0; i < dc.DownloadingCountLimit; i++ {
//		dc.RunningCountControlChan <- true
//	}
//	return dc
//}
//
//func (dc *DownloadChannel) pauseListSize() int {
//	return dc.pauseList.Size()
//}
//
//func (dc *DownloadChannel) idleChannelSize() int{
//	return len(dc.IdleChannel)
//}
//
//func (dc *DownloadChannel) downloadingListSize() int {
//	return dc.DownloadingList.Size()
//}
//
//func (dc *DownloadChannel) pushTaskToPauseList(task ...interface{}) {
//	dc.pauseListLock.Lock()
//	defer dc.pauseListLock.Unlock()
//
//	dc.pauseList.Add(task...)
//}
//
//func (dc *DownloadChannel) pushTaskToDownloading(task ...interface{}) {
//	dc.DownloadingListLock.Lock()
//	defer dc.DownloadingListLock.Unlock()
//
//	dc.DownloadingList.Add(task...)
//}
//
//func (dc *DownloadChannel) popFistTaskFromPauseList() *Task {
//	dc.pauseListLock.Lock()
//	defer dc.pauseListLock.Unlock()
//
//	task, exist := dc.pauseList.Get(0)
//	if !exist {
//		return nil
//	}
//
//	dc.pauseList.Remove(0)
//	return task.(*Task)
//}
//
//func (dc *DownloadChannel) popRandTaskFromPauseList() *Task {
//	dc.pauseListLock.Lock()
//	defer dc.pauseListLock.Unlock()
//
//	size := dc.pauseList.Size()
//	if size <= 0 {
//		return nil
//	}
//	index := rand.Intn(size)
//	task, exist := dc.pauseList.Get(index)
//	if !exist {
//		return nil
//	}
//
//	dc.pauseList.Remove(index)
//	return task.(*Task)
//}
//
//func (dc *DownloadChannel) run() {
//	sr.New_Panic_Redo(func() {
//		for true {
//			//get token
//			<-dc.RunningCountControlChan
//			select {
//			case task:=<-dc.IdleChannel:
//				//smart-routine
//				sr.New_Panic_Return(func() {
//					defer func() {
//						// token back
//						dc.RunningCountControlChan <- true
//					}()
//
//					//start download task
//					_ = task
//
//				}).Start()
//			default:
//				if  {
//
//				}
//			}
//
//
//			//for {
//			//	dc.IdleListLock.RLock()
//			//	if dc.idleListSize() > 0 {
//			//		dc.IdleListLock.RUnlock()
//			//		break
//			//	}
//			//	time.Sleep(500 * time.Millisecond)
//			//}
//			//
//			////get task form idleList
//			//task := dc.popFistTaskFromIdle()
//			////smart-routine
//			//sr.New_Panic_Return(func() {
//			//	defer func() {
//			//		// token back
//			//		dc.RunningCountControlChan <- true
//			//	}()
//			//
//			//	//start download task
//			//	_ = task
//			//
//			//}).Start()
//
//		}
//	})
//}
