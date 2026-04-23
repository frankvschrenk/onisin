package db

// mongo_caller.go — MongoDB Datasource als plugin.Caller.
//
// MongoCaller implementiert plugin.Caller und reicht Abfragen direkt
// an MongoDB durch. Die KI generiert MQL als JSON-String, OOSP führt
// es aus und gibt das Ergebnis als JSON zurück — keine Transformation.
//
// Call-Konventionen:
//   tool = Context-Name (= Collection-Name in MongoDB)
//   args["query"]  = MQL Filter als JSON, z.B. {"status": "active"}
//   args["limit"]  = optional, max. Anzahl Dokumente (default: 100)
//   args["id"]     = optional, einzelnes Dokument per _id
//
// Rückgabe: JSON-Array von Dokumenten (oder einzelnes Dokument bei id-Lookup)

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoCaller verbindet eine MongoDB-Datenbank mit dem plugin.Caller Interface.
type MongoCaller struct {
	client   *mongo.Client
	database string
}

// connectMongo baut eine MongoDB-Verbindung auf und gibt einen MongoCaller zurück.
func connectMongo(cfg DatasourceConfig, creds Credentials) (*MongoCaller, error) {
	uri := fmt.Sprintf("mongodb://%s:%s@%s/%s",
		creds.Username, creds.Password, cfg.Host, cfg.Database)

	if tls, ok := cfg.Options["tls"]; ok && tls == "true" {
		uri += "?tls=true"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongodb verbinden (%s): %w", cfg.Host, err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx) //nolint:errcheck
		return nil, fmt.Errorf("mongodb ping (%s): %w", cfg.Host, err)
	}

	log.Printf("[db] verbunden: type=mongodb host=%s database=%s", cfg.Host, cfg.Database)
	return &MongoCaller{client: client, database: cfg.Database}, nil
}

// Call führt eine MongoDB-Abfrage aus.
// tool  = Collection-Name (entspricht ctx.Source in der ctx.xml)
// args  = Abfrageparameter (query, limit, id)
func (m *MongoCaller) Call(tool string, args map[string]string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	collection := m.client.Database(m.database).Collection(tool)

	// Einzeldokument per id
	if id, ok := args["id"]; ok && id != "" {
		return m.findByID(ctx, collection, id)
	}

	// Filter aus query-Argument (MQL JSON)
	filter := bson.M{}
	if q, ok := args["query"]; ok && q != "" {
		if err := json.Unmarshal([]byte(q), &filter); err != nil {
			return "", fmt.Errorf("mongodb: query kein gültiges JSON: %w", err)
		}
	}

	// Limit
	limit := int64(100)
	if l, ok := args["limit"]; ok && l != "" {
		if parsed, err := strconv.ParseInt(l, 10, 64); err == nil {
			limit = parsed
		}
	}

	opts := options.Find().SetLimit(limit)
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return "", fmt.Errorf("mongodb find [%s]: %w", tool, err)
	}
	defer cursor.Close(ctx)

	// Ergebnis als []map — direkt JSON-serialisierbar
	var results []map[string]any
	if err := cursor.All(ctx, &results); err != nil {
		return "", fmt.Errorf("mongodb cursor [%s]: %w", tool, err)
	}

	// _id (ObjectID) ist nicht direkt JSON-serialisierbar — als String ausgeben
	for _, doc := range results {
		if id, ok := doc["_id"]; ok {
			doc["_id"] = fmt.Sprintf("%v", id)
		}
	}

	data, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("mongodb json [%s]: %w", tool, err)
	}
	return string(data), nil
}

func (m *MongoCaller) findByID(ctx context.Context, col *mongo.Collection, id string) (string, error) {
	var result map[string]any
	err := col.FindOne(ctx, bson.M{"_id": id}).Decode(&result)
	if err == mongo.ErrNoDocuments {
		return "null", nil
	}
	if err != nil {
		return "", fmt.Errorf("mongodb findByID: %w", err)
	}

	if oid, ok := result["_id"]; ok {
		result["_id"] = fmt.Sprintf("%v", oid)
	}

	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
