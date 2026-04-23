package store

// new.go — Erstellt den ContextStore.
//
// Einziges aktives Backend: PostgresStore (reines PostgreSQL, kein AGE).
//
//   oos.ctx  → CTX-Dateien als XML → AST wird dynamisch gebaut
//   oos.dsl  → DSL-Dateien als XML → wird on-demand gelesen
//   public.* → Anwendungsdaten + Referenztabellen

import "fmt"

func New(dsn string) (ContextStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("OOSP_CTX_DSN fehlt (postgres://host/db?sslmode=disable)")
	}
	return NewPostgresStore(dsn), nil
}
