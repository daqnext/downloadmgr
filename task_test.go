package downloadmgr

import (
	"fmt"
	"github.com/daqnext/go-smart-routine/sr"
	"log"
	"testing"
	"time"
)

func d(a, b int) int {
	return a / b
}

func Test_smartGoRoutine(t *testing.T) {
	count := 0
	for true {

		c := count
		//smart-routine
		sr.New_Panic_Return(func() {
			defer func() {
				// token back
				log.Println(c, "ccc")
			}()
			d(1, 10-c)
			log.Println(c)

		}).Start()

		count++
		if sr.PanicExist {
			fmt.Println(sr.PanicJson.GetContentAsString())
			sr.ClearPanics()
		}
		time.Sleep(500 * time.Millisecond)
	}
}
