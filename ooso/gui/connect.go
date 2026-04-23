package gui

// connect.go — DSN-Eingabe und Verbindungsstatus.
//
// Connection ist ein gemeinsamer Zustand der von allen Panels genutzt wird.
// Die Verbindung wird explizit per Button aufgebaut — kein auto-connect.
//
// Panels rufen conn.Importer() auf um den PGImporter zu bekommen.
// Ist die Verbindung nicht aktiv, gibt Importer() nil zurück.

import (
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"onisin.com/oos-common/importer"
)

// Connection hält den aktiven PGImporter und den Verbindungsstatus.
type Connection struct {
	imp    *importer.PGImporter
	status *widget.Label
	icon   *widget.Icon
}

func newConnection() *Connection {
	return &Connection{
		status: widget.NewLabel("nicht verbunden"),
		icon:   widget.NewIcon(theme.MediaStopIcon()),
	}
}

// Importer gibt den aktiven PGImporter zurück, oder nil wenn nicht verbunden.
func (c *Connection) Importer() *importer.PGImporter {
	return c.imp
}

// IsConnected gibt an ob eine aktive Verbindung besteht.
func (c *Connection) IsConnected() bool {
	return c.imp != nil
}

// connect versucht eine Verbindung mit dem gegebenen DSN aufzubauen.
// Gibt einen Fehler zurück wenn die Verbindung fehlschlägt.
func (c *Connection) connect(dsn string) error {
	// Alte Verbindung schließen
	if c.imp != nil {
		c.imp.Close()
		c.imp = nil
	}

	imp, err := importer.New(dsn)
	if err != nil {
		c.setStatus(false, "Verbindung fehlgeschlagen")
		return err
	}

	c.imp = imp
	c.setStatus(true, "verbunden")
	return nil
}

func (c *Connection) setStatus(ok bool, msg string) {
	c.status.SetText(msg)
	if ok {
		c.icon.SetResource(theme.ConfirmIcon())
	} else {
		c.icon.SetResource(theme.ErrorIcon())
	}
}

// buildConnectBar baut die Verbindungsleiste oben im Hauptfenster.
// DSN-Feld + Connect-Button + Status-Anzeige.
func buildConnectBar(conn *Connection) fyne.CanvasObject {
	dsnEntry := widget.NewEntry()
	dsnEntry.SetPlaceHolder(defaultDemoDSN)
	dsnEntry.SetText(resolveDefaultDSN())

	errorLabel := widget.NewLabel("")
	errorLabel.Wrapping = fyne.TextWrapWord

	connectBtn := widget.NewButtonWithIcon("Verbinden", theme.LoginIcon(), func() {
		dsn := dsnEntry.Text
		if dsn == "" {
			errorLabel.SetText("DSN fehlt")
			return
		}
		errorLabel.SetText("verbinde...")
		if err := conn.connect(dsn); err != nil {
			errorLabel.SetText(fmt.Sprintf("Fehler: %v", err))
			return
		}
		errorLabel.SetText("")
	})

	statusRow := container.NewHBox(conn.icon, conn.status)
	dsnRow := container.NewBorder(nil, nil, nil,
		container.NewHBox(connectBtn, statusRow),
		dsnEntry,
	)

	return container.NewVBox(dsnRow, errorLabel)
}

// defaultDemoDSN is the DSN that matches the local demo setup from
// demo.toml (superuser "postgres", password "demo", database "onisin").
// Pre-filling this saves the usual first click; operators running
// against a real database override it via OOSO_DSN or by typing.
const defaultDemoDSN = "postgres://postgres:demo@localhost:5432/onisin?sslmode=disable"

// resolveDefaultDSN returns the DSN to pre-fill the connection bar
// with. Environment variables take precedence over the built-in demo
// default so the importer can be pointed at a different database
// without touching the UI.
func resolveDefaultDSN() string {
	if v := os.Getenv("OOSO_DSN"); v != "" {
		return v
	}
	if v := os.Getenv("OOSP_CTX_DSN"); v != "" {
		return v
	}
	return defaultDemoDSN
}
