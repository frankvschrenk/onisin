package appdirs_test

import (
	"strings"
	"testing"

	"onisin.com/oos/appdirs"
)

func TestNew_OhneVersion(t *testing.T) {
	app := appdirs.New("meine-app", "")

	pfade := map[string]string{
		"UserConfig": app.UserConfig(),
		"UserCache":  app.UserCache(),
		"UserData":   app.UserData(),
		"UserLogs":   app.UserLogs(),
	}

	for name, pfad := range pfade {
		if pfad == "" {
			t.Errorf("%s: leerer Pfad zurückgegeben", name)
		}
		if !strings.Contains(pfad, "meine-app") {
			t.Errorf("%s: App-Name fehlt im Pfad %q", name, pfad)
		}
		// Ohne Version darf keine Versionsnummer am Ende stehen
		if strings.HasSuffix(pfad, "/") {
			t.Errorf("%s: Pfad endet unerwartet mit Separator: %q", name, pfad)
		}
	}
}

func TestNew_MitVersion(t *testing.T) {
	app := appdirs.New("meine-app", "2.0")

	pfade := map[string]string{
		"UserConfig": app.UserConfig(),
		"UserCache":  app.UserCache(),
		"UserData":   app.UserData(),
		"UserLogs":   app.UserLogs(),
	}

	for name, pfad := range pfade {
		if !strings.Contains(pfad, "2.0") {
			t.Errorf("%s: Version fehlt im Pfad %q", name, pfad)
		}
		if !strings.Contains(pfad, "meine-app") {
			t.Errorf("%s: App-Name fehlt im Pfad %q", name, pfad)
		}
	}
}

func TestUserLogs_EnthaeltLogs(t *testing.T) {
	app := appdirs.New("meine-app", "")
	if !strings.Contains(app.UserLogs(), "log") {
		t.Errorf("UserLogs-Pfad enthält kein 'log': %q", app.UserLogs())
	}
}
