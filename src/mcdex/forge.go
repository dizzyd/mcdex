package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"archive/zip"

	"bytes"

	"strings"

	"github.com/Jeffail/gabs"
)

func isForgeInstalled(minecraftVsn, forgeVsn string) bool {
	id := minecraftVsn + "-" + forgeVsn
	forgeDir := filepath.Join(MinecraftDir(), "versions", id)
	_, err := os.Stat(forgeDir)
	return os.IsExist(err)
}

func installForge(minecraftVsn, forgeVsn string) error {
	// Construct the download URL
	forgeURL := fmt.Sprintf("http://files.minecraftforge.net/maven/net/minecraftforge/forge/%s-%s/forge-%s-%s-installer.jar",
		minecraftVsn, forgeVsn, minecraftVsn, forgeVsn)

	fmt.Printf("Downloading Forge %s: %s\n", forgeVsn, forgeURL)

	// Download the Forge installer (into memory)
	resp, err := HttpGet(forgeURL)
	if err != nil {
		return fmt.Errorf("failed to download Forge %s: %+v", forgeVsn, err)
	}
	defer resp.Body.Close()

	fmt.Printf("Response: %+v\n", resp)

	installerBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to download Forge %s: %+v", forgeVsn, err)
	}

	// Extract the install_profile.json from ZIP containing forge
	installProfile, forgeJar, err := extractInstaller(installerBytes)
	if err != nil {
		return err
	}

	// From the install_profile.json, get the ID we should use for this install
	fmt.Printf("%+v\n", installProfile)
	forgeID, ok := installProfile.Path("versionInfo.id").Data().(string)
	if !ok {
		return fmt.Errorf("failed to find versionInfo.id in Forge %s", forgeVsn)
	}

	// Create the versions/ registry directory
	forgeDir := filepath.Join(MinecraftDir(), "versions", forgeID)
	err = os.MkdirAll(forgeDir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create dir %s: %+v", forgeDir, err)
	}

	// Extract the versionInfo section and write it to disk
	versionInfoBytes := installProfile.Path("versionInfo").Bytes()
	err = ioutil.WriteFile(filepath.Join(forgeDir, forgeID+".json"), versionInfoBytes, 0644)
	if err != nil {
		return fmt.Errorf("failed to write %s/%s.json: %+v", forgeDir, forgeID, err)
	}

	// Create the directory in which to install the forgeJar
	forgeJarID := fmt.Sprintf("%s-%s", minecraftVsn, forgeVsn)
	forgeJarDir := filepath.Join(MinecraftDir(), "libraries", "net", "minecraftforge", "forge", forgeJarID)
	err = os.MkdirAll(forgeJarDir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create dir %s: %+v", forgeJarDir, err)
	}

	// Write the forge JAR into a file in the directory
	jarFilename := filepath.Join(forgeJarDir, fmt.Sprintf("forge-%s.jar", forgeJarID))
	return writeStream(jarFilename, forgeJar)
}

// http://files.minecraftforge.net/maven/net/minecraftforge/forge/1.10.2-12.18.3.2185/forge-1.10.2-12.18.3.2185-installer.jar
// http://files.minecraftforge.net/maven/net/minecraftforge/forge/1.10.2-12.18.3.2254/forge-1.10.2-12.18.3.2254-installer.jar
// http://files.minecraftforge.net/maven/net/minecraftforge/forge/1.10.2-12.18.3.2185/forge-1.10.2-12.18.3.2185.jar

func extractInstaller(forgeInstallerBytes []byte) (*gabs.Container, io.Reader, error) {
	// Open the installer byte stream as a ZIP file
	installerReader := bytes.NewReader(forgeInstallerBytes)
	installerReaderSz := int64(len(forgeInstallerBytes))
	file, err := zip.NewReader(installerReader, installerReaderSz)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read Forge installer: %+v", err)
	}

	var installProfile *gabs.Container
	var forgeJar io.ReadCloser

	// Look for the necessary files
	for _, f := range file.File {
		if f.Name == "install_profile.json" {
			// Parse the JSON for install_profile.json
			installProfile, err = zipEntryToJSON("install_profile.json", f)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to parse install_profile.json: %+v", err)
			}
		} else if strings.HasSuffix(f.Name, "-universal.jar") {
			forgeJar, err = f.Open()
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read %s: %+v", f.Name, err)
			}
		}
	}

	if installProfile == nil || forgeJar == nil {
		return nil, nil, fmt.Errorf("failed to find install_profile.json or *-universal.jar")
	}

	return installProfile, forgeJar, nil
}
