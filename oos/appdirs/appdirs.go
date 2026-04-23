// Package appdirs liefert plattformkonforme Verzeichnispfade für
// Konfiguration, Cache, Daten und Logs einer Anwendung.
//
// Verwendung:
//
//	app := appdirs.New("meine-app", "1.0")
//	fmt.Println(app.UserConfig())   // ~/.config/meine-app/1.0  (Linux)
//	fmt.Println(app.UserCache())    // ~/.cache/meine-app/1.0   (Linux)
package appdirs

// App enthält den App-Namen und die optionale Version.
// Beide werden beim Pfadaufbau als Unterverzeichnisse verwendet.
type App struct {
	name    string
	version string
}

// New erstellt eine neue App-Instanz.
// version darf leer sein — dann wird kein Versions-Unterordner angehängt.
func New(name, version string) *App {
	return &App{name: name, version: version}
}

// UserConfig gibt das benutzerspezifische Konfig-Verzeichnis zurück.
//
//	Linux:   $XDG_CONFIG_HOME/<name>/<version>   (~/.config/<name>/<version>)
//	macOS:   ~/Library/Application Support/<name>/<version>
//	Windows: %APPDATA%\<name>\<version>
func (a *App) UserConfig() string {
	return a.appendVersion(userConfigBase(a.name))
}

// UserCache gibt das benutzerspezifische Cache-Verzeichnis zurück.
//
//	Linux:   $XDG_CACHE_HOME/<name>/<version>    (~/.cache/<name>/<version>)
//	macOS:   ~/Library/Caches/<name>/<version>
//	Windows: %LOCALAPPDATA%\<name>\<version>
func (a *App) UserCache() string {
	return a.appendVersion(userCacheBase(a.name))
}

// UserData gibt das benutzerspezifische Daten-Verzeichnis zurück.
//
//	Linux:   $XDG_DATA_HOME/<name>/<version>     (~/.local/share/<name>/<version>)
//	macOS:   ~/Library/Application Support/<name>/<version>
//	Windows: %LOCALAPPDATA%\<name>\<version>
func (a *App) UserData() string {
	return a.appendVersion(userDataBase(a.name))
}

// UserLogs gibt das benutzerspezifische Log-Verzeichnis zurück.
//
//	Linux:   $XDG_CACHE_HOME/<name>/logs/<version>
//	macOS:   ~/Library/Logs/<name>/<version>
//	Windows: %LOCALAPPDATA%\<name>\logs\<version>
func (a *App) UserLogs() string {
	return a.appendVersion(userLogsBase(a.name))
}

// appendVersion hängt die Version als Unterordner an — falls gesetzt.
func (a *App) appendVersion(base string) string {
	if a.version == "" {
		return base
	}
	return base + pathSep + a.version
}
