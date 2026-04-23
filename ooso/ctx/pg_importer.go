package ctx

// pg_importer.go — Thin Wrapper um oos-common/importer.
// Die eigentliche Logik liegt in oos-common/importer/pg.go.

import "onisin.com/oos-common/importer"

type PGImporter = importer.PGImporter

func NewPGImporter(dsn string) (*PGImporter, error) {
	return importer.New(dsn)
}
