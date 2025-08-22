package utils

import (
	"fmt"
	"forq/common"
	"os"
	"path/filepath"
	"runtime"
)

const (
	forqDir    = "forq"
	fordDbFile = "forq.db"
)

func GetOrCreateDefaultDBPath() (string, error) {
	possiblePaths := getAllPossibleDBPaths()

	// First, check if any existing DB files exist, as the OS settings (e.g., env vars) might have changed,
	// so we need to make sure we won't miss the existing DB file
	var existingPaths []string
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			existingPaths = append(existingPaths, path)
		}
	}

	// If multiple DB files exist, panic - user needs to resolve manually
	if len(existingPaths) > 1 {
		return "", fmt.Errorf("multiple database files found at: %v. Please remove duplicates manually", existingPaths)
	}

	// If exactly one exists, use it
	if len(existingPaths) == 1 {
		return existingPaths[0], nil
	}

	// No existing DB found, create new one at preferred location
	preferredPath := getPreferredDBPath()

	// Ensure directory exists
	dir := filepath.Dir(preferredPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	return preferredPath, nil
}

func getAllPossibleDBPaths() []string {
	var paths []string

	switch runtime.GOOS {
	case common.WindowsOS:
		if appData := os.Getenv("APPDATA"); appData != "" {
			paths = append(paths, toDbFilePath(appData))
		}
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			paths = append(paths, toDbFilePath(localAppData))
		}
		if homeDir, _ := os.UserHomeDir(); homeDir != "" {
			paths = append(paths, toDbFilePath(homeDir))
		}
	case common.MacOS:
		if homeDir, _ := os.UserHomeDir(); homeDir != "" {
			paths = append(paths, toDbFilePath(filepath.Join(homeDir, "Library", "Application Support")))
			paths = append(paths, toDbFilePath(homeDir)) // fallback location
		}
	case common.LinuxOS:
		if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
			paths = append(paths, toDbFilePath(xdgData))
		}
		if homeDir, _ := os.UserHomeDir(); homeDir != "" {
			paths = append(paths, toDbFilePath(filepath.Join(homeDir, ".local", "share")))
			paths = append(paths, toDbFilePath(homeDir)) // fallback location
		}
	}

	return paths
}

func getPreferredDBPath() string {
	// This is your current GetDefaultDBPath logic
	switch runtime.GOOS {
	case common.WindowsOS:
		if appData := os.Getenv("APPDATA"); appData != "" {
			return toDbFilePath(appData)
		}
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return toDbFilePath(localAppData)
		}
		if homeDir, _ := os.UserHomeDir(); homeDir != "" {
			return toDbFilePath(homeDir)
		}
	case common.MacOS:
		if homeDir, _ := os.UserHomeDir(); homeDir != "" {
			return toDbFilePath(filepath.Join(homeDir, "Library", "Application Support"))
		}
	case common.LinuxOS:
		if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
			return toDbFilePath(xdgData)
		}
		if homeDir, _ := os.UserHomeDir(); homeDir != "" {
			return toDbFilePath(filepath.Join(homeDir, ".local", "share"))
		}
	}

	return toDbFilePath("")
}

func toDbFilePath(dataDir string) string {
	return filepath.Join(dataDir, forqDir, fordDbFile)
}
