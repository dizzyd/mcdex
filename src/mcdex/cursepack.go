package main

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jeffail/gabs"
)

type CursePack struct {
	name     string
	url      string
	path     string
	manifest *gabs.Container
}

func NewCursePack(name string, url string) (*CursePack, error) {
	cp := new(CursePack)
	cp.name = name
	cp.path = filepath.Join(env().McdexDir, "pack", name)
	cp.url = url

	// Create the directory
	err := os.MkdirAll(cp.path, 0700)
	if err != nil {
		return nil, fmt.Errorf("Failed to create %s: %+v", cp.path, err)
	}

	return cp, nil
}

func (cp *CursePack) download() error {
	// If the pack.zip file already exists, shortcut out
	packFilename := filepath.Join(cp.path, "pack.zip")
	if _, err := os.Stat(packFilename); os.IsExist(err) {
		return nil
	}

	fmt.Printf("Starting download of modpack: %s\n", cp.url)

	// Start the download
	resp, err := HttpGet(cp.url)
	if err != nil {
		return fmt.Errorf("Failed to download %s: %+v", cp.name, err)
	}
	defer resp.Body.Close()

	// Store pack.zip in the working dir
	return writeStream(packFilename, resp.Body)
}

func (cp *CursePack) processManifest() error {
	// Open the pack.zip and parse the manifest
	pack, err := zip.OpenReader(filepath.Join(cp.path, "pack.zip"))
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
	fmt.Printf("mvsn: %s\n", cp.manifest.Path("manifestVersion"))
	mvsn := cp.manifest.Path("manifestVersion").Data().(float64)
	if mvsn != 1.0 {
		return fmt.Errorf("unexpected manifest version: %4.0f", mvsn)
	}

	mtype, ok := cp.manifest.Path("manifestType").Data().(string)
	if !ok || mtype != "minecraftModpack" {
		return fmt.Errorf("unexpected manifest type: %s", mtype)
	}

	return nil
}

func (cp *CursePack) createLauncherProfile() error {
	// Using manifest config version + mod loader, look for an installed
	// version of forge with the appropriate version
	minecraftVsn := cp.manifest.Path("minecraft.version").Data().(string)
	forgeVsn := cp.manifest.Path("minecraft.modLoaders.id").Index(0).Data().(string)

	// Strip the "forge-"" prefix off the version string
	forgeVsn = strings.TrimPrefix(forgeVsn, "forge-")
	var forgeID string
	var err error

	// Install forge if necessary
	if !isForgeInstalled(minecraftVsn, forgeVsn) {
		forgeID, err = installForge(minecraftVsn, forgeVsn)
		if err != nil {
			return fmt.Errorf("failed to install Forge %s: %+v", forgeVsn, err)
		}
	}

	// Finally, load the launcher_profiles.json and make a new entry
	// with appropriate name and reference to our pack directory and forge version
	lc, err := newLauncherConfig()
	if err != nil {
		return fmt.Errorf("failed to load launcher_profiles.json: %+v", err)
	}

	fmt.Printf("Creating profile: %s\n", cp.name)
	lc.createProfile(cp.name, forgeID, cp.path)
	lc.save()

	return nil
}

func (cp *CursePack) installMods() error {
	// Using manifest, download each mod file into pack directory
	return nil
}
