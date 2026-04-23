package core

import (
	"fmt"
	"log"

	"onisin.com/oos-common/db"
	"onisin.com/oos-common/dsl"
	"onisin.com/oos-common/gql"
	"onisin.com/oos-common/plugin"
)

type OOS struct {
	AST         *dsl.OOSAst
	DSNRegistry map[string]any
}

func Init(ctxPath string, withInfra bool) (*OOS, error) {
	var files []*dsl.DSLFile
	var err error
	if withInfra {
		files, err = dsl.LoadWithInfra(ctxPath)
	} else {
		files, err = dsl.Load(ctxPath)
	}
	if err != nil {
		return nil, err
	}
	files, err = dsl.Merge(files)
	if err != nil {
		return nil, err
	}

	ast := dsl.BuildAST(files)
	log.Printf("[oos-core] AST geladen: %d Contexts, %d Prompts", len(ast.Contexts), len(ast.Prompts))

	registry := buildRegistry(ast)
	if err := gql.BuildSchema(ast, registry); err != nil {
		return nil, err
	}

	return &OOS{AST: ast, DSNRegistry: registry}, nil
}

func InitWithDSNs(dsns map[string]string) (*OOS, error) {
	if len(dsns) == 0 {
		return nil, fmt.Errorf("keine Datasources übergeben")
	}

	registry := make(map[string]any)
	for name, dsn := range dsns {
		client, err := db.ConnectPostgres(dsn)
		if err != nil {
			log.Printf("[oos-core] ❌ DSN '%s' fehlgeschlagen: %v", name, err)
			continue
		}
		registry[name] = client
		log.Printf("[oos-core] ✅ DSN '%s' verbunden", name)
	}

	if len(registry) == 0 {
		return nil, fmt.Errorf("keine DSN-Verbindung konnte aufgebaut werden")
	}

	return &OOS{DSNRegistry: registry}, nil
}

func buildRegistry(ast *dsl.OOSAst) map[string]any {
	registry := make(map[string]any)
	for _, source := range ast.Sources {
		switch source.Type {
		case "postgres":
			client, err := db.ConnectPostgres(source.DSN)
			if err != nil {
				log.Printf("[oos-core] ❌ DSN '%s' fehlgeschlagen: %v", source.Name, err)
				continue
			}
			registry[source.Name] = client
			log.Printf("[oos-core] ✅ DSN '%s' verbunden", source.Name)
		case "plugin":
			url := source.URL
			if url == "" {
				url = source.DSN
			}
			caller, err := plugin.NewHTTPClient(url)
			if err != nil {
				log.Printf("[oos-core] ❌ Plugin '%s' nicht erreichbar: %v", source.Name, err)
				continue
			}
			if caller == nil {
				log.Printf("[oos-core] ⚠️ Plugin '%s': keine Factory registriert", source.Name)
				continue
			}
			registry[source.Name] = caller
			log.Printf("[oos-core] ✅ Plugin '%s' → %s", source.Name, url)
		default:
			log.Printf("[oos-core] ⚠️ Unbekannter DSN-Typ '%s'", source.Type)
		}
	}
	return registry
}
