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
	modPath  string
	manifest *gabs.Container
}

func NewCursePack(name string, url string) (*CursePack, error) {
	cp := new(CursePack)
	cp.name = name
	cp.path = filepath.Join(env().McdexDir, "pack", name)
	cp.modPath = filepath.Join(cp.path, "mods")
	cp.url = url

	// Create the directories
	err := os.MkdirAll(cp.path, 0700)
	if err != nil {
		return nil, fmt.Errorf("Failed to create %s: %+v", cp.path, err)
	}

	err = os.MkdirAll(cp.modPath, 0700)
	if err != nil {
		return nil, fmt.Errorf("Failed to create %s: %+v", cp.modPath, err)
	}

	return cp, nil
}

func (cp *CursePack) download() error {
	// If the pack.zip file already exists, shortcut out
	packFilename := filepath.Join(cp.path, "pack.zip")
	if fileExists(packFilename) {
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
	forgeID, err = installForge(minecraftVsn, forgeVsn)
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
	lc.createProfile(cp.name, forgeID, cp.path)
	lc.save()

	return nil
}

func (cp *CursePack) installMods() error {
	// Using manifest, download each mod file into pack directory
	files, _ := cp.manifest.Path("files").Children()
	for _, f := range files {
		projectID := int(f.Path("projectID").Data().(float64))
		fileID := int(f.Path("fileID").Data().(float64))
		err := cp.installMod(projectID, fileID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (cp *CursePack) installMod(projectID, fileID int) error {
	// First, resolve the project ID
	baseURL, err := getRedirectURL(fmt.Sprintf("https://minecraft.curseforge.com/projects/%d?cookieTest=1", projectID))
	if err != nil {
		return fmt.Errorf("failed to resolve project %d: %+v", projectID, err)
	}

	// Append the file ID to the baseURL
	finalURL := fmt.Sprintf("%s/files/%d/download", baseURL, fileID)

	// Start the download
	resp, err := HttpGet(finalURL)
	if err != nil {
		return fmt.Errorf("Failed to download %s: %+v", finalURL, err)
	}
	defer resp.Body.Close()

	// If we didn't get back a 200, bail
	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to download %s status %d", finalURL, resp.StatusCode)
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
		return nil
	}

	// Save the stream of the response to the file
	fmt.Printf("Downloading %s\n", filepath.Base(filename))

	err = writeStream(filename, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write %s: %+v", filename, err)
	}
	return nil
}

func (cp *CursePack) installOverrides() error {
	// Open the pack.zip
	pack, err := zip.OpenReader(filepath.Join(cp.path, "pack.zip"))
	if err != nil {
		return fmt.Errorf("Failed to open pack.zip: %v", err)
	}
	defer pack.Close()

	// Walk over every file in the pack that is prefixed with installOverrides
	// and write it out
	for _, f := range pack.File {
		if !strings.HasPrefix(f.Name, "overrides/") {
			continue
		}

		filename := filepath.Join(cp.path, strings.Replace(f.Name, "overrides/", "", -1))

		// Make sure the directory for the file exists
		os.MkdirAll(filepath.Dir(filename), 0700)

		freader, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open %s: %+v", f.Name, err)
		}

		fmt.Printf("Unpacking %s\n", filepath.Base(filename))
		err = writeStream(filename, freader)
		if err != nil {
			return fmt.Errorf("failed to save: %+v", err)
		}
	}

	return nil
}
