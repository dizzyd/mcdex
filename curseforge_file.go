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

	// Parse out minecraft version so we can traverse
	major, minor, patch, err := parseVersion(pack.minecraftVersion())
	if err != nil {
		// Invalid version string
		return err
	}

	// Now walk the major.minor.[path] versions of Minecraft and find the latest file for our version
	for i := patch; i > -1; i-- {
		var vsn string
		if i > 0 {
			vsn = fmt.Sprintf("%d.%d.%d", major, minor, i)
		} else {
			vsn = fmt.Sprintf("%d.%d", major, minor)
		}

		modFile, err := pack.db.findModFile(projectID, vsn)
		if err != nil {
			// No mod found; keep walking versions
			continue
		}

		modFile.clientOnly = clientOnly

		// Found a valid mod; add it to the pack manifest
		err = pack.selectMod(modFile)
		if err != nil {
			return err
		}

		// Now scan all deps for the mod
		// TODO: Figure out a better way to avoid multiple hits to database per dep slug
		deps, err := pack.db.getDeps(modFile.fileID)
		if err != nil {
			return fmt.Errorf("error pulling deps for %s: %+v", modFile.name, err)
		}

		// Recursively add each dep to the pack
		for _, dep := range deps {
			err = SelectCurseForgeModFile(pack, dep, "", clientOnly)
			if err != nil {
				return err
			}
		}

		return nil
	}

	// Didn't find a file that matches :(
	return fmt.Errorf("no compatible file found for %s\n", mod)
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
	latestFile, err := pack.db.getLatestFileID(f.projectID, pack.minecraftVersion())
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
	return false
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
