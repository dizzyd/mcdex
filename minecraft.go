package main

import (
	"fmt"
	"path"
	"path/filepath"

	"github.com/Jeffail/gabs"
)

const GLOBAL_MANIFEST = "https://launchermeta.mojang.com/mc/game/version_manifest.json"

// Install (if necessary) the minecraft JAR file of the requested version and type (client, server)
func installMinecraftJar(version string, isClient bool, baseDir string) (string, error) {
	// First, check to see if a JAR is present in versions/<vsn>/<vsn>.jar (client) or in base
	// directory for servers
	var filename string
	if isClient {
		filename = filepath.Join(baseDir, "versions", version, version+".jar")
	} else {
		filename = filepath.Join(baseDir, fmt.Sprintf("minecraft_server.%s.jar", version))
	}

	if fileExists(filename) {
		return filename, nil
	}

	// JAR doesn't exist; grab the global index and the version specific manifest
	globalManifest, err := getJSONFromURL(GLOBAL_MANIFEST)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve global manifest: %+v", err)
	}

	// Find the object associated with the version and retrieve the version manifest
	var manifest *gabs.Container
	versionObjs, _ := globalManifest.Path("versions").Children()
	for _, versionObj := range versionObjs {
		if versionObj.Path("id").Data().(string) == version {
			manifest, err = getJSONFromURL(versionObj.Path("url").Data().(string))
			if err != nil {
				return "", fmt.Errorf("failed to retrieve manifest for %s: %+v", version, err)
			}
			break
		}
	}

	// Search came up empty
	if manifest == nil {
		return "", fmt.Errorf("failed to find a manifest for %s", version)
	}

	// Grab the appropriate URL from the version manifest
	key := "client"
	if !isClient {
		key = "server"
	}
	url := manifest.Path("downloads." + key + ".url").Data().(string)

	// Download the version into appropriate place
	logAction("Downloading %s: %s\n", path.Base(filename), url)
	err = downloadHttpFile(url, filename)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve URL for %s: %+v", version, err)
	}

	return filename, nil
}
