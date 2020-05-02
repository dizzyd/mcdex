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

const NamePlaceholder = "*"

var VALID_URL_PREFIXES = []string{
	"https://www.feed-the-beast.com/download",
	"https://www.curseforge.com/minecraft/modpacks/",
	"https://minecraft.curseforge.com/",
}

// ModPack is a directory, manifest and other components that represent a pack
type ModPack struct {
	name     string
	rootPath string
	gameDir  string
	modDir   string
	manifest *gabs.Container
	modCache *MetaCache
	db       *Database
}

func (pack *ModPack) gamePath() string { return filepath.Join(pack.rootPath, pack.gameDir) }
func (pack *ModPack) modPath() string  { return filepath.Join(pack.gamePath(), pack.modDir) }
func (pack *ModPack) fullName() string {
	return fmt.Sprintf(
		"%s - %s",
		pack.manifest.Path("name").Data().(string),
		pack.manifest.Path("version").Data().(string),
	)
}

func NewModPack(dir string, requireManifest bool, enableMultiMC bool) (*ModPack, error) {
	pack := new(ModPack)

	// Open a copy of the database for modpack related ops
	db, err := OpenDatabase()
	if err != nil {
		return nil, fmt.Errorf("failed to open database for modpack: %+v", err)
	}
	pack.db = db

	// Initialize path & name
	if filepath.IsAbs(dir) {
		pack.rootPath = dir
		pack.name = filepath.Base(dir)
	} else if enableMultiMC {
		pack.name = dir
		if mmcDir, err := _mmcInstancesDir(); err == nil {
			pack.rootPath = filepath.Join(mmcDir, dir)
		} else {
			return nil, err
		}
	} else if dir == "." {
		pack.rootPath, _ = os.Getwd()
		pack.name = filepath.Base(pack.rootPath)
	} else {
		pack.rootPath = filepath.Join(env().McdexDir, "pack", dir)
		pack.name = dir
	}

	// Use a temp directory until manifest is downloaded
	if pack.name == NamePlaceholder && !requireManifest {
		pack.rootPath, _ = ioutil.TempDir(filepath.Dir(pack.rootPath), "mcdex-")
	}

	if enableMultiMC {
		pack.gameDir = "minecraft"
	}

	// Try to load the manifest; only raise an error if we require it to be loaded
	err = pack.loadManifest()
	if requireManifest && err != nil {
		return nil, err
	}

	fmt.Printf("-- %s --\n", pack.gamePath())

	// Create the directories
	err = os.MkdirAll(pack.gamePath(), 0700)
	if err != nil {
		return nil, fmt.Errorf("Failed to create %s: %+v", pack.gamePath(), err)
	}

	pack.modDir = "mods"
	err = os.MkdirAll(pack.modPath(), 0700)
	if err != nil {
		return nil, fmt.Errorf("Failed to create %s: %+v", pack.modPath(), err)
	}

	pack.modCache, err = OpenMetaCache(pack)
	if err != nil {
		return nil, fmt.Errorf("Failed to open mod cache: %+v", err)
	}

	return pack, nil
}

func (pack *ModPack) download(url string) error {
	// Check for a pack.url file; we use this to track where the pack
	// file came from so that we can re-download the pack when it changes.
	// This supports the use case of installing v 1.0.x of a pack and then updating
	// to 1.0.x+1 in the same directory
	packURLFile := filepath.Join(pack.gamePath(), "pack.url")
	origURL, _ := readStringFile(packURLFile)
	origURL = strings.TrimSpace(origURL)

	packFilename := filepath.Join(pack.gamePath(), "pack.zip")

	if origURL != url {
		// Remove pack.zip; this used to also remove the mods, but with the more
		// advanced metacache tracking, we can intelligently only update files that changed
		os.Remove(packFilename)

	} else if fileExists(packFilename) {
		return nil
	}

	fmt.Printf("Starting download of modpack: %s\n", url)

	// For the moment, we only support modpacks from Curseforge or FTB; check and enforce these conditions
	if !hasAnyPrefix(url, VALID_URL_PREFIXES...) {
		return fmt.Errorf("Invalid modpack URL; we only support Curseforge & feed-the-beast.com right now")
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
	zipFile, err := zip.OpenReader(filepath.Join(pack.gamePath(), "pack.zip"))
	if err != nil {
		return fmt.Errorf("Failed to open pack.zip: %v", err)
	}

	// Find the manifest file and decode it
	pack.manifest, err = findJSONFile(zipFile, "manifest.json")
	_ = zipFile.Close()
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

	if pack.name == NamePlaceholder {
		baseName := pack.fullName()
		name := baseName
		for i := 1; dirExists(filepath.Join(filepath.Dir(pack.rootPath), name)); i++ {
			name = fmt.Sprintf("%s (%d)", baseName, i)
		}
		fmt.Printf("Modpack %q will be installed to directory %q\n", baseName, name)
		newRoot := filepath.Join(filepath.Dir(pack.rootPath), name)
		if err = os.Rename(pack.rootPath, newRoot); err != nil {
			fmt.Printf("Unable to install to %q, will remain in temp directory %q:\n\t%+v\n", name, filepath.Base(pack.rootPath), err)
		} else {
			pack.rootPath = newRoot
			pack.name = name
		}
	}

	return pack.saveManifest()
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
	pack.manifest.SetP("0.0.1", "version")

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

	// Check the manifest for any Java arguments
	javaArgs := ""
	if pack.manifest.ExistsP("minecraft.javaArgs") {
		javaArgs = pack.manifest.Path("minecraft.javaArgs").Data().(string)
	}

	// Finally, load the launcher_profiles.json and make a new entry
	// with appropriate name and reference to our pack directory and forge version
	lc, err := newLauncherConfig()
	if err != nil {
		return fmt.Errorf("failed to load launcher_profiles.json: %+v", err)
	}

	fmt.Printf("Creating profile: %s\n", pack.name)
	err = lc.createProfile(pack.name, forgeID, pack.gamePath(), javaArgs)
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
	os.MkdirAll(pack.modPath(), 0700)

	// Using manifest, download each mod file into pack directory from Curseforge
	files, _ := pack.manifest.Path("files").Children()
	for _, f := range files {
		clientOnlyMod, ok := f.S("clientOnly").Data().(bool)
		if ok && clientOnlyMod && !isClient {
			fmt.Printf("Skipping client-only mod %s\n", f.Path("desc").Data().(string))
			continue
		}

		// Get the project & file ID
		projectID := int(f.Path("projectID").Data().(float64))
		fileID := int(f.Path("fileID").Data().(float64))

		// Check the mod cache to see if we already have the right file ID installed
		lastFileId, lastFilename := pack.modCache.GetLastModFile(projectID)
		if lastFileId == fileID {
			// Nothing to do; we can skip this installed file
			fmt.Printf("Skipping %s\n", lastFilename)
			continue
		} else if lastFileId > 0 {
			// A different version of the file is installed; clean it up
			pack.modCache.CleanupModFile(projectID)
		}

		filename, err := pack.installMod(projectID, fileID)
		if err != nil {
			if ignoreFailedDownloads {
				fmt.Printf("Ignoring failed download: %+v\n", err)
			} else {
				return err
			}
		} else {
			// Download succeeded; register this mod as installed in the cache
			pack.modCache.AddModFile(projectID, fileID, filename)
		}
	}

	// Also process any extfiles entries
	extFiles, _ := pack.manifest.S("extfiles").ChildrenMap()
	for key, url := range extFiles {
		// Check the cache to see if the URL has changed
		lastUrl, lastFilename := pack.modCache.GetLastExtURL(key)
		if lastUrl == url.Data().(string) {
			// Nothing to do; we already have a file installed
			fmt.Printf("Skipping %s\n", lastFilename)
			continue
		} else if lastUrl != "" {
			// A different version of the file is installed; clean it up
			pack.modCache.CleanupExtFile(key)
		}

		filename, err := pack.installModURL(url.Data().(string))
		if err != nil {
			if ignoreFailedDownloads {
				fmt.Printf("Ignoring failed download: %+v\n", err)
			} else {
				return err
			}
		} else {
			// Download succeeded; register this file
			pack.modCache.AddExtFile(key, url.String(), filename)
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
		childProjectID, _ := intValue(child, "projectID")
		if childProjectID == modFile.modID {
			// Found a matching project ID; note the index so we can replace it
			existingIndex = i
			break
		}
	}

	if existingIndex > -1 {
		pack.manifest.S("files").SetIndex(modInfo, existingIndex)
	} else {
		pack.manifest.ArrayAppendP(modInfo, "files")
	}

	fmt.Printf("Registered %s (clientOnly=%t)\n", modFile.modName, clientOnly)
	return nil
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

func (pack *ModPack) updateMods(db *Database, dryRun bool) error {
	// Walk over each file, looking for a more recent file ID for the
	// appropriate version
	files, _ := pack.manifest.S("files").Children()
	for _, child := range files {
		isLocked := child.Exists("locked") && child.S("locked").Data().(bool)
		modID, _ := intValue(child, "projectID")
		fileID, _ := intValue(child, "fileID")
		latestFile, err := db.getLatestModFile(modID, pack.minecraftVersion())
		if err == nil && latestFile.fileID > fileID {
			// Skip locked mods that have an update available
			if isLocked {
				fmt.Printf("Skipping %s (locked)\n", latestFile.modName)
				continue
			}

			// If this is a dry run, don't make any actual changes
			if dryRun {
				fmt.Printf("Update available for %s: %d -> %d\n", latestFile.modName, fileID, latestFile.fileID)
				continue
			}

			// Save the more recent file ID
			child.Set(latestFile.fileID, "fileID")
			child.Set(latestFile.modName, "desc")
			fmt.Printf("Updating %s: %d -> %d\n", latestFile.modName, fileID, latestFile.fileID)
		}
	}

	if !dryRun {
		return pack.saveManifest()
	}
	return nil
}

func (pack *ModPack) saveManifest() error {
	// Write the manifest file
	err := writeJSON(pack.manifest, filepath.Join(pack.gamePath(), "manifest.json"))
	if err != nil {
		return fmt.Errorf("failed to save manifest.json: %+v", err)
	}
	return nil
}

func (pack *ModPack) loadManifest() error {
	// Load the manifest
	manifest, err := gabs.ParseJSONFile(filepath.Join(pack.gamePath(), "manifest.json"))
	if err != nil {
		return fmt.Errorf("Failed to load manifest from %s: %+v", pack.gamePath, err)
	}
	pack.manifest = manifest
	return nil
}

func (pack *ModPack) installMod(projectID, fileID int) (string, error) {
	// First, resolve the project ID into a slug
	slug, err := pack.db.findSlugByProject(projectID)
	if err != nil {
		return "", fmt.Errorf("failed to find slug for project %d: %+v", projectID, err)
	}

	// Now, retrieve the JSON descriptor for this file so we can get the CDN url
	descriptorUrl := fmt.Sprintf("https://addons-ecs.forgesvc.net/api/v2/addon/%d/file/%d", projectID, fileID)
	descriptor, err := getJSONFromURL(descriptorUrl)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve descriptor for %s: %+v", slug, err)
	}

	finalUrl := descriptor.Path("downloadUrl").Data().(string)
	return pack.installModURL(finalUrl)
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

	// Check for Content-Disposition header
	attachmentID := resp.Header.Get("Content-Disposition")
	if strings.HasPrefix(attachmentID, "attachment; filename=") {
		filename = strings.TrimPrefix(attachmentID, "attachment; filename=")
	}

	if !strings.HasSuffix(filename, ".jar") {
		return "", fmt.Errorf("%s does not link to a valid mod file", url)
	}

	// Cleanup the filename
	filename = strings.Replace(filename, " r", "-", -1)
	filename = strings.Replace(filename, " ", "-", -1)
	filename = strings.Replace(filename, "+", "-", -1)
	filename = strings.Replace(filename, "(", "-", -1)
	filename = strings.Replace(filename, ")", "", -1)
	filename = strings.Replace(filename, "[", "-", -1)
	filename = strings.Replace(filename, "]", "", -1)
	filename = strings.Replace(filename, "'", "", -1)
	filename = filepath.Join(pack.modPath(), filename)

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
	zipFile, err := zip.OpenReader(filepath.Join(pack.gamePath(), "pack.zip"))
	if err != nil {
		return fmt.Errorf("Failed to open pack.zip: %v", err)
	}
	defer zipFile.Close()

	fmt.Printf("Installing files from modpack archive\n")
	overrides := pack.manifest.Path("overrides").Data().(string) + "/"

	// Walk over every file in the pack that is prefixed with installOverrides
	// and write it out
	for _, f := range zipFile.File {
		if f.FileInfo().IsDir() || !strings.HasPrefix(f.Name, overrides) {
			continue
		}

		filename := filepath.Join(pack.gamePath(), strings.Replace(f.Name, overrides, "", -1))
		filename = stripBadUTF8(filename)

		// Make sure the directory for the file exists
		os.MkdirAll(filepath.Dir(filename), 0700)

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
	absFilename := filepath.Join(pack.gamePath(), filename)

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

	_, err := installServerForge(minecraftVsn, forgeVsn, pack.gamePath())
	if err != nil {
		return fmt.Errorf("failed to install forge: %+v", err)
	}

	return nil
}

func (pack *ModPack) generateMMCConfig() error {
	return generateMMCConfig(pack)
}
