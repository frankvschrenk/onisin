package importer

// dsl.go — Liest und validiert *.dsl.xml Dateien für den Import.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	base "onisin.com/oos-dsl-base/base"
	"onisin.com/oos-dsl/dsl"
)

// DSLFile repräsentiert eine gelesene und validierte DSL-Datei.
type DSLFile struct {
	ScreenID string        // Wert des id-Attributs aus <screen id="...">
	Root     *dsl.Node // Geparster Node-Baum (zur Validierung)
	RawXML   string        // Original-XML — wird unverändert in oos.dsl gespeichert
	Filename string
}

// ParseDSLFile liest und validiert eine *.dsl.xml Datei.
func ParseDSLFile(path string) (*DSLFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("dsl: %q nicht lesbar: %w", path, err)
	}

	root, err := base.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("dsl parse [%s]: %w", filepath.Base(path), err)
	}

	screenID := root.Attr("id", "")
	if screenID == "" {
		return nil, fmt.Errorf("dsl [%s]: <screen> hat kein id-Attribut", filepath.Base(path))
	}

	return &DSLFile{
		ScreenID: screenID,
		Root:     root,
		RawXML:   string(data),
		Filename: filepath.Base(path),
	}, nil
}

// ParseDSLDir liest alle *.dsl.xml Dateien aus einem Verzeichnis.
func ParseDSLDir(dir string) ([]*DSLFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("dsl dir %q: %w", dir, err)
	}

	var files []*DSLFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".dsl.xml") {
			continue
		}
		f, err := ParseDSLFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// ParseDSLBytes parst DSL-Inhalt direkt aus einem Byte-Slice.
// Nützlich wenn die Datei aus embed.FS kommt.
func ParseDSLBytes(data []byte, filename string) (*DSLFile, error) {
	root, err := base.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("dsl parse [%s]: %w", filename, err)
	}

	screenID := root.Attr("id", "")
	if screenID == "" {
		return nil, fmt.Errorf("dsl [%s]: <screen> hat kein id-Attribut", filename)
	}

	return &DSLFile{
		ScreenID: screenID,
		Root:     root,
		RawXML:   string(data),
		Filename: filename,
	}, nil
}
