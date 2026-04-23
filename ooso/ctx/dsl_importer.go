package ctx

// dsl_importer.go — Thin Wrapper um oos-common/importer.
// Die eigentliche Logik liegt in oos-common/importer/dsl.go.

import "onisin.com/oos-common/importer"

type DSLFile = importer.DSLFile

func ParseDSLFile(path string) (*DSLFile, error) {
	return importer.ParseDSLFile(path)
}

func ParseDSLDir(dir string) ([]*DSLFile, error) {
	return importer.ParseDSLDir(dir)
}
