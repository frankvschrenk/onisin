package boot

// app.go — Fyne application entry point.
//
// StartFyneApp initialises the Fyne app, shows a splash screen and runs the
// boot sequence (config load → login → shell).

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"onisin.com/oos/helper"
)

var fyneApp fyne.App
var fyneWindow fyne.Window

// StartFyneApp creates the Fyne application and runs the main event loop.
// This function blocks until the application exits.
func StartFyneApp() {
	// NewWithID sets the app identifier for Fyne's preference store
	// and — more visibly on macOS — determines the name shown in the
	// system menu bar. Without it, macOS falls back to the binary
	// name ("oos_macos") which leaks the platform suffix into the UI.
	fyneApp = app.NewWithID("com.onisin.oos")
	fyneWindow = fyneApp.NewWindow("OOS")
	fyneWindow.Resize(fyne.NewSize(640, 400))
	fyneWindow.CenterOnScreen()

	setupMainMenu()
	showSplash()
	go runBoot()
	fyneWindow.ShowAndRun()
}

func setupMainMenu() {
	settingsItem := fyne.NewMenuItem("Settings…", func() {
		OpenSettingsDialog()
	})
	oosMenu := fyne.NewMenu("OOS", settingsItem)

	viewMenu := fyne.NewMenu("View",
		fyne.NewMenuItem("Light theme", func() { switchThemeVariant("light") }),
		fyne.NewMenuItem("Dark theme", func() { switchThemeVariant("dark") }),
	)

	fyneWindow.SetMainMenu(fyne.NewMainMenu(oosMenu, viewMenu))
}

// switchThemeVariant flips the active variant, persists the choice
// to oos.toml and reloads the theme for the new variant from oos.config.
//
// Light and dark are two separate rows (theme.light, theme.dark), so
// the switch is a real reload — not just a flag flip on one theme.
// If the reload fails, we fall back to the compiled-in default for
// the new variant so the UI still renders cleanly.
func switchThemeVariant(variant string) {
	if err := helper.SaveThemeVariant(variant); err != nil {
		log.Printf("[ui] theme variant save: %v", err)
	}
	loadAndApplyTheme()
}

func showSplash() {
	label := widget.NewLabel("Starting OOS...")
	fyneWindow.SetContent(container.NewCenter(label))
}

func showStatus(msg string) {
	fyneWindow.Canvas().Refresh(fyneWindow.Content())
	label := widget.NewLabel(msg)
	fyneWindow.SetContent(container.NewCenter(label))
}

func runBoot() {
	if needsSetup() {
		fyne.Do(func() { runSetupFlow() })
		return
	}

	fyne.Do(func() { showStatus("Loading configuration...") })
	if err := initFromConfig(); err != nil {
		log.Printf("[boot] config error: %v", err)
		fyne.Do(func() {
			showStatus("Configuration missing — please open Settings")
			runSetupFlow()
		})
		return
	}

	session := loadCachedSession()
	if session != nil && !session.IsExpired() {
		log.Println("[boot] valid session found")
		fyne.Do(func() { showStatus("Restoring session...") })
		if err := restoreSession(session); err != nil {
			log.Printf("[boot] session restore failed: %v", err)
			runLoginFlow()
			return
		}
		runShell()
		return
	}

	runLoginFlow()
}

func runLoginFlow() {
	fyne.Do(func() { showStatus("Opening browser login...") })

	result, err := runPKCELogin()
	if err != nil {
		log.Printf("[boot] login failed: %v", err)
		fyne.Do(func() { showStatus("Login failed — please restart") })
		return
	}

	helper.ApplyJWTClaims(result.Claims)
	helper.ActiveIDToken = result.IDToken

	if !helper.UnsecureMode {
		if err := initSecretsFromJWT(result.IDToken); err != nil {
			log.Printf("[boot] vault JWT auth: %v", err)
		}
	}

	storeSession(result.IDToken, result.Claims)
	runShell()
}

func restoreSession(session *helper.CachedSession) error {
	helper.ApplyJWTClaims(session.Claims)
	helper.ActiveIDToken = session.IDToken
	if helper.UnsecureMode {
		return nil
	}
	return initSecretsFromJWT(session.IDToken)
}

func runShell() {
	fyne.Do(func() { showStatus("Connecting to OOSP...") })

	if helper.Meta.OOSPUrl != "" {
		connectOOSP(helper.Meta.OOSPUrl)
	}

	fyne.Do(func() { showStatus("Loading schema...") })
	initOOS()

	fyne.Do(func() { openShellWindow() })
}
