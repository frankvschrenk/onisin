package store

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	_ "github.com/lib/pq"
	"onisin.com/oos-common/dsl"
	base "onisin.com/oos-dsl-base/base"
)

// PostgresStore implements ContextStore using PostgreSQL (oos.ctx, oos.dsl, oos.config).
type PostgresStore struct {
	dsn       string
	db        *sql.DB
	ast       *dsl.OOSAst
	groups    map[string]string
	ctxByName map[string]dsl.ContextAst
}

// NewPostgresStore creates a new PostgresStore for the given DSN.
func NewPostgresStore(dsn string) *PostgresStore {
	return &PostgresStore{
		dsn:       dsn,
		groups:    make(map[string]string),
		ctxByName: make(map[string]dsl.ContextAst),
	}
}

// LoadAll connects to the database and builds the in-memory AST from oos.ctx.
// If oos.ctx does not exist yet (pre-seed), starts with an empty AST.
func (s *PostgresStore) LoadAll() error {
	db, err := sql.Open("postgres", s.dsn)
	if err != nil {
		return fmt.Errorf("postgres connect: %w", err)
	}
	if err := db.Ping(); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}
	s.db = db

	if err := s.buildASTFromDB(); err != nil {
		log.Printf("[store/pg] oos.ctx not found — starting with empty AST (run --seed): %v", err)
		s.ast = dsl.BuildAST(nil)
		return nil
	}

	log.Printf("[store/pg] connected — %d contexts loaded", len(s.ast.Contexts))
	return nil
}

// Reload re-reads all oos.ctx rows and rebuilds the in-memory AST.
// Called automatically after --seed populates the tables via pg_notify.
func (s *PostgresStore) Reload() error {
	if err := s.buildASTFromDB(); err != nil {
		return fmt.Errorf("AST reload: %w", err)
	}
	log.Printf("[store/pg] reloaded — %d contexts", len(s.ast.Contexts))
	return nil
}

// GetAST returns the current AST for the first matching group.
func (s *PostgresStore) GetAST(groups []string) (*dsl.OOSAst, string, bool) {
	for _, groupName := range groups {
		role, found := s.groups[groupName]
		if !found {
			continue
		}
		log.Printf("[store/pg] gruppe %q → rolle %q", groupName, role)
		return s.ast, role, true
	}
	log.Printf("[store/pg] keine bekannte gruppe: %v", groups)
	return nil, "", false
}

// GetDSL returns the raw XML for a DSL screen definition.
func (s *PostgresStore) GetDSL(id string) (string, bool, error) {
	var xmlStr string
	err := s.db.QueryRow(`SELECT xml FROM oos.dsl WHERE id = $1`, id).Scan(&xmlStr)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("oos.dsl query: %w", err)
	}
	return xmlStr, true, nil
}

// GetConfigXML returns the xml column of the oos.config row identified
// by namespace. The second return value is false when the row does not
// exist or the xml column is NULL.
func (s *PostgresStore) GetConfigXML(namespace string) (string, bool, error) {
	var xmlStr sql.NullString
	err := s.db.QueryRow(
		`SELECT xml FROM oos.config WHERE namespace = $1`, namespace,
	).Scan(&xmlStr)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("oos.config query: %w", err)
	}
	if !xmlStr.Valid {
		return "", false, nil
	}
	return xmlStr.String, true, nil
}

// SetConfigXML upserts the xml column of the oos.config row identified
// by namespace. Other columns (data, json) are left untouched on update
// and default to empty on insert.
func (s *PostgresStore) SetConfigXML(namespace, xml string) error {
	_, err := s.db.Exec(`
		INSERT INTO oos.config (namespace, xml)
		VALUES ($1, $2)
		ON CONFLICT (namespace) DO UPDATE
			SET xml = $2, updated_at = now()
	`, namespace, xml)
	if err != nil {
		return fmt.Errorf("oos.config upsert: %w", err)
	}
	return nil
}

// GetCTXRaw returns the raw XML for a CTX context definition.
func (s *PostgresStore) GetCTXRaw(id string) (string, bool, error) {
	var xmlStr string
	err := s.db.QueryRow(`SELECT xml FROM oos.ctx WHERE id = $1`, id).Scan(&xmlStr)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("oos.ctx query: %w", err)
	}
	return xmlStr, true, nil
}

// ContextsByCTXID fetches a single oos.ctx row and returns its parsed
// ContextAst slice — the same transformation LoadAll applies to every row,
// but scoped to one id.
//
// Rows that carry no <context> (e.g. global.conf, groups) yield an empty
// slice with no error: the caller decides whether that is significant.
// Parse errors are surfaced so the caller can log them; an unparseable
// ctx row is a real problem worth noticing.
func (s *PostgresStore) ContextsByCTXID(ctxID string) ([]dsl.ContextAst, error) {
	xmlStr, found, err := s.GetCTXRaw(ctxID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	f, err := dsl.ParseBytes([]byte(xmlStr), ctxID+".ctx.xml")
	if err != nil {
		return nil, fmt.Errorf("ctx %q parse: %w", ctxID, err)
	}
	ast := dsl.BuildAST([]*dsl.DSLFile{f})
	return ast.Contexts, nil
}

// GetEnvelope wraps content with meta data and the parsed DSL tree for the
// given context.
//
// The envelope shape expected by the oos client is:
//
//	{
//	  "dsl":     <parsed DSL node tree>,   // from oos.dsl
//	  "content": <content passed in>,
//	  "meta":    <context metadata>        // optional
//	}
//
// If oos.dsl has no row for contextName the envelope comes back without
// a `dsl` key — the client then renders an error, which is still more
// useful than silently returning content-only.
func (s *PostgresStore) GetEnvelope(contextName string, content map[string]any) (map[string]any, error) {
	envelope := map[string]any{"content": content}

	// 1. Parsed DSL node tree, if present.
	xmlStr, found, err := s.GetDSL(contextName)
	if err != nil {
		return nil, fmt.Errorf("dsl fetch: %w", err)
	}
	if found {
		node, perr := base.Parse(strings.NewReader(xmlStr))
		if perr != nil {
			return nil, fmt.Errorf("dsl parse %q: %w", contextName, perr)
		}
		envelope["dsl"] = node
	} else {
		log.Printf("[store/pg] dsl %q not found", contextName)
	}

	// 2. Meta block from the context AST, if we know this context.
	if ctxAst, ok := s.ctxByName[contextName]; ok {
		meta := s.LoadMeta(&ctxAst, nil)
		if len(meta) > 0 {
			envelope["meta"] = meta
		}
	}

	return envelope, nil
}

// buildASTFromDB reads all oos.ctx rows and builds the AST and group map.
func (s *PostgresStore) buildASTFromDB() error {
	rows, err := s.db.Query(`SELECT id, xml FROM oos.ctx ORDER BY id`)
	if err != nil {
		return fmt.Errorf("oos.ctx lesen: %w", err)
	}
	defer rows.Close()

	var dslFiles []*dsl.DSLFile
	var groupsXML string

	for rows.Next() {
		var id, xmlStr string
		if err := rows.Scan(&id, &xmlStr); err != nil {
			return fmt.Errorf("oos.ctx scan: %w", err)
		}
		// Meta rows in oos.ctx that are not context definitions.
		// "groups" is handled below; "theme" is served by GetTheme()
		// and must not be parsed as a context (its root is <oos-theme>,
		// which the ctx parser rightly rejects).
		if id == "groups" {
			groupsXML = xmlStr
			continue
		}
		if id == "theme" {
			continue
		}
		f, err := dsl.ParseBytes([]byte(xmlStr), id+".ctx.xml")
		if err != nil {
			log.Printf("[store/pg] ctx %q: parse fehler: %v — übersprungen", id, err)
			continue
		}
		dslFiles = append(dslFiles, f)
	}

	s.ast = dsl.BuildAST(dslFiles)
	s.ctxByName = make(map[string]dsl.ContextAst)
	for _, c := range s.ast.Contexts {
		s.ctxByName[c.Name] = c
	}

	if groupsXML != "" {
		if err := s.parseGroups(groupsXML); err != nil {
			log.Printf("[store/pg] groups.xml: %v", err)
		}
	}

	log.Printf("[store/pg] AST: %d Contexts, %d Gruppen", len(s.ast.Contexts), len(s.groups))
	return nil
}

// parseGroups parses the groups XML and populates the groups map.
func (s *PostgresStore) parseGroups(xmlStr string) error {
	type xmlGroup struct {
		Name string `xml:"name,attr"`
		Role string `xml:"role,attr"`
	}
	type xmlGroupsFile struct {
		Groups []xmlGroup `xml:"group"`
	}

	var gf xmlGroupsFile
	if err := parseXMLString(xmlStr, &gf); err != nil {
		return fmt.Errorf("groups.xml parsen: %w", err)
	}

	for _, g := range gf.Groups {
		if g.Name != "" && g.Role != "" {
			s.groups[g.Name] = g.Role
		}
	}
	return nil
}
