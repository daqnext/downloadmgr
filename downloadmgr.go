package downloadmgr

type taskStatus string

type task struct {
	Id         uint64
	OriginUrl  []string
	SavePath   string
	RetryTimes int
	StartTime  int64
	Status     taskStatus
}
