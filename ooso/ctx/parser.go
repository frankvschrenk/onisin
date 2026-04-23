package ctx

// parser.go — Thin Wrapper um oos-common/importer.
// Die eigentliche Logik liegt in oos-common/importer/groups.go.

import (
	"os"

	"onisin.com/oos-common/importer"
)

type groupEntry = importer.Group

func parseGroupsFile(path string) ([]groupEntry, error) {
	return importer.ParseGroupsFile(path)
}

func resolveEnvDSN() string {
	if v := os.Getenv("OOSO_DSN"); v != "" {
		return v
	}
	if v := os.Getenv("OOSP_CTX_DSN"); v != "" {
		return v
	}
	return ""
}
