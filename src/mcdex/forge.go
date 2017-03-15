package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"archive/zip"

	"bytes"

	"strings"

	"encoding/binary"

	"github.com/Jeffail/gabs"
	"github.com/xi2/xz"
)

func isForgeInstalled(minecraftVsn, forgeVsn string) bool {
	id := minecraftVsn + "-" + forgeVsn
	forgeDir := filepath.Join(env().MinecraftDir, "versions", id)
	_, err := os.Stat(forgeDir)
	return os.IsExist(err)
}

func installForge(minecraftVsn, forgeVsn string) (string, error) {
	// Construct the download URL
	forgeURL := fmt.Sprintf("http://files.minecraftforge.net/maven/net/minecraftforge/forge/%s-%s/forge-%s-%s-installer.jar",
		minecraftVsn, forgeVsn, minecraftVsn, forgeVsn)

	fmt.Printf("Downloading Forge %s: %s\n", forgeVsn, forgeURL)

	// Download the Forge installer (into memory)
	resp, err := HttpGet(forgeURL)
	if err != nil {
		return "", fmt.Errorf("failed to download Forge %s: %+v", forgeVsn, err)
	}
	defer resp.Body.Close()

	fmt.Printf("Response: %+v\n", resp)

	installerBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to download Forge %s: %+v", forgeVsn, err)
	}

	// Extract the install_profile.json from ZIP containing forge
	installProfile, forgeJar, err := extractInstaller(installerBytes)
	if err != nil {
		return "", err
	}

	// From the install_profile.json, get the ID we should use for this install
	fmt.Printf("%+v\n", installProfile)
	forgeID, ok := installProfile.Path("versionInfo.id").Data().(string)
	if !ok {
		return "", fmt.Errorf("failed to find versionInfo.id in Forge %s", forgeVsn)
	}

	// Create the versions/ registry directory
	forgeDir := filepath.Join(env().MinecraftDir, "versions", forgeID)
	err = os.MkdirAll(forgeDir, 0700)
	if err != nil {
		return "", fmt.Errorf("failed to create dir %s: %+v", forgeDir, err)
	}

	// Extract the versionInfo section and write it to disk
	versionInfoBytes := installProfile.Path("versionInfo").Bytes()
	err = ioutil.WriteFile(filepath.Join(forgeDir, forgeID+".json"), versionInfoBytes, 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write %s/%s.json: %+v", forgeDir, forgeID, err)
	}

	// Create the directory in which to install the forgeJar
	forgeJarID := fmt.Sprintf("%s-%s", minecraftVsn, forgeVsn)
	forgeJarDir := filepath.Join(env().MinecraftDir, "libraries", "net", "minecraftforge", "forge", forgeJarID)
	err = os.MkdirAll(forgeJarDir, 0700)
	if err != nil {
		return "", fmt.Errorf("failed to create dir %s: %+v", forgeJarDir, err)
	}

	// Write the forge JAR into a file in the directory
	jarFilename := filepath.Join(forgeJarDir, fmt.Sprintf("forge-%s.jar", forgeJarID))
	err = writeStream(jarFilename, forgeJar)
	if err != nil {
		return "", fmt.Errorf("failed to write %s: %+v", jarFilename, err)
	}

	// Install libraries
	libs, _ := installProfile.Path("versionInfo.libraries").Children()
	for _, lib := range libs {
		err = installForgeLibrary(lib)
		if err != nil {
			fmt.Printf("Install forge: %s: %+v\n", lib, err)
		}
	}

	return forgeID, nil
}

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

func installForgeLibrary(library *gabs.Container) error {
	// TODO: Add support for handling server-side libraries
	c := library.S("clientreq").Data()
	if c == nil || c.(bool) != true {
		return nil
	}

	// Extract key parts of library name
	name := library.Path("name").Data().(string)
	url, ok := library.Path("url").Data().(string)
	if !ok {
		return nil
	}

	// Unpack the name into maven standard: groupId, artifactId and version
	parts := strings.SplitN(name, ":", 3)
	groupID := parts[0]
	artifactID := parts[1]
	vsn := parts[2]

	// Replace all periods in groupId with path delimiters
	groupID = strings.Replace(groupID, ".", "/", -1)

	// Construct the libDir and libName; if the file already exists, bail
	libName := fmt.Sprintf("%s-%s.jar", artifactID, vsn)
	libDir := filepath.Join(env().MinecraftDir, "libraries", groupID, artifactID, vsn)
	if fileExists(filepath.Join(libDir, libName)) {
		return nil
	}

	// Construct the URL to download
	finalURL := fmt.Sprintf("%s/%s/%s/%s/%s-%s.jar.pack.xz", url, groupID, artifactID, vsn, artifactID, vsn)
	resp, err := HttpGet(finalURL)
	if err != nil {
		return fmt.Errorf("failed to download %s: %+v", name, err)
	}
	defer resp.Body.Close()

	// If we got anything other than 200, bail
	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to download %s: unexpected HTTP response %d", name, resp.StatusCode)
	}

	// Open a XZ decompressor
	xzResponse, err := xz.NewReader(resp.Body, 0)
	if err != nil {
		return fmt.Errorf("failed to download %s: unexpected xz error: %+v", name, err)
	}

	// Stream the whole decompressed response into memory; we need to strip off the oddball
	// signatures before we invoke unpack200 to convert it into a JAR file
	var packDataBuf bytes.Buffer
	packSz, err := packDataBuf.ReadFrom(xzResponse)
	if err != nil {
		return fmt.Errorf("failed to decompress %s: %+v", name, err)
	}

	// Grab the raw bytes to the data for munging purposes
	packData := packDataBuf.Bytes()

	// Get the signature length
	sigLen, err := signatureLen(packData)
	if err != nil {
		return fmt.Errorf("failed to strip signatures: %+v", err)
	}

	// Create the directory for the library
	err = os.MkdirAll(libDir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create lib directory %s: %+v", name, err)
	}

	// Write the packData (minus the signature) to disk
	err = writeStream(filepath.Join(libDir, "tmp.pack"), bytes.NewReader(packData[0:packSz-sigLen]))
	if err != nil {
		fmt.Printf("failed to write %s: %+v", libName, err)
	}

	// Invoke unpack200 on tmp.pack and output to the appropriate JAR name
	err = invokeUnpack200(libDir, libName)
	if err != nil {
		return err
	}
	return nil
}

func signatureLen(data []byte) (int64, error) {
	dataSz := len(data)
	if string(data[dataSz-4:dataSz]) != "SIGN" {
		return 0, fmt.Errorf("invalid signature bytes")
	}

	var sigLen uint32
	err := binary.Read(bytes.NewReader(data[dataSz-8:dataSz-4]), binary.LittleEndian, &sigLen)
	if err != nil {
		return 0, fmt.Errorf("invalid signature len: %+v", err)
	}

	return int64(sigLen + 8), nil
}

func invokeUnpack200(libDir, libName string) error {
	err := exec.Command(unpack200Cmd(), "-r",
		filepath.Join(libDir, "tmp.pack"),
		filepath.Join(libDir, libName)).Run()
	if err != nil {
		return fmt.Errorf("failed to run unpack200 on %s: %+v", libName, err)
	}
	return nil
}
