package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type CurseManifest struct {
	ManifestType    string
	ManifestVersion int
	Name            string
	Version         string
	Description     string
	Files           []CurseManifestFile
	Overrides       string
	Config          CurseMinecraftInfo `json:"minecraft"`
}

type CurseMinecraftInfo struct {
	Version    string
	ModLoaders []CurseModLoader
}

type CurseModLoader struct {
	Id      string
	Primary bool
}

type CurseManifestFile struct {
	ProjectID int
	FileID    int
}

type CursePack struct {
	name     string
	url      string
	path     string
	manifest *CurseManifest
}

func NewCursePack(name string, url string) (*CursePack, error) {
	cp := new(CursePack)
	cp.name = name
	cp.path = filepath.Join(McdexDir(), "pack", name)
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

	fmt.Printf("Processing manifest..\n")

	// Find the manifest file (manifest.json)
	for _, f := range pack.File {
		if f.Name == "manifest.json" {
			reader, err := f.Open()
			if err != nil {
				return fmt.Errorf("Failed to open manifest.json: %v", err)
			}

			buf := new(bytes.Buffer)
			_, err = buf.ReadFrom(reader)
			if err != nil {
				return fmt.Errorf("Failed to load manifest.json into memory: %v", err)
			}

			manifest := new(CurseManifest)
			err = json.Unmarshal(buf.Bytes(), manifest)
			if err != nil {
				return fmt.Errorf("Failed to unmarshal manifest.json: %+v", err)
			}

			// Validate that the manifest matches our expected version
			if manifest.ManifestType != "minecraftModpack" || manifest.ManifestVersion != 1 {
				return fmt.Errorf("Unexpected manifest type: %s v.%d", manifest.ManifestType, manifest.ManifestVersion)
			}

			// Save manifeset to our current pack
			cp.manifest = manifest
			return nil
		}
	}

	// If we reached this point, no manifest was found
	return fmt.Errorf("Failed to find a manifest.json")
}

func (cp *CursePack) createLauncherProfile() error {
	// Using manifest config version + mod loader, look for an installed
	// version of forge with the appropriate version
	minecraftVsn := cp.manifest.Config.Version
	forgeVsn := cp.manifest.Config.ModLoaders[0].Id

	// Strip the "forge-"" prefix off the version string
	forgeVsn = strings.TrimPrefix(forgeVsn, "forge-")

	// Install forge if necessary
	if !isForgeInstalled(minecraftVsn, forgeVsn) {
		err := installForge(minecraftVsn, forgeVsn)
		if err != nil {
			return fmt.Errorf("failed to install Forge %s: %+v", forgeVsn, err)
		}
	}

	// Finally, load the launcher_profiles.json and make a new entry
	// with appropriate name and reference to our pack directory and forge version
	return nil
}

func (cp *CursePack) installMods() error {
	// Using manifest, download each mod file into pack directory
	return nil
}
