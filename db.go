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
	"compress/bzip2"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"

	_ "github.com/mattn/go-sqlite3"

	"mcdex/algo"
)

type Database struct {
	sqlDb     *sql.DB
	sqlDbPath string
	version   string
}

type DepType int

const (
	Required = 1
	Optional = 2
	Embedded = 3
)

func OpenDatabase() (*Database, error) {
	db := new(Database)

	db.sqlDbPath = filepath.Join(env().McdexDir, "mcdex.dat")
	if !fileExists(db.sqlDbPath) {
		return nil, fmt.Errorf("No database available; use db.update command first")
	}

	sqlDb, err := sql.Open("sqlite3", db.sqlDbPath)
	if err != nil {
		return nil, err
	}

	_, err = sqlDb.Exec("PRAGMA integrity_check;")
	if err != nil {
		return nil, err
	}

	db.sqlDb = sqlDb

	return db, nil
}

func InstallDatabase() error {
	// Get the latest version
	version, err := readStringFromUrl("http://files.mcdex.net/data/latest.v4")
	if err != nil {
		return err
	}

	// Download the latest data file to mcdex/mcdex.dat
	url := fmt.Sprintf("http://files.mcdex.net/data/mcdex-v4-%s.dat.bz2", version)
	res, err := HttpGet(url)
	if err != nil {
		return fmt.Errorf("Failed to retrieve %s data file: %+v", version, err)
	}
	defer res.Body.Close()

	// Stream the data file to mcdex.dat.tmp
	tmpFileName := filepath.Join(env().McdexDir, "mcdex.dat.tmp")
	err = writeStream(tmpFileName, bzip2.NewReader(res.Body))
	if err != nil {
		return err
	}

	// Open the temporary database and validate it
	tmpDb, err := sql.Open("sqlite3", tmpFileName)
	if err != nil {
		// TODO: Add log entry about the file being corrupt
		return nil
	}
	defer tmpDb.Close()

	_, err = tmpDb.Exec("PRAGMA integrity_check;")
	if err != nil {
		return nil
	}

	// Force the tmpDb to close so that (on Windows), we can ensure
	// the rename works
	tmpDb.Close()

	// Close the database and rename the tmp file
	err = os.Rename(tmpFileName, filepath.Join(env().McdexDir, "mcdex.dat"))
	if err != nil {
		return fmt.Errorf("Failed to rename mcdex.dat.tmp: %+v", err)
	}
	return nil
}

func (db *Database) listForge(mcvsn string, verbose bool) error {
	rows, err := db.sqlDb.Query("select version, isrec from forge where mcvsn = ? order by version desc", mcvsn)
	switch {
	case err == sql.ErrNoRows:
		return fmt.Errorf("No Forge version found for %s", mcvsn)
	case err != nil:
		return err
	}

	latest := false

	defer rows.Close()
	for rows.Next() {
		var version string
		var isrec bool
		err := rows.Scan(&version, &isrec)
		if err != nil {
			return err
		}
		if isrec {
			fmt.Printf("%s (recommended)\n", version)
		} else if !latest {
			fmt.Printf("%s (latest)\n", version)
			latest = true
		} else if verbose {
			fmt.Printf("%s\n", version)
		}
	}
	return nil
}

func (db *Database) lookupForgeVsn(mcvsn string) (string, error) {
	var forgeVsn string
	err := db.sqlDb.QueryRow("select version from forge where mcvsn = ? and isrec = 1", mcvsn).Scan(&forgeVsn)
	switch {
	case err == sql.ErrNoRows:
		return "", fmt.Errorf("No Forge version found for %s", mcvsn)
	case err != nil:
		return "", err
	}
	return forgeVsn, nil
}

func (db *Database) lookupMcVsn(forgeVsn string) (string, error) {
	var mcVsn string
	err := db.sqlDb.QueryRow("select mcvsn from forge where version = ?", forgeVsn).Scan(&mcVsn)
	switch {
	case err == sql.ErrNoRows:
		return "", fmt.Errorf("No Minecraft version found for %s", mcVsn)
	case err != nil:
		return "", err
	}
	return mcVsn, nil
}

func (db *Database) printProjects(slug, mcvsn string, ptype int) error {
	// Turn the name into a pre-compiled regex
	slugRegex, err := regexp.Compile("(?i)" + slug)
	if err != nil {
		return fmt.Errorf("Failed to convert %s into regex: %s", slug, err)
	}

	query := "select slug, description, downloads from projects where type = ? and projectid in (select projectid from files where version = ?) order by slug"
	if mcvsn == "" {
		query = "select slug, description, downloads from projects where type = ? order by slug"
	}

	rows, err := db.sqlDb.Query(query, ptype, mcvsn)
	if err != nil {
		return fmt.Errorf("Query failed: %+v", err)
	}
	defer rows.Close()

	// For each row, check the name against the pre-compiled regex
	for rows.Next() {
		var slug, desc string
		var downloads int
		err = rows.Scan(&slug, &desc, &downloads)
		if err != nil {
			return err
		}

		if slug == "" || slugRegex.MatchString(slug) {
			msg := message.NewPrinter(language.English)
			msg.Printf("%s | %s | %d downloads\n", slug, desc, downloads)
		}
	}

	return nil
}

func (db *Database) printLatestProjects(mcvsn string, ptype int) error {
	rows, err := db.sqlDb.Query(`select slug, description, downloads from projects 
									    where type = ? and projectid in 
									    (select projectid from files order by tstamp desc) limit 100`, ptype)
	if err != nil {
		return fmt.Errorf("Query failed: %+v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var modSlug, modDesc string
		var modDownloads int

		err = rows.Scan(&modSlug, &modDesc, &modDownloads)
		if err != nil {
			return err
		}

		msg := message.NewPrinter(language.English)
		msg.Printf("%s | %s | %d downloads\n", modSlug, modDesc, modDownloads)
	}
	return nil
}

func (db *Database) getLatestFileTstamp() (int, error) {
	var tstamp int
	err := db.sqlDb.QueryRow("select value from meta where key = 'dbtunix'").Scan(&tstamp)
	return tstamp, err
}

func (db *Database) getLatestModFile(modID int, mcvsn string) (*ModFile, error) {
	// First, look up the modid for the given name
	var name, slug, desc string
	err := db.sqlDb.QueryRow("select name, slug, description from projects where type = 0 and projectid = ?", modID).Scan(&name, &slug, &desc)
	switch {
	case err == sql.ErrNoRows:
		return nil, fmt.Errorf("No mod found %d", modID)
	case err != nil:
		return nil, err
	}

	// Now find the latest release or beta version
	var fileID int
	err = db.sqlDb.QueryRow("select fileid from files where projectid = ? and version = ? order by tstamp desc limit 1",
		modID, mcvsn).Scan(&fileID)
	switch {
	case err == sql.ErrNoRows:
		return nil, fmt.Errorf("No file found for %s on Minecraft %s", name, mcvsn)
	case err != nil:
		return nil, err
	}

	return &ModFile{fileID: fileID, modID: modID, modName: name, slug: slug, modDesc: desc}, nil
}

func (db *Database) findProjectBySlug(slug string, ptype int) (int, error) {
	var modID int
	err := db.sqlDb.QueryRow("select projectid from projects where type = ? and slug = ?", ptype, slug).Scan(&modID)
	switch {
	case err == sql.ErrNoRows:
		return -1, fmt.Errorf("No mod found %s", slug)
	case err != nil:
		return -1, err
	}
	return modID, nil
}

func (db *Database) findModBySlug(slug string) (int, error) {
	return db.findProjectBySlug(slug, 0)
}

func (db *Database) findModByName(name string) (int, error) {
	var modID int
	err := db.sqlDb.QueryRow("select projectid from projects where type = 0 and (name = ? or slug = ?)", name, name).Scan(&modID)
	switch {
	case err == sql.ErrNoRows:
		return -1, fmt.Errorf("No mod found %s", name)
	case err != nil:
		return -1, err
	}
	return modID, nil
}

func (db *Database) findModFile(modID, fileID int, mcversion string) (*ModFile, error) {
	// Try to match the file ID
	if fileID > 0 {
		err := db.sqlDb.QueryRow("select projectid from files where fileid = ? and version = ?", fileID, mcversion).Scan(&modID)
		if err != nil {
			return nil, fmt.Errorf("No matching file ID for %s version", mcversion)
		}
	} else {
		err := db.sqlDb.QueryRow("select fileid from files where projectid = ? and version = ? order by tstamp desc limit 1",
			modID, mcversion).Scan(&fileID)
		if err != nil {
			return nil, fmt.Errorf("No recent file for mod %d / %s version", modID, mcversion)
		}
	}

	// We matched some file; pull the name and description for the mod
	var name, slug, desc string
	err := db.sqlDb.QueryRow("select name, slug, description from projects where projectid = ?", modID).Scan(&name, &slug, &desc)
	if err != nil {
		return nil, fmt.Errorf("Failed to retrieve name, description for mod %d: %+v", modID, err)
	}

	return &ModFile{fileID: fileID, modID: modID, modName: name, slug: slug, modDesc: desc}, nil
}

func (db *Database) getDeps(fileID int) ([]int, error) {
	var result []int
	rows, err := db.sqlDb.Query("SELECT projectid, level FROM deps WHERE fileid = ? and level == 1", fileID)

	switch {
	case err == sql.ErrNoRows:
		return []int{}, nil
	case err != nil:
		return []int{}, fmt.Errorf("Failed to query deps for %d: %+v", fileID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var modID, level int
		err = rows.Scan(&modID, &level)
		if err != nil {
			return []int{}, fmt.Errorf("Failed to query dep rows for %d: %+v", fileID, err)
		}

		result = append(result, modID)
	}

	return result, nil
}

func (db *Database) getLatestPackURL(slug string) (string, error) {
	// First try to find the pack by looking for the specific slug
	pid, err := db.findProjectBySlug(slug, 1)
	if err != nil {
		return "", err
	}

	// Find the latest file given the project ID; we don't need to worry about matching the MC version,
	// since modpacks are always locked to a specific version anyways
	var fileID int
	err = db.sqlDb.QueryRow("select fileid from files where projectid = ? order by tstamp desc limit 1", pid).Scan(&fileID)
	switch {
	case err == sql.ErrNoRows:
		return "", fmt.Errorf("No modpack file found for %s", slug)
	case err != nil:
		return "", err
	}

	// Construct a URL using the slug and file ID
	return fmt.Sprintf("https://minecraft.curseforge.com/projects/%d/files/%d/download", pid, fileID), nil
}

func (db *Database) buildDepGraph(m *ModPack) (algo.Graph, error) {

	var fileIds map[int]*ManifestFileEntry
	var fileIdsString strings.Builder
	g := algo.MakeGraph()

	// Load all files from manifest
	{
		nameQuery, err := db.sqlDb.Prepare("SELECT DISTINCT name FROM files f, projects p WHERE f.fileid = ? AND f.projectid = ? AND p.projectid = f.projectid")
		if err != nil {
			return nil, err
		}
		defer nameQuery.Close()

		files, _ := m.manifest.S("files").Children()
		fileIds = make(map[int]*ManifestFileEntry, len(files))
		for i, file := range files {
			record := ManifestFileEntry{idx: i}
			if file.Exists("filename") {
				record.file = file.S("filename").Data().(string)
			}

			record.projId = int(file.S("projectID").Data().(float64))
			record.fileId = int(file.S("fileID").Data().(float64))
			err = nameQuery.QueryRow(record.fileId, record.projId).Scan(&record.name)
			switch {
			case err == sql.ErrNoRows:
				log.Printf("No mod found in database with project id %d and file id %d - File: %q\n\tDependency resolution may be incomplete!", record.projId, record.fileId, record.file)
			case err != nil:
				return nil, err
			}

			fileIds[record.fileId] = &record
			g.AddNode(&record)

			fileIdsString.WriteString(strconv.Itoa(record.fileId))
			fileIdsString.WriteByte(',')
		}
		// Simple hack to deal with trailing comma or empty
		fileIdsString.WriteByte('0')
	}

	// Load dependencies and add to graph
	{
		rows, err := db.sqlDb.Query(fmt.Sprintf("SELECT DISTINCT d.fileid, f.fileid, level FROM deps d, files f WHERE level <> 3 AND f.projectid = d.projectid AND d.fileid IN (%[1]s) AND f.fileid IN (%[1]s)", fileIdsString.String()))
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var fileid, depid int
			var depType DepType
			if err = rows.Scan(&fileid, &depid, &depType); err != nil {
				return nil, err
			}

			node := g[fileIds[fileid]]
			dep := fileIds[depid]
			switch depType {
			case Required:
				node.AddDependencies(dep)
			case Optional:
				node.AddOptionals(dep)
			}
		}
	}

	return g, nil
}
