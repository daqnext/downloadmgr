package downloadmgr

import (
	"log"
	"math"
	"sync"
	"time"
)

type DownloadMgr struct {
	globalDownloadTaskChan chan *Task
	currentId              uint64
	idLock                 sync.Mutex
	downloadChannel        map[string]*DownloadChannel
	ignoreHeaderMap        sync.Map
}

//NewDownloadMgr new instance of Download manager
func NewDownloadMgr() *DownloadMgr {
	dm := &DownloadMgr{
		globalDownloadTaskChan: make(chan *Task, 1024*10),
		currentId:              0,
	}
	dm.genDownloadChannel()
	return dm
}

//init download channel
func (dm *DownloadMgr) genDownloadChannel() {
	dm.downloadChannel = map[string]*DownloadChannel{}
	//quickChannel
	qc := initChannel(1, 6)
	qc.checkDownloadingState = func(task *Task) bool {
		if task.response.Duration().Seconds() > 15 && task.response.BytesPerSecond() < 1 {
			//speed is too low
			//task fail,delete
		}

		if time.Now().Unix() > task.expireTime {
			//task expire
			//task fail, delete
		}

		return true
	}
	qc.handleBrokenTask = func(task *Task) {

	}
	dm.downloadChannel[quickChannel] = qc

	//randomChannel
	rc := initChannel(1, 6)
	rc.checkDownloadingState = func(task *Task) bool {
		if task.response.Duration().Seconds() > 15 && task.response.BytesPerSecond() < 1 {
			//speed is too low

		}

		return true
	}
	rc.handleBrokenTask = func(task *Task) {

	}
	dm.downloadChannel[randomChannel] = rc

	//unpauseFastChannel
	ufc := initChannel(1, 6)
	ufc.checkDownloadingState = func(task *Task) bool {

		return true
	}
	ufc.handleBrokenTask = func(task *Task) {

	}
	dm.downloadChannel[unpauseFastChannel] = ufc

	//unpauseSlowChannel
	usc := initChannel(1, 6)
	usc.checkDownloadingState = func(task *Task) bool {

		return true
	}
	usc.handleBrokenTask = func(task *Task) {

	}
	dm.downloadChannel[unpauseSlowChannel] = usc

}

func (dm *DownloadMgr) addDownloadTask(savePath string, originUrl []string, taskType TaskType, expireTime int64) uint64 {
	dm.idLock.Lock()
	if dm.currentId >= math.MaxUint64 {
		dm.currentId = 0
	}
	dm.currentId++
	dm.idLock.Unlock()

	task := &Task{
		id:             dm.currentId,
		savePath:       savePath,
		originUrlArray: originUrl,
		taskType:       taskType,
		expireTime:     expireTime,
		status:         New,
	}

	dm.globalDownloadTaskChan <- task
	return task.id
}

//classifyNewTask loop classify task to different channel
func (dm *DownloadMgr) classifyNewTask() {
	go func() {
		for {
			select {
			case task := <-dm.globalDownloadTaskChan:
				log.Println(task)
				//classify task
				err := dm.classify(task)
				if err != nil {
					//log error
				}
			}
		}
	}()
}

//classify check header and distribute to different channel
func (dm *DownloadMgr) classify(task *Task) error {
	task.status = Idle
	//if quickTask, push into quickChannel
	if task.taskType == quickTask {

		dm.downloadChannel[quickChannel].pushTaskToIdle(task)
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
		dm.downloadChannel[randomChannel].pushTaskToIdle(task)
	} else {
		dm.downloadChannel[unpauseFastChannel].pushTaskToIdle(task)
	}
	return nil
}
