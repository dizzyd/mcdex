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
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
)

var version string

var ARG_MMC bool
var ARG_VERBOSE bool
var ARG_SKIPMODS bool

type command struct {
	Fn        func() error
	Desc      string
	ArgsCount int
	Args      string
}

var gCommands = map[string]command{
	"pack.create": command{
		Fn:        cmdPackCreate,
		Desc:      "Create a new mod pack",
		ArgsCount: 2,
		Args:      "<directory> <minecraft version> [<forge version>]",
	},
	"pack.install": command{
		Fn:        cmdPackInstall,
		Desc:      "Install a mod pack, optionally using a URL to download",
		ArgsCount: 1,
		Args:      "<directory> [<url>]",
	},
	"info": command{
		Fn:        cmdInfo,
		Desc:      "Show runtime info",
		ArgsCount: 0,
	},
	"mod.list": command{
		Fn:        cmdModList,
		Desc:      "List mods matching a name and Minecraft version",
		ArgsCount: 1,
		Args:      "<mod name> [<minecraft version>]",
	},
	"mod.select": command{
		Fn:        cmdModSelect,
		Desc:      "Select a mod to include in the specified pack",
		ArgsCount: 2,
		Args:      "<directory> <mod name or URL> [<tag>]",
	},
	"mod.select.client": command{
		Fn:        cmdModSelectClient,
		Desc:      "Select a client-side only mod to include in the specified pack",
		ArgsCount: 2,
		Args:      "<directory> <mod name or URL> [<tag>]",
	},
	"server.install": command{
		Fn:        cmdServerInstall,
		Desc:      "Install a Minecraft server using an existing pack",
		ArgsCount: 1,
		Args:      "<directory>",
	},
	"db.update": command{
		Fn:        cmdDBUpdate,
		Desc:      "Update local database of available mods",
		ArgsCount: 0,
	},
	"forge.list": command{
		Fn:        cmdForgeList,
		Desc:      "List available versions of Forge",
		ArgsCount: 1,
		Args:      "<minecraft version>",
	},
}

func cmdPackCreate() error {
	dir := flag.Arg(1)
	minecraftVsn := flag.Arg(2)
	forgeVsn := flag.Arg(3)

	// If no forge version was specified, open the database and find
	// a recommended one
	if forgeVsn == "" {
		db, err := OpenDatabase()
		if err != nil {
			return err
		}

		forgeVsn, err = db.lookupForgeVsn(minecraftVsn)
		if err != nil {
			return err
		}
	}

	// Create a new pack directory
	cp, err := NewModPack(dir, false, ARG_MMC)
	if err != nil {
		return err
	}

	// Create the manifest for this new pack
	err = cp.createManifest(dir, minecraftVsn, forgeVsn)
	if err != nil {
		return err
	}

	// Create the launcher profile (and install forge if necessary)
	err = cp.createLauncherProfile()
	if err != nil {
		return err
	}

	return nil
}

func cmdPackInstall() error {
	dir := flag.Arg(1)
	url := flag.Arg(2)

	// Only require a manifest if we're not installing from a URL
	requireManifest := (url == "")

	cp, err := NewModPack(dir, requireManifest, ARG_MMC)
	if err != nil {
		return err
	}

	if url != "" {
		// Download the pack
		err = cp.download(url)
		if err != nil {
			return err
		}

		// Process manifest
		err = cp.processManifest()
		if err != nil {
			return err
		}

		// Install overrides from the modpack; this is a bit of a misnomer since
		// under usual circumstances there are no mods in the modpack file that
		// will be also be downloaded
		err = cp.installOverrides()
		if err != nil {
			return err
		}
	}

	// If the -mmc flag is provided, don't create a launcher profile; just generate
	// an instance.cfg for MultiMC to use
	if ARG_MMC == true {
		err = cp.generateMMCConfig()
		if err != nil {
			return err
		}
	} else {
		// Create launcher profile
		err = cp.createLauncherProfile()
		if err != nil {
			return err
		}
	}

	if ARG_SKIPMODS == false {
		// Install mods (include client-side only mods)
		err = cp.installMods(true)
		if err != nil {
			return err
		}
	}

	return nil
}

func cmdInfo() error {
	// Try to retrieve the latest available version info
	publishedVsn, err := getLatestVersion("release")

	if err != nil && ARG_VERBOSE {
		fmt.Printf("%s\n", err)
	}

	if err == nil && publishedVsn != "" && version != publishedVsn {
		fmt.Printf("Version: %s (%s is available for download)\n", version, publishedVsn)
	} else {
		fmt.Printf("Version: %s\n", version)
	}

	// Print the environment
	fmt.Printf("Environment:\n")
	fmt.Printf("* Minecraft dir: %s\n", env().MinecraftDir)
	fmt.Printf("* mcdex dir: %s\n", env().McdexDir)
	fmt.Printf("* Java dir: %s\n", env().JavaDir)
	return nil
}

func cmdModSelect() error {
	return _modSelect(false)
}

func cmdModSelectClient() error {
	return _modSelect(true)
}

func _modSelect(clientOnly bool) error {
	dir := flag.Arg(1)
	mod := flag.Arg(2)
	tag := flag.Arg(3)

	// Try to open the mod pack
	cp, err := NewModPack(dir, true, ARG_MMC)
	if err != nil {
		return err
	}

	// If the mod doesn't start with https://, assume it's a name and try to look it up
	if !strings.HasPrefix(mod, "https://") {
		db, err := OpenDatabase()
		if err != nil {
			return err
		}

		// Get the primary and secondary versions (e.g. if mcVersion is 1.12.2, we want to check first
		// for a mod with 1.12.2 and then fallback to 1.12)
		primaryVsn, secondaryVsn := parseVersion(cp.minecraftVersion())

		// First, look for mod with primary version
		modFile, err := db.findModFile(mod, primaryVsn)
		if err != nil {
			// Try again with secondary version
			modFile, err = db.findModFile(mod, secondaryVsn)
			if err != nil {
				return err
			}
		}
		return cp.selectModFile(modFile, clientOnly)

	} else if !strings.Contains(mod, "minecraft.curseforge.com") && tag == "" {
		return fmt.Errorf("Non-CurseForge URLs must include a tag argument")
	} else {
		return cp.selectModURL(mod, tag, clientOnly)
	}
}

func cmdModList() error {
	name := flag.Arg(1)
	mcvsn := flag.Arg(2)

	db, err := OpenDatabase()
	if err != nil {
		return err
	}

	return db.listMods(name, mcvsn)
}

func cmdForgeList() error {
	mcvsn := flag.Arg(1)

	db, err := OpenDatabase()
	if err != nil {
		return err
	}

	return db.listForge(mcvsn, ARG_VERBOSE)
}

func cmdServerInstall() error {
	dir := flag.Arg(1)

	if ARG_MMC == true {
		return fmt.Errorf("-mmc arg not supported when installing a server")
	}

	// Open the pack; we require the manifest and any
	// config files to already be present
	cp, err := NewModPack(dir, true, false)
	if err != nil {
		return err
	}

	// Install the server jar, Forge and dependencies
	err = cp.installServer()
	if err != nil {
		return err
	}

	// Make sure all mods are installed (do NOT include client-side only)
	err = cp.installMods(false)
	if err != nil {
		return err
	}

	return nil
	// Setup the command-line
	// java -jar <forge.jar>
}

func cmdDBUpdate() error {
	return InstallDatabase()
}

func console(f string, args ...interface{}) {
	fmt.Printf(f, args...)
}

func usage() {
	console("usage: mcdex [<options>] <command> [<args>]\n")
	console(" commands:\n")

	// Sort the list of keys in gCommands
	keys := []string{}
	for k := range gCommands {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, cmd := range keys {
		console(" - %s: %s\n", cmd, gCommands[cmd].Desc)
	}
}

func main() {
	// Register
	flag.BoolVar(&ARG_MMC, "mmc", false, "Generate MultiMC instance.cfg when installing a pack")
	flag.BoolVar(&ARG_VERBOSE, "v", false, "Enable verbose logging of operations")
	flag.BoolVar(&ARG_SKIPMODS, "skipmods", false, "Skip download of mods when installing a pack")

	// Process command-line args
	flag.Parse()
	if !flag.Parsed() || flag.NArg() < 1 {
		usage()
		os.Exit(-1)
	}

	// Initialize our environment
	err := initEnv()
	if err != nil {
		log.Fatalf("Failed to initialize: %s\n", err)
	}

	commandName := flag.Arg(0)
	command, exists := gCommands[commandName]
	if !exists {
		console("ERROR: unknown command '%s'\n", commandName)
		usage()
		os.Exit(-1)
	}

	// Check that the required number of arguments is present
	if flag.NArg() < command.ArgsCount+1 {
		console("ERROR: insufficient arguments for %s\n", commandName)
		console("usage: mcdex %s %s\n", commandName, command.Args)
		os.Exit(-1)
	}

	err = command.Fn()
	if err != nil {
		log.Fatalf("%+v\n", err)
	}
}
