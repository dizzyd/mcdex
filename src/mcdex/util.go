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

	"github.com/Jeffail/gabs"
	"github.com/viki-org/dnscache"
)

var resolver = dnscache.New(time.Minute * 15)

func NewHttpClient() http.Client {
	return http.Client{
		Transport: &http.Transport{
			Dial: func(network string, address string) (net.Conn, error) {
				separator := strings.LastIndex(address, ":")
				ip, _ := resolver.FetchOneString(address[:separator])
				return net.Dial("tcp", ip+address[separator:])
			},
		},
	}
}

func HttpGet(url string) (*http.Response, error) {
	client := NewHttpClient()
	return client.Get(url)
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
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create %s: %v", filename, err)
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	_, err = io.Copy(writer, data)
	if err != nil {
		return fmt.Errorf("failed to write %s: %v", filename, err)
	}
	writer.Flush()
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
