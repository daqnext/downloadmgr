package downloadmgr

import (
	"github.com/daqnext/go-smart-routine/sr"
	"github.com/emirpasic/gods/lists/arraylist"
	"math/rand"
	"sync"
	"time"
)

type DownloadChannel struct {
	SpeedLimitBPS         float64
	DownloadingCountLimit int

	DownloadingListLock sync.RWMutex
	DownloadingList     *arraylist.List

	IdleListLock sync.RWMutex
	IdleList     *arraylist.List

	//checkDownloadingState check state to decide is continue or stop
	checkDownloadingState func(task *Task) bool

	//handleBrokenTask how to handle if task broken
	handleBrokenTask func(task *Task)

	RunningCountControlChan chan bool
}

const (
	quickChannel       = "quickChannel"
	randomChannel      = "randomChannel"
	unpauseFastChannel = "unpauseFastChannel"
	unpauseSlowChannel = "unpauseSlowChannel"
)

func initChannel(speedLimitBPS float64, downloadingCountLimit int) *DownloadChannel {
	dc := &DownloadChannel{
		SpeedLimitBPS:           speedLimitBPS,
		DownloadingCountLimit:   downloadingCountLimit,
		RunningCountControlChan: make(chan bool, downloadingCountLimit),
		DownloadingList:         arraylist.New(),
		IdleList:                arraylist.New(),
	}
	for i := 0; i < dc.DownloadingCountLimit; i++ {
		dc.RunningCountControlChan <- true
	}
	return dc
}

func (dc *DownloadChannel) idleListSize() int {
	return dc.IdleList.Size()
}

func (dc *DownloadChannel) downloadingListSize() int {
	return dc.DownloadingList.Size()
}

func (dc *DownloadChannel) pushTaskToIdle(task ...interface{}) {
	dc.IdleListLock.Lock()
	defer dc.IdleListLock.Unlock()

	dc.IdleList.Add(task...)
}

func (dc *DownloadChannel) pushTaskToDownloading(task ...interface{}) {
	dc.DownloadingListLock.Lock()
	defer dc.DownloadingListLock.Unlock()

	dc.DownloadingList.Add(task...)
}

func (dc *DownloadChannel) popFistTaskFromIdle() *Task {
	dc.IdleListLock.Lock()
	defer dc.IdleListLock.Unlock()

	task, exist := dc.IdleList.Get(0)
	if !exist {
		return nil
	}

	dc.IdleList.Remove(0)
	return task.(*Task)
}

func (dc *DownloadChannel) popRandTaskFromIdle() *Task {
	dc.IdleListLock.Lock()
	defer dc.IdleListLock.Unlock()

	size := dc.IdleList.Size()
	if size <= 0 {
		return nil
	}
	index := rand.Intn(size)
	task, exist := dc.IdleList.Get(index)
	if !exist {
		return nil
	}

	dc.IdleList.Remove(index)
	return task.(*Task)
}

func (dc *DownloadChannel) run() {
	go func() {
		for true {
			//get token
			<-dc.RunningCountControlChan

			for {
				dc.IdleListLock.RLock()
				if dc.idleListSize() > 0 {
					dc.IdleListLock.RUnlock()
					break
				}
				time.Sleep(500 * time.Millisecond)
			}

			//get task form idleList
			task := dc.popFistTaskFromIdle()
			//smart-routine
			sr.New_Panic_Return(func() {
				defer func() {
					// token back
					dc.RunningCountControlChan <- true
				}()

				//start download task
				_ = task

			}).Start()

		}
	}()
}
