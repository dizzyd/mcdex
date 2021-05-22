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
	"os"
	"path/filepath"
	"strconv"
	"math"

	"regexp"

	"golang.org/x/text/language"
	"golang.org/x/text/message"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	sqlDb     *sql.DB
	sqlDbPath string
	version   string
}

func OpenDatabase() (*Database, error) {
	db := new(Database)

	err := InstallDatabase(true)
	if err != nil {
		return nil, fmt.Errorf("Database not available; try using db.update command")
	}

	db.sqlDbPath = filepath.Join(env().McdexDir, "mcdex.dat")
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

func InstallDatabase(skipIfExists bool) error {
	if skipIfExists && fileExists(filepath.Join(env().McdexDir, "mcdex.dat")) {
		return nil
	}

	// Get the latest version
	version, err := readStringFromUrl("http://files.mcdex.net/data/latest.v5")
	if err != nil {
		return err
	}

	// Download the latest data file to mcdex/mcdex.dat
	url := fmt.Sprintf("http://files.mcdex.net/data/mcdex-v5-%s.dat.bz2", version)
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
		return err
	}
	defer tmpDb.Close()

	_, err = tmpDb.Exec("PRAGMA integrity_check;")
	if err != nil {
		return err
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

	query := "select slug, description from projects where type = ? and projectid in (select projectid from versions where mcvsn = ?) order by slug"
	if mcvsn == "" {
		query = "select slug, description from projects where type = ? order by slug"
	}

	rows, err := db.sqlDb.Query(query, ptype, mcvsn)
	if err != nil {
		return fmt.Errorf("Query failed: %+v", err)
	}
	defer rows.Close()

	// For each row, check the name against the pre-compiled regex
	for rows.Next() {
		var slug, desc string
		err = rows.Scan(&slug, &desc)
		if err != nil {
			return err
		}

		if slug == "" || slugRegex.MatchString(slug) {
			msg := message.NewPrinter(language.English)
			msg.Printf("%s | %s\n", slug, desc)
		}
	}

	return nil
}

func (db *Database) printLatestProjects(mcvsn string, ptype int) error {
	rows, err := db.sqlDb.Query(`select slug, description from projects 
									    where type = ? and projectid in 
									    (select projectid from files order by tstamp desc) limit 100`, ptype)
	if err != nil {
		return fmt.Errorf("Query failed: %+v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var modSlug, modDesc string

		err = rows.Scan(&modSlug, &modDesc)
		if err != nil {
			return err
		}

		msg := message.NewPrinter(language.English)
		msg.Printf("%s | %s\n", modSlug, modDesc)
	}
	return nil
}

func (db *Database) getLatestFileTstamp() (int, error) {
	var tstamp int
	err := db.sqlDb.QueryRow("select value from meta where key = 'dbtunix'").Scan(&tstamp)
	return tstamp, err
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

func (db *Database) findSlugByProject(id int) (string, error) {
	var slug string
	err := db.sqlDb.QueryRow("select slug from projects where projectid = ?", id).Scan(&slug)
	switch {
	case err == sql.ErrNoRows:
		return "", fmt.Errorf("no project found %d", id)
	case err != nil:
		return slug, err
	}
	return slug, nil
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

func (db *Database) getProjectInfo(projectID int) (string, string, string, error) {
	var slug, name, desc string
	err := db.sqlDb.QueryRow("select slug, name, description from projects where projectid = ? and type = 0", projectID).Scan(&slug, &name, &desc)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get project info for %d: %+v", projectID, err)
	}

	return slug, name, desc, nil
}

func (db *Database) getDeps(fileID int) ([]string, error) {
	var result []string
	rows, err := db.sqlDb.Query("SELECT projectid, level FROM deps WHERE fileid = ? and level == 1", fileID)

	switch {
	case err == sql.ErrNoRows:
		return []string{}, nil
	case err != nil:
		return []string{}, fmt.Errorf("Failed to query deps for %d: %+v", fileID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var projectID, level int
		err = rows.Scan(&projectID, &level)
		if err != nil {
			return []string{}, fmt.Errorf("Failed to query dep rows for %d: %+v", fileID, err)
		}

		// Resolve the project ID to a slug
		var slug string
		err = db.sqlDb.QueryRow("select slug from projects where projectid = ?", projectID).Scan(&slug)
		if err != nil {
			return []string{}, fmt.Errorf("failed to resolve dep project %d to a slug", projectID)
		}

		result = append(result, slug)
	}

	return result, nil
}

func (db *Database) getLatestPackURL(slug string, fileID string) (string, error) {
	// First try to find the pack by looking for the specific slug
	projectID, err := db.findProjectBySlug(slug, 1)
	if err != nil {
		projectID, err = strconv.Atoi(slug)
		if err != nil {
			return "", fmt.Errorf("No modpack found %s", slug)
		} else {
			fmt.Printf("Interpretting %s as project ID\n", slug);
		}
	}
	
	
	url := ""
	
	if fileID != "" {		
		// Retrieve the JSON descriptor for this file so we can get the CDN url
		descriptorUrl := fmt.Sprintf("https://addons-ecs.forgesvc.net/api/v2/addon/%d/file/%s", projectID, fileID)
		descriptor, err := getJSONFromURL(descriptorUrl)
		if err != nil {
			return "", fmt.Errorf("failed to retrieve descriptor for %s: %+v", slug, err)
		}

		// Download the file to the pack mod directory
		url = descriptor.Path("downloadUrl").Data().(string)
	} else {
		projectUrl := fmt.Sprintf("https://addons-ecs.forgesvc.net/api/v2/addon/%d", projectID)
		project, err := getJSONFromURL(projectUrl)
		if err != nil {
			return "", fmt.Errorf("failed to retrieve project for %s: %+v", slug, err)
		}

		selectedFileType := math.MaxInt8

		// Look for the file with most stable release type
		files, _ := project.Path("latestFiles").Children()
		for _, file := range files {
			fileType, _ := intValue(file, "releaseType") // 1 = release, 2 = beta, 3 = alpha
			fileUrl, _ := file.Path("downloadUrl").Data().(string)

			// Prefer releases over beta/alpha
			if fileType < selectedFileType {
				selectedFileType = fileType
				url = fileUrl
			}
		}

		if url == "" {
			return "", fmt.Errorf("Unable to find download URL for %s\n", slug)
		}
	}

	return url, nil

}
