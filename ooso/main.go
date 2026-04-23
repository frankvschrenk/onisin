package main

// main.go — ooso, der OOS Synthetist.
//
// Ohne Argumente: startet die grafische Oberfläche.
// Mit Argumenten:  CLI-Modus (cobra commands — unverändert).
//
// GUI-Features:
//   - CTX  : groups.xml wählen, Gruppen importieren
//   - DSL  : *.dsl.xml Dateien importieren + live Preview
//   - Theme: Dark/Light wählen und in CTX speichern
//
// CLI-Features (Batch/Scripting):
//   ooso add group
//   ooso add group oos-admin
//   ooso add context person
//   ooso dsl add person-detail.dsl.xml
//   ooso dsl add-dir ./dsl
//   ooso dsl preview --dsl <datei> --data <json>

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"onisin.com/ooso/ctx"
	"onisin.com/ooso/gui"
)

var VERSION = "dev"

func main() {
	// Kein Argument → GUI starten
	if len(os.Args) == 1 {
		gui.Run()
		return
	}

	// Mit Argumenten → CLI-Modus
	root := buildCLI()
	if err := root.Execute(); err != nil {
		log.Printf("Fehler: %v", err)
		os.Exit(1)
	}
}

// buildCLI registriert alle cobra-Commands (unverändert gegenüber vorher).
func buildCLI() *cobra.Command {
	root := &cobra.Command{
		Use:   "ooso",
		Short: "OOS Synthetist — importiert CTX und DSL in PostgreSQL",
		Long: `Verwaltet OOS Context- und DSL-Definitionen in PostgreSQL.

Ohne Argumente startet die grafische Oberfläche.

Beispiele:
  ooso --groups ./ctx/groups.xml add group
  ooso --groups ./ctx/groups.xml add group oos-admin
  ooso --groups ./ctx/groups.xml add context person
  ooso dsl add ./dsl/person-detail.dsl.xml
  ooso dsl add-dir ./dsl
  ooso dsl preview --dsl ./dsl/person-detail.dsl.xml --data ./data/person.json`,
	}

	ctx.RegisterCommands(root)

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Zeigt die Version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("ooso %s\n", VERSION)
		},
	})

	return root
}
