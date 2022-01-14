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

func (dm *DownloadMgr) saveHeader(filePath string, originHeader http.Header, backupFileName string) error {
	//todo add lock??? delete folder and create new file
	folder := filepath.Dir(filePath)
	err := os.MkdirAll(folder, 0777)
	if err != nil {
		return err
	}
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	isNeedDelete := false
	defer func() {
		if isNeedDelete {
			_ = os.Remove(filePath)
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
			if backupFileName != "" && k == "Content-Disposition" && !strings.Contains(v, "filename=") {
				v += fmt.Sprintf("; filename=\"%s\"", backupFileName)
			}
			_, err = write.WriteString(fmt.Sprintf("%s\n", v))
			if err != nil {
				isNeedDelete = true
				return err
			}
		}
	}
	_, ok := originHeader["Content-Disposition"]
	if backupFileName != "" && !ok {
		_, err = write.WriteString(fmt.Sprintf("Content-Disposition\n1\nfilename=\"%s\"", backupFileName))
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
