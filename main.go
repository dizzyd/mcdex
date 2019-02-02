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
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xeonx/timeago"

	"mcdex/algo"
)

var version string

var ARG_MMC bool
var ARG_VERBOSE bool
var ARG_SKIPMODS bool
var ARG_IGNORE_FAILED_DOWNLOADS bool
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
		Desc:      fmt.Sprintf("Install a mod pack, optionally using a URL to download. Use %s for the directory with a URL to use the name from the downloaded manifest", NamePlaceholder),
		ArgsCount: 1,
		Args:      "<directory/name> [<url>]",
	},
	"pack.show": {
		Fn:        cmdPackShow,
		Desc:      "List all mods included in the specified installed pack",
		ArgsCount: 1,
		Args:      "<directory/name>",
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
		Args:      "<directory/name> <mod name or URL> [<tag>]",
	},
	"mod.select.client": {
		Fn:        cmdModSelectClient,
		Desc:      "Select a client-side only mod to include in the specified pack",
		ArgsCount: 2,
		Args:      "<directory/name> <mod name or URL> [<tag>]",
	},
	"mod.remove.single": {
		Fn:        cmdModRemoveSingle,
		Desc:      "Remove individual mods from the specified pack, without handling dependencies",
		ArgsCount: 2,
		Args:      "<directory/name> <mod name> [mod names...]",
	},
	"mod.remove.recursive": {
		Fn:        cmdModRemoveRecursive,
		Desc:      "Remove specified mods, and all their dependant mods, from the specified pack",
		ArgsCount: 2,
		Args:      "<directory/name> <mod name> [mod names...]",
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
	"openeye.to.manifest": {
		Fn:        cmdOpenEyeToManifest,
		Desc:      "Convert an OpenEye crash dump into a manifest.json",
		ArgsCount: 1,
		Args:      "<OpenEye Crash URL>",
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
	cp, err := NewModPack(dir, false, ARG_MMC)
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
	url := flag.Arg(2)

	db, err := OpenDatabase()
	if err != nil {
		return err
	}

	// If the URL is provided and doesn't actually conform with a URL spec, try to translate
	// by treating as a slug and finding the latest file URL
	if url != "" && !strings.HasPrefix(url, "https://") {
		url, err = db.getLatestPackURL(url)
		if err != nil {
			return err
		}
	}

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
		err = cp.installMods(true, ARG_IGNORE_FAILED_DOWNLOADS)
		if err != nil {
			return err
		}
	}

	return nil
}

func cmdPackShow() error {
	// Try to open the mod pack
	cp, err := NewModPack(flag.Arg(1), true, ARG_MMC)
	if err != nil {
		return err
	}

	db, err := OpenDatabase()
	if err != nil {
		return err
	}

	mods, err := cp.getSelected(db)
	sort.Slice(mods, func(i, j int) bool {return mods[i].name < mods[j].name})
	fmt.Println( "  File ID || Project ID|| Name || Slug || Description || Released || Filename")
	for _, mod := range mods {
		console("%9d || %9d || %s || %s || %s || %v || %s\n", mod.fileID, mod.projectID, mod.name, mod.slug, mod.description, mod.timestamp, mod.filename)
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


func _initRemove(cp *ModPack, modNames []string, maxDepth int) (info *DepInfo, err error) {
	db, err := OpenDatabase()
	if err != nil {
		return
	}

	// Try to lookup the mod IDs by name
	modIDs := make(map[int]struct{}, len(modNames))
	for _, modName := range modNames {
		modID, err := db.findModByName(modName)
		if err != nil {
			log.Println(err)
			continue
		}

		modID, err = cp.lookupFileId(modID)
		if err != nil {
			log.Println(err)
			continue
		}

		modIDs[modID] = struct{}{}
	}

	if len(modIDs) == 0 {
		err = fmt.Errorf("no mods found")
		return
	}

	return _processDeps(cp, db, modIDs, maxDepth)
}

type DepInfo struct {
	targets map[*ManifestFileEntry]struct{}
	dependents map[*ManifestFileEntry][]*ManifestFileEntry
	dependencies map[*ManifestFileEntry][]*ManifestFileEntry
	optionals map[*ManifestFileEntry][]*ManifestFileEntry
}
func _processDeps(cp *ModPack, db *Database, targetModIds map[int]struct{}, maxDepth int) (*DepInfo, error) {
	mods, err := db.buildDepGraph(cp)
	if err != nil {
		return nil, err
	}

	if maxDepth < 0 {
		maxDepth = math.MaxInt32
	}

	relatedMods := make(map[*algo.Node]int)
	result := DepInfo{
		targets:      make(map[*ManifestFileEntry]struct{}, len(targetModIds)),
		dependents:   make(map[*ManifestFileEntry][]*ManifestFileEntry),
		dependencies: make(map[*ManifestFileEntry][]*ManifestFileEntry),
		optionals:    make(map[*ManifestFileEntry][]*ManifestFileEntry),
	}

	// Find all dependents
	{
		deps := make([]*algo.Node, 0, len(targetModIds))
		depths := make([]int, 0, len(targetModIds))
		for _, m := range mods {
			entry := m.Value.(*ManifestFileEntry)
			if _, found := relatedMods[m]; found {
				continue
			}
			if _, found := targetModIds[entry.fileId]; !found {
				continue
			}

			deps = append(deps, m)
			depths = append(depths, 0)
			result.targets[entry] = struct{}{}
		}

		for len(deps) > 0 && depths[0] < maxDepth {
			d := deps[0]
			depth := depths[0]
			deps = deps[1:]
			depths = depths[1:]

			// Already included
			if _, found := relatedMods[d]; found {
				continue
			}

			relatedMods[d] = depth
			for dd := range d.Dependents {
				depths = append(depths, depth+1)
				deps = append(deps, dd)
				r := result.dependents[dd.Value.(*ManifestFileEntry)]
				result.dependents[dd.Value.(*ManifestFileEntry)] = append(r, d.Value.(*ManifestFileEntry))
			}
		}
	}

	// Find all dependencies that will no longer have a dependent
	{
		sorted := mods.Sorted()
	Outer:
		for _, m := range sorted {
			// Only looking at dependencies here, any roots would have been included above
			if m.IsRoot() {
				continue
			}
			// Already included
			if _, found := relatedMods[m]; found {
				continue
			}
			var parents []*ManifestFileEntry
			minDepth := 0
			for d := range m.Dependents {
				depth, found := relatedMods[d]
				if !found || depth >= maxDepth {
					continue Outer // Still has a dependent
				}
				if (depth + 1) < minDepth {
					minDepth = depth + 1
				}
				parents = append(parents, d.Value.(*ManifestFileEntry))
			}

			// All dependents included
			relatedMods[m] = minDepth
			result.dependencies[m.Value.(*ManifestFileEntry)] = parents
		}
	}

	// Find all related optional dependencies
	{
		for _, m := range mods {
			if _, found := relatedMods[m]; found {
				continue
			}
			var opts []*ManifestFileEntry
			for o := range m.Optionals {
				if depth, found := relatedMods[o]; !found || depth >= maxDepth {
					continue
				}

				entry := o.Value.(*ManifestFileEntry)
				if _, found := result.dependencies[entry]; !found {
					opts = append(opts, entry)
				} else {
					// Remove dependency that has an optional dependent
					delete(relatedMods, o)
					delete(result.dependencies, entry)
				}
			}
			if len(opts) > 0 {
				result.optionals[m.Value.(*ManifestFileEntry)] = opts
			}
		}
	}

	return &result, nil
}

func _removeMods(cp *ModPack, mods []*ManifestFileEntry) error {
	if ARG_DRY_RUN {
		return nil
	}

	// Reverse sort by index so we remove from the back
	sort.Slice(mods, func(i, j int) bool {
		return mods[j].idx < mods[i].idx
	})

	fmt.Println()

	var done int
	for _, m := range mods {
		fmt.Printf("Removing [%7d] %q - %q\n", m.fileId, m.name, m.file)
		if err := cp.manifest.ArrayRemove(m.idx, "files"); err != nil {
			log.Printf("Failed to remove mod %q from manifest\n", m.name)
			continue
		}
		done++
		if err := cp.modCache.CleanupModFile(m.projId); err != nil {
			log.Println("Warning: ", err)
		}
	}
	if done > 0 {
		if err := cp.saveManifest(); err != nil {
			return fmt.Errorf("failed to save changes to manifest")
		}
	} else {
		return fmt.Errorf("failed to remove any mods; no changes have been made")
	}
	if done < len(mods) {
		return fmt.Errorf("some mods could not be removed; pack may be in an invalid state")
	}

	return nil
}

func cmdModRemoveSingle() error {
	// Try to open the mod pack
	cp, err := NewModPack(flag.Arg(1), true, ARG_MMC)
	if err != nil {
		return err
	}

	depInfo, err := _initRemove(cp, flag.Args()[2:], 1)
	if err != nil {
		return err
	}

	rmList := make([]*ManifestFileEntry, 0, len(depInfo.targets))

	fmt.Println()
	fmt.Println("Preparing to remove the mod(s):")
	for target := range depInfo.targets {
		fmt.Printf("\t[%7d] %q (%s)\n", target.fileId, target.name, target.file)
		rmList = append(rmList, target)
	}

	fmt.Println()
	fmt.Println("The following mods depend on a mod being removed and will no longer work:")
	for m, d := range depInfo.dependents {
		fmt.Printf("\t[%7d] %q (%s)\n\t\tDepends on %s\n", m.fileId, m.name, m.file, QuoteJoin(d, ", "))
	}

	fmt.Println()
	fmt.Println("The following mods were added as a dependency for a mod being removed and are no longer required:")
	for m, d := range depInfo.dependencies {
		fmt.Printf("\t[%7d] %q (%s)\n\t\tRequired by %s\n", m.fileId, m.name, m.file, QuoteJoin(d, ", "))
	}

	fmt.Println()
	fmt.Println("The following mods optionally depend on a mod being removed:")
	for d, o := range depInfo.optionals {
		fmt.Printf("\t[%7d] %q optionally depends on %s\n", d.fileId, d.name, QuoteJoin(o, ", "))
	}

	return _removeMods(cp, rmList)
}

func cmdModRemoveRecursive() error {
	// Try to open the mod pack
	cp, err := NewModPack(flag.Arg(1), true, ARG_MMC)
	if err != nil {
		return err
	}

	depInfo, err := _initRemove(cp, flag.Args()[2:], -1)
	if err != nil {
		return err
	}

	rmList := make([]*ManifestFileEntry, 0, len(depInfo.targets)+len(depInfo.dependencies)+len(depInfo.dependents))

	fmt.Println()
	fmt.Println("Preparing to remove the mod(s):")
	for target := range depInfo.targets {
		fmt.Printf("\t[%7d] %q (%s)\n", target.fileId, target.name, target.file)
		rmList = append(rmList, target)
	}

	fmt.Println()
	fmt.Println("The following dependent mods will also be removed:")
	for m, d := range depInfo.dependents {
		fmt.Printf("\t[%7d] %q (%s)\n\t\tDepends on %s\n", m.fileId, m.name, m.file, QuoteJoin(d,", "))
		rmList = append(rmList, m)
	}

	fmt.Println()
	fmt.Println("The following dependencies will also be removed:")
	for m, d := range depInfo.dependencies {
		fmt.Printf("\t[%7d] %q (%s)\n\t\tRequired by %s\n", m.fileId, m.name, m.file, QuoteJoin(d, ", "))
		rmList = append(rmList, m)
	}

	fmt.Println()
	fmt.Println("The following mods optionally depend on a mod being removed:")
	for d, o := range depInfo.optionals {
		fmt.Printf("\t[%7d] %q optionally depends on %s\n", d.fileId, d.name, QuoteJoin(o, ", "))
	}

	return _removeMods(cp, rmList)
}


var curseForgeRegex = regexp.MustCompile("/projects/([\\w-]*)(/files/(\\d+))?")

func _modSelect(dir, mod, tag string, clientOnly bool) error {
	// Try to open the mod pack
	cp, err := NewModPack(dir, true, ARG_MMC)
	if err != nil {
		return err
	}

	db, err := OpenDatabase()
	if err != nil {
		return err
	}

	var modID int
	var fileID int

	// Try to parse the mod as a URL
	url, err := url.Parse(mod)
	if err == nil && (url.Scheme == "http" || url.Scheme == "https") {
		// We have a URL; if it's not a CurseForge URL, treat it as an external file
		if url.Host != "minecraft.curseforge.com" {
			return cp.selectModURL(mod, tag, clientOnly)
		}

		// Otherwise, try to parse the project name & file ID out of the URL path
		parts := curseForgeRegex.FindStringSubmatch(url.Path)
		if len(parts) < 4 {
			// Unknown structure on the CurseForge path; bail
			return fmt.Errorf("invalid CurseForge URL")
		}

		modSlug := parts[1]
		fileID, _ = strconv.Atoi(parts[3])

		// Lookup the modID using the slug in a URL
		modID, err = db.findModBySlug("https://minecraft.curseforge.com/projects/" + modSlug)
		if err != nil {
			return err
		}
	} else {
		// Try to lookup the mod ID by name
		modID, err = db.findModByName(mod)
		if err != nil {
			return err
		}
	}

	err = _selectModFromID(cp, db, modID, fileID, clientOnly)
	if err == nil && !ARG_DRY_RUN {
		return cp.saveManifest()
	}

	return err
}

func _selectModFromID(pack *ModPack, db *Database, modID, fileID int, clientOnly bool) error {
	if modFile, err := _lookupModByID(pack, db, modID, fileID); err == nil {
		err := pack.selectModFile(modFile, clientOnly)
		if err != nil {
			return err
		}

		deps, err := db.getDeps(modFile.fileID)
		if err != nil {
			return fmt.Errorf("Error pulling deps for %d: %+v", modFile.fileID, err)
		}

		for _, dep := range deps {
			err = _selectModFromID(pack, db, dep, 0, clientOnly)
			if err != nil {
				return err
			}
		}

		return nil
	} else {
		return err
	}
}

func _lookupModByID(pack *ModPack, db *Database, modID, fileID int) (*ModFile, error) {
	// At this point, we should have a modID and we may have a fileID. We want to walk major.minor.[patch]
	// versions, and find either the latest file for our version of minecraft or verify that the fileID
	// we have will work on this version
	major, minor, patch, err := parseVersion(pack.minecraftVersion())
	if err != nil {
		// Invalid version string?!
		return nil, err
	}

	// Walk down patch versions, looking for our mod + file (or latest file if no fileID available)
	for i := patch; i > -1; i-- {
		var vsn string
		if i > 0 {
			vsn = fmt.Sprintf("%d.%d.%d", major, minor, i)
		} else {
			vsn = fmt.Sprintf("%d.%d", major, minor)
		}

		if modFile, err := db.findModFile(modID, fileID, vsn); err == nil {
			return modFile, nil
		}
	}

	return nil, fmt.Errorf("No compatible file found for %d\n", modID)
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

	cp, err := NewModPack(dir, true, ARG_MMC)
	if err != nil {
		return err
	}

	db, err := OpenDatabase()
	if err != nil {
		return err
	}

	err = cp.updateMods(db, ARG_DRY_RUN)
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
	err = cp.installMods(false, ARG_IGNORE_FAILED_DOWNLOADS)
	if err != nil {
		return err
	}

	return nil
}

func cmdDBUpdate() error {
	err := InstallDatabase()
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

func cmdOpenEyeToManifest() error {
	url := flag.Arg(1)

	if !strings.Contains(url, "/browse/raw/crashes") {
		return fmt.Errorf("Please provide the raw crash data URL")
	}

	crashData, err := getJSONFromURL(url)
	if err != nil {
		return err
	}

	db, err := OpenDatabase()
	if err != nil {
		return err
	}

	modPack, err := NewModPack(".", false, false)
	if err != nil {
		return err
	}

	// Lookup the minecraft version that corresponds with this version of Forge
	forgeVsn := crashData.Path("forge").Index(0).Data().(string)
	mcVsn, err := db.lookupMcVsn(forgeVsn)
	if err != nil {
		return err
	}

	err = modPack.createManifest("CrashGen", mcVsn, forgeVsn)
	if err != nil {
		return err
	}

	modPack.manifest.Set(url, "url")
	modPack.manifest.Array("skipped")

	// Retrieve all the individual file descriptors
	sigs, _ := crashData.Path("allSignatures").Children()
	for _, sig := range sigs {
		fileData, err := getJSONFromURL(fmt.Sprintf("https://openeye.openmods.info/browse/raw/files/%s", sig.Data().(string)))
		if err != nil {
			fmt.Printf("Error retrieving %s: %+s\n", sig, err)
			continue
		}

		// Get the mod name (if available) and use that to find a mod in the database
		modNames, _ := fileData.Path("mods.name").Children()
		for _, nameData := range modNames {
			name := nameData.Data().(string)
			modID, _ := db.findModByName(name)
			if modID > 0 {
				f, err := db.getLatestModFile(modID, mcVsn)
				if err != nil {
					modPack.manifest.ArrayAppend(name, "skipped")
					continue
				}
				modPack.selectModFile(f, false)
			} else {
				modPack.manifest.ArrayAppend(name, "skipped")
				fmt.Printf("Skipping unknown mod: %s\n", name)
			}
		}
	}

	return modPack.saveManifest()
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
	flag.BoolVar(&ARG_IGNORE_FAILED_DOWNLOADS, "ignore", false, "Ignore failed mod downloads when installing a pack")
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

	if ARG_DRY_RUN {
		fmt.Printf("--- DRY RUN ---\n")
	}

	err = command.Fn()
	if err != nil {
		log.Fatalf("%+v\n", err)
	}
}
