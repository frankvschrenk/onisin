package ctx

// command.go — CLI Subcommands für den Synthetist.
//
//	ooso add group              — alle Gruppen + ihre CTX-Dateien importieren
//	ooso add group oos-admin    — nur Gruppe "oos-admin" importieren
//	ooso add context person     — nur person.ctx.xml aktualisieren
//	ooso add global             — global.conf.xml schreiben
//	ooso dsl add <datei>        — x-DSL Datei(en) in PostgreSQL schreiben
//	ooso dsl add-dir <verz>     — alle *.dsl.xml aus einem Verzeichnis
//	ooso dsl list               — zeigt alle bekannten DSL-IDs
//	ooso dsl preview            — DSL live rendern mit File Watcher
//	ooso list                   — zeigt alle CTX-IDs in der DB

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// newDSLPreviewCommand — siehe preview.go

// RegisterCommands registriert alle Flags und Sub-Commands direkt am Root-Command.
func RegisterCommands(root *cobra.Command) {
	var pgDSN string
	var groupsFile string

	root.PersistentFlags().StringVar(&pgDSN, "dsn",
		"", "PostgreSQL DSN (oder OOSO_DSN env var)")
	root.PersistentFlags().StringVar(&groupsFile, "groups",
		"groups.xml", "Pfad zur groups.xml Datei")

	root.AddCommand(newAddCommand(&pgDSN, &groupsFile))
	root.AddCommand(newDSLCommand(&pgDSN))
	root.AddCommand(newListCommand(&pgDSN))
}

// ── add (CTX) ─────────────────────────────────────────────────────────────────

func newAddCommand(dsn, groupsFile *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "CTX-Dateien in PostgreSQL schreiben",
	}
	cmd.AddCommand(newAddGroupCommand(dsn, groupsFile))
	cmd.AddCommand(newAddContextCommand(dsn, groupsFile))
	cmd.AddCommand(newAddGlobalCommand(dsn, groupsFile))
	return cmd
}

// ooso add group [name]
func newAddGroupCommand(dsn, groupsFile *string) *cobra.Command {
	return &cobra.Command{
		Use:   "group [name]",
		Short: "Gruppe(n) aus groups.xml importieren",
		Long: `Liest groups.xml und schreibt alle referenzierten *.ctx.xml Dateien
sowie groups.xml selbst in oos.ctx.

Beispiele:
  ooso --groups ./ctx/groups.xml add group
  ooso --groups ./ctx/groups.xml add group oos-admin`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			imp, err := NewPGImporter(resolveDSN(*dsn))
			if err != nil {
				return err
			}
			defer imp.Close()

			groups, err := parseGroupsFile(*groupsFile)
			if err != nil {
				return fmt.Errorf("groups.xml lesen: %w", err)
			}

			// groups.xml selbst importieren
			groupsXML, err := os.ReadFile(*groupsFile)
			if err != nil {
				return fmt.Errorf("groups.xml lesen: %w", err)
			}
			if err := imp.ImportGroupsFile(string(groupsXML)); err != nil {
				return fmt.Errorf("groups.xml importieren: %w", err)
			}

			var toImport []groupEntry
			if len(args) == 1 {
				name := args[0]
				for _, g := range groups {
					if g.Name == name {
						toImport = append(toImport, g)
						break
					}
				}
				if len(toImport) == 0 {
					return fmt.Errorf("gruppe %q nicht in groups.xml gefunden", name)
				}
			} else {
				toImport = groups
			}

			// CTX-Dateien jeder Gruppe importieren
			ctxDir := filepath.Dir(*groupsFile)
			imported := map[string]bool{} // Deduplizierung über Gruppen hinweg
			for _, g := range toImport {
				files, err := readGroupFiles(g, ctxDir)
				if err != nil {
					return fmt.Errorf("gruppe %q: %w", g.Name, err)
				}
				if err := imp.ImportGroup(g.Name, files); err != nil {
					return fmt.Errorf("gruppe %q: %w", g.Name, err)
				}
				for filename := range files {
					if !imported[filename] {
						imported[filename] = true
						fmt.Printf("✓  %s\n", filename)
					}
				}
				fmt.Printf("✓  Gruppe %s (%s)\n", g.Name, g.Role)
			}

			fmt.Printf("\n✅ %d Gruppe(n), %d CTX-Datei(en) importiert\n",
				len(toImport), len(imported))
			return nil
		},
	}
}

// ooso add context <name>
func newAddContextCommand(dsn, groupsFile *string) *cobra.Command {
	return &cobra.Command{
		Use:   "context <name>",
		Short: "Eine einzelne CTX-Datei aktualisieren",
		Long: `Liest <name>.ctx.xml und schreibt sie in oos.ctx.

Beispiel:
  ooso --groups ./ctx/groups.xml add context person`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			imp, err := NewPGImporter(resolveDSN(*dsn))
			if err != nil {
				return err
			}
			defer imp.Close()

			filename := args[0] + ".ctx.xml"
			ctxDir := filepath.Dir(*groupsFile)
			path := filepath.Join(ctxDir, filename)

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("datei %q nicht lesbar: %w", path, err)
			}
			if err := imp.ImportCTXFile(args[0], string(data)); err != nil {
				return err
			}
			fmt.Printf("✅ CTX %q aktualisiert\n", args[0])
			return nil
		},
	}
}

// ooso add global
func newAddGlobalCommand(dsn, groupsFile *string) *cobra.Command {
	return &cobra.Command{
		Use:   "global",
		Short: "global.conf.xml schreiben",
		RunE: func(cmd *cobra.Command, args []string) error {
			imp, err := NewPGImporter(resolveDSN(*dsn))
			if err != nil {
				return err
			}
			defer imp.Close()

			ctxDir := filepath.Dir(*groupsFile)
			path := filepath.Join(ctxDir, "global.conf.xml")

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("global.conf.xml nicht lesbar: %w", err)
			}
			if err := imp.ImportGlobalFile(string(data)); err != nil {
				return err
			}
			fmt.Println("✅ global.conf.xml importiert")
			return nil
		},
	}
}

// ── dsl (x-DSL) ───────────────────────────────────────────────────────────────

func newDSLCommand(dsn *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dsl",
		Short: "x-DSL Dateien (Loomer-Views) in PostgreSQL schreiben oder live vorschauen",
	}
	cmd.AddCommand(newDSLAddCommand(dsn))
	cmd.AddCommand(newDSLAddDirCommand(dsn))
	cmd.AddCommand(newDSLListCommand(dsn))
	cmd.AddCommand(newDSLPreviewCommand())
	return cmd
}

// ooso dsl add <datei> [datei2 ...]
func newDSLAddCommand(dsn *string) *cobra.Command {
	return &cobra.Command{
		Use:   "add <datei> [datei2 ...]",
		Short: "x-DSL Datei(en) in PostgreSQL schreiben",
		Long: `Validiert *.dsl.xml Dateien und schreibt das XML in oos.dsl.
Der <screen id="..."> wird als Schlüssel verwendet.

Beispiele:
  ooso dsl add person-detail.dsl.xml
  ooso dsl add ./dsl/person-detail.dsl.xml ./dsl/person-list.dsl.xml`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			imp, err := NewPGImporter(resolveDSN(*dsn))
			if err != nil {
				return err
			}
			defer imp.Close()

			for _, path := range args {
				f, err := ParseDSLFile(path)
				if err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				if err := imp.ImportDSL(f.ScreenID, f.RawXML); err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				fmt.Printf("✓  %s → oos.dsl[%q]\n", path, f.ScreenID)
			}

			fmt.Printf("\n✅ %d DSL-Datei(en) importiert\n", len(args))
			return nil
		},
	}
}

// ooso dsl add-dir <verzeichnis>
func newDSLAddDirCommand(dsn *string) *cobra.Command {
	return &cobra.Command{
		Use:   "add-dir <verzeichnis>",
		Short: "Alle *.dsl.xml Dateien eines Verzeichnisses importieren",
		Long: `Liest alle *.dsl.xml Dateien aus einem Verzeichnis und schreibt sie in oos.dsl.

Beispiel:
  ooso dsl add-dir ./dsl`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			imp, err := NewPGImporter(resolveDSN(*dsn))
			if err != nil {
				return err
			}
			defer imp.Close()

			files, err := ParseDSLDir(args[0])
			if err != nil {
				return err
			}
			if len(files) == 0 {
				fmt.Println("Keine *.dsl.xml Dateien gefunden")
				return nil
			}

			for _, f := range files {
				if err := imp.ImportDSL(f.ScreenID, f.RawXML); err != nil {
					return fmt.Errorf("%s: %w", f.Filename, err)
				}
				fmt.Printf("✓  %s → oos.dsl[%q]\n", f.Filename, f.ScreenID)
			}

			fmt.Printf("\n✅ %d DSL-Datei(en) importiert\n", len(files))
			return nil
		},
	}
}

// ooso dsl list
func newDSLListCommand(dsn *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Zeigt alle DSL-IDs in oos.dsl",
		RunE: func(cmd *cobra.Command, args []string) error {
			imp, err := NewPGImporter(resolveDSN(*dsn))
			if err != nil {
				return err
			}
			defer imp.Close()

			ids, err := imp.GetDSLIDs()
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				fmt.Println("Keine DSL-Dateien — ooso dsl add <datei> ausführen")
				return nil
			}
			fmt.Printf("\nDSL-Dateien in oos.dsl (%d):\n", len(ids))
			for _, id := range ids {
				fmt.Printf("  → %s\n", id)
			}
			fmt.Println()
			return nil
		},
	}
}

// ── list ──────────────────────────────────────────────────────────────────────

func newListCommand(dsn *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Zeigt alle CTX-IDs in oos.ctx",
		RunE: func(cmd *cobra.Command, args []string) error {
			imp, err := NewPGImporter(resolveDSN(*dsn))
			if err != nil {
				return err
			}
			defer imp.Close()

			ids, err := imp.GetCTXIDs()
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				fmt.Println("Keine CTX-Dateien — ooso add group ausführen")
				return nil
			}
			fmt.Printf("\nCTX-Dateien in oos.ctx (%d):\n", len(ids))
			for _, id := range ids {
				fmt.Printf("  → %s\n", id)
			}
			fmt.Println()
			return nil
		},
	}
}

// ── Hilfsfunktionen ───────────────────────────────────────────────────────────

// readGroupFiles liest alle CTX-Dateien einer Gruppe als map[filename]rawXML.
func readGroupFiles(g groupEntry, ctxDir string) (map[string]string, error) {
	files := make(map[string]string)
	for _, inc := range g.Includes {
		path := filepath.Join(ctxDir, inc)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("datei %q nicht lesbar: %w", path, err)
		}
		files[inc] = string(data)
	}
	return files, nil
}

func resolveDSN(flagDSN string) string {
	if flagDSN != "" {
		return flagDSN
	}
	if dsn := resolveEnvDSN(); dsn != "" {
		return dsn
	}
	return "postgres://postgres:postgres@localhost:5432/oos?sslmode=disable"
}
