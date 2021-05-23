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

package main

import (
	"fmt"
	"math"

	"github.com/Jeffail/gabs"
)

type CurseForgeModFile struct {
	projectID  int
	fileID     int
	desc       string
	name       string
	clientOnly bool
}

func SelectCurseForgeModFile(pack *ModPack, mod string, url string, clientOnly bool) error {
	// Try to find the project ID using the mod name as a slug
	projectID, err := pack.db.findModBySlug(mod)
	if err != nil {
		return fmt.Errorf("unknown mod %s", mod)
	}

	// Look up the slug, name and description
	_, name, desc, err := pack.db.getProjectInfo(projectID)
	if err != nil {
		return fmt.Errorf("no name/description available for %s (%d): %+v", mod, projectID, err)
	}

	// Setup a mod file entry and then pull the latest file info
	modFile := CurseForgeModFile{projectID: projectID, desc: desc, name: name, clientOnly: clientOnly}
	fileId, err := modFile.getLatestFile(pack.minecraftVersion())
	if err != nil {
		return fmt.Errorf("failed to get latest file for %s (%d): %+v", mod, projectID, err)
	}

	// If we found a newer file, update entry and then the pack
	if fileId > modFile.fileID {
		modFile.fileID = fileId
		err = pack.selectMod(&modFile)
		if err != nil {
			return err
		}
	}

	return nil
}

func NewCurseForgeModFile(modJson *gabs.Container) *CurseForgeModFile {
	projectID, _ := intValue(modJson, "projectID")
	fileID, _ := intValue(modJson, "fileID")
	name, ok := modJson.Path("desc").Data().(string)
	if !ok {
		name = fmt.Sprintf("Curseforge project %d: %d", projectID, fileID)
	}
	clientOnly, ok := modJson.S("clientOnly").Data().(bool)
	return &CurseForgeModFile{projectID, fileID, name, name, ok && clientOnly}
}

func (f CurseForgeModFile) install(pack *ModPack) error {
	// Check the mod cache to see if we already have the right file ID installed
	lastFileId, lastFilename := pack.modCache.GetLastModFile(f.projectID)
	if lastFileId == f.fileID {
		// Nothing to do; we can skip this installed file
		fmt.Printf("Skipping %s\n", lastFilename)
		return nil
	} else if lastFileId > 0 {
		// A different version of the file is installed; clean it up
		pack.modCache.CleanupModFile(f.projectID)
	}

	// Now, retrieve the JSON descriptor for this file so we can get the CDN url
	descriptorUrl := fmt.Sprintf("https://addons-ecs.forgesvc.net/api/v2/addon/%d/file/%d", f.projectID, f.fileID)
	descriptor, err := getJSONFromURL(descriptorUrl)
	if err != nil {
		// Resolve the project ID into a slug
		slug, err2 := pack.db.findSlugByProject(f.projectID)
		if err2 != nil {
			return fmt.Errorf("failed to find slug and download url for project %d: %+v\n%+v", f.projectID, err, err2)
		}
		return fmt.Errorf("failed to retrieve descriptor for %s: %+v", slug, err)
	}

	// Download the file to the pack mod directory
	finalUrl := descriptor.Path("downloadUrl").Data().(string)

	filename, err := downloadHttpFileToDir(finalUrl, pack.modPath(), true)
	if err != nil {
		return err
	}

	// Download succeeded; register this mod as installed in the cache
	pack.modCache.AddModFile(f.projectID, f.fileID, filename)
	return nil
}

func (f *CurseForgeModFile) update(pack *ModPack) (bool, error) {
	latestFile, err := f.getLatestFile(pack.minecraftVersion())
	if err != nil {
		return false, err
	}

	if latestFile > f.fileID {
		f.fileID = latestFile
		return true, nil
	}

	return false, nil
}

func (f CurseForgeModFile) getName() string {
	return f.name
}

func (f CurseForgeModFile) isClientOnly() bool {
	return f.clientOnly
}

func (f CurseForgeModFile) equalsJson(modJson *gabs.Container) bool {
	projectID, ok := modJson.Path("projectID").Data().(float64)
	return ok && int(projectID) == f.projectID
}

func (f CurseForgeModFile) toJson() map[string]interface{} {
	result := map[string]interface{}{
		"projectID": f.projectID,
		"fileID":    f.fileID,
		"required":  true,
		"desc":      f.name,
	}
	if f.clientOnly {
		result["clientOnly"] = true
	}
	return result
}

func (f CurseForgeModFile) getLatestFile(minecraftVersion string) (int, error) {
	// Pull the project's descriptor, which has a list of the latest files for each version of Minecraft
	projectUrl := fmt.Sprintf("https://addons-ecs.forgesvc.net/api/v2/addon/%d", f.projectID)
	project, err := getJSONFromURL(projectUrl)
	if err != nil {
		return -1, fmt.Errorf("failed to retrieve project for %s: %+v", f.name, err)
	}

	selectedFileType := math.MaxInt8
	selectedFileId := 0

	// Look for the file with the matching version
	files, _ := project.Path("gameVersionLatestFiles").Children()
	for _, file := range files {
		fileType, _ := intValue(file, "fileType") // 1 = release, 2 = beta, 3 = alpha
		fileId, _ := intValue(file, "projectFileId")
		targetVsn := file.Path("gameVersion").Data().(string)

		if targetVsn != minecraftVersion {
			continue
		}

		// Matched on version; prefer releases over beta/alpha
		if fileType < selectedFileType {
			selectedFileType = fileType
			selectedFileId = fileId
		}
	}

	if selectedFileId == 0 {
		return -1, fmt.Errorf("no version found for Minecraft %s\n", minecraftVersion)
	}

	// TODO: Pull file descriptor and check for deps
	return selectedFileId, nil
}
