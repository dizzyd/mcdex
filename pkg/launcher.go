// ***************************************************************************
//
//  Copyright 2017 David (Dizzy) Smith, dizzyd@dizzyd.com
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
// ***************************************************************************

package pkg

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"

	"github.com/Jeffail/gabs"
)

var nameRegex = regexp.MustCompile("^[\\w][\\w-.]*$")

type launcherConfig struct {
	data      *gabs.Container
	filename  string
	nameRegex *regexp.Regexp
}

func newLauncherConfig() (*launcherConfig, error) {
	lc := new(launcherConfig)
	lc.filename = filepath.Join(Env().MinecraftDir, "launcher_profiles.json")
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

func (lc *launcherConfig) createProfile(name, version, gameDir, javaArgs string) error {
	if !nameRegex.MatchString(name) {
		return fmt.Errorf("invalid profile name: %s", name)
	}
	lc.data.Set(name, "profiles", name, "name")
	lc.data.Set(version, "profiles", name, "lastVersionId")
	lc.data.Set(gameDir, "profiles", name, "gameDir")
	if javaArgs != "" {
		lc.data.Set(javaArgs, "profiles", name, "javaArgs")
	}
	return nil
}

func (lc *launcherConfig) save() error {
	return writeJSON(lc.data, lc.filename)
}
