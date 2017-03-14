package main

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
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

func MinecraftDir() string {
	user, _ := user.Current()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(user.HomeDir, "Library", "Application Support", "minecraft")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), ".minecraft")
	default:
		return filepath.Join(user.HomeDir, ".minecraft")
	}
}

func McdexDir() string {
	return filepath.Join(MinecraftDir(), "mcdex")
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
