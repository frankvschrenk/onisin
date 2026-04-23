package seed

// ctx.go — imports CTX and DSL XML definitions into oos.ctx and oos.dsl.

import (
	"database/sql"
	"fmt"

	oostheme "onisin.com/oos-common/theme"
)

// seedCTX writes all CTX definition files into oos.ctx.
func seedCTX(db *sql.DB) error {
	// Serialise the built-in default theme so the UI has a real theme
	// to render from on first launch. Without this row the desktop
	// client falls back to its compiled-in default, and the theme
	// editor in ooso shows an empty skeleton — the two views then
	// diverge visually until someone authors a theme by hand.
	themeXML, err := oostheme.DefaultTheme("light").ToXML()
	if err != nil {
		return fmt.Errorf("serialise default theme: %w", err)
	}

	entries := []struct {
		id  string
		xml string
	}{
		{"global.conf", globalConfXML},
		{"groups",      groupsXML},
		{"person",      personCTXXML},
		{"note",        noteCTXXML},
		{"theme",       themeXML},
	}

	for _, e := range entries {
		_, err := db.Exec(`
			INSERT INTO oos.ctx (id, xml)
			VALUES ($1, $2)
			ON CONFLICT (id) DO UPDATE SET xml = $2, updated_at = now()
		`, e.id, e.xml)
		if err != nil {
			return err
		}
	}
	return nil
}

// seedDSL writes all DSL screen definition files into oos.dsl.
func seedDSL(db *sql.DB) error {
	entries := []struct {
		id  string
		xml string
	}{
		{"person_list",   personListDSL},
		{"person_detail", personDetailDSL},
		{"note_list",     noteListDSL},
		{"note_detail",   noteDetailDSL},
	}

	for _, e := range entries {
		_, err := db.Exec(`
			INSERT INTO oos.dsl (id, xml)
			VALUES ($1, $2)
			ON CONFLICT (id) DO UPDATE SET xml = $2, updated_at = now()
		`, e.id, e.xml)
		if err != nil {
			return err
		}
	}
	return nil
}
