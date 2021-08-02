package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

type fabricContext struct {
	baseDir string
	minecraftVsn string
	fabricVsn string
	isClient bool
	tmpDir string
}

func installClientFabric(minecraftVsn, fabricVsn string) (string, error) {
	ctx := fabricContext{
		baseDir: env().MinecraftDir,
		minecraftVsn: minecraftVsn,
		fabricVsn:    fabricVsn,
		isClient:     true,
	}
	return ctx.installFabric()
}

func installServerFabric(minecraftVsn, fabricVsn string, targetDir string) error {
	ctx := fabricContext{
		baseDir: targetDir,
		minecraftVsn: minecraftVsn,
		fabricVsn: fabricVsn,
		isClient: false,
	}
	_, err := ctx.installFabric()
	return err
}


func (ctx fabricContext) fabricId() string {
	return fmt.Sprintf("fabric-loader-%s-%s", ctx.fabricVsn, ctx.minecraftVsn)
}

func (ctx fabricContext) isFabricInstalled() bool {
	if ctx.isClient {
		return fileExists(filepath.Join(ctx.baseDir, "versions", ctx.fabricId(), ctx.fabricId() + ".jar"))
	} else {
		return fileExists(filepath.Join(ctx.baseDir, "fabric-server-launch.jar"))
	}
}

func(ctx fabricContext) installFabric() (string, error) {
	// If fabric is already installed, bail early
	if ctx.isFabricInstalled() {
		logAction("Fabric %s is already available.\n", ctx.fabricVsn)
		return ctx.fabricId(), nil
	}

	// Setup a temp directory that will get cleaned up (for downloads, etc)
	ctx.tmpDir, _ = ioutil.TempDir("", "*-fabricinstall")
	defer os.RemoveAll(ctx.tmpDir)

	// Get the latest fabric-installer URL from maven
	url, err := ctx.getLatestInstallerUrl()
	if err != nil {
		return "", fmt.Errorf("failed to get URL of fabric installer: %+v", err)
	}

	// Download the installer
	installerFilename := filepath.Join(ctx.tmpDir, "fabric-installer.jar")
	err = downloadHttpFile(url, installerFilename)
	if err != nil {
		return "", fmt.Errorf("failed to download fabric installer from %s: %+v", url, err)
	}

	// Setup arguments for the installer
	args := []string{"-Djava.awt.headless=true", "-jar", installerFilename}
	if ctx.isClient {
		args = append(args, "client")
	} else {
		args = append(args, "server", "-downloadMinecraft")
	}

	args = append(args, "-mcversion", ctx.minecraftVsn, "-loader", ctx.fabricVsn)

	// Run the installer!
	// TODO: Investigate if we need to set the path in which to execute installer
	logAction("Running fabric installer for %s\n", ctx.fabricId())
	cmd := exec.Command(javaCmd(), args...)
	if ARG_VERBOSE {
		fmt.Printf("Fabric installer command: %s\n", cmd.String())
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("%s\n", out)
		return "", fmt.Errorf("failed to run fabric installer %s: %+v", ctx.fabricId(), err)
	}

	return ctx.fabricId(), nil
}

func (ctx fabricContext) getLatestInstallerUrl() (string, error) {
	mavenMod, _ := NewMavenModule("net.fabricmc:fabric-installer")
	metadata, err := mavenMod.loadMetadata("https://maven.fabricmc.net")
	if err != nil {
		return "", fmt.Errorf("failed to load fabric installer metadata: %+v", err)
	}

	return mavenMod.toVersionPath("https://maven.fabricmc.net", metadata.VersionInfo.Release, "jar")
}


