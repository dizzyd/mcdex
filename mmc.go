package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/Jeffail/gabs"
)

const MMC_CONFIG = `InstanceType=OneSix
iconKey=flame
name=%s
`
const InstanceDirKey = "InstanceDir="

func _mmcInstancesDir() (string, error) {
	// Default if not found in config file
	dir := "instances"

	if env().MultiMCDir == "" {
		return "", errors.New("MultiMC directory is not set")
	}

	cfg, err := ioutil.ReadFile(filepath.Join(env().MultiMCDir, "multimc.cfg"))
	if err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(cfg)))
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, InstanceDirKey) {
			dir = strings.TrimSpace(line[len(InstanceDirKey):])
			break
		}
	}

	if !filepath.IsAbs(dir) {
		dir = filepath.Join(env().MultiMCDir, dir)
	}

	return dir, err
}

func generateMMCConfig(pack *ModPack) error {
	fmt.Printf("Generating instance.cfg for MultiMC\n")
	cfg := fmt.Sprintf(MMC_CONFIG, pack.fullName())

	// Write it out
	if err := ioutil.WriteFile(filepath.Join(pack.rootPath, "instance.cfg"), []byte(cfg), 0644); err != nil {
		return fmt.Errorf("failed to save instance.cfg: %+v", err)
	}

	minecraftVsn, forgeVsn := pack.getVersions()
	fmt.Printf("Generating mmc-pack.json for MultiMC\n")
	mmcpack := gabs.New()
	_, _ = mmcpack.Array("components")
	_ = mmcpack.ArrayAppend(map[string]interface{}{
		"important": true,
		"uid":       "net.minecraft",
		"version":   minecraftVsn,
	}, "components")
	_ = mmcpack.ArrayAppend(map[string]interface{}{
		"uid":     "net.minecraftforge",
		"version": forgeVsn,
	}, "components")
	_, _ = mmcpack.Set(1, "formatVersion")

	if err := writeJSON(mmcpack, filepath.Join(pack.rootPath, "mmc-pack.json")); err != nil {
		return fmt.Errorf("failed to save mmc-pack.json: %+v", err)
	}

	return nil
}
