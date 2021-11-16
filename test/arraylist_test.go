package test

import (
	"github.com/emirpasic/gods/lists/arraylist"
	"log"
	"testing"
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
