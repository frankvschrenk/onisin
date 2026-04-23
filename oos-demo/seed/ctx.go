package seed

// ctx.go — imports CTX and DSL XML definitions into oos.ctx and oos.dsl.

import (
	"database/sql"
)

// seedCTX writes all CTX definition files into oos.ctx.
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
