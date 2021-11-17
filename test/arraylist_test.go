package test

import (
	"fmt"
	"github.com/daqnext/downloadmgr/grab"
	"github.com/emirpasic/gods/lists/arraylist"
	"log"
	"os"
	"testing"
	"time"
)

func Test_arrayList(t *testing.T) {
	list := arraylist.New()
	list.Add(0, 1, 2, 3, 4, 5, 6, 7, 9, 10)

	it := list.Iterator()
	insertPos := -1
	for it.End(); it.Prev(); {
		index, value := it.Index(), it.Value()
		t := value.(int)
		if t < (12) {
			insertPos = index
			break
		}
	}

	list.Insert(insertPos+1, 8)

	it = list.Iterator()
	for it.Next() {
		_, value := it.Index(), it.Value()
		log.Println(value)
	}
}

func Test_grab(tt *testing.T) {
	client := grab.NewClient()
	req, err := grab.NewRequest(".", "file:///Users/zhangzhenbo/workspace/go/project/downloadmgr/README.md")
	fmt.Println(err)
	// start download
	fmt.Printf("Downloading %v...\n", req.URL())
	resp := client.Do(req)
	fmt.Printf("  %v\n", resp.HTTPResponse.Status)

	// start UI loop
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()

Loop:
	for {
		select {
		case <-t.C:
			fmt.Printf("  transferred %v / %v bytes (%.2f%%)\n",
				resp.BytesComplete(),
				resp.Size(),
				100*resp.Progress())

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}

	// check for errors
	if err := resp.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Download saved to ./%v \n", resp.Filename)
}
