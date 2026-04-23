package db

// connect.go — Datenbankverbindung aus DatasourceConfig aufbauen.
//
// Connect gibt je nach Typ einen anderen Verbindungstyp zurück:
//
//	postgres / mysql / oracle  →  *sql.DB
//	mongodb                    →  *MongoCaller   (implementiert plugin.Caller)
//	neo4j / memgraph           →  *CypherCaller  (implementiert plugin.Caller)
//
// Der Aufrufer (initDatasources) legt das Ergebnis direkt in dsnRegistry[name].
// schema.go erkennt den Typ über den bestehenden Switch:
//   case *sql.DB      → SQL Query/Mutation Builder
//   case plugin.Caller → direkte JSON-Durchreichung

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	_ "github.com/go-sql-driver/mysql" // MySQL Treiber
	_ "github.com/lib/pq"              // PostgreSQL Treiber
	_ "github.com/sijms/go-ora/v2"     // Oracle Treiber (kein instantclient nötig)
)

// Connect baut eine Verbindung aus einer DatasourceConfig auf.
// Rückgabe ist *sql.DB, *MongoCaller oder *CypherCaller — je nach Type.
// vaultURL und vaultToken werden nur benötigt wenn credentials.source = "vault".
func Connect(cfg DatasourceConfig, vaultURL, vaultToken string) (any, error) {
	creds, err := ResolveCredentials(cfg.Credentials, vaultURL, vaultToken)
	if err != nil {
		return nil, fmt.Errorf("credentials [%s/%s]: %w", cfg.Type, cfg.Database, err)
	}

	switch cfg.Type {
	case "postgres", "mysql", "oracle":
		return connectSQL(cfg, creds)
	case "mongodb":
		return connectMongo(cfg, creds)
	case "neo4j", "memgraph":
		return connectCypher(cfg, creds)
	default:
		return nil, fmt.Errorf("unbekannter db-typ %q — erlaubt: postgres, mysql, oracle, mongodb, neo4j, memgraph", cfg.Type)
	}
}

// ── SQL ───────────────────────────────────────────────────────────────────────

func connectSQL(cfg DatasourceConfig, creds Credentials) (*sql.DB, error) {
	dsn, driver, err := buildDSN(cfg, creds)
	if err != nil {
		return nil, fmt.Errorf("dsn bauen: %w", err)
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("db öffnen: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("db ping: %w", err)
	}

	log.Printf("[db] verbunden: type=%s host=%s database=%s", cfg.Type, cfg.Host, cfg.Database)
	return db, nil
}

func buildDSN(cfg DatasourceConfig, creds Credentials) (dsn, driver string, err error) {
	switch cfg.Type {
	case "postgres":
		return buildPostgresDSN(cfg, creds), "postgres", nil
	case "mysql":
		return buildMySQLDSN(cfg, creds), "mysql", nil
	case "oracle":
		return buildOracleDSN(cfg, creds), "oracle", nil
	default:
		return "", "", fmt.Errorf("kein SQL-Typ: %q", cfg.Type)
	}
}

func buildPostgresDSN(cfg DatasourceConfig, creds Credentials) string {
	dsn := fmt.Sprintf("postgres://%s:%s@%s/%s",
		creds.Username, creds.Password, cfg.Host, cfg.Database)
	if len(cfg.Options) > 0 {
		dsn += "?" + buildQueryParams(cfg.Options)
	}
	return dsn
}

func buildMySQLDSN(cfg DatasourceConfig, creds Credentials) string {
	opts := map[string]string{"parseTime": "true"}
	for k, v := range cfg.Options {
		opts[k] = v
	}
	return fmt.Sprintf("%s:%s@tcp(%s)/%s?%s",
		creds.Username, creds.Password, cfg.Host, cfg.Database,
		buildQueryParams(opts))
}

func buildOracleDSN(cfg DatasourceConfig, creds Credentials) string {
	dsn := fmt.Sprintf("oracle://%s:%s@%s/%s",
		creds.Username, creds.Password, cfg.Host, cfg.Database)
	if len(cfg.Options) > 0 {
		dsn += "?" + buildQueryParams(cfg.Options)
	}
	return dsn
}

func buildQueryParams(opts map[string]string) string {
	parts := make([]string, 0, len(opts))
	for k, v := range opts {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "&")
}
