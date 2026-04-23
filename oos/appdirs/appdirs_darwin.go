//go:build darwin

package appdirs

// macOS: Apple Human Interface Guidelines für Verzeichnisse
// https://developer.apple.com/library/archive/documentation/FileManagement/Conceptual/FileSystemProgrammingGuide/

import (
	"os"
	"path/filepath"
)

const pathSep = string(filepath.Separator)

// userConfigBase gibt ~/Library/Application Support/<n> zurück.
// macOS kennt kein separates Config-Verzeichnis — Application Support ist Standard.
func userConfigBase(name string) string {
	return filepath.Join(homeDir(), "Library", "Application Support", name)
}

// userCacheBase gibt ~/Library/Caches/<n> zurück.
func userCacheBase(name string) string {
	return filepath.Join(homeDir(), "Library", "Caches", name)
}

// userDataBase gibt ~/Library/Application Support/<n> zurück.
// Auf macOS ist Application Support für Daten und Config identisch.
func userDataBase(name string) string {
	return filepath.Join(homeDir(), "Library", "Application Support", name)
}

// userLogsBase gibt ~/Library/Logs/<n> zurück.
// macOS hat ein eigenes Logs-Verzeichnis in der Library.
func userLogsBase(name string) string {
	return filepath.Join(homeDir(), "Library", "Logs", name)
}

// homeDir gibt das Home-Verzeichnis des aktuellen Nutzers zurück.
func homeDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return dir
}
