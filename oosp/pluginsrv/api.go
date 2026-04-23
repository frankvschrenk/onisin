package pluginsrv

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
	"onisin.com/oos-common/dsl"
	"onisin.com/oos-common/gql"
	"onisin.com/oosp/pluginsrv/store"
)

func GetAST(groups []string) (*dsl.OOSAst, string, bool) {
	if activeStore == nil {
		return nil, "", false
	}
	ast, role, ok := activeStore.GetAST(groups)
	if !ok {
		return nil, "", false
	}
	astWithTools := *ast
	astWithTools.Tools = dsl.OOSPTools()
	return &astWithTools, role, true
}

func ExecuteQuery(query string) (any, error) {
	result, err := gql.Execute(query, nil)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	return result.Data, nil
}

// ExecuteMutation runs a raw GraphQL mutation string against the active
// schema. It is the counterpart to ExecuteQuery and uses the exact same
// executor — in graphql-go there is no separate entry point for mutations,
// the document root ("mutation { ... }") is what distinguishes them.
//
// The statement is not validated against a role here; enforcement is the
// handler's job and — ultimately — the GraphQL resolver's job. This
// function is only concerned with turning the string into a result.
//
// graphql-go quirk: resolver errors do NOT come back as the top-level
// error return; they sit in result.Errors as plain strings in this
// wrapper. We surface the first one explicitly — otherwise a failed
// mutation would return HTTP 200 with an empty data field, which on the
// client looks exactly like the "nothing happens" symptom we were
// debugging.
func ExecuteMutation(statement string) (any, error) {
	result, err := gql.Execute(statement, nil)
	if err != nil {
		return nil, fmt.Errorf("mutation: %w", err)
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("mutation: %s", result.Errors[0])
	}
	return result.Data, nil
}

func ExecuteSave(ctxName string, data map[string]any) (any, error) {
	mutation, err := gql.BuildMutationFromMap(ctxName, data)
	if err != nil {
		return nil, fmt.Errorf("mutation bauen: %w", err)
	}
	result, err := gql.Execute(mutation, nil)
	if err != nil {
		return nil, fmt.Errorf("save: %w", err)
	}
	return result.Data, nil
}

func GetDSL(id string) (string, bool, error) {
	if activeStore == nil {
		return "", false, fmt.Errorf("kein Store konfiguriert")
	}
	return activeStore.GetDSL(id)
}

func GetEnvelope(contextName string, content map[string]any) (map[string]any, error) {
	if activeStore == nil {
		return map[string]any{"content": content}, nil
	}
	return activeStore.GetEnvelope(contextName, content)
}

func Embed(text string) ([]float32, error) {
	if activeEmbed == nil {
		return nil, fmt.Errorf("Embed Store nicht konfiguriert")
	}
	return activeEmbed.Embed(context.Background(), text)
}

func VectorUpsert(collection string, id uint64, vector []float32, payload map[string]string) error {
	if activeVector == nil {
		return fmt.Errorf("Vector Store nicht konfiguriert")
	}
	if err := activeVector.EnsureCollection(context.Background(), collection, uint64(len(vector))); err != nil {
		return fmt.Errorf("collection: %w", err)
	}
	return activeVector.Upsert(context.Background(), collection, store.VectorPoint{
		ID:      id,
		Vector:  vector,
		Payload: payload,
	})
}

func VectorSearch(collection string, vector []float32, filter map[string]string, n uint64) (any, error) {
	if activeVector == nil {
		return nil, fmt.Errorf("Vector Store nicht konfiguriert")
	}
	return activeVector.Search(context.Background(), collection, vector, filter, n)
}

// GetTheme returns the active UI theme XML from oos.ctx.
func GetTheme() (string, error) {
	if activeStore == nil {
		return "", nil
	}
	xml, _, err := activeStore.GetCTXRaw("theme")
	return xml, err
}

// SchemaAll returns all schema chunks without embedding search.
// Used by oos at startup to inject the full schema into the AI prompt.
func SchemaAll() ([]store.SchemaChunk, error) {
	if activeSchema == nil {
		return nil, fmt.Errorf("schema store not initialised")
	}
	return activeSchema.All()
}

// SchemaSearch returns the top n schema chunks most similar to query.
// Used by oos to inject only the relevant schema context into the AI prompt.
func SchemaSearch(query string, n int) ([]store.SchemaChunk, error) {
	if activeSchema == nil {
		return nil, fmt.Errorf("schema store not initialised")
	}
	return activeSchema.Search(context.Background(), query, n)
}

// openDB opens a new *sql.DB connection to dsn.
func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("db open: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("db ping: %w", err)
	}
	return db, nil
}
