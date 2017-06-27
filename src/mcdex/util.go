package main

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"net/url"

	"github.com/Jeffail/gabs"
	"github.com/viki-org/dnscache"
)

var resolver = dnscache.New(time.Minute * 15)
var getterClient = NewHttpClient(true)
var redirectClient = NewHttpClient(false)

func NewHttpClient(followRedirects bool) http.Client {
	t := &http.Transport{
		Dial: func(network string, address string) (net.Conn, error) {
			separator := strings.LastIndex(address, ":")
			ip, _ := resolver.FetchOneString(address[:separator])
			return net.Dial("tcp", ip+address[separator:])
		},
	}

	if !followRedirects {
		return http.Client{Transport: t, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return http.Client{Transport: t}

}

func HttpGet(url string) (*http.Response, error) {
	return getterClient.Get(url)
}

func getRedirectURL(url string) (*url.URL, error) {
	resp, err := redirectClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP error on %s: %+v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 301 || resp.StatusCode == 302 {
		u, err := resp.Location()
		if err != nil {
			return nil, fmt.Errorf("invalid location url: %+v", err)
		}
		return u, nil
	}

	return nil, nil
}

func findJSONFile(z *zip.ReadCloser, name string) (*gabs.Container, error) {
	for _, f := range z.File {
		if f.Name == name {
			freader, err := f.Open()
			if err != nil {
				return nil, err
			}

			json, err := gabs.ParseJSONBuffer(freader)
			if err != nil {
				return nil, fmt.Errorf("failed to parse JSON %s: %+v", name, err)
			}
			return json, nil
		}
	}

	return nil, fmt.Errorf("failed to find %s", name)
}

func zipEntryToJSON(name string, f *zip.File) (*gabs.Container, error) {
	if f == nil {
		return nil, fmt.Errorf("failed to find %s", name)
	}

	freader, err := f.Open()
	if err != nil {
		return nil, err
	}

	json, err := gabs.ParseJSONBuffer(freader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s JSON: %+v", name, err)
	}

	return json, nil
}

func writeStream(filename string, data io.Reader) error {
	// Construct a filename to hold the stream while writing; once the download is complete, we'll move it into place
	// and delete the temporary file. This ensures that partial/failed streams are properly detected.
	tempFilename := filename + ".part"

	// Create the temporary file
	f, err := os.Create(tempFilename)
	if err != nil {
		return fmt.Errorf("failed to create %s: %v", filename, err)
	}
	defer f.Close()

	// Stream the data into the temp file
	writer := bufio.NewWriter(f)
	_, err = io.Copy(writer, data)
	if err != nil {
		return fmt.Errorf("failed to write %s: %v", filename, err)
	}
	writer.Flush()
	f.Close()

	// Ok, write completed successfully, move the file
	err = os.Rename(tempFilename, filename)
	if err != nil {
		return fmt.Errorf("failed to rename %s: %+v", tempFilename, err)
	}

	return nil
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil || os.IsExist(err)
}

func dirExists(dirname string) bool {
	stat, err := os.Stat(dirname)
	return err == nil && stat.IsDir()
}
