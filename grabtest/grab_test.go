package grabtest

import (
	"context"
	"fmt"
	"github.com/daqnext/downloadmgr/grab"
	"log"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

func TimeoutDialer(cTimeout time.Duration, rwTimeout time.Duration) func(ctx context.Context, net, addr string) (c net.Conn, err error) {
	return func(ctx context.Context, netw, addr string) (net.Conn, error) {
		conn, err := net.DialTimeout(netw, addr, cTimeout)
		if err != nil {
			return nil, err
		}
		conn.SetDeadline(time.Now().Add(rwTimeout))
		return conn, nil
	}
}

func Test_grab(t *testing.T) {

	//targetUrl := "https://golang.org/dl/go1.17.2.darwin-amd64.pkg"
	//saveFile := "./downloadFile/go1.17.2.darwin-amd64.pkg"

	targetUrl := "https://image.baidu.com/search/down?tn=download&word=download&ie=utf8&fr=detail&url=https%3A%2F%2Fgimg2.baidu.com%2Fimage_search%2Fsrc%3Dhttp%253A%252F%252Fadfa.org.au%252Fwp-content%252Fuploads%252F2016%252F08%252Fwhereisasbestos-1024x889.png%26refer%3Dhttp%253A%252F%252Fadfa.org.au%26app%3D2002%26size%3Df9999%2C10000%26q%3Da80%26n%3D0%26g%3D0n%26fmt%3Djpeg%3Fsec%3D1632884767%26t%3Dbf86afc7d333af922f306f47fe41a272&thumburl=https%3A%2F%2Fimg1.baidu.com%2Fit%2Fu%3D281490118%2C363118662%26fm%3D15%26fmt%3Dauto%26gp%3D0.jpg"
	saveFile := "./downloadFile/a.jpg"
	client := grab.NewClient()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connectTimeout := 5 * time.Second
	readWriteTimeout := 150 * time.Second
	c := &http.Client{Transport: &http.Transport{
		Proxy:       http.ProxyFromEnvironment,
		DialContext: TimeoutDialer(connectTimeout, readWriteTimeout),
	}}
	client.HTTPClient = c
	req, err := grab.NewRequest(saveFile+".download", targetUrl)
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

	// check for errors
	//if err := resp.Err(); err != nil {
	//	fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
	//	os.Exit(1)
	//}

	//log.Printf("  %v\n", resp.HTTPResponse.Status)

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
	info, _ := os.Stat(saveFile + ".download")
	log.Println("fileSize:", info.Size())
	fmt.Printf("Download saved to %v \n", resp.Filename)

	//download success
	os.Rename(resp.Filename, saveFile)

	// Output:
	// Downloading http://www.golang-book.com/public/pdf/gobook.pdf...
	//   200 OK
	//   transferred 42970 / 2893557 bytes (1.49%)
	//   transferred 1207474 / 2893557 bytes (41.73%)
	//   transferred 2758210 / 2893557 bytes (95.32%)
	// Download saved to ./gobook.pdf
}
