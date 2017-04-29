package main

import (
	"fmt"
	"io/ioutil"
	"path"
	"regexp"

	"github.com/Jeffail/gabs"
)

var NameRegex = regexp.MustCompile("^\\w+$")

type launcherConfig struct {
	data      *gabs.Container
	filename  string
	nameRegex *regexp.Regexp
}

func newLauncherConfig() (*launcherConfig, error) {
	lc := new(launcherConfig)
	lc.filename = path.Join(env().MinecraftDir, "launcher_profiles.json")
	lc.data = gabs.New()

	if fileExists(lc.filename) {
		rawdata, err := ioutil.ReadFile(lc.filename)
		if err != nil {
			return nil, err
		}

		lc.data, err = gabs.ParseJSON(rawdata)
		if err != nil {
			return nil, err
		}
	}
	return lc, nil
}

func (lc *launcherConfig) createProfile(name, version, gameDir string) error {
	if !NameRegex.MatchString(name) {
		return fmt.Errorf("invalid profile name: %s", name)
	}
	path := "profiles." + name
	lc.data.SetP(name, path+".name")
	lc.data.SetP(version, path+".lastVersionId")
	lc.data.SetP(gameDir, path+".gameDir")
	return nil
}

func (lc *launcherConfig) save() error {
	return ioutil.WriteFile(lc.filename, lc.data.Bytes(), 0644)
}
