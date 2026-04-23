package db

// cypher_caller.go — Neo4j / Memgraph Datasource als plugin.Caller.
//
// CypherCaller implementiert plugin.Caller und führt Cypher-Abfragen
// direkt gegen Neo4j oder Memgraph aus. Die KI generiert Cypher,
// OOSP führt es aus und gibt das Ergebnis als JSON zurück.
//
// Call-Konventionen:
//   tool = Context-Name (wird als Label-Hinweis verwendet)
//   args["query"]  = vollständige Cypher-Query, z.B.:
//                    MATCH (n:Person) WHERE n.active = true RETURN n LIMIT 50
//   args["id"]     = optional, einzelner Knoten per id-Property
//
// Rückgabe: JSON-Array der RETURN-Spalten
//
// Hinweis: Memgraph und Neo4j sprechen beide Bolt und sind mit demselben
// Treiber (neo4j-go-driver/v5) ansprechbar. type="memgraph" und type="neo4j"
// landen beide hier.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// CypherCaller verbindet eine Bolt-kompatible Graph-DB mit dem plugin.Caller Interface.
type CypherCaller struct {
	driver   neo4j.DriverWithContext
	dbType   string // "neo4j" oder "memgraph" — nur für Logging
	database string // Neo4j: Datenbankname / Memgraph: ignoriert
}

// connectCypher baut eine Bolt-Verbindung auf und gibt einen CypherCaller zurück.
func connectCypher(cfg DatasourceConfig, creds Credentials) (*CypherCaller, error) {
	uri := fmt.Sprintf("bolt://%s", cfg.Host)
	auth := neo4j.BasicAuth(creds.Username, creds.Password, "")

	driver, err := neo4j.NewDriverWithContext(uri, auth)
	if err != nil {
		return nil, fmt.Errorf("%s verbinden (%s): %w", cfg.Type, cfg.Host, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := driver.VerifyConnectivity(ctx); err != nil {
		driver.Close(ctx) //nolint:errcheck
		return nil, fmt.Errorf("%s ping (%s): %w", cfg.Type, cfg.Host, err)
	}

	log.Printf("[db] verbunden: type=%s host=%s database=%s", cfg.Type, cfg.Host, cfg.Database)
	return &CypherCaller{driver: driver, dbType: cfg.Type, database: cfg.Database}, nil
}

// Call führt eine Cypher-Query aus.
// tool  = Context-Name
// args  = Abfrageparameter (query, id)
func (c *CypherCaller) Call(tool string, args map[string]string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Einzelknoten per id
	if id, ok := args["id"]; ok && id != "" {
		query := fmt.Sprintf("MATCH (n:%s {id: $id}) RETURN n", tool)
		result, err := neo4j.ExecuteQuery(ctx, c.driver, query,
			map[string]any{"id": id},
			neo4j.EagerResultTransformer,
		)
		if err != nil {
			return "", fmt.Errorf("%s query [%s]: %w", c.dbType, tool, err)
		}
		return recordsToJSON(result.Records)
	}

	// Cypher-Query aus args["query"] — von der KI generiert
	query, ok := args["query"]
	if !ok || query == "" {
		// Fallback: alle Knoten mit dem Label des Context-Namens
		query = fmt.Sprintf("MATCH (n:%s) RETURN n LIMIT 100", tool)
	}

	result, err := neo4j.ExecuteQuery(ctx, c.driver, query,
		map[string]any{},
		neo4j.EagerResultTransformer,
	)
	if err != nil {
		return "", fmt.Errorf("%s query [%s]: %w", c.dbType, tool, err)
	}

	return recordsToJSON(result.Records)
}

// recordsToJSON wandelt Neo4j/Memgraph Records in einen JSON-String um.
// Nodes werden zu ihren Properties abgeflacht.
func recordsToJSON(records []*neo4j.Record) (string, error) {
	results := make([]map[string]any, 0, len(records))

	for _, record := range records {
		row := map[string]any{}
		for _, key := range record.Keys {
			val, _ := record.Get(key)
			row[key] = flattenValue(val)
		}
		results = append(results, row)
	}

	data, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("cypher json: %w", err)
	}
	return string(data), nil
}

// flattenValue konvertiert Neo4j-Typen (Node, Relationship) in einfache Maps.
// Primitive Typen werden direkt durchgereicht.
func flattenValue(val any) any {
	switch v := val.(type) {
	case neo4j.Node:
		props := map[string]any{}
		for k, pv := range v.Props {
			props[k] = flattenValue(pv)
		}
		return props
	case neo4j.Relationship:
		props := map[string]any{
			"_type":  v.Type,
			"_start": v.StartElementId,
			"_end":   v.EndElementId,
		}
		for k, pv := range v.Props {
			props[k] = flattenValue(pv)
		}
		return props
	default:
		return v
	}
}
