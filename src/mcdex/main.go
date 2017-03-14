package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

type command struct {
	Fn    func() error
	Usage string
}

var gCommands = map[string]command{
	"installPack": command{
		Fn:    cmdInstallPack,
		Usage: "Install a mod pack",
	},
	"update": command{
		Fn:    cmdUpdate,
		Usage: "Download latest index",
	},
}

func cmdInstallPack() error {
	// If there are not enough arguments, bail
	if flag.NArg() < 3 {
		return fmt.Errorf("Insufficient arguments")
	}

	// Get ZIP file
	cp, err := NewCursePack(flag.Arg(1), flag.Arg(2))
	if err != nil {
		return err
	}

	// Download the pack
	err = cp.download()
	if err != nil {
		return err
	}

	fmt.Printf("Processing manifest!\n")

	// Process manifest
	err = cp.processManifest()
	if err != nil {
		return err
	}

	// Create launcher profile
	err = cp.createLauncherProfile()
	if err != nil {
		return err
	}

	// Install overrides
	return nil
}

func cmdUpdate() error {
	db, err := NewDatabase()
	if err != nil {
		log.Fatalf("%+v\n", err)
	}

	return db.Download()
}

func console(f string, args ...interface{}) {
	fmt.Printf(f, args...)
}

func usage() {
	console("usage: mcdex [<options>] <command> [<args>]\n")
	// console(" options:\n")
	// flag.PrintDefaults()
	console(" commands:\n")
	for id, cmd := range gCommands {
		console(" - %s: %s\n", id, cmd.Usage)
	}
}

func main() {
	// Make sure the mcdex directory exists
	os.Mkdir(McdexDir(), 0700)

	flag.Parse()
	if !flag.Parsed() || flag.NArg() < 1 {
		usage()
		os.Exit(-1)
	}

	command, exists := gCommands[flag.Arg(0)]
	if !exists {
		console("ERROR: unknown command '%s'\n", flag.Arg(0))
		usage()
		os.Exit(-1)
	}

	err := command.Fn()
	if err != nil {
		log.Fatalf("%+v\n", err)
	}

	// lconfig, err := NewLauncherConfig()
	// if err != nil {
	// 	log.Fatalf("Failed to load launcher_profiles.json: %+v\n", err)
	// }
	// lconfig.CreateProfile("test", "1.10.2")
	// lconfig.Save()
	// fmt.Printf("%s", lconfig.data.StringIndent("", "  "))
}

//mcdex update - download latest mcdex.sqlite
//mcdex forge.install <name> [<vsn>]
//mcdex forge.list

//mcdex init <name> <vsn> <desc>
//mcdex install <modname> [<vsn>]
