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
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

type envConsts struct {
	MinecraftDir string
	MultiMCDir   string
	McdexDir     string
	JavaDir      string
}

var envData envConsts

func initEnv() error {
	// Get the minecraft directory, based on platform
	mcDir := envData.MinecraftDir
	if mcDir == "" {
		mcDir = _minecraftDir()
		envData.MinecraftDir = mcDir
	}
	os.Mkdir(mcDir, 0700)

	// Get the mcdex directory, create if necessary
	mcdexDir := filepath.Join(mcDir, "mcdex")
	os.Mkdir(mcdexDir, 0700)
	envData.McdexDir = mcdexDir

	// Figure out where the JVM (and unpack200) commands can be found
	javaDir := _findJavaDir(mcDir)
	if javaDir == "" {
		return fmt.Errorf("missing Java directory")
	}
	envData.JavaDir = javaDir

	return nil
}

func env() envConsts {
	return envData
}

func unpack200Cmd() string {
	return filepath.Join(envData.JavaDir, "bin", "unpack200"+_executableExt())
}

func javaCmd() string {
	return filepath.Join(envData.JavaDir, "bin", "java" + _executableExt())
}

func vlog(f string, args ...interface{}) {
	if ARG_VERBOSE {
		fmt.Printf("V: "+f, args...)
	}
}

func _minecraftDir() string {
	user, _ := user.Current()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(user.HomeDir, "Library", "Application Support", "minecraft")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), ".minecraft")
	default:
		return filepath.Join(user.HomeDir, ".minecraft")
	}
}

func _findJavaDir(mcdir string) string {
	// Check for JAVA_HOME; validate that contains bin/java
	javaDir := os.Getenv("JAVA_HOME")
	vlog("JAVA_HOME: %s\n", javaDir)
	if javaDir != "" && _javaExists(javaDir) {
		return javaDir
	}

	// Check for JRE_HOME
	javaDir = os.Getenv("JRE_HOME")
	vlog("JRE_HOME: %s\n", javaDir)
	if javaDir != "" && _javaExists(javaDir) {
		return javaDir
	}

	// Look for JDK installed in minecraft directory
	javaDir = _getEmbeddedMinecraftRuntime(mcdir)
	if javaDir != "" {
		return javaDir
	}

	// Run the equivalent of "which java" (last attempt!)
	var whichJavaCmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		whichJavaCmd = exec.Command("where", "java")
	default:
		whichJavaCmd = exec.Command("sh", "-c", "which java")
	}

	if whichJavaCmd != nil {
		out, err := whichJavaCmd.Output()
		if err != nil {
			vlog("%s failed: %+v\n", whichJavaCmd.Args, err)
			return ""
		}

		javaDir = filepath.Dir(filepath.Dir(strings.TrimSpace(string(out))))
		vlog("%s -> %s\n", whichJavaCmd.Args, javaDir)
		if _javaExists(javaDir) {
			return javaDir
		}
	}
	return ""
}

func _executableExt() string {
	switch runtime.GOOS {
	case "windows":
		return ".exe"
	default:
		return ""
	}
}

func _javaExists(dir string) bool {
	name := filepath.Join(dir, "bin", "java"+_executableExt())
	exists := fileExists(name)
	vlog("_javaExists: %s -> %t\n", name, exists)
	return exists
}

func _getEmbeddedMinecraftRuntime(mcDir string) string {
	var mcAppDir string
	switch runtime.GOOS {
	case "windows":
		mcAppDir = filepath.Join(os.Getenv("ProgramFiles(x86)"), "Minecraft", "runtime", "jre-x64")
	default:
		mcAppDir = filepath.Join(mcDir, "runtime", "jre-x64")
	}

	vlog("Embedded MC dir: %s\n", mcAppDir)

	baseDir, err := os.Open(mcAppDir)
	if err != nil {
		vlog("Failed to open mcAppDir: %+v\n", err)
		return ""
	}

	names, err := baseDir.Readdirnames(5)
	if err != nil {
		vlog("Failed to read directory %s: %+v\n", mcAppDir, err)
		return ""
	}

	for _, name := range names {
		if name == "." || name == ".." {
			continue
		}
		dir := filepath.Join(mcAppDir, name)
		if _javaExists(dir) {
			return dir
		}
	}

	return ""
}
