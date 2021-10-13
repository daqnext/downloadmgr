package downloadmgr

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"path"
	"sync"
)

var ignoreHeaderMap sync.Map

func SetIgnoreHeader(ignores []string) {
	ignoreHeaderMap.Range(func(key, value interface{}) bool {
		ignoreHeaderMap.Delete(key)
		return true
	})
	for _, v := range ignores {
		ignoreHeaderMap.Store(v, struct{}{})
	}
}

func SaveHeader(filePath string, originHeader http.Header) error {
	folder := path.Dir(filePath)
	err := os.MkdirAll(folder, 0666)
	if err != nil {
		return err
	}
	headerPath := filePath + ".header"
	file, err := os.Create(headerPath)
	if err != nil {
		return err
	}
	isNeedDelete := false
	defer func() {
		if isNeedDelete {
			os.Remove(headerPath)
		}
	}()
	defer file.Close()

	write := bufio.NewWriter(file)

	for k, va := range originHeader {
		_, exist := ignoreHeaderMap.Load(k)
		if exist {
			continue
		}
		_, err = write.WriteString(fmt.Sprintf("%s\n", k))
		if err != nil {
			isNeedDelete = true
			return err
		}
		_, err = write.WriteString(fmt.Sprintf("%d\n", len(va)))
		if err != nil {
			isNeedDelete = true
			return err
		}
		for _, v := range va {
			_, err = write.WriteString(fmt.Sprintf("%s\n", v))
			if err != nil {
				isNeedDelete = true
				return err
			}
		}
	}
	//Flush
	err = write.Flush()
	if err != nil {
		isNeedDelete = true
		return err
	}
	return nil
}
