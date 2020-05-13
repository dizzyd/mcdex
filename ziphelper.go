package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"github.com/Jeffail/gabs"
	"io"
	"os"
	"path"
)

type ZipHelper struct {
	data   []byte
	dataSz int64
	files  map[string]int
}

func NewZipHelper(data []byte) (*ZipHelper, error) {
	var zh ZipHelper
	zh.data = data
	zh.dataSz = int64(len(data))

	// Open the zip data and cache all the filenames and offsets; also allows
	// reduced error checking later on, since we've validated the file works
	r, err := zip.NewReader(bytes.NewReader(zh.data), zh.dataSz)
	if err != nil {
		return nil, fmt.Errorf("failed to open ZIP data: %+v", err)
	}

	// Cache filenames and offsets into list of files
	zh.files = make(map[string]int)
	for i, f := range r.File {
		zh.files[f.Name] = i

	}
	return &zh, nil
}

func (zh *ZipHelper) getFile(name string) (io.ReadCloser, error) {
	index, ok := zh.files[name]
	if !ok {
		return nil, fmt.Errorf("file not found in ZIP: %s", name)
	}

	r, _ := zip.NewReader(bytes.NewReader(zh.data), zh.dataSz)
	file := r.File[index]
	return file.Open()
}

func (zh *ZipHelper) getJsonFile(name string) (*gabs.Container, error) {
	r, err := zh.getFile(name)
	if err != nil {
		return nil, err
	}

	json, err := gabs.ParseJSONBuffer(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s JSON: %+v", name, err)
	}

	return json, nil
}

func (zh *ZipHelper) writeFileToDir(zipFilename string, targetDir string) (string, error) {
	return zh.writeFile(zipFilename, path.Join(targetDir, zipFilename))
}

func (zh *ZipHelper) writeFile(zipFilename string, filename string) (string, error) {
	r, err := zh.getFile(zipFilename)
	if err != nil {
		return "", err
	}

	// Make sure all the directories in the filename actually exist
	err = os.MkdirAll(path.Dir(filename), 0700)
	if err != nil {
		return "", fmt.Errorf("failed to create directores for %s: %+v", filename, err)
	}

	return filename, writeStream(filename, r)
}
