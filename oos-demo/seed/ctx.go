package seed

// ctx.go — imports CTX and DSL XML definitions into oos.ctx and oos.dsl,
// and seeds the built-in themes into oos.config.

import (
	"database/sql"
	"fmt"

	oostheme "onisin.com/oos-common/theme"
)

// seedCTX writes all CTX definition files into oos.ctx.
//
// Themes used to live here too (under id "theme"), but they are now
// served from oos.config under namespaces "theme.light" / "theme.dark".
// seedThemes below does that, and seedCTX drops the legacy row so
// existing databases stay consistent with fresh installs.
func seedCTX(db *sql.DB) error {
	entries := []struct {
		id  string
		xml string
	}{
		{"global.conf", globalConfXML},
		{"groups",      groupsXML},
		{"person",      personCTXXML},
		{"note",        noteCTXXML},
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

	// Clean up the legacy theme row if it is still around.
	if _, err := db.Exec(`DELETE FROM oos.ctx WHERE id = 'theme'`); err != nil {
		return fmt.Errorf("drop legacy oos.ctx[theme]: %w", err)
	}
	return nil
}

// seedThemes writes the built-in light and dark themes into oos.config.
//
// One row per variant, keyed as "theme.<variant>". The XML goes into
// the xml column so the row can be fetched directly by oosp's theme
// endpoint and edited by the ooso theme panel without any envelope
// parsing. The data and json columns stay empty.
func seedThemes(db *sql.DB) error {
	for _, variant := range []string{"light", "dark"} {
		xml, err := oostheme.DefaultTheme(variant).ToXML()
		if err != nil {
			return fmt.Errorf("serialise %s theme: %w", variant, err)
		}
		ns := "theme." + variant
		if _, err := db.Exec(`
			INSERT INTO oos.config (namespace, xml)
			VALUES ($1, $2)
			ON CONFLICT (namespace) DO UPDATE SET xml = $2, updated_at = now()
		`, ns, xml); err != nil {
			return fmt.Errorf("upsert %s: %w", ns, err)
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
