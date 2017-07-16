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
	"bufio"
	"bytes"
	"compress/bzip2"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"regexp"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	sqlDb     *sql.DB
	sqlDbPath string
}

func OpenDatabase() (*Database, error) {
	db := new(Database)

	db.sqlDbPath = filepath.Join(env().McdexDir, "mcdex.dat")
	if !fileExists(db.sqlDbPath) {
		return nil, fmt.Errorf("No database available; use updateDB command first")
	}

	sqlDb, err := sql.Open("sqlite3", db.sqlDbPath)
	if err != nil {
		return nil, err
	}

	db.sqlDb = sqlDb

	return db, nil
}

func InstallDatabase() error {
	// Get the latest version
	version, err := getLatestVersion()
	if err != nil {
		return err
	}

	// Download the latest data file to mcdex/mcdex.dat
	url := fmt.Sprintf("http://files.mcdex.net/data/mcdex-%s.dat.bz2", version)
	res, err := HttpGet(url)
	if err != nil {
		return fmt.Errorf("Failed to retrieve %s data file: %+v", err)
	}
	defer res.Body.Close()

	// Stream the data file to mcdex.dat.tmp
	tmpFileName := filepath.Join(env().McdexDir, "mcdex.dat.tmp")
	tmpFile, err := os.Create(tmpFileName)
	if err != nil {
		return fmt.Errorf("Failed to create mcdex.dat.tmp: %+v", err)
	}
	defer tmpFile.Close()

	writer := bufio.NewWriter(tmpFile)

	_, err = io.Copy(writer, bzip2.NewReader(res.Body))
	if err != nil {
		return fmt.Errorf("Failed to write mcdex.dat.tmp: %+v", err)
	}
	writer.Flush()

	// Open the temporary database and validate it
	tmpDb, err := sql.Open("sqlite3", tmpFileName)
	if err != nil {
		// TODO: Add log entry about the file being corrupt
		return nil
	}
	defer tmpDb.Close()

	_, err = tmpDb.Exec("PRAGMA integrity_check;")
	if err != nil {
		tmpDb.Close()
		return nil
	}

	// Close the database and rename the tmp file
	err = os.Rename(tmpFileName, filepath.Join(env().McdexDir, "mcdex.dat"))
	if err != nil {
		return fmt.Errorf("Failed to rename mcdex.dat.tmp: %+v", err)
	}

	fmt.Printf("Updated mod database.\n")

	return nil
}

func getLatestVersion() (string, error) {
	res, e := HttpGet("http://files.mcdex.net/data/latest")
	if e != nil {
		return "", fmt.Errorf("Failed to retrieve data/latest: %+v", e)
	}
	defer res.Body.Close()

	// Dump the body into a string
	buf := new(bytes.Buffer)
	buf.ReadFrom(res.Body)
	return strings.TrimSpace(buf.String()), nil
}

func (db *Database) listMods(name, mcvsn string) error {
	// Turn the name into a pre-compiled regex
	nameRegex, err := regexp.Compile(name)
	if err != nil {
		return fmt.Errorf("Failed to convert %s into regex: %s", name, err)
	}

	rows, err := db.sqlDb.Query("select name, description from mods where rowid in (select modid from filevsns where version = ?);", mcvsn)
	if err != nil {
		return fmt.Errorf("Query failed: %+v", err)
	}
	defer rows.Close()

	// For each row, check the name against the pre-compiled regex
	for rows.Next() {
		var modName, modDesc string
		err = rows.Scan(&modName, &modDesc)
		if err != nil {
			return err
		}

		if nameRegex.MatchString(modName) {
			fmt.Printf("%s - %s\n", modName, modDesc)
		}
	}

	return nil
}

func (db *Database) findModFile(name, mcvsn string) (string, error) {
	// First, look up the modid for the given name
	var modid int
	err := db.sqlDb.QueryRow("select rowid from mods where name = ?", name).Scan(&modid)
	switch {
	case err == sql.ErrNoRows:
		return "", fmt.Errorf("No mod found %s", name)
	case err != nil:
		return "", err
	}

	// Now find the latest release or beta version
	var url string
	err = db.sqlDb.QueryRow("select url from files where rowid in (select fileid from filevsns where modid=? and version=? order by tstamp desc limit 1)",
		modid, mcvsn).Scan(&url)
	switch {
	case err == sql.ErrNoRows:
		return "", fmt.Errorf("No file found for %s on Minecraft %s", name, mcvsn)
	case err != nil:
		return "", err
	}
	return url, nil
}
