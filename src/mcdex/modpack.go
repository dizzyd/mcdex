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
	cp := new(ModPack)

	// Initialize path & name
	if dir == "." {
		cp.rootPath, _ = os.Getwd()
		cp.name = filepath.Base(dir)
	} else if strings.HasPrefix(dir, "/") || strings.HasPrefix(dir, "C:") {
		cp.rootPath = dir
		cp.name = filepath.Base(dir)
	} else {
		cp.rootPath = filepath.Join(env().McdexDir, "pack", dir)
		cp.name = dir
	}

	if enableMultiMC == true {
		cp.gamePath = filepath.Join(cp.rootPath, "minecraft")
	} else {
		cp.gamePath = cp.rootPath
	}

	cp.modPath = filepath.Join(cp.gamePath, "mods")
	fmt.Printf("-- %s --\n", cp.gamePath)

	// Create the directories
	err := os.MkdirAll(cp.gamePath, 0700)
	if err != nil {
		return nil, fmt.Errorf("Failed to create %s: %+v", cp.gamePath, err)
	}

	err = os.MkdirAll(cp.modPath, 0700)
	if err != nil {
		return nil, fmt.Errorf("Failed to create %s: %+v", cp.modPath, err)
	}

	// Try to load the manifest; only raise an error if we require it to be loaded
	err = cp.loadManifest()
	if requireManifest && err != nil {
		return nil, err
	}

	return cp, nil
}

func (cp *ModPack) download(url string) error {
	// If the pack.zip file already exists, shortcut out
	packFilename := filepath.Join(cp.gamePath, "pack.zip")
	if fileExists(packFilename) {
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
		return fmt.Errorf("Failed to download %s: %+v", cp.name, err)
	}
	defer resp.Body.Close()

	// Store pack.zip in the working dir
	return writeStream(packFilename, resp.Body)
}

func (cp *ModPack) processManifest() error {
	// Open the pack.zip and parse the manifest
	pack, err := zip.OpenReader(filepath.Join(cp.gamePath, "pack.zip"))
	if err != nil {
		return fmt.Errorf("Failed to open pack.zip: %v", err)
	}
	defer pack.Close()

	// Find the manifest file and decode it
	cp.manifest, err = findJSONFile(pack, "manifest.json")
	if err != nil {
		return err
	}

	// Check the type and version of the manifest
	mvsn, ok := cp.manifest.Path("manifestVersion").Data().(float64)
	if !ok || mvsn != 1.0 {
		return fmt.Errorf("unexpected manifest version: %4.0f", mvsn)
	}

	mtype, ok := cp.manifest.Path("manifestType").Data().(string)
	if !ok || mtype != "minecraftModpack" {
		return fmt.Errorf("unexpected manifest type: %s", mtype)
	}

	return nil
}

func (cp *ModPack) minecraftVersion() string {
	return cp.manifest.Path("minecraft.version").Data().(string)
}

func (cp *ModPack) createManifest(name, minecraftVsn, forgeVsn string) error {
	// Create the manifest and set basic info
	cp.manifest = gabs.New()
	cp.manifest.SetP(minecraftVsn, "minecraft.version")
	cp.manifest.SetP("minecraftModpack", "manifestType")
	cp.manifest.SetP(1, "manifestVersion")
	cp.manifest.SetP(name, "name")

	loader := make(map[string]interface{})
	loader["id"] = forgeVsn
	loader["primary"] = true

	cp.manifest.ArrayOfSizeP(1, "minecraft.modLoaders")
	cp.manifest.Path("minecraft.modLoaders").SetIndex(loader, 0)

	// Write the manifest file
	err := cp.saveManifest()
	if err != nil {
		return fmt.Errorf("failed to save manifest.json: %+v", err)
	}

	return nil
}

func (cp *ModPack) getVersions() (string, string) {
	minecraftVsn := cp.manifest.Path("minecraft.version").Data().(string)
	forgeVsn := cp.manifest.Path("minecraft.modLoaders.id").Index(0).Data().(string)
	forgeVsn = strings.TrimPrefix(forgeVsn, "forge-")
	return minecraftVsn, forgeVsn
}

func (cp *ModPack) createLauncherProfile() error {
	// Using manifest config version + mod loader, look for an installed
	// version of forge with the appropriate version
	minecraftVsn, forgeVsn := cp.getVersions()

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

	fmt.Printf("Creating profile: %s\n", cp.name)
	lc.createProfile(cp.name, forgeID, cp.gamePath)
	lc.save()

	return nil
}

func (cp *ModPack) installMods(isClient bool) error {
	// Make sure mods directory already exists
	os.MkdirAll(cp.modPath, 0700)

	// Using manifest, download each mod file into pack directory from Curseforge
	files, _ := cp.manifest.Path("files").Children()
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
			filename := filepath.Join(cp.modPath, baseFilename.(string))
			if fileExists(filename) {
				fmt.Printf("Skipping %s\n", baseFilename.(string))
				continue
			}
		}

		projectID := int(f.Path("projectID").Data().(float64))
		fileID := int(f.Path("fileID").Data().(float64))
		filename, err := cp.installMod(projectID, fileID)
		if err != nil {
			return err
		}

		f.Set(filename, "filename")

		err = cp.saveManifest()
		if err != nil {
			return err
		}
	}

	// Also process any extfiles entries
	extFiles, _ := cp.manifest.S("extfiles").ChildrenMap()
	for _, url := range extFiles {
		_, err := cp.installModURL(url.Data().(string))
		if err != nil {
			return err
		}
	}

	return nil
}

func (cp *ModPack) registerMod(url, name string, clientOnly bool) error {
	// If the URL doesn't contain minecraft.curseforge.com, assume we're only being given
	// the URL and a tagname (to be registered in extfiles)
	if !strings.Contains(url, "minecraft.curseforge.com") {
		// Insert the url by name into extfiles map
		cp.manifest.Set(url, "extfiles", name)

		fmt.Printf("Registered %s as %s (clientOnly=%t)\n", url, name, clientOnly)
	} else {
		cfile, err := getCurseForgeFile(url)
		if err != nil {
			return err
		}

		// Make sure files entry exists in manifest
		if !cp.manifest.Exists("files") {
			cp.manifest.ArrayOfSizeP(0, "files")
		}

		// Add project & file IDs to manifest
		modInfo := make(map[string]interface{})
		modInfo["projectID"] = cfile.ProjectID
		modInfo["fileID"] = cfile.ID
		modInfo["required"] = true
		modInfo["desc"] = cfile.Desc

		if clientOnly {
			modInfo["clientOnly"] = true
		}

		// Walk through the list of files; if we find one with same project ID, delete it
		existingIndex := -1
		files, _ := cp.manifest.S("files").Children()
		for i, child := range files {
			childProjectID := int(child.S("projectID").Data().(float64))
			if childProjectID == cfile.ProjectID {
				// Found a matching project ID; note the index so we can replace it
				existingIndex = i

				// Also, delete any mod files listed by name
				filename, ok := child.S("filename").Data().(string)
				filename = filepath.Join(cp.modPath, filename)
				if ok && fileExists(filename) {
					// Try to remove the file; don't worry about error case
					os.Remove(filename)
				}

				break
			}
		}

		if existingIndex > -1 {
			cp.manifest.S("files").SetIndex(modInfo, existingIndex)
		} else {
			cp.manifest.ArrayAppendP(modInfo, "files")
		}

		fmt.Printf("Registered %s (clientOnly=%t)\n", cfile.Desc, clientOnly)
	}

	// Finally, update the manifest file
	return cp.saveManifest()
}

func (cp *ModPack) saveManifest() error {
	// Write the manifest file
	manifestStr := cp.manifest.StringIndent("", "  ")
	err := ioutil.WriteFile(filepath.Join(cp.gamePath, "manifest.json"), []byte(manifestStr), 0644)
	if err != nil {
		return fmt.Errorf("failed to save manifest.json: %+v", err)
	}
	return nil
}

func (cp *ModPack) loadManifest() error {
	// Load the manifest
	manifest, err := gabs.ParseJSONFile(filepath.Join(cp.gamePath, "manifest.json"))
	if err != nil {
		return fmt.Errorf("Failed to load manifest from %s: %+v", cp.gamePath, err)
	}
	cp.manifest = manifest
	return nil
}

func (cp *ModPack) installMod(projectID, fileID int) (string, error) {
	// First, resolve the project ID
	baseURL, err := getRedirectURL(fmt.Sprintf("https://minecraft.curseforge.com/projects/%d?cookieTest=1", projectID))
	if err != nil {
		return "", fmt.Errorf("failed to resolve project %d: %+v", projectID, err)
	}

	// Append the file ID to the baseURL
	finalURL := fmt.Sprintf("%s/files/%d/download", baseURL, fileID)
	return cp.installModURL(finalURL)
}

func (cp *ModPack) installModURL(url string) (string, error) {
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
	filename = filepath.Join(cp.modPath, filename)

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

func (cp *ModPack) installOverrides() error {
	// Open the pack.zip
	pack, err := zip.OpenReader(filepath.Join(cp.gamePath, "pack.zip"))
	if err != nil {
		return fmt.Errorf("Failed to open pack.zip: %v", err)
	}
	defer pack.Close()

	fmt.Printf("Installing files from modpack archive\n")

	// Walk over every file in the pack that is prefixed with installOverrides
	// and write it out
	for _, f := range pack.File {
		if !strings.HasPrefix(f.Name, "overrides/") {
			continue
		}

		filename := filepath.Join(cp.gamePath, strings.Replace(f.Name, "overrides/", "", -1))

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

func (cp *ModPack) installServer() error {
	// Get the minecraft + forge versions from manifest
	minecraftVsn := cp.manifest.Path("minecraft.version").Data().(string)
	forgeVsn := cp.manifest.Path("minecraft.modLoaders.id").Index(0).Data().(string)
	forgeVsn = strings.TrimPrefix(forgeVsn, "forge-")

	// Extract the version from manifest and setup a URL
	filename := fmt.Sprintf("minecraft_server.%s.jar", minecraftVsn)
	serverURL := fmt.Sprintf("https://s3.amazonaws.com/Minecraft.Download/versions/%s/%s", minecraftVsn, filename)
	absFilename := filepath.Join(cp.gamePath, filename)

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

	_, err := installServerForge(minecraftVsn, forgeVsn, cp.gamePath)
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

func (cp *ModPack) generateMMCConfig() error {
	name := cp.manifest.S("name").Data().(string)
	version := cp.manifest.S("version").Data().(string)

	// Generate the instance config string
	minecraftVsn, forgeVsn := cp.getVersions()
	cfg := fmt.Sprintf(MMC_CONFIG, minecraftVsn, forgeVsn, name, version)

	fmt.Printf("Generating instance.cfg for MultiMC\n")

	// Write it out
	err := ioutil.WriteFile(filepath.Join(cp.rootPath, "instance.cfg"), []byte(cfg), 0644)
	if err != nil {
		return fmt.Errorf("failed to save instance.cfg: %+v", err)
	}

	return nil
}
