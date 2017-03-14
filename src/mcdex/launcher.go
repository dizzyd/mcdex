package main

import (
	"fmt"
	"io/ioutil"
	"path"
	"regexp"

	"github.com/Jeffail/gabs"
)

var NameRegex = regexp.MustCompile("^\\w+$")

type LauncherConfig struct {
	data      *gabs.Container
	filename  string
	nameRegex *regexp.Regexp
}

func NewLauncherConfig() (*LauncherConfig, error) {
	lc := new(LauncherConfig)
	lc.filename = path.Join(MinecraftDir(), "launcher_profiles.json")

	rawdata, err := ioutil.ReadFile(lc.filename)
	if err != nil {
		return nil, err
	}

	lc.data, err = gabs.ParseJSON(rawdata)
	if err != nil {
		return nil, err
	}
	return lc, nil
}

func (lc *LauncherConfig) CreateProfile(name string, version string) error {
	if !NameRegex.MatchString(name) {
		return fmt.Errorf("Invalid profile name: %d", name)
	}
	path := "profiles." + name
	lc.data.SetP(name, path+".name")
	lc.data.SetP(version, path+".lastVersionId")
	lc.data.SetP(MinecraftDir()+"."+name, path+".gameDir")
	return nil
}

func (lc *LauncherConfig) Save() error {
	return ioutil.WriteFile(lc.filename, lc.data.Bytes(), 0644)
}
