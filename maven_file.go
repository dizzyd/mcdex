package main

import (
	"fmt"

	"github.com/Jeffail/gabs"
)

type MavenModFile struct {
}

func SelectMavenModFile(pack *ModPack, mod string, url string, clientOnly bool) error {
	return fmt.Errorf("not implemented")
}

func NewMavenModFile(modJson *gabs.Container) *MavenModFile {
	//artifactID := modJson.Path("artifactID").Data().(string)
	//url := modJson.Path("url").Data().(string)
	return &MavenModFile{}
}

func (f MavenModFile) install(pack *ModPack) error {
	return nil
}

func (f *MavenModFile) update(pack *ModPack) (bool, error) {
	return false, nil
}

func (f MavenModFile) getName() string {
	return ""
}

func (f MavenModFile) isClientOnly() bool {
	return false
}

func (f MavenModFile) equalsJson(modJson *gabs.Container) bool {
	return false
}

func (f MavenModFile) toJson() map[string]interface{} {
	return map[string]interface{}{}
}
