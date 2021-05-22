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

type ModPackFile interface {
	install(pack *ModPack) error
	update(pack *ModPack) (bool, error)

	getName() string
	isClientOnly() bool

	equalsJson(modJson *gabs.Container) bool
	toJson() map[string]interface{}
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

func NewModPack(dir string, TrequireManifest int, enableMultiMC bool) (*ModPack, error) {
	pack := new(ModPack)
	requireManifest := false;
	if TrequireManifest > 0 {
		requireManifest = true;
	}

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
		if TrequireManifest == 2 {
			return nil, fmt.Errorf("%v\nIf you meant to install a modpack identified by %[2]s run:\n\tmcdex pack.install %[2]s %[2]s\n", err, dir)
		} else {
			return nil, err
		}
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

	// This doesn't work any more.
	// For the moment, we only support modpacks from Curseforge or FTB; check and enforce these conditions
	//if !hasAnyPrefix(url, VALID_URL_PREFIXES...) {
	//	return fmt.Errorf("Invalid modpack URL; we only support Curseforge & feed-the-beast.com right now")
	//}

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

func (pack *ModPack) installMods(isClient bool) error {
	// Make sure mods directory already exists
	os.MkdirAll(pack.modPath(), 0700)

	// Using manifest, download each mod file into pack directory
	files, _ := pack.manifest.Path("files").Children()
	for _, f := range files {
		modFile, err := newModPackFile(f)
		if err != nil {
			return err
		}

		if !isClient && modFile.isClientOnly() {
			fmt.Printf("Skipping client-only mod %s\n", modFile.getName())
			continue
		}

		err = modFile.install(pack)
		if err != nil {
			return fmt.Errorf("error installing mod file: %+v", err)
		}
	}

	return nil
}

func (pack *ModPack) selectMod(modFile ModPackFile) error {
	// Make sure files entry exists in manifest
	if !pack.manifest.Exists("files") {
		pack.manifest.ArrayOfSizeP(0, "files")
	}

	// Walk through the list of files; if we find one with same project ID, delete it
	existingIndex := -1
	files, _ := pack.manifest.S("files").Children()
	for i, child := range files {
		if modFile.equalsJson(child) {
			// Found a matching project ID; note the index so we can replace it
			existingIndex = i
			break
		}
	}

	if existingIndex > -1 {
		pack.manifest.S("files").SetIndex(modFile.toJson(), existingIndex)
	} else {
		pack.manifest.ArrayAppendP(modFile.toJson(), "files")
	}

	fmt.Printf("Registering: %s\n", modFile.getName())
	return pack.saveManifest()
}

func (pack *ModPack) updateMods(dryRun bool) error {
	// Walk over each file, looking for a more recent file ID for the
	// appropriate version
	files, _ := pack.manifest.S("files").Children()
	for _, child := range files {
		modFile, err := newModPackFile(child)
		if err != nil {
			return fmt.Errorf("unable to update: %+v", err)
		}

		isLocked := child.Exists("locked") && child.S("locked").Data().(bool)
		if isLocked {
			fmt.Printf("Skipping update: %s (locked)\n", modFile.getName())
			continue
		}

		updated, err := modFile.update(pack)
		if err != nil {
			return err
		}

		if updated {
			if dryRun {
				fmt.Printf("Update available: %s\n", modFile.getName())
			} else {
				pack.selectMod(modFile)
			}
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

	_, err := installServerForge(minecraftVsn, forgeVsn, pack.gamePath())
	if err != nil {
		return fmt.Errorf("failed to install forge: %+v", err)
	}

	return nil
}

func (pack *ModPack) generateMMCConfig() error {
	return generateMMCConfig(pack)
}

func newModPackFile(modJson *gabs.Container) (ModPackFile, error) {
	if modJson.ExistsP("projectID") {
		return NewCurseForgeModFile(modJson), nil
	} else if modJson.ExistsP("module") {
		return NewMavenModFile(modJson), nil
	}
	return nil, fmt.Errorf("unknown mod file entry: %s", modJson.String())
}
