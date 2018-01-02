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
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"io/ioutil"

	"github.com/Jeffail/gabs"
)

// ModPack is a directory, manifest and other components that represent a pack
type ModPack struct {
	name     string
	rootPath string
	gamePath string
	modPath  string
	manifest *gabs.Container
}

func NewModPack(dir string, requireManifest bool, enableMultiMC bool) (*ModPack, error) {
	pack := new(ModPack)

	// Initialize path & name
	if dir == "." {
		pack.rootPath, _ = os.Getwd()
		pack.name = filepath.Base(dir)
	} else if filepath.IsAbs(dir) {
		pack.rootPath = dir
		pack.name = filepath.Base(dir)
	} else {
		pack.rootPath = filepath.Join(env().McdexDir, "pack", dir)
		pack.name = dir
	}

	if enableMultiMC == true {
		pack.gamePath = filepath.Join(pack.rootPath, "minecraft")
	} else {
		pack.gamePath = pack.rootPath
	}

	pack.modPath = filepath.Join(pack.gamePath, "mods")
	fmt.Printf("-- %s --\n", pack.gamePath)

	// Create the directories
	err := os.MkdirAll(pack.gamePath, 0700)
	if err != nil {
		return nil, fmt.Errorf("Failed to create %s: %+v", pack.gamePath, err)
	}

	err = os.MkdirAll(pack.modPath, 0700)
	if err != nil {
		return nil, fmt.Errorf("Failed to create %s: %+v", pack.modPath, err)
	}

	// Try to load the manifest; only raise an error if we require it to be loaded
	err = pack.loadManifest()
	if requireManifest && err != nil {
		return nil, err
	}

	return pack, nil
}

func (pack *ModPack) download(url string) error {
	// Check for a pack.url file; we use this to track where the pack
	// file came from so that we can re-download the pack when it changes.
	// This supports the use case of installing v 1.0.x of a pack and then updating
	// to 1.0.x+1 in the same directory
	packURLFile := filepath.Join(pack.gamePath, "pack.url")
	origURL, _ := readStringFile(packURLFile)
	origURL = strings.TrimSpace(origURL)

	packFilename := filepath.Join(pack.gamePath, "pack.zip")

	if origURL != url {
		// Remove pack.zip and all mod files
		os.Remove(packFilename)
		os.RemoveAll(pack.modPath)

	} else if fileExists(packFilename) {
		return nil
	}

	fmt.Printf("Starting download of modpack: %s\n", url)

	// For the moment, we only support modpacks from Curseforge and we must have the URL
	// end in /download; check and enforce these conditions
	if !strings.HasPrefix(url, "https://minecraft.curseforge.com/projects/") && !strings.HasPrefix(url, "https://www.feed-the-beast.com") {
		return fmt.Errorf("Invalid modpack URL; we only support Curseforge & feed-the-beast.com right now")
	}

	if !strings.HasSuffix(url, "/download") {
		url += "/download"
	}

	// Start the download
	resp, err := HttpGet(url)
	if err != nil {
		return fmt.Errorf("Failed to download %s: %+v", pack.name, err)
	}
	defer resp.Body.Close()

	// Store pack.zip in the working dir
	err = writeStream(packFilename, resp.Body)
	if err != nil {
		return err
	}

	// Note the URL from which we downloaded the pack
	return writeStringFile(packURLFile, url)
}

func (pack *ModPack) processManifest() error {
	// Open the pack.zip and parse the manifest
	zipFile, err := zip.OpenReader(filepath.Join(pack.gamePath, "pack.zip"))
	if err != nil {
		return fmt.Errorf("Failed to open pack.zip: %v", err)
	}
	defer zipFile.Close()

	// Find the manifest file and decode it
	pack.manifest, err = findJSONFile(zipFile, "manifest.json")
	if err != nil {
		return err
	}

	// Check the type and version of the manifest
	mvsn, ok := pack.manifest.Path("manifestVersion").Data().(float64)
	if !ok || mvsn != 1.0 {
		return fmt.Errorf("unexpected manifest version: %4.0f", mvsn)
	}

	mtype, ok := pack.manifest.Path("manifestType").Data().(string)
	if !ok || mtype != "minecraftModpack" {
		return fmt.Errorf("unexpected manifest type: %s", mtype)
	}

	return nil
}

func (pack *ModPack) minecraftVersion() string {
	return pack.manifest.Path("minecraft.version").Data().(string)
}

func (pack *ModPack) createManifest(name, minecraftVsn, forgeVsn string) error {
	// Create the manifest and set basic info
	pack.manifest = gabs.New()
	pack.manifest.SetP(minecraftVsn, "minecraft.version")
	pack.manifest.SetP("minecraftModpack", "manifestType")
	pack.manifest.SetP(1, "manifestVersion")
	pack.manifest.SetP(name, "name")

	loader := make(map[string]interface{})
	loader["id"] = "forge-" + forgeVsn
	loader["primary"] = true

	pack.manifest.ArrayOfSizeP(1, "minecraft.modLoaders")
	pack.manifest.Path("minecraft.modLoaders").SetIndex(loader, 0)

	// Write the manifest file
	err := pack.saveManifest()
	if err != nil {
		return fmt.Errorf("failed to save manifest.json: %+v", err)
	}

	return nil
}

func (pack *ModPack) getVersions() (string, string) {
	minecraftVsn := pack.manifest.Path("minecraft.version").Data().(string)
	forgeVsn := pack.manifest.Path("minecraft.modLoaders.id").Index(0).Data().(string)
	forgeVsn = strings.TrimPrefix(forgeVsn, "forge-")
	return minecraftVsn, forgeVsn
}

func (pack *ModPack) createLauncherProfile() error {
	// Using manifest config version + mod loader, look for an installed
	// version of forge with the appropriate version
	minecraftVsn, forgeVsn := pack.getVersions()

	var forgeID string
	var err error

	// Install forge if necessary
	forgeID, err = installClientForge(minecraftVsn, forgeVsn)
	if err != nil {
		return fmt.Errorf("failed to install Forge %s: %+v", forgeVsn, err)
	}

	// Finally, load the launcher_profiles.json and make a new entry
	// with appropriate name and reference to our pack directory and forge version
	lc, err := newLauncherConfig()
	if err != nil {
		return fmt.Errorf("failed to load launcher_profiles.json: %+v", err)
	}

	fmt.Printf("Creating profile: %s\n", pack.name)
	err = lc.createProfile(pack.name, forgeID, pack.gamePath)
	if err != nil {
		return fmt.Errorf("failed to create profile: %+v", err)
	}

	err = lc.save()
	if err != nil {
		return fmt.Errorf("failed to save profile: %+v", err)
	}

	return nil
}

func (pack *ModPack) installMods(isClient bool, ignoreFailedDownloads bool) error {
	// Make sure mods directory already exists
	os.MkdirAll(pack.modPath, 0700)

	// Using manifest, download each mod file into pack directory from Curseforge
	files, _ := pack.manifest.Path("files").Children()
	for _, f := range files {
		clientOnlyMod, ok := f.S("clientOnly").Data().(bool)
		if ok && clientOnlyMod && !isClient {
			fmt.Printf("Skipping client-only mod %s\n", f.Path("desc").Data().(string))
			continue
		}

		// If we have an entry with the filename, check to see if it exists;
		// bail if so
		baseFilename := f.Path("filename").Data()
		if baseFilename != nil && baseFilename != "" {
			filename := filepath.Join(pack.modPath, baseFilename.(string))
			if fileExists(filename) {
				fmt.Printf("Skipping %s\n", baseFilename.(string))
				continue
			}
		}

		projectID := int(f.Path("projectID").Data().(float64))
		fileID := int(f.Path("fileID").Data().(float64))
		filename, err := pack.installMod(projectID, fileID)
		if err != nil {
			if ignoreFailedDownloads {
				fmt.Printf("Ignoring failed download: %+v\n", err)
			} else {
				return err
			}
		}

		f.Set(filename, "filename")

		err = pack.saveManifest()
		if err != nil {
			return err
		}
	}

	// Also process any extfiles entries
	extFiles, _ := pack.manifest.S("extfiles").ChildrenMap()
	for _, url := range extFiles {
		_, err := pack.installModURL(url.Data().(string))
		if err != nil {
			if ignoreFailedDownloads {
				fmt.Printf("Ignoring failed download: %+v\n", err)
			} else {
				return err
			}
		}
	}

	return nil
}

func (pack *ModPack) selectModFile(modFile *ModFile, clientOnly bool) error {
	// Make sure files entry exists in manifest
	if !pack.manifest.Exists("files") {
		pack.manifest.ArrayOfSizeP(0, "files")
	}

	// Add project & file IDs to manifest
	modInfo := make(map[string]interface{})
	modInfo["projectID"] = modFile.modID
	modInfo["fileID"] = modFile.fileID
	modInfo["required"] = true
	modInfo["desc"] = modFile.modName

	if clientOnly {
		modInfo["clientOnly"] = true
	}

	// Walk through the list of files; if we find one with same project ID, delete it
	existingIndex := -1
	files, _ := pack.manifest.S("files").Children()
	for i, child := range files {
		childProjectID := int(child.S("projectID").Data().(float64))
		if childProjectID == modFile.modID {
			// Found a matching project ID; note the index so we can replace it
			existingIndex = i

			// Also, delete any mod files listed by name
			filename, ok := child.S("filename").Data().(string)
			filename = filepath.Join(pack.modPath, filename)
			if ok && fileExists(filename) {
				// Try to remove the file; don't worry about error case
				os.Remove(filename)
			}

			break
		}
	}

	if existingIndex > -1 {
		pack.manifest.S("files").SetIndex(modInfo, existingIndex)
	} else {
		pack.manifest.ArrayAppendP(modInfo, "files")
	}

	fmt.Printf("Registered %s (clientOnly=%t)\n", modFile.modName, clientOnly)
	return pack.saveManifest()
}

func (pack *ModPack) selectModURL(url, name string, clientOnly bool) error {
	if name == "" {
		return fmt.Errorf("No tag provided for %s: ", url)
	}
	// Insert the url by name into extfiles map
	pack.manifest.Set(url, "extfiles", name)
	fmt.Printf("Registered %s as %s (clientOnly=%t)\n", url, name, clientOnly)
	return pack.saveManifest()
}

func (pack *ModPack) updateMods(db *Database) error {
	// Walk over each file, looking for a more recent file ID for the
	// appropriate version
	files, _ := pack.manifest.S("files").Children()
	for _, child := range files {
		isLocked := child.Exists("locked") && child.S("locked").Data().(bool)
		modID := int(child.S("projectID").Data().(float64))
		fileID := int(child.S("fileID").Data().(float64))
		latestFile, err := db.getLatestModFile(modID, pack.minecraftVersion())
		if err == nil && latestFile.fileID > fileID {
			// Skip locked mods that have an update available
			if isLocked {
				fmt.Printf("Skipping %s (locked)\n", latestFile.modName)
				continue
			}

			// Save the more recent file ID
			child.Set(latestFile.fileID, "fileID")
			child.Set(latestFile.modName, "desc")
			fmt.Printf("Updating %s: %d -> %d\n", latestFile.modName, fileID, latestFile.fileID)

			// Delete the old file if it exists
			filename, ok := child.S("filename").Data().(string)
			filename = filepath.Join(pack.modPath, filename)
			if ok && fileExists(filename) {
				// Try to remove the file; don't worry about error case
				os.Remove(filename)
			}
			child.Delete("filename")
		}
	}

	return pack.saveManifest()
}

func (pack *ModPack) saveManifest() error {
	// Write the manifest file
	err := writeJSON(pack.manifest, filepath.Join(pack.gamePath, "manifest.json"))
	if err != nil {
		return fmt.Errorf("failed to save manifest.json: %+v", err)
	}
	return nil
}

func (pack *ModPack) loadManifest() error {
	// Load the manifest
	manifest, err := gabs.ParseJSONFile(filepath.Join(pack.gamePath, "manifest.json"))
	if err != nil {
		return fmt.Errorf("Failed to load manifest from %s: %+v", pack.gamePath, err)
	}
	pack.manifest = manifest
	return nil
}

func (pack *ModPack) installMod(projectID, fileID int) (string, error) {
	// First, resolve the project ID
	baseURL, err := getRedirectURL(fmt.Sprintf("https://minecraft.curseforge.com/projects/%d?cookieTest=1", projectID))
	if err != nil {
		return "", fmt.Errorf("failed to resolve project %d: %+v", projectID, err)
	}

	// Append the file ID to the baseURL
	finalURL := fmt.Sprintf("%s/files/%d/download", baseURL, fileID)
	return pack.installModURL(finalURL)
}

func (pack *ModPack) installModURL(url string) (string, error) {
	// Start the download
	resp, err := HttpGet(url)
	if err != nil {
		return "", fmt.Errorf("Failed to download %s: %+v", url, err)
	}
	defer resp.Body.Close()

	// If we didn't get back a 200, bail
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to download %s status %d", url, resp.StatusCode)
	}

	// Extract the filename from the actual request (after following all redirects)
	filename := filepath.Base(resp.Request.URL.Path)

	// Cleanup the filename
	filename = strings.Replace(filename, " r", "-", -1)
	filename = strings.Replace(filename, " ", "-", -1)
	filename = strings.Replace(filename, "+", "-", -1)
	filename = strings.Replace(filename, "(", "-", -1)
	filename = strings.Replace(filename, ")", "", -1)
	filename = strings.Replace(filename, "'", "", -1)
	filename = filepath.Join(pack.modPath, filename)

	if fileExists(filename) {
		fmt.Printf("Skipping %s\n", filepath.Base(filename))
		return filepath.Base(filename), nil
	}

	// Save the stream of the response to the file
	fmt.Printf("Downloading %s\n", filepath.Base(filename))

	err = writeStream(filename, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to write %s: %+v", filename, err)
	}
	return filepath.Base(filename), nil
}

func (pack *ModPack) installOverrides() error {
	// Open the pack.zip
	zipFile, err := zip.OpenReader(filepath.Join(pack.gamePath, "pack.zip"))
	if err != nil {
		return fmt.Errorf("Failed to open pack.zip: %v", err)
	}
	defer zipFile.Close()

	fmt.Printf("Installing files from modpack archive\n")

	// Walk over every file in the pack that is prefixed with installOverrides
	// and write it out
	for _, f := range zipFile.File {
		if !strings.HasPrefix(f.Name, "overrides/") {
			continue
		}

		filename := filepath.Join(pack.gamePath, strings.Replace(f.Name, "overrides/", "", -1))

		// Make sure the directory for the file exists
		os.MkdirAll(filepath.Dir(filename), 0700)

		if f.FileInfo().IsDir() {
			continue
		}

		freader, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open %s: %+v", f.Name, err)
		}

		err = writeStream(filename, freader)
		if err != nil {
			return fmt.Errorf("failed to save: %+v", err)
		}
	}

	return nil
}

func (pack *ModPack) installServer() error {
	// Get the minecraft + forge versions from manifest
	minecraftVsn := pack.manifest.Path("minecraft.version").Data().(string)
	forgeVsn := pack.manifest.Path("minecraft.modLoaders.id").Index(0).Data().(string)
	forgeVsn = strings.TrimPrefix(forgeVsn, "forge-")

	// Extract the version from manifest and setup a URL
	filename := fmt.Sprintf("minecraft_server.%s.jar", minecraftVsn)
	serverURL := fmt.Sprintf("https://s3.amazonaws.com/Minecraft.Download/versions/%s/%s", minecraftVsn, filename)
	absFilename := filepath.Join(pack.gamePath, filename)

	// Only install if file isn't already present
	if !fileExists(absFilename) {
		// Download the file into root of the pack directory
		resp, err := HttpGet(serverURL)
		if err != nil {
			return fmt.Errorf("failed to download server for %s: %+v", minecraftVsn, err)
		}
		defer resp.Body.Close()

		// If we didn't get back a 200, bail
		if resp.StatusCode != 200 {
			return fmt.Errorf("failed to download server %s status %d from %s", minecraftVsn, resp.StatusCode, serverURL)
		}

		// Save the stream of the response to the file
		fmt.Printf("Downloading %s\n", filename)
		err = writeStream(absFilename, resp.Body)
		if err != nil {
			return fmt.Errorf("failed to write %s: %+v", filename, err)
		}
	}

	_, err := installServerForge(minecraftVsn, forgeVsn, pack.gamePath)
	if err != nil {
		return fmt.Errorf("failed to install forge: %+v", err)
	}

	return nil
}

const MMC_CONFIG = `InstanceType=OneSix
IntendedVersion=%s
ForgeVersion=%s
LogPrePostOutput=true
OverrideCommands=false
OverrideConsole=false
OverrideJavaArgs=false
OverrideJavaLocation=false
OverrideMemory=false
OverrideWindow=false
iconKey=default
lastLaunchTime=0
name=%s - %s
totalTimePlayed=0`

func (pack *ModPack) generateMMCConfig() error {
	name := pack.manifest.S("name").Data().(string)
	version := pack.manifest.S("version").Data().(string)

	// Generate the instance config string
	minecraftVsn, forgeVsn := pack.getVersions()
	cfg := fmt.Sprintf(MMC_CONFIG, minecraftVsn, forgeVsn, name, version)

	fmt.Printf("Generating instance.cfg for MultiMC\n")

	// Write it out
	err := ioutil.WriteFile(filepath.Join(pack.rootPath, "instance.cfg"), []byte(cfg), 0644)
	if err != nil {
		return fmt.Errorf("failed to save instance.cfg: %+v", err)
	}

	return nil
}
