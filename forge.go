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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"bytes"

	"strings"

	"encoding/binary"

	"github.com/Jeffail/gabs"
	"github.com/xi2/xz"
)

type forgeContext struct {
	baseDir string
	tmpDir string
	minecraftVsn string
	forgeVsn string
	installArchive *ZipHelper
	installJson *gabs.Container
	versionJson *gabs.Container
	isClient bool
	isLegacy bool
}

func (fc forgeContext) artifactDir() string {
	return path.Join(fc.baseDir, "libraries")
}

func (fc forgeContext) versionDir() string {
	return path.Join(fc.baseDir, "versions", fc.forgeId())
}

func (fc forgeContext) forgeId() string {
	return fc.minecraftVsn + "-forge-" + fc.forgeVsn
}

func (fc forgeContext) isForgeInstalled() bool {
	if fc.isClient {
		forgeFile := path.Join(fc.versionDir(), fc.forgeId(), fc.forgeId() + ".jar")
		return fileExists(forgeFile)
	}
	return false
}

func installServerForge(minecraftVsn, forgeVsn, targetDir string) (string, error) {
	return installForge(forgeContext{
		baseDir:        targetDir,
		minecraftVsn:   minecraftVsn,
		forgeVsn:       forgeVsn,
		isClient:       false,
	})
}

func installClientForge(minecraftVsn, forgeVsn string) (string, error) {
	return installForge(forgeContext{
		baseDir:        env().MinecraftDir,
		minecraftVsn:   minecraftVsn,
		forgeVsn:       forgeVsn,
		isClient:       true,
	})
}

func installForge(context forgeContext) (string, error) {
	// If this version of forge is already installed, exit early
	if context.isForgeInstalled() {
		logAction("Forge %s already available.\n", context.forgeVsn)
		return context.forgeId(), nil
	}

	// Setup a temp directory that will get cleaned up (for processors)
	context.tmpDir, _ = ioutil.TempDir("", "*-forgeinstall")
	defer os.RemoveAll(context.tmpDir)

	// Choose the right format for the download URL; some older versions
	// of Forge are a tad inconsistent
	var forgeURL string
	switch context.minecraftVsn {
	case "1.7.10":
		forgeURL = fmt.Sprintf("http://files.minecraftforge.net/maven/net/minecraftforge/forge/%s-%s-%s/forge-%s-%s-%s-installer.jar",
			context.minecraftVsn, context.forgeVsn, context.minecraftVsn, context.minecraftVsn, context.forgeVsn, context.minecraftVsn)
	default:
		forgeURL = fmt.Sprintf("http://files.minecraftforge.net/maven/net/minecraftforge/forge/%s-%s/forge-%s-%s-installer.jar",
			context.minecraftVsn, context.forgeVsn, context.minecraftVsn, context.forgeVsn)
	}

	// Construct the download URL
	logAction("Downloading Forge %s\n", context.forgeVsn)

	// Download the Forge installer (into memory)
	resp, err := HttpGet(forgeURL)
	if err != nil {
		return "", fmt.Errorf("download failed: %+v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP error %d", resp.StatusCode)
	}

	installerBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to download Forge %s: %+v", context.forgeVsn, err)
	}

	// Setup a zip helper for the forge installer
	context.installArchive, err = NewZipHelper(installerBytes)
	if err != nil {
		return "", fmt.Errorf("failed to open Forge installer: %+v", err)
	}

	// Get install_profile.json from the installer
	context.installJson, err = context.installArchive.getJsonFile("install_profile.json")
	if err != nil {
		return "", fmt.Errorf("failed to get JSON for install_profile.json: %+v", err)
	}

	// If we didn't find a version.json in the installer package, look inside the install_profile.json for
	// the older section "versionInfo" and use that instead
	context.versionJson, _ = context.installArchive.getJsonFile("version.json")
	if context.versionJson == nil {
		if !context.installJson.ExistsP("versionInfo") {
			return "", fmt.Errorf("failed to find either version.json or versionInfo section")
		}

		// Ok, confirmed we're in legacy mode. There's some fix-up work to do...
		context.isLegacy = true

		// First, pull out the version.json from install_profile
		context.versionJson = context.installJson.Path("versionInfo")

		// Finally, replace the installJson with the "install" sub-section
		context.installJson = context.installJson.Path("install")
	}

	// Fix up the versionInfo.id in the profile to use the correct ID
	// (Forge uses a weird repeating version by default)
	context.versionJson.SetP(context.forgeId(), "id")

	// Install forge artifacts (i.e. forge JAR and version file, as appropriate)
	err = installForgeArtifacts(&context)
	if err != nil {
		fmt.Printf("Failed to install Forge artifacts: %+v\n", err)
		return "", err
	}

	logSection("Installed forge artifacts\n")

	// Install libraries for install_profile.json
	err = installForgeLibraries(context.installJson, &context)
	if err != nil {
		fmt.Printf("Failed to install libraries for install_profile.json: %+v\n", err)
		return "", err
	}

	// Install libraries for version.json (or versionInfo)
	err = installForgeLibraries(context.versionJson, &context)
	if err != nil {
		fmt.Printf("Failed to install libraries for version.json: %+v\n", err)
		return "", err
	}

	logSection("Installed all libraries\n")

	// Make sure appropriate minecraft JAR is available
	minecraftJar, err := installMinecraftJar(context.minecraftVsn, context.isClient, context.baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to install minecraft jar %s: %+v", context.minecraftVsn, err)
	}

	logSection("Installed Minecraft %s jar\n", context.minecraftVsn)

	// Run any processors we find in install_profile.json
	err = runForgeProcessors(&context, minecraftJar)
	if err != nil {
		fmt.Printf("Failed to run processores from install_profile.json: %+v\n", err)
		return "", err
	}

	logSection("Executed forge processors\n")

	return context.forgeId(), nil
}

func installForgeArtifacts(context *forgeContext) error {
	// For client installs, the version file needs to be written to disk
	if context.isClient {
		versionFile := fmt.Sprintf("%s.json", context.forgeId())
		err := writeStringFile(path.Join(context.versionDir(), versionFile),
			context.versionJson.StringIndent("", " "))
		if err != nil {
			return fmt.Errorf("failed to write version.json: %+v", err)
		}

		// If this isn't a legacy install, we're all done here; remaining artifacts will
		// be installed as part of libraries
		if !context.isLegacy {
			return nil
		}
	}

	// Permutations of forge JAR installs:
	// - Legacy, client - get universal jar from ZIP and place in artifacts dir
	// - Legacy, server - get universal jar from ZIP and place in base dir
	// - Current, server - get from ZIP and place in base dir
	artifactId := context.installJson.S("path").Data().(string)
	forgeFilename := fmt.Sprintf("forge-%s-%s.jar", context.minecraftVsn, context.forgeVsn)
	var sourceFile string
	var targetFile string
	if context.isLegacy {
		sourceFile = context.installJson.S("filePath").Data().(string)
		if context.isClient {
			targetFile = path.Join(context.artifactDir(), path.Dir(artifactToPath(artifactId)), forgeFilename)
		} else {
			targetFile = path.Join(context.baseDir, forgeFilename)
		}
	} else {
		sourceFile = path.Join("maven", artifactToPath(artifactId))
		targetFile = path.Join(context.baseDir, forgeFilename)
	}

	logAction("Installing %s...\n", artifactId)
	_, err := context.installArchive.writeFile(sourceFile, targetFile)
	if err != nil {
		return fmt.Errorf("failed to write %s: %+v", targetFile, err)
	}

	return nil
}

func installForgeLibraries(versionInfo *gabs.Container, context *forgeContext) error {
	libs, _ := versionInfo.Path("libraries").Children()
	for _, lib := range libs {
		err := installForgeLibrary(lib, context)
		if err != nil {
			return fmt.Errorf("%s: %+v", lib, err)
		}
	}
	return nil
}

func installForgeLibrary(library *gabs.Container, context *forgeContext) error {
	// Extract key parts of library name
	name := library.Path("name").Data().(string)
	var url string

	// The libraries section has two formats:
	// * Legacy - { name, url, clientreq, serverreq}
	// * Current - { name, downloads.artifact.url, etc}
	if library.ExistsP("downloads.artifact.url") {
		url = library.Path("downloads.artifact.url").Data().(string)
		if url == "" {
			// No URL provided, so we need to look in the installer, under maven/
			filename := library.Path("downloads.artifact.path").Data().(string)
			sourceFile := path.Join("maven", filename)
			targetFile := path.Join(context.artifactDir(), filename)

			logAction("Installing %s...\n", name)
			_, err := context.installArchive.writeFile(sourceFile, targetFile)
			if err != nil {
				return fmt.Errorf("failed to write %s: %+v", filename, err)
			}

			return nil
		}
	} else {
		var isClientLib = getFlag(library, "clientreq")
		var isServerLib = getFlag(library, "serverreq")

		if !isClientLib && !isServerLib {
			return nil
		}

		if library.ExistsP("url") {
			url = library.Path("url").Data().(string)
		}

		if url == "" {
			url = "https://libraries.minecraft.net"
		}
	}

	logAction("Installing %s...\n", name)

	// Convert name from maven format to path
	artifactName := artifactToPath(name)

	// Construct the libDir and libName; if the file already exists, bail
	filename := filepath.Join(context.artifactDir(), artifactName)
	if fileExists(filename) {
		return nil
	}

	// Construct the URL to download, if necessary
	if context.isLegacy {
		url = url + "/" + artifactName
	}

	err := downloadXzPack(url, filename)
	if err != nil {
		err = downloadJar(url, filename)
		if err != nil {
			return err
		}
	}

	return nil
}

func getFlag(obj *gabs.Container, flag string) bool {
	fdata := obj.S(flag).Data()
	fval, ok := fdata.(bool)
	if !ok {
		return false
	}
	return fval
}

func downloadXzPack(url, filename string) error {
	dir := filepath.Dir(filename)
	filename = filepath.Base(filename)

	// Construct the URL to download
	finalURL := fmt.Sprintf("%s.pack.xz", url)
	resp, err := HttpGet(finalURL)
	if err != nil {
		return fmt.Errorf("failed to download %s: %+v", finalURL, err)
	}
	defer resp.Body.Close()

	// If we got anything other than 200, bail
	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to download %s: unexpected HTTP response %d", finalURL, resp.StatusCode)
	}

	// Open a XZ decompressor
	xzResponse, err := xz.NewReader(resp.Body, 0)
	if err != nil {
		return fmt.Errorf("failed to download %s: unexpected xz error: %+v", finalURL, err)
	}

	// Stream the whole decompressed response into memory; we need to strip off the oddball
	// signatures before we invoke unpack200 to convert it into a JAR file
	var packDataBuf bytes.Buffer
	packSz, err := packDataBuf.ReadFrom(xzResponse)
	if err != nil {
		return fmt.Errorf("failed to decompress %s: %+v", finalURL, err)
	}

	// Grab the raw bytes to the data for munging purposes
	packData := packDataBuf.Bytes()

	// Get the signature length
	sigLen, err := signatureLen(packData)
	if err != nil {
		return fmt.Errorf("failed to strip signatures: %+v", err)
	}

	// Create the directory
	err = os.MkdirAll(dir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create lib directory %s: %+v", dir, err)
	}

	// Write the packData (minus the signature) to disk
	err = writeStream(filepath.Join(dir, "tmp.pack"), bytes.NewReader(packData[0:packSz-sigLen]))
	if err != nil {
		fmt.Printf("failed to write %s: %+v", dir, err)
		return err
	}

	// Invoke unpack200 on tmp.pack and output to the appropriate JAR name
	err = invokeUnpack200(dir, filename)
	if err != nil {
		return err
	}
	return nil
}

func downloadJar(url, filename string) error {
	dir := filepath.Dir(filename)
	filename = filepath.Base(filename)

	// Construct the URL to download
	resp, err := HttpGet(url)
	if err != nil {
		return fmt.Errorf("failed to download %s: %+v", url, err)
	}
	defer resp.Body.Close()

	// If we got anything other than 200, bail
	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to download %s: unexpected HTTP response %d", url, resp.StatusCode)
	}

	// Create the directory
	err = os.MkdirAll(dir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create lib directory %s: %+v", dir, err)
	}

	// Save the stream to disk
	err = writeStream(filepath.Join(dir, filename), resp.Body)
	if err != nil {
		fmt.Printf("failed to write %s: %+v", dir, err)
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

func invokeProcessor(name string, args []string) error {
	logAction("Running processor %s...\n", name)
	cmd := exec.Command(javaCmd(), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("%s\n", out)
		return fmt.Errorf("failed to run processor %s: %+v", name, err)
	}
	return nil
}

func runForgeProcessors(context *forgeContext, minecraftJar string) error {
	processors, _ := context.installJson.Path("processors").Children()
	if processors == nil {
		// Nothing to do, bail
		logAction("Skipping Forge processors...\n")
		return nil
	}

	// Process the data section
	data, err := loadForgeData(context)
	if err != nil {
		return fmt.Errorf("failed to parse install_profile.json data section: %+v", err)
	}

	// The data section also requires a key pointing to the installed Minecraft JAR
	data["MINECRAFT_JAR"] = minecraftJar

	for _, p := range processors {
		var args []string

		// Translate the processor artifact to a path
		processor := p.Path("jar").Data().(string)
		processorJarName := path.Join(context.artifactDir(), artifactToPath(processor))

		// Build a classpath string
		classpathItems, _ := p.Path("classpath").Children()
		var classpathJars []string
		for _, item := range classpathItems {
			entry := path.Join(context.artifactDir(), artifactToPath(item.Data().(string)))
			classpathJars = append(classpathJars, entry)
		}

		// Add the processor jar as the final entry on the classpath
		classpathJars = append(classpathJars, processorJarName)
		args = append(args, "-classpath", strings.Join(classpathJars, ":"))

		// Get the Java main class from processor jar
		mainClass, err := getJavaMainClass(processorJarName)
		if err != nil {
			return fmt.Errorf("failed to get main class for processor %s: %+v", processor, err)
		}

		args = append(args, mainClass)

		// Finally, walk all the arguments and resolve using data section
		args = append(args, parseProcessorArgs(p, context, data)...)

		err = invokeProcessor(processor, args)
		if err != nil {
			return err
		}
	}

	return nil
}

func parseProcessorArgs(processor *gabs.Container, context *forgeContext, data map[string]string) []string {
	var result []string
	args, _ := processor.Path("args").Children()
	for _, argItem := range args {
		argStr := argItem.Data().(string)
		if strings.HasPrefix(argStr,"{") {
			// Reference to a variable in data
			result = append(result, data[strings.Trim(argStr, "{}")])
		} else if strings.HasPrefix(argStr, "[") {
			// Reference to an artifact
			result = append(result, path.Join(context.artifactDir(), artifactToPath(strings.Trim(argStr, "[]"))))
		} else {
			result = append(result, argStr)
		}
	}
	return result
}

func loadForgeData(context *forgeContext) (map[string]string, error) {
	dataJsonMap, err := context.installJson.Path("data").ChildrenMap()
	if err != nil || dataJsonMap == nil {
		// No data section; bail
		return nil, fmt.Errorf("missing/empty data section: %+v", err)
	}

	side := "client"
	if !context.isClient {
		side = "server"
	}

	// For each data entry, pull out the appropriately sided data
	dataMap := make(map[string]string)
	for k, v := range dataJsonMap {
		value := v.Path(side).Data().(string)
		if strings.HasPrefix(value,"[") {
			// Artifact reference
			dataMap[k] = path.Join(context.artifactDir(), artifactToPath(strings.Trim(value, "[]")))
		} else if strings.HasPrefix(value,"'") {
			// Literal
			dataMap[k] = strings.Trim(value, "'")
		} else {
			// File in installer that should be extracted to temp directory
			// and resolved to an absolute path
			tmpFilename, err := context.installArchive.writeFileToDir(strings.TrimLeft(value, "/"), context.tmpDir)
			if err != nil {
				return nil, fmt.Errorf("failed to extract temp file %s (%s): %+v", k, side, err)
			}
			dataMap[k] = tmpFilename
		}
	}

	return dataMap, nil
}

func artifactToPath(id string) string{
	// First, break up the string into maven components: group, artifact and version
	parts := strings.SplitN(id, ":", 3)
	if len(parts) < 3 {
		return id
	}

	// Break up the group ID by periods; these are path components
	groupID := strings.Split(parts[0], ".")
	artifactID := parts[1]
	vsn := parts[2]
	ext := "jar"
	suffix := ""

	// The version string MAY contain an @ that indicates an alternate file extension (i.e. not .jar)
	if strings.Contains(vsn, "@") {
		vsnParts := strings.SplitN(vsn, "@", 2)
		vsn = vsnParts[0]
		ext = vsnParts[1]
	}

	// The version string MAY also have a suffix, delimited by :
	if strings.Contains(vsn, ":") {
		vsnParts := strings.SplitN(vsn, ":", 2)
		vsn = vsnParts[0]
		suffix = "-" + vsnParts[1]
	}

	return path.Join(path.Join(groupID...), artifactID, vsn,
		fmt.Sprintf("%s-%s%s.%s", artifactID, vsn, suffix, ext))
}