//go:build !windows && !darwin

package appdirs

// Linux / BSD: XDG Base Directory Specification
// https://specifications.freedesktop.org/basedir-spec/latest/

import (
	"os"
	"path/filepath"
)

const pathSep = string(filepath.Separator)

// userConfigBase gibt $XDG_CONFIG_HOME/<name> zurück.
// Fallback: ~/.config/<name>
func userConfigBase(name string) string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, name)
	}
	return filepath.Join(homeDir(), ".config", name)
}

// userCacheBase gibt $XDG_CACHE_HOME/<name> zurück.
// Fallback: ~/.cache/<name>
func userCacheBase(name string) string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, name)
	}
	return filepath.Join(homeDir(), ".cache", name)
}

// userDataBase gibt $XDG_DATA_HOME/<name> zurück.
// Fallback: ~/.local/share/<name>
func userDataBase(name string) string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, name)
	}
	return filepath.Join(homeDir(), ".local", "share", name)
}

// userLogsBase gibt $XDG_CACHE_HOME/<name>/logs zurück.
// Logs haben in XDG kein eigenes Verzeichnis — Cache/logs ist Konvention.
func userLogsBase(name string) string {
	return filepath.Join(userCacheBase(name), "logs")
}

// homeDir gibt das Home-Verzeichnis des aktuellen Nutzers zurück.
// os.UserHomeDir() ist seit Go 1.12 die empfohlene Methode.
func homeDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return dir
}
