package downloadmgr

import (
	"bytes"
	"context"
	"fmt"
	"github.com/daqnext/downloadmgr/grab"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

// check is url support range get or not, and get origin header at same time
func PreHandleOrigin(targetUrl string) (http.Header, bool, error) {
	var req http.Request
	var err error
	req.Method = "GET"
	req.Close = true
	req.URL, err = url.Parse(targetUrl)
	if err != nil {
		return nil, false, err
	}
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	req.WithContext(ctx)
	header := http.Header{}
	header.Set("Range", "bytes=0-0")
	req.Header = header
	resp, err := http.DefaultClient.Do(&req)
	if err != nil {
		return nil, false, err
	}
	if resp.Body == nil {
		return resp.Header, false, nil
	}
	defer resp.Body.Close()
	result, exist := resp.Header["Accept-Ranges"]
	if exist {
		for _, v := range result {
			if v == "bytes" {
				log.Println("support request range")
				return resp.Header, true, nil
			}
		}
	}

	buf := &bytes.Buffer{}
	written, err := io.CopyN(buf, resp.Body, 2)
	if err == io.EOF && written == 1 {
		return resp.Header, true, nil
	}
	if err != nil {
		return resp.Header, false, nil
	}
	return resp.Header, false, nil
}

func TimeoutDialer(cTimeout time.Duration, rwTimeout time.Duration) func(ctx context.Context, net, addr string) (c net.Conn, err error) {
	return func(ctx context.Context, netw, addr string) (net.Conn, error) {
		conn, err := net.DialTimeout(netw, addr, cTimeout)
		if err != nil {
			return nil, err
		}
		if rwTimeout > 0 {
			conn.SetDeadline(time.Now().Add(rwTimeout))
		}
		return conn, nil
	}
}

func Download(savePath string, originPath string, transferTimeoutSec int) {
	tempSaveFile := savePath + ".download"
	client := grab.NewClient()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connectTimeout := 5 * time.Second
	readWriteTimeout := time.Duration(transferTimeoutSec) * time.Second
	c := &http.Client{Transport: &http.Transport{
		Proxy:       http.ProxyFromEnvironment, //use system proxy
		DialContext: TimeoutDialer(connectTimeout, readWriteTimeout),
	}}
	client.HTTPClient = c
	req, err := grab.NewRequest(tempSaveFile, originPath)
	req.WithContext(ctx)
	if err != nil {
		log.Panicln(err)
	}

	// start download
	log.Printf("Downloading %v...\n", req.URL())
	resp := client.Do(req)
	//log.Println(resp)
	if resp == nil {
		log.Println("download fail", "err:", err)
		return
	}

	// start UI loop
	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()

	zeroSpeedCount := 0
Loop:
	for {
		select {
		case <-ticker.C:
			fmt.Printf("  transferred %v / %v bytes (%.2f%%)\n",
				resp.BytesComplete(),
				resp.Size(),
				100*resp.Progress())
			//log.Println(resp.BytesPerSecond()/1000000,"MB/s")
			speed := resp.BytesPerSecond()
			if speed == 0 {
				zeroSpeedCount++
				// if zeroSpeed keep 10 seconds,cancel this download
				if zeroSpeedCount >= 10 {
					cancel()
				}
			} else {
				zeroSpeedCount = 0
			}

			// keep zero speed for 10 sec
			if zeroSpeedCount >= 10 {
				log.Println("speed is zero")
			}

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}

	if err := resp.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		//
		//os.Exit(1)
		return
	}

	log.Println("transferSize:", resp.BytesComplete())
	log.Println(resp.BytesPerSecond())
	info, _ := os.Stat(tempSaveFile)
	log.Println("fileSize:", info.Size())
	fmt.Printf("Download saved to %v \n", resp.Filename)

	//download success
	os.Rename(resp.Filename, savePath)

	// Output:
	// Downloading http://www.golang-book.com/public/pdf/gobook.pdf...
	//   200 OK
	//   transferred 42970 / 2893557 bytes (1.49%)
	//   transferred 1207474 / 2893557 bytes (41.73%)
	//   transferred 2758210 / 2893557 bytes (95.32%)
	// Download saved to ./gobook.pdf
}
