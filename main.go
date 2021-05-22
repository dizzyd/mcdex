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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/xeonx/timeago"
)

var version string

var ARG_MMC bool
var ARG_VERBOSE bool
var ARG_SKIPMODS bool
var ARG_DRY_RUN bool

type command struct {
	Fn        func() error
	Desc      string
	ArgsCount int
	Args      string
}

var gCommands = map[string]command{
	"pack.create": {
		Fn:        cmdPackCreate,
		Desc:      "Create a new mod pack",
		ArgsCount: 2,
		Args:      "<directory/name> <minecraft version> [<forge version>]",
	},
	"pack.list": {
		Fn:        cmdPackList,
		Desc:      "List available mod packs",
		ArgsCount: 0,
		Args:      "[<pack name> <minecraft version>]",
	},
	"pack.list.latest": {
		Fn:        cmdPackListLatest,
		Desc:      "List most recently updated packs",
		ArgsCount: 0,
		Args:      "[<minecraft version>]",
	},
	"pack.install": {
		Fn:        cmdPackInstall,
		Desc:      fmt.Sprintf("Install a mod pack, using either a manifest in the provided folder, or using  slug or project ID.\nOptionally using a fileID to specify a specific version of the mod pack to download.\nUse %s for the directory with a slug/projectID to use the name from the downloaded manifest", NamePlaceholder),
		ArgsCount: 1,
		Args:      "<directory/name> [<slug/projectID> <fileID>]",
	},
	"info": {
		Fn:        cmdInfo,
		Desc:      "Show runtime info",
		ArgsCount: 0,
	},
	"mod.list": {
		Fn:        cmdModList,
		Desc:      "List mods matching a name and Minecraft version",
		ArgsCount: 0,
		Args:      "[<mod name> <minecraft version>]",
	},
	"mod.list.latest": {
		Fn:        cmdModListLatest,
		Desc:      "List most recently updated mods",
		ArgsCount: 0,
		Args:      "[<minecraft version>]",
	},
	"mod.select": {
		Fn:        cmdModSelect,
		Desc:      "Select a mod to include in the specified pack",
		ArgsCount: 2,
		Args:      "<directory/name> <mod name or maven artifact ID> [<URL>]",
	},
	"mod.select.client": {
		Fn:        cmdModSelectClient,
		Desc:      "Select a client-side only mod to include in the specified pack",
		ArgsCount: 2,
		Args:      "<directory/name> <mod name or maven artifact ID> [<URL>]",
	},
	"mod.update.all": {
		Fn:        cmdModUpdateAll,
		Desc:      "Update all mods entries to latest available file",
		ArgsCount: 1,
		Args:      "<directory/name>",
	},
	"server.install": {
		Fn:        cmdServerInstall,
		Desc:      "Install a Minecraft server using an existing pack",
		ArgsCount: 1,
		Args:      "<directory/name>",
	},
	"db.update": {
		Fn:        cmdDBUpdate,
		Desc:      "Update local database of available mods",
		ArgsCount: 0,
	},
	"forge.list": {
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

	if dir == NamePlaceholder {
		return fmt.Errorf("%q is not allowed for the directory when creating a new pack", NamePlaceholder)
	}

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
	cp, err := NewModPack(dir, 0, ARG_MMC)
	if err != nil {
		return err
	}

	// Create the manifest for this new pack
	err = cp.createManifest(cp.name, minecraftVsn, forgeVsn)
	if err != nil {
		return err
	}

	// If the -mmc flag is provided, don't create a launcher profile; just generate
	// an instance.cfg for MultiMC to use
	if ARG_MMC {
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

	return nil
}

func cmdPackInstall() error {
	dir := flag.Arg(1)
	slug := flag.Arg(2)
	fileID := flag.Arg(3)
	url := ""

	db, err := OpenDatabase()
	if err != nil {
		return err
	}

	if slug != "" {
		url, err = db.getLatestPackURL(slug, fileID)
		if err != nil {
			return err
		}
	}

	// Only require a manifest if we're not installing from a URL
	requireManifest := 0
	if url == "" {
		requireManifest = 2
	}

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
	publishedVsn, err := readStringFromUrl("http://files.mcdex.net/release/latest")

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
	fmt.Printf("* MultiMC dir: %s\n", env().MultiMCDir)
	fmt.Printf("* mcdex dir: %s\n", env().McdexDir)
	fmt.Printf("* Java dir: %s\n", env().JavaDir)
	return nil
}

func cmdModSelect() error {
	return _modSelect(flag.Arg(1), flag.Arg(2), flag.Arg(3), false)
}

func cmdModSelectClient() error {
	return _modSelect(flag.Arg(1), flag.Arg(2), flag.Arg(3), true)
}

var curseForgeRegex = regexp.MustCompile("/projects/([\\w-]*)(/files/(\\d+))?")

func _modSelect(dir, modId, url string, clientOnly bool) error {
	// Try to open the mod pack
	cp, err := NewModPack(dir, 1, ARG_MMC)
	if err != nil {
		return err
	}

	// First, try to select the mod using Maven
	err = SelectMavenModFile(cp, modId, url, clientOnly)
	if err != nil {
		// Hmm, not a maven-based mod; let's try as a CurseForge mod
		err = SelectCurseForgeModFile(cp, modId, url, clientOnly)
		if err != nil {
			return err
		}
	}

	return cp.saveManifest()
}

func listProjects(ptype int) error {
	name := flag.Arg(1)
	mcvsn := flag.Arg(2)

	db, err := OpenDatabase()
	if err != nil {
		return err
	}

	return db.printProjects(name, mcvsn, ptype)
}

func cmdModList() error {
	return listProjects(0)
}

func cmdPackList() error {
	return listProjects(1)
}

func listLatestProjects(ptype int) error {
	mcvsn := flag.Arg(1)

	db, err := OpenDatabase()
	if err != nil {
		return err
	}

	return db.printLatestProjects(mcvsn, ptype)
}

func cmdModListLatest() error {
	return listLatestProjects(0)
}

func cmdPackListLatest() error {
	return listLatestProjects(1)
}

func cmdModUpdateAll() error {
	dir := flag.Arg(1)

	cp, err := NewModPack(dir, 1, ARG_MMC)
	if err != nil {
		return err
	}

	err = cp.updateMods(ARG_DRY_RUN)
	if err != nil {
		return err
	}

	return nil
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
	cp, err := NewModPack(dir, 1, false)
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
}

func cmdDBUpdate() error {
	err := InstallDatabase(false)
	if err != nil {
		return err
	}

	// Display last updated file in database (simple way to know how recent a file we have)
	db, err := OpenDatabase()
	if err != nil {
		return err
	}

	tstamp, err := db.getLatestFileTstamp()
	if err != nil {
		return err
	}

	elapsed := time.Unix(int64(tstamp), 0)
	elapsedFriendly := timeago.English.Format(elapsed)

	fmt.Printf("Database up-to-date as of %s (%s)\n", elapsedFriendly, elapsed)
	return nil
}

func console(f string, args ...interface{}) {
	fmt.Printf(f, args...)
}

func usage() {
	console("usage: mcdex [<options>] <command> [<args>]\n")
	console("<options>\n")
	flag.PrintDefaults()
	console("\n<commands>\n")

	// Sort the list of keys in gCommands
	keys := []string{}
	for k := range gCommands {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, cmd := range keys {
		console("  - %s: %s\n", cmd, gCommands[cmd].Desc)
	}
}

type StrValue struct {
	isSet bool
	value string
}

func (v *StrValue) String() string {
	return v.value
}
func (v *StrValue) Set(val string) error {
	v.isSet = true
	v.value = val
	return nil
}

func main() {
	mcDir := StrValue{
		isSet: false,
		value: _minecraftDir(),
	}

	// Look for MultiMC on the path
	var mmcDir string
	if path, err := exec.LookPath("MultiMC"); err == nil {
		if path, err := filepath.EvalSymlinks(path); err == nil {
			mmcDir = filepath.Dir(path)
		}
	}

	// Register
	flag.BoolVar(&ARG_MMC, "mmc", false, "Generate MultiMC instance.cfg when installing a pack")
	flag.StringVar(&mmcDir, "mmcdir", mmcDir, "Path to directory containing MultiMC executable.")
	flag.Var(&mcDir, "mcdir", "Minecraft home folder to use. If -mmc is used, will use the value of -mmcdir as the default.")
	flag.BoolVar(&ARG_VERBOSE, "v", false, "Enable verbose logging of operations")
	flag.BoolVar(&ARG_SKIPMODS, "skipmods", false, "Skip download of mods when installing a pack")
	flag.BoolVar(&ARG_DRY_RUN, "n", false, "Dry run; don't save any changes to manifest")

	// Process command-line args
	flag.Parse()
	if !flag.Parsed() || flag.NArg() < 1 {
		usage()
		os.Exit(-1)
	}

	if ARG_MMC {
		if mmcDir == "" {
			log.Fatal("-mmc specified, but could not find MultiMC executable! Set MultiMC directory using -mmcdir")
		}
		if _, err := exec.LookPath(filepath.Join(mmcDir, "MultiMC")); err != nil {
			log.Fatalf("Invalid MultiMC path specified: %s", mmcDir)
		}
		if !mcDir.isSet {
			_ = mcDir.Set(mmcDir)
		}
	}
	envData.MinecraftDir = mcDir.String()
	envData.MultiMCDir = mmcDir

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
