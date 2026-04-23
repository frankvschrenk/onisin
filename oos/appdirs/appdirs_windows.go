//go:build windows

package appdirs

// Windows: bekannte Shell-Verzeichnisse via Umgebungsvariablen
// %APPDATA%      = C:\Users\<User>\AppData\Roaming  → Config (roaming)
// %LOCALAPPDATA% = C:\Users\<User>\AppData\Local    → Cache, Data, Logs

import (
	"os"
	"path/filepath"
)

const pathSep = string(filepath.Separator)

// userConfigBase gibt %APPDATA%\<n> zurück (Roaming — wird bei Domain-Login synchronisiert).
func userConfigBase(name string) string {
	if dir := os.Getenv("APPDATA"); dir != "" {
		return filepath.Join(dir, name)
	}
	return filepath.Join(homeDir(), "AppData", "Roaming", name)
}

// userCacheBase gibt %LOCALAPPDATA%\<n> zurück (lokal, kein Roaming).
func userCacheBase(name string) string {
	return filepath.Join(localAppData(), name)
}

// userDataBase gibt %LOCALAPPDATA%\<n> zurück.
func userDataBase(name string) string {
	return filepath.Join(localAppData(), name)
}

// userLogsBase gibt %LOCALAPPDATA%\<n>\logs zurück.
func userLogsBase(name string) string {
	return filepath.Join(localAppData(), name, "logs")
}

// localAppData gibt %LOCALAPPDATA% zurück.
// Fallback auf AppData\Local falls die Variable fehlt.
func localAppData() string {
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return dir
	}
	return filepath.Join(homeDir(), "AppData", "Local")
}

// homeDir gibt das Home-Verzeichnis des aktuellen Nutzers zurück.
func homeDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return dir
}
