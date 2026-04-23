package store

import (
	"database/sql"
	"fmt"
	"log"

	"onisin.com/oos-common/dsl"
)

type OptionEntry struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

func (s *PostgresStore) LoadMeta(ctxAst *dsl.ContextAst, registry map[string]*sql.DB) map[string][]OptionEntry {
	if len(ctxAst.Metas) == 0 {
		return nil
	}

	result := make(map[string][]OptionEntry, len(ctxAst.Metas))

	for _, meta := range ctxAst.Metas {
		entries, err := s.loadMetaEntries(meta, registry)
		if err != nil {
			log.Printf("[store/meta] %s/%s: %v", ctxAst.Name, meta.Name, err)
			continue
		}
		result[meta.Name] = entries
		log.Printf("[store/meta] %s/%s: %d Einträge", ctxAst.Name, meta.Name, len(entries))
	}

	return result
}

func (s *PostgresStore) loadMetaEntries(meta dsl.MetaAst, registry map[string]*sql.DB) ([]OptionEntry, error) {
	db := s.dbForDSN(meta.DSN, registry)
	if db == nil {
		return nil, fmt.Errorf("DSN %q nicht in Registry", meta.DSN)
	}

	query := buildMetaQuery(meta)
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var entries []OptionEntry
	for rows.Next() {
		var value, label string
		if err := rows.Scan(&value, &label); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		entries = append(entries, OptionEntry{Value: value, Label: label})
	}
	return entries, nil
}

func buildMetaQuery(meta dsl.MetaAst) string {
	q := fmt.Sprintf(
		`SELECT %s::text AS value, %s::text AS label FROM public.%s`,
		meta.Value, meta.Label, meta.Table,
	)
	if meta.OrderBy != "" {
		q += fmt.Sprintf(` ORDER BY %s`, meta.OrderBy)
	}
	return q
}

func (s *PostgresStore) dbForDSN(dsnName string, registry map[string]*sql.DB) *sql.DB {
	if registry != nil {
		if db, ok := registry[dsnName]; ok {
			return db
		}
	}
	return s.db
}
