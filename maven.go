package main

import (
	"encoding/xml"
	"fmt"
	"path"
	"strings"
)

type MavenModule struct {
	groupId    string
	artifactId string
	version    string
	extension  string
	suffix     string
}

type MavenMetadata struct {
	XmlName     xml.Name                 `xml:"metadata"`
	GroupId     string                   `xml:"groupId"`
	ArtifactId  string                   `xml:"artifactId"`
	VersionInfo MavenMetadataVersionInfo `xml:"versioning"`
}

type MavenMetadataVersionInfo struct {
	XmlName  xml.Name `xml:"versioning"`
	Latest   string   `xml:"latest"`
	Release  string   `xml:"release"`
	Versions []string `xml:"versions>version"`
}

func NewMavenModule(module string) (MavenModule, error) {
	// First, break up the string into maven components: group, artifact and version
	parts := strings.SplitN(module, ":", 3)
	if len(parts) < 2 {
		return MavenModule{}, fmt.Errorf("maven module requires at least group and artifact IDs")
	}

	groupID := parts[0]
	artifactID := parts[1]

	var vsn string
	if len(parts) > 2 {
		vsn = parts[2]
	}

	ext := "jar"
	suffix := ""

	// The version string MAY contain an @ that indicates an alternate file extension (i.e. not .jar)
	if strings.Contains(vsn, "@") {
		vsnParts := strings.SplitN(vsn, "@", 2)
		vsn = vsnParts[0]
		ext = vsnParts[1]
	}

	// The version string MAY also have a suffix, delimited by :
	if strings.Contains(vsn, ":") {
		vsnParts := strings.SplitN(vsn, ":", 2)
		vsn = vsnParts[0]
		suffix = vsnParts[1]
	}

	return MavenModule{
		groupId:    groupID,
		artifactId: artifactID,
		version:    vsn,
		extension:  ext,
		suffix:     suffix,
	}, nil
}

func (m MavenModule) String() string {
	base := fmt.Sprintf("%s:%s:%s", m.groupId, m.artifactId, m.version)
	if m.suffix != "" {
		base = base + ":" + m.suffix
	}
	if m.extension != "" {
		base = base + "@" + m.extension
	}
	return base
}

func (m MavenModule) toRepositoryPath(repo string) (string, error) {
	if m.version == "" {
		return "", fmt.Errorf("version not available; repository path incomplete for %s", m)
	}
	var filename string
	if m.suffix != "" {
		filename = fmt.Sprintf("%s-%s-%s.%s", m.artifactId, m.version, m.suffix, m.extension)
	} else {
		filename = fmt.Sprintf("%s-%s.%s", m.artifactId, m.version, m.extension)
	}

	groupPath := path.Join(strings.Split(m.groupId, ".")...)
	return urlJoin(repo, groupPath, m.artifactId, m.version, filename)
}

func (m MavenModule) loadMetadata(repo string) (MavenMetadata, error) {
	groupPath := path.Join(strings.Split(m.groupId, ".")...)
	metadataUrl, err := urlJoin(repo, groupPath, m.artifactId, "maven-metadata.xml")
	if err != nil {
		return MavenMetadata{}, err
	}

	metadataXml, err := readStringFromUrl(metadataUrl)
	if err != nil {
		return MavenMetadata{}, fmt.Errorf("unable to retrieve %s: %+v", metadataUrl, err)
	}

	var metadata MavenMetadata
	err = xml.Unmarshal([]byte(metadataXml), &metadata)
	if err != nil {
		return MavenMetadata{}, fmt.Errorf("unable to parse %s: %+v", metadataUrl, err)
	}

	return metadata, nil
}
