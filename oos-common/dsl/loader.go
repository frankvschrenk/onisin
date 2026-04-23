package dsl

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// skipFiles sind Dateinamen die der Loader immer überspringt.
// Diese Dateien haben ein anderes XML-Format und gehören nicht zum DSL.
var skipFiles = map[string]bool{
	"infra.conf.xml": true,
	"groups.xml":     true,
}

func Load(path string) ([]*DSLFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("dsl: Pfad %q nicht gefunden: %w", path, err)
	}
	if info.IsDir() {
		return loadFromDir(path)
	}
	if strings.HasSuffix(strings.ToLower(path), ".zip") {
		return loadFromZip(path)
	}
	return nil, fmt.Errorf("dsl: %q ist kein Verzeichnis und keine .zip Datei", path)
}

func loadFromDir(dir string) ([]*DSLFile, error) {
	return loadFromDirFiltered(dir, false)
}

func LoadWithInfra(path string) ([]*DSLFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("dsl: Pfad %q nicht gefunden: %w", path, err)
	}
	if info.IsDir() {
		return loadFromDirFiltered(path, true)
	}
	if strings.HasSuffix(strings.ToLower(path), ".zip") {
		return loadFromZip(path)
	}
	return nil, fmt.Errorf("dsl: %q ist kein Verzeichnis und keine .zip Datei", path)
}

func loadFromDirFiltered(dir string, includeInfra bool) ([]*DSLFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("dsl: Verzeichnis %q nicht lesbar: %w", dir, err)
	}
	var files []*DSLFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".xml") {
			continue
		}
		if skipFiles[e.Name()] {
			continue
		}
		if !includeInfra && e.Name() == "infra.conf.xml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("dsl: %q nicht lesbar: %w", path, err)
		}
		f, err := ParseBytes(data, e.Name())
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

func loadFromZip(zipPath string) ([]*DSLFile, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("dsl: ZIP %q nicht lesbar: %w", zipPath, err)
	}
	defer r.Close()
	var files []*DSLFile
	for _, f := range r.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		base := filepath.Base(f.Name)
		if strings.HasPrefix(base, ".") || skipFiles[base] {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("dsl: ZIP Eintrag %q nicht lesbar: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("dsl: ZIP Eintrag %q Lesefehler: %w", f.Name, err)
		}
		parsed, err := ParseBytes(data, base)
		if err != nil {
			return nil, err
		}
		files = append(files, parsed)
	}
	return files, nil
}

func LoadFromZipBytes(data []byte) ([]*DSLFile, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("dsl: ZIP bytes nicht lesbar: %w", err)
	}
	var files []*DSLFile
	for _, f := range r.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		base := filepath.Base(f.Name)
		if skipFiles[base] {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("dsl: ZIP Eintrag %q: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		parsed, err := ParseBytes(data, filepath.Base(f.Name))
		if err != nil {
			return nil, err
		}
		files = append(files, parsed)
	}
	return files, nil
}

func ParseFile(path string) (*DSLFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("dsl: Datei %q nicht lesbar: %w", path, err)
	}
	return ParseBytes(data, filepath.Base(path))
}

func ParseBytes(data []byte, filename string) (*DSLFile, error) {
	var ctxFile CTXFile
	if err := xmlUnmarshal(data, &ctxFile); err != nil {
		return nil, fmt.Errorf("dsl xml [%s]: %w", filename, err)
	}
	f := &DSLFile{Filename: filename, CTX: &ctxFile}
	if err := Validate(f); err != nil {
		return nil, err
	}
	return f, nil
}

func Merge(files []*DSLFile) ([]*DSLFile, error) {
	seen := map[string]string{}
	for _, f := range files {
		if f.CTX == nil {
			continue
		}
		for _, ctx := range f.CTX.Contexts {
			if prev, exists := seen[ctx.Name]; exists {
				return nil, fmt.Errorf("dsl: Context %q doppelt in %q und %q", ctx.Name, prev, f.Filename)
			}
			seen[ctx.Name] = f.Filename
		}
	}
	return files, nil
}
