package internal

import (
	"fmt"

	"github.com/Jeffail/gabs"
)

type MavenModFile struct {
	module     MavenModule
	url        string
	clientOnly bool
}

func SelectMavenModFile(pack *ModPack, mod string, url string, clientOnly bool) error {
	module, err := NewMavenModule(mod)
	if err != nil {
		return fmt.Errorf("invalid module %s: %+v", mod, err)
	}

	if url == "" {
		url = "http://files.mcdex.net/maven2"
	}

	// If no version is provided, load metadata
	if module.version == "" {
		metadata, err := module.loadMetadata(url)
		if err != nil {
			return fmt.Errorf("failed to load metadata for %s: %+v", mod, err)
		}

		module.version = metadata.VersionInfo.Release
	}

	return pack.selectMod(&MavenModFile{module, url, clientOnly})
}

func NewMavenModFile(modJson *gabs.Container) *MavenModFile {
	module, err := NewMavenModule(modJson.Path("module").Data().(string))
	if err != nil {
		return nil
	}
	url, ok := modJson.Path("url").Data().(string)
	if !ok {
		url = "https://files.mcdex.net/maven2"
	}
	clientOnly, ok := modJson.Path("clientOnly").Data().(bool)
	return &MavenModFile{module, url, ok && clientOnly}
}

func (f MavenModFile) install(pack *ModPack) error {
	// If no version is specified, bail
	if f.module.version == "" {
		return fmt.Errorf("no version specified for %s", f.module)
	}

	// Download it
	downloadUrl, _ := f.module.toRepositoryPath(f.url)
	_, err := downloadHttpFileToDir(downloadUrl, pack.modPath(), true)
	return err
}

func (f *MavenModFile) update(pack *ModPack) (bool, error) {
	fmt.Printf("%s is not eligible for update; not yet implemented", f.getName())
	return false, nil
}

func (f MavenModFile) getName() string {
	return f.module.String()
}

func (f MavenModFile) isClientOnly() bool {
	return f.clientOnly
}

func (f MavenModFile) equalsJson(modJson *gabs.Container) bool {
	moduleId, ok := modJson.Path("module").Data().(string)
	if !ok {
		return false
	}

	module, err := NewMavenModule(moduleId)
	if err != nil {
		return false
	}

	return f.module.groupId == module.groupId && f.module.artifactId == module.artifactId
}

func (f MavenModFile) toJson() map[string]interface{} {
	result := map[string]interface{}{
		"module": f.module.String(),
		"url":    f.url,
	}

	if f.clientOnly {
		result["clientOnly"] = true
	}

	return result
}
