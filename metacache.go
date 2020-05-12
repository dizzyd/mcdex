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
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// MetaCache is a local cache file that tracks the installed files so that updates
// don't need to re-download every file
type MetaCache struct {
	modPath string
	db     *sql.DB
	dbPath string
}

func OpenMetaCache(pack *ModPack) (*MetaCache, error) {
	mc := new(MetaCache)

	mc.modPath = pack.modPath()
	mc.dbPath = filepath.Join(pack.gamePath(), ".mcdex.cache")

	db, err := sql.Open("sqlite3", mc.dbPath)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS mods(pid INT PRIMARY KEY, fid INT, filename)")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS extfiles(key PRIMARY KEY, url, filename)")
	if err != nil {
		return nil, err
	}

	mc.db = db

	// Cleanup the cache; make sure that any entries are files that actually exist
	err = mc.Cleanup(pack)
	if err != nil {
		return nil, err
	}

	return mc, nil
}

// AddMod registers a new mod install file in the cache
func (mc *MetaCache) AddModFile(projectId, fileId int, filename string) error {
	_, err := mc.db.Exec("INSERT OR REPLACE INTO mods(pid, fid, filename) VALUES (?, ?, ?)",
		projectId, fileId, filename)
	return err
}

// AddExtFile registers a new external file install in the cache
func (mc *MetaCache) AddExtFile(key, url, filename string) error {
	_, err := mc.db.Exec("INSERT OR REPLACE INTO extfiles(key, url, filename) VALUES (?, ?, ?)",
		key, filename)
	return err
}

// GetLastModFile returns the file ID of the last installed file for a given mod
func (mc *MetaCache) GetLastModFile(projectId int) (int, string) {
	var fileId int
	var filename string
	err := mc.db.QueryRow("SELECT fid, filename FROM mods WHERE pid = ?", projectId).Scan(&fileId, &filename)
	switch {
	case err == sql.ErrNoRows:
		return 0, ""
	case err != nil:
		fmt.Printf("Error looking up file ID from meta cache for %d: %+v\n", projectId, err)
		return -1, ""
	}

	return fileId, filename
}

// GetLastExtURL returns the URL of the last installed file for a given extfile
func (mc *MetaCache) GetLastExtURL(key string) (string, string) {
	var url string
	var filename string
	err := mc.db.QueryRow("SELECT url, filename FROM extfiles WHERE key = ?", key).Scan(&url, &filename)
	switch {
	case err == sql.ErrNoRows:
		return "", ""
	case err != nil:
		fmt.Printf("Error looking up extfiles key from meta cache for %s: %+v\n", key, err)
		return "", ""
	}
	return url, filename
}


func (mc *MetaCache) CleanupModFile(projectId int) error {
	var filename string
	err := mc.db.QueryRow("SELECT filename FROM mods WHERE pid = ?", projectId).Scan(&filename)
	switch {
	case err == sql.ErrNoRows:
		return nil
	case err != nil:
		return err
	}

	os.Remove(filepath.Join(mc.modPath, filename))

	_, err = mc.db.Exec("DELETE FROM mods WHERE pid = ?", projectId)
	return err
}

func (mc *MetaCache) CleanupExtFile(key string) error {
	var filename string
	err := mc.db.QueryRow("SELECT filename FROM extfiles WHERE key = ?", key).Scan(&filename)
	switch {
	case err == sql.ErrNoRows:
		return nil
	case err != nil:
		return err
	}

	err = os.Remove(filepath.Join(mc.modPath, filename))
	if err != nil {
		return err
	}

	_, err = mc.db.Exec("DELETE FROM extfiles WHERE key = ?", key)
	return err
}

func (mc *MetaCache) Cleanup(pack *ModPack) error {
	// Build a map of the current project IDs in the pack for easy reference
	knownProjects := make(map[int]bool)
	packFiles, _ := pack.manifest.Path("files").Children()
	for _, f := range packFiles {
		// Get the project & file ID
		projectID := int(f.Path("projectID").Data().(float64))
		knownProjects[projectID] = true
	}

	// Copy mod cache into a map for traversal
	cache, err := mc.listCache()
	if err != nil {
		return err
	}

	for filename, pid := range cache {
		// If the file in the cache doesn't actually exist, remove it
		if !fileExists(filepath.Join(mc.modPath, filename)) {
			err = mc.CleanupModFile(pid)
			if err != nil {
				fmt.Printf("Failed to cleanup missing file %s: %+v\n", filename, err)
			}
		}

		// If the project ID in the cache doesn't exist in the manifest, remove it
		if _, ok := knownProjects[pid]; !ok {
			err = mc.CleanupModFile(pid)
			if err != nil {
				fmt.Printf("Failed to cleanup missing project %d: %+v\n", pid, err)
			}
		}
	}

	return nil
}

func (mc *MetaCache) listCache() (map[string]int, error) {
	rows, err := mc.db.Query("SELECT pid, filename FROM mods")
	switch {
	case err == sql.ErrNoRows:
		return nil, nil
	case err != nil:
		return nil, err
	}

	defer rows.Close()

	result := make(map[string]int)

	for rows.Next() {
		var projectId int
		var filename string
		err := rows.Scan(&projectId, &filename)
		if err != nil {
			return nil, err
		}

		result[filename] = projectId
	}

	return result, nil
}