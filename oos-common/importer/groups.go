package importer

// groups.go — Liest groups.xml und gibt alle Gruppen zurück.

import (
	"encoding/xml"
	"fmt"
	"os"
)

// Group beschreibt eine Gruppe aus groups.xml.
type Group struct {
	Name     string
	Role     string
	Includes []string // Dateinamen der CTX-Dateien
}

type xmlGroupsFile struct {
	Groups []xmlGroup `xml:"group"`
}

type xmlGroup struct {
	Name     string       `xml:"name,attr"`
	Role     string       `xml:"role,attr"`
	Includes []xmlInclude `xml:"include"`
}

type xmlInclude struct {
	Ctx string `xml:"ctx,attr"`
}

// ParseGroupsFile liest groups.xml und gibt alle Gruppen zurück.
func ParseGroupsFile(path string) ([]Group, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("groups.xml nicht lesbar (%s): %w", path, err)
	}

	var gf xmlGroupsFile
	if err := xml.Unmarshal(data, &gf); err != nil {
		return nil, fmt.Errorf("groups.xml parsen: %w", err)
	}

	groups := make([]Group, 0, len(gf.Groups))
	for _, g := range gf.Groups {
		includes := make([]string, 0, len(g.Includes))
		for _, inc := range g.Includes {
			includes = append(includes, inc.Ctx)
		}
		groups = append(groups, Group{
			Name:     g.Name,
			Role:     g.Role,
			Includes: includes,
		})
	}
	return groups, nil
}
