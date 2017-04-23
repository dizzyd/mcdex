package main

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"io/ioutil"

	"strconv"

	"github.com/Jeffail/gabs"
	"github.com/PuerkitoBio/goquery"
	"github.com/robertkrimen/otto"
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

	// Make sure the target directory doesn't yet exist
	if dirExists(cp.path) {
		return nil, fmt.Errorf("Pack directory already exists: %s", cp.path)
	}

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

func OpenCursePack(name string) (*CursePack, error) {
	cp := new(CursePack)
	cp.name = name
	cp.path = filepath.Join(env().McdexDir, "pack", name)
	cp.modPath = filepath.Join(cp.path, "mods")

	// Make sure the target directory exists
	if !dirExists(cp.path) {
		return nil, fmt.Errorf("Pack directory does not exist: %s", cp.path)
	}

	// Load the manifest; bail if we can't find one
	manifest, err := gabs.ParseJSONFile(filepath.Join(cp.path, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("Failed to load manifest from %s: %+v", name, err)
	}

	cp.manifest = manifest

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

func (cp *CursePack) createManifest(name, minecraftVsn, forgeVsn string) error {
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
	err := ioutil.WriteFile(filepath.Join(cp.path, "manifest.json"), []byte(cp.manifest.String()), 0644)
	if err != nil {
		return fmt.Errorf("failed to save manifest.json: %+v", err)
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
	// Using manifest, download each mod file into pack directory from Curseforge
	files, _ := cp.manifest.Path("files").Children()
	for _, f := range files {
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

func (cp *CursePack) registerMod(url string) error {
	// Retrieve the URL (we assume it's a HTML webpage)
	res, e := HttpGet(url)
	if e != nil {
		return fmt.Errorf("failed to get %s: %+v", url, e)
	}
	defer res.Body.Close()
	doc, err := goquery.NewDocumentFromResponse(res)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %+v", url, e)
	}

	// Extract the description of this mod file for addition to manifest
	desc, _ := doc.Find("meta[property='og:description']").Attr("content")

	// Setup a JS VM and run the HTML through it; we want to process any
	// script sections in the head so we can extract Elerium meta-data
	vm := otto.New()
	vm.Run("Elerium = {}; Elerium.ProjectFileDetails = {}")
	doc.Find("head script").Each(func(i int, sel *goquery.Selection) {
		vm.Run(sel.Text())
	})

	// Convert the Elerium data into JSON, then a string to get it out the VM
	data, err := vm.Run("JSON.stringify(Elerium.ProjectFileDetails)")
	if err != nil {
		return fmt.Errorf("failed to extract project file details: %+v", err)
	}

	// Reparse from string into JSON (blech)
	dataStr, _ := data.ToString()
	projectDetails, _ := gabs.ParseJSON([]byte(dataStr))

	// Make sure files entry exists in manifest
	if !cp.manifest.Exists("files") {
		cp.manifest.ArrayOfSizeP(0, "files")
	}

	projectID := projectDetails.S("projectID").Data().(string)
	fileID := projectDetails.S("projectFileID").Data().(string)

	// We should now have the project & file IDs; add them to the manifest and
	// save it
	modInfo := make(map[string]interface{})
	modInfo["projectID"], _ = strconv.Atoi(projectID)
	modInfo["fileID"], _ = strconv.Atoi(fileID)
	modInfo["required"] = true
	modInfo["desc"] = desc

	// Walk through the list of files; if we find one with same project ID, delete it
	existingIndex := -1
	files, _ := cp.manifest.S("files").Children()
	for i, child := range files {
		if child.S("projectID").Data() == projectID {
			existingIndex = i
			break
		}
	}

	if existingIndex > -1 {
		cp.manifest.S("files").SetIndex(modInfo, existingIndex)
	} else {
		cp.manifest.ArrayAppendP(modInfo, "files")
	}

	// Write the manifest file
	return cp.saveManifest()
}

func (cp *CursePack) saveManifest() error {
	// Write the manifest file
	manifestStr := cp.manifest.StringIndent("", "  ")
	err := ioutil.WriteFile(filepath.Join(cp.path, "manifest.json"), []byte(manifestStr), 0644)
	if err != nil {
		return fmt.Errorf("failed to save manifest.json: %+v", err)
	}
	return nil
}

func (cp *CursePack) registerExtMod(name, url string) error {
	// Insert the url by name into extfiles map
	cp.manifest.Set(url, "extfiles", name)

	// Write the manifest file
	return cp.saveManifest()
}

func (cp *CursePack) installMod(projectID, fileID int) (string, error) {
	// First, resolve the project ID
	baseURL, err := getRedirectURL(fmt.Sprintf("https://minecraft.curseforge.com/projects/%d?cookieTest=1", projectID))
	if err != nil {
		return "", fmt.Errorf("failed to resolve project %d: %+v", projectID, err)
	}

	// Append the file ID to the baseURL
	finalURL := fmt.Sprintf("%s/files/%d/download", baseURL, fileID)
	return cp.installModURL(finalURL)
}

func (cp *CursePack) installModURL(url string) (string, error) {
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

		if f.FileInfo().IsDir() {
			fmt.Printf("Skipping dir: %s\n", f.Name)
			continue
		}

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
