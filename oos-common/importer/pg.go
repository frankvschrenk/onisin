package importer

// pg.go — Schreibt CTX- und DSL-Dateien als XML in PostgreSQL.
//
// Schema:
//   oos.ctx  → *.ctx.xml Dateien (raw XML, id = Dateiname ohne Extension)
//   oos.dsl  → *.dsl.xml Dateien (raw XML, id = screen id="..." Attribut)

import (
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	_ "github.com/lib/pq"
)

// PGImporter schreibt OOS-Daten in PostgreSQL.
type PGImporter struct {
	db *sql.DB
}

// New öffnet eine PostgreSQL-Verbindung.
func New(dsn string) (*PGImporter, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres verbinden: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	log.Printf("[importer] verbunden mit PostgreSQL")
	return &PGImporter{db: db}, nil
}

func (imp *PGImporter) Close() {
	imp.db.Close()
}

// ImportCTXFile schreibt eine *.ctx.xml Datei in oos.ctx.
// id = Dateiname ohne Extension, z.B. "person" für person.ctx.xml
func (imp *PGImporter) ImportCTXFile(id, rawXML string) error {
	_, err := imp.db.Exec(`
		INSERT INTO oos.ctx (id, xml)
		VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE
		    SET xml        = EXCLUDED.xml,
		        updated_at = now()
	`, id, rawXML)
	if err != nil {
		return fmt.Errorf("oos.ctx upsert %q: %w", id, err)
	}
	return nil
}

// ImportGroupsFile schreibt groups.xml in oos.ctx mit id="groups".
func (imp *PGImporter) ImportGroupsFile(rawXML string) error {
	return imp.ImportCTXFile("groups", rawXML)
}

// ImportGlobalFile schreibt global.conf.xml in oos.ctx mit id="global.conf".
func (imp *PGImporter) ImportGlobalFile(rawXML string) error {
	return imp.ImportCTXFile("global.conf", rawXML)
}

// ImportGroup schreibt alle CTX-Dateien einer Gruppe in oos.ctx.
func (imp *PGImporter) ImportGroup(groupName string, files map[string]string) error {
	for filename, rawXML := range files {
		id := ctxID(filename)
		if err := imp.ImportCTXFile(id, rawXML); err != nil {
			return fmt.Errorf("gruppe %q / datei %q: %w", groupName, filename, err)
		}
	}
	return nil
}

// ImportDSL schreibt eine DSL-Datei in oos.dsl.
func (imp *PGImporter) ImportDSL(screenID, rawXML string) error {
	_, err := imp.db.Exec(`
		INSERT INTO oos.dsl (id, xml)
		VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE
		    SET xml        = EXCLUDED.xml,
		        updated_at = now()
	`, screenID, rawXML)
	if err != nil {
		return fmt.Errorf("oos.dsl upsert %q: %w", screenID, err)
	}
	return nil
}

// GetDSLIDs gibt alle bekannten DSL Screen-IDs zurück.
func (imp *PGImporter) GetDSLIDs() ([]string, error) {
	rows, err := imp.db.Query(`SELECT id FROM oos.dsl ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// GetCTXIDs gibt alle bekannten CTX IDs zurück.
func (imp *PGImporter) GetCTXIDs() ([]string, error) {
	rows, err := imp.db.Query(`SELECT id FROM oos.ctx ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ctxID leitet die DB-ID aus einem Dateinamen ab.
// "person.ctx.xml" → "person", "global.conf.xml" → "global.conf"
func ctxID(filename string) string {
	base := filepath.Base(filename)
	base = strings.TrimSuffix(base, ".ctx.xml")
	base = strings.TrimSuffix(base, ".xml")
	return base
}
