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
	McdexDir     string
	JavaDir      string
}

var envData envConsts

func initEnv() error {
	// Get the minecraft directory, based on platform we're running on
	mcDir := _minecraftDir()
	if !dirExists(mcDir) {
		return fmt.Errorf("missing Minecraft directory")
	}

	// Get the mcdex directory, create if necessary
	mcdexDir := filepath.Join(mcDir, "mcdex")
	os.Mkdir(mcdexDir, 0700)

	// Figure out where the JVM (and unpack200) commands can be found
	javaDir := _findJavaDir(mcDir)
	if javaDir == "" {
		return fmt.Errorf("missing Java directory")
	}
	fmt.Printf("Java found in %s\n", javaDir)

	envData = envConsts{
		MinecraftDir: mcDir,
		McdexDir:     mcdexDir,
		JavaDir:      javaDir,
	}
	return nil
}

func env() envConsts {
	return envData
}

func unpack200Cmd() string {
	return filepath.Join(envData.JavaDir, "bin", "unpack200"+_executableExt())
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
	case "linux":
	case "darwin":
		whichJavaCmd = exec.Command("sh", "-c", "which java")
	default:
		break
	}

	if whichJavaCmd != nil {
		out, err := whichJavaCmd.Output()
		if err != nil {
			return ""
		}

		javaDir = filepath.Dir(filepath.Dir(strings.TrimSpace(string(out))))
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
	return fileExists(name)
}

func _getEmbeddedMinecraftRuntime(mcDir string) string {
	var mcAppDir string
	switch runtime.GOOS {
	case "windows":
		mcAppDir = filepath.Join(os.Getenv("ProgramFiles(x86)"), "Minecraft", "runtime", "jre-x64")
	default:
		mcAppDir = filepath.Join(mcDir, "runtime", "jre-x64")
	}

	baseDir, err := os.Open(mcAppDir)
	if err != nil {
		return ""
	}

	names, err := baseDir.Readdirnames(5)
	if err != nil {
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
