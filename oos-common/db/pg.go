package db

// pg.go — ConnectPostgres bleibt für interne Nutzung erhalten.
// Für Datasources aus etcd: db.Connect(DatasourceConfig, ...) in connect.go verwenden.

import (
	"database/sql"
	"log"

	_ "github.com/lib/pq"
)

func ConnectPostgres(path string) (*sql.DB, error) {
	db, err := sql.Open("postgres", path)
	if err != nil {
		log.Printf("error opening postgres dsn: %v", err)
		return nil, err
	}
	if err := db.Ping(); err != nil {
		log.Printf("postgres ping failed: %v", err)
		return nil, err
	}
	log.Printf("postgres connection established")
	return db, nil
}
