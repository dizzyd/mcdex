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
	projectID, err := pack.db.findModBySlug(mod, pack.modLoader)
	if err != nil {
		return fmt.Errorf("unknown mod %s: %+v", mod, err)
	}

	// Look up the slug, name and description
	_, name, desc, err := pack.db.getProjectInfo(projectID)
	if err != nil {
		return fmt.Errorf("no name/description available for %s (%d): %+v", mod, projectID, err)
	}

	// Setup a mod file entry and then pull the latest file info
	modFile := CurseForgeModFile{projectID: projectID, desc: desc, name: name, clientOnly: clientOnly}
	fileId, err := modFile.getLatestFile(pack.minecraftVersion(), pack.modLoader)
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

	// Resolve the project ID into a slug
	slug, err := pack.db.findSlugByProject(f.projectID)
	if err != nil {
		return fmt.Errorf("failed to find slug for project %d: %+v", f.projectID, err)
	}

	// Now, retrieve the JSON descriptor for this file so we can get the CDN url
	descriptorUrl := fmt.Sprintf("https://addons-ecs.forgesvc.net/api/v2/addon/%d/file/%d", f.projectID, f.fileID)
	descriptor, err := getJSONFromURL(descriptorUrl)
	if err != nil {
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
	latestFile, err := f.getLatestFile(pack.minecraftVersion(), pack.modLoader)
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

func (f CurseForgeModFile) getLatestFile(minecraftVersion string, modLoader string) (int, error) {
	// Setup a retry counter to deal with long timeouts (a recent problem)
	retryCount := 3

	// Pull the project's descriptor, which has a list of the latest files for each version of Minecraft
	retry:
		projectUrl := fmt.Sprintf("https://addons-ecs.forgesvc.net/api/v2/addon/%d", f.projectID)
		project, err := getJSONFromURL(projectUrl)
		if err != nil {
			if retryCount > 0 {
				fmt.Printf("Retrying update check for %s (%s)\n", f.name, projectUrl)
				retryCount -= 1
				goto retry
			} else {
				return -1, fmt.Errorf("failed to retrieve project for %s: %+v", f.name, err)
			}
		}

	selectedFileType := math.MaxInt8
	selectedFileId := 0

	// Look for the file with the matching version
	files, _ := project.Path("gameVersionLatestFiles").Children()
	for _, file := range files {
		fileType, _ := intValue(file, "fileType") // 1 = release, 2 = beta, 3 = alpha
		fileId, _ := intValue(file, "projectFileId")
		modLoaderId, _ := intValue(file, "modLoader")
		targetVsn := file.Path("gameVersion").Data().(string)

		if targetVsn != minecraftVersion {
			continue
		}

		if modLoaderId == 1 && modLoader != "forge" {
			continue
		}

		if modLoaderId == 4 && modLoader != "fabric" {
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

func PrintCurseForgeModInfo(projectId int) error {
	// Pull the project's descriptor, which has a list of the latest files for each version of Minecraft
	projectUrl := fmt.Sprintf("https://addons-ecs.forgesvc.net/api/v2/addon/%d", projectId)
	project, err := getJSONFromURL(projectUrl)
	if err != nil {
		return fmt.Errorf("failed to retrieve project %d: %+v", projectId, err)
	}

	name, _ := strValue(project, "name")
	slug, _ := strValue(project, "slug")
	summary, _ := strValue(project, "summary")

	fmt.Printf("%s (%s)\n  %s\nFiles:\n", name, slug, summary)

	// List recent files
	files, _ := project.Path("gameVersionLatestFiles").Children()
	for _, file := range files {
		filename, _ := strValue(file, "projectFileName")
		fileType, _ := intValue(file, "fileType") // 1 = release, 2 = beta, 3 = alpha
		modLoaderId, _ := intValue(file, "modLoader") // 1 == forge, 4 == fabric
		targetVsn, _ := strValue(file, "gameVersion")

		var releaseType string
		switch fileType {
		case 1:
			releaseType = "release"
		case 2:
			releaseType = "beta"
		case 3:
			releaseType = "alpha"
		default:
			releaseType = "unknown-release"
		}

		var modLoader string
		switch modLoaderId {
		case 1:
			modLoader = "forge"
		case 4:
			modLoader = "fabric"
		default:
			modLoader = "forge"
		}

		fmt.Printf("* %s for Minecraft %s, %s, %s\n", filename, targetVsn, modLoader, releaseType)
	}

	return nil;
}
