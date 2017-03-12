package main

import (
	"time"
	"strings"
	"net"
	"net/http"
	"runtime"
	"os"
	"os/user"
	"path/filepath"
	"github.com/viki-org/dnscache"
)

var resolver = dnscache.New(time.Minute * 15)

func NewHttpClient() http.Client {
	return http.Client {
		Transport: &http.Transport {
			Dial: func(network string, address string) (net.Conn, error) {
				separator := strings.LastIndex(address, ":")
				ip, _ := resolver.FetchOneString(address[:separator])
				return net.Dial("tcp", ip + address[separator:])
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
