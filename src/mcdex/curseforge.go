package main

import (
	"fmt"
	"strconv"

	"github.com/Jeffail/gabs"
	"github.com/PuerkitoBio/goquery"
	"github.com/robertkrimen/otto"
)

func getCurseForgeModUrl(url string) (string, error) {
	// Retrieve the URL (we assume it's a HTML webpage)
	res, e := HttpGet(url)
	if e != nil {
		return "", fmt.Errorf("failed to get %s: %+v", url, e)
	}

	defer res.Body.Close()
	doc, err := goquery.NewDocumentFromResponse(res)
	if err != nil {
		return "", fmt.Errorf("failed to parse %s: %+v", url, e)
	}

	// Extract the description of this mod file for addition to manifest
	desc, _ := doc.Find("meta[property='og:description']").Attr("content")

	// Setup a JS VM and run the HTML through it; we want to process any
	// script sections in the head so we can extract Elerium meta-data
	vm := otto.New()
	vm.Run("Elerium = {}; Elerium.ProjectFileDetails = {}")
	doc.Find("head script").Each(func(i int, sel *goquery.Selection) {
		vm.Run(sel.Text())
	})

	// Convert the Elerium data into JSON, then a string to get it out the VM
	data, err := vm.Run("JSON.stringify(Elerium.ProjectFileDetails)")
	if err != nil {
		return "", fmt.Errorf("failed to extract project file details: %+v", err)
	}

	// Reparse from string into JSON (blech)
	dataStr, _ := data.ToString()
	projectDetails, _ := gabs.ParseJSON([]byte(dataStr))

	// Make sure files entry exists in manifest
	if !cp.manifest.Exists("files") {
		cp.manifest.ArrayOfSizeP(0, "files")
	}

	projectID, _ := strconv.Atoi(projectDetails.S("projectID").Data().(string))
	fileID, _ := strconv.Atoi(projectDetails.S("projectFileID").Data().(string))

	// We should now have the project & file IDs; add them to the manifest and
	// save it
	modInfo := make(map[string]interface{})
	modInfo["projectID"] = projectID
	modInfo["fileID"] = fileID
	modInfo["required"] = true
	modInfo["desc"] = desc

	// Walk through the list of files; if we find one with same project ID, delete it
	existingIndex := -1
	files, _ := cp.manifest.S("files").Children()
	for i, child := range files {
		childProjectID := int(child.S("projectID").Data().(float64))
		if childProjectID == projectID {
			existingIndex = i
			break
		}
	}

	if existingIndex > -1 {
		cp.manifest.S("files").SetIndex(modInfo, existingIndex)
	} else {
		cp.manifest.ArrayAppendP(modInfo, "files")
	}
}
