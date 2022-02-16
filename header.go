package downloadmgr

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (dm *DownloadMgr) SetIgnoreHeader(ignores []string) {
	tempMap := map[string]struct{}{}
	for _, v := range ignores {
		tempMap[v] = struct{}{}
	}

	dm.ignoreHeaderMapLock.Lock()
	defer dm.ignoreHeaderMapLock.Unlock()

	dm.ignoreHeaderMap = tempMap
}

func (dm *DownloadMgr) genHeaderFile(headerFilePath string) (*os.File, error) {
	folder := filepath.Dir(headerFilePath)
	err := os.MkdirAll(folder, 0777)
	if err != nil {
		return nil, err
	}
	file, err := os.Create(headerFilePath)
	if err != nil {
		return nil, err
	}
	return file, nil
}

//saveHeader save response header to disk. Header file name will be fileHashName.header
func (dm *DownloadMgr) saveHeader(headerFilePath string, originHeader http.Header, originFileName string) error {
	file, err := dm.genHeaderFile(headerFilePath)
	if err != nil {
		return err
	}

	isNeedDelete := false
	defer func() {
		if isNeedDelete {
			os.Remove(headerFilePath)
		}
	}()
	defer file.Close()

	write := bufio.NewWriter(file)

	dm.ignoreHeaderMapLock.RLock()
	defer dm.ignoreHeaderMapLock.RUnlock()
	for k, va := range originHeader {
		_, exist := dm.ignoreHeaderMap[k]
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
			if originFileName != "" && k == "Content-Disposition" && !strings.Contains(v, "filename=") {
				v += fmt.Sprintf("; filename=\"%s\"", originFileName)
			}
			_, err = write.WriteString(fmt.Sprintf("%s\n", v))
			if err != nil {
				isNeedDelete = true
				return err
			}
		}
	}
	_, ok := originHeader["Content-Disposition"]
	if originFileName != "" && !ok {
		_, err = write.WriteString(fmt.Sprintf("Content-Disposition\n1\nfilename=\"%s\"", originFileName))
		if err != nil {
			isNeedDelete = true
			return err
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
