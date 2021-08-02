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

package internal

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/http2"

	"github.com/Jeffail/gabs"
	"github.com/viki-org/dnscache"
)

const connTimeout = time.Duration(5) * time.Second

var resolver = dnscache.New(time.Minute * 15)
var getterClient = NewHttpClient(true)
var redirectClient = NewHttpClient(false)

func NewHttpClient(followRedirects bool) http.Client {
	t := http.Transport{
		MaxIdleConnsPerHost:   10,
		ResponseHeaderTimeout: time.Duration(10 * time.Second),
		ExpectContinueTimeout: time.Duration(10 * time.Second),
		Dial: func(network string, address string) (net.Conn, error) {
			separator := strings.LastIndex(address, ":")
			ip, _ := resolver.FetchOne(address[:separator])
			ipStr := ip.String()
			if ip.To4() == nil {
				// IPv6 address; need to wrap it in brackets
				ipStr = fmt.Sprintf("[%s]", ipStr)
			}
			conn, err := net.DialTimeout("tcp", ipStr+address[separator:], connTimeout)
			if err != nil {
				return nil, err
			}
			return conn, nil
		},
	}
	err := http2.ConfigureTransport(&t)
	if err != nil {
		fmt.Printf("Error configuring http2: %+v\n", err)
	}

	if !followRedirects {
		return http.Client{Transport: &t, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return http.Client{Transport: &t}

}

func HttpGet(url string) (*http.Response, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Add("User-Agent", "Mozilla/5.0 AppleWebKit/589.6 (KHTML, like Gecko) Brave Chrome/79.1.3945.88 Safari/577.36")
	return getterClient.Do(req)
}

func downloadHttpFile(url string, targetFile string) error {
	resp, err := HttpGet(url)
	if err != nil {
		return fmt.Errorf("failed to retrieve %s: %+v", url, err)
	}
	defer resp.Body.Close()

	// Make sure all directories exist for the given filename
	err = os.MkdirAll(path.Dir(targetFile), 0700)
	if err != nil {
		return fmt.Errorf("failed to create directories for %s: %+v", targetFile, err)
	}

	// Copy the stream into the filename
	return writeStream(targetFile, resp.Body)
}

func downloadHttpFileToDir(url string, targetDir string, skipIfExists bool) (string, error) {
	// Start the download
	resp, err := HttpGet(url)
	if err != nil {
		return "", fmt.Errorf("failed to download %s: %+v", url, err)
	}
	defer resp.Body.Close()

	// If we didn't get back a 200, bail
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to download %s status %d", url, resp.StatusCode)
	}

	// Extract the filename from the actual request (after following all redirects)
	filename := filepath.Base(resp.Request.URL.Path)

	// Check for Content-Disposition header
	attachmentID := resp.Header.Get("Content-Disposition")
	if strings.HasPrefix(attachmentID, "attachment; filename=") {
		filename = strings.TrimPrefix(attachmentID, "attachment; filename=")
	}

	// Cleanup the filename
	filename = strings.Replace(filename, " r", "-", -1)
	filename = strings.Replace(filename, " ", "-", -1)
	filename = strings.Replace(filename, "+", "-", -1)
	filename = strings.Replace(filename, "(", "-", -1)
	filename = strings.Replace(filename, ")", "", -1)
	filename = strings.Replace(filename, "[", "-", -1)
	filename = strings.Replace(filename, "]", "", -1)
	filename = strings.Replace(filename, "'", "", -1)
	filename = filepath.Join(targetDir, filename)

	if skipIfExists && fileExists(filename) {
		return filepath.Base(filename), nil
	}

	// Save the stream of the response to the file
	fmt.Printf("Downloading %s\n", filepath.Base(filename))

	err = writeStream(filename, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to write %s: %+v", filename, err)
	}

	return filepath.Base(filename), nil
}

func findJSONFile(z *zip.ReadCloser, name string) (*gabs.Container, error) {
	for _, f := range z.File {
		if f.Name == name {
			freader, err := f.Open()
			if err != nil {
				return nil, err
			}

			json, err := gabs.ParseJSONBuffer(freader)
			if err != nil {
				return nil, fmt.Errorf("failed to parse JSON %s: %+v", name, err)
			}
			return json, nil
		}
	}

	return nil, fmt.Errorf("failed to find %s", name)
}

func writeStream(filename string, data io.Reader) error {
	// Construct a filename to hold the stream while writing; once the download is complete, we'll move it into place
	// and delete the temporary file. This ensures that partial/failed streams are properly detected.
	tempFilename := filename + ".part"

	// Create the temporary file
	f, err := os.Create(tempFilename)
	if err != nil {
		return fmt.Errorf("failed to create %s: %v", filename, err)
	}
	defer f.Close()

	// Stream the data into the temp file
	writer := bufio.NewWriter(f)
	_, err = io.Copy(writer, data)
	if err != nil {
		return fmt.Errorf("failed to write %s: %v", filename, err)
	}
	writer.Flush()
	f.Close()

	// Ok, write completed successfully, move the file
	err = os.Rename(tempFilename, filename)
	if err != nil {
		return fmt.Errorf("failed to rename %s: %+v", tempFilename, err)
	}

	return nil
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil || os.IsExist(err)
}

func dirExists(dirname string) bool {
	stat, err := os.Stat(dirname)
	return err == nil && stat.IsDir()
}

func ReadStringFromUrl(url string) (string, error) {
	res, err := HttpGet(url)
	if err != nil {
		return "", fmt.Errorf("Failed to read string from %s: %+v", url, err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return "", fmt.Errorf("Failed to read string from %s: HTTP %d", url, res.StatusCode)
	}

	// Dump the body into a string
	buf := new(bytes.Buffer)
	buf.ReadFrom(res.Body)
	return strings.TrimSpace(buf.String()), nil
}

func writeJSON(json *gabs.Container, filename string) error {
	jsonStr := json.StringIndent("", " ")
	return ioutil.WriteFile(filename, []byte(jsonStr), 0644)
}

func readStringFile(filename string) (string, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func writeStringFile(filename, data string) error {
	// Ensure all the necessary directories exist
	err := os.MkdirAll(filepath.Dir(filename), 0700)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filename, []byte(data), 0644)
}

func parseVersion(version string) (int, int, int, error) {
	parts := strings.SplitN(version, ".", 3)
	// Walk over all the parts and convert to ints
	intParts := make([]int, len(parts))
	for i := 0; i < len(parts); i++ {
		value, err := strconv.Atoi(parts[i])
		if err != nil {
			intParts[i] = -1
		} else {
			intParts[i] = value
		}
	}

	if len(intParts) > 2 {
		return intParts[0], intParts[1], intParts[2], nil
	} else if len(intParts) > 1 {
		return intParts[0], intParts[1], 0, nil
	} else {
		return -1, -1, -1, fmt.Errorf("invalid version %s", version)
	}
}

func stripBadUTF8(s string) string {
	// Noop if we've already got a valid string
	if utf8.ValidString(s) {
		return s
	}

	// Walk the string, checking each rune
	v := make([]rune, 0, len(s))
	for i, r := range s {
		if r == utf8.RuneError {
			_, size := utf8.DecodeRuneInString(s[i:])
			if size == 1 {
				continue
			}
		}
		v = append(v, r)
	}
	return string(v)
}

func getJSONFromURL(url string) (*gabs.Container, error) {
	res, e := HttpGet(url)
	if e != nil {
		return nil, fmt.Errorf("Failed to complete HTTP request: %s %+v", url, e)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("Failed to retrieve %s: %d", url, res.StatusCode)
	}

	// Parse the data using gabs
	return gabs.ParseJSONBuffer(res.Body)
}

func intValue(c *gabs.Container, path string) (int, error) {
	data := c.Path(path).Data()
	switch v := data.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("Invalid type for %s: %+v", path, data)
	}
}

func strValue(c *gabs.Container, path string) (string, error) {
	data := c.Path(path).Data()
	switch v := data.(type) {
	case string:
		return v, nil
	default:
		return "", fmt.Errorf("Invalid type for %s: %+v", path, data)
	}
}

func hasAnyPrefix(url string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(url, p) {
			return true
		}
	}

	return false
}

func getJavaMainClass(jarfile string) (string, error) {
	zr, err := zip.OpenReader(jarfile)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == "META-INF/MANIFEST.MF" {
			manifest, err := f.Open()
			if err != nil {
				return "", err
			}

			scanner := bufio.NewScanner(manifest)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "Main-Class:") {
					return strings.TrimSpace(strings.TrimPrefix(line, "Main-Class:")), nil
				}
			}
		}
	}
	return "", fmt.Errorf("main class not found")
}

func urlJoin(urlBase string, paths ...string) (string, error) {
	// Parse the base URL
	u, err := url.Parse(urlBase)
	if err != nil {
		return "", fmt.Errorf("invalid url %s: %+v", urlBase, err)
	}

	// Append all the provided paths to the base URL path
	u.Path = path.Join(append([]string{u.Path}, paths...)...)
	return u.String(), nil
}
