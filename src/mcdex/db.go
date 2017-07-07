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

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	sqlDb     *sql.DB
	sqlDbPath string
	version   string
}

func NewDatabase() (*Database, error) {
	db := new(Database)

	db.sqlDbPath = filepath.Join(env().McdexDir, "mcdex.dat")

	sqlDb, err := sql.Open("sqlite3", db.sqlDbPath)
	if err != nil {
		return nil, err
	}

	db.sqlDb = sqlDb

	return db, nil
}

func (db *Database) Download() error {
	// Get the latest version
	version, err := getLatestVersion()
	if err != nil {
		return err
	}

	// Compare the latest against current; if it's the same or earlier noop
	if version <= db.version {
		return nil
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
