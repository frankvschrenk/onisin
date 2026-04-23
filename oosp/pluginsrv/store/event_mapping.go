package store

// event_mapping.go — Database-driven event mapping registry for oosp.
//
// Architecture:
//   public.police_incidents  → public.police_embeddings
//   public.support_tickets   → public.support_embeddings
//
// The mapping itself lives in oos.event_mappings (oosp-internal config).
// Source and target tables live wherever the application chooses — for
// the demo in public, in real deployments possibly in dedicated schemas
// or even a separate database.
//
// Auto-discovery: on start, oosp LISTENs on every notify_channel
// declared by an enabled mapping row.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	_ "github.com/lib/pq"
)

// EventMapping represents a source table → target vector table connection
type EventMapping struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`             // "criminal", "support"
	
	// Source (Event Table)
	SourceSchema     string `json:"source_schema"`    // "customer_events"
	SourceTable      string `json:"source_table"`     // "incidents"
	SourceTextField  string `json:"source_text_field"` // "text"
	SourceIDField    string `json:"source_id_field"`  // "id"
	NotifyChannel    string `json:"notify_channel"`   // "incidents_notify"
	
	// Target (Vector Table)  
	TargetSchema     string `json:"target_schema"`    // "customer_vectors"
	TargetTable      string `json:"target_table"`     // "embeddings"
	
	Enabled          bool   `json:"enabled"`
}

// EventMappingStore handles event mapping configuration
type EventMappingStore struct {
	db *sql.DB
}

// NewEventMappingStore opens a connection to the event mapping registry.
//
// DDL (schema and table creation) is the responsibility of the seed
// pipeline (oos-demo --seed), not of oosp at runtime. The oosp user holds
// only DML permissions, so any CREATE attempt here would fail.
func NewEventMappingStore(dsn string) (*EventMappingStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("event mapping store: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("event mapping store ping: %w", err)
	}

	return &EventMappingStore{db: db}, nil
}

// LoadEnabledMappings loads all enabled event mappings
func (s *EventMappingStore) LoadEnabledMappings(ctx context.Context) ([]EventMapping, error) {
	query := `
		SELECT id, name, 
		       source_schema, source_table, source_text_field, source_id_field, notify_channel,
		       target_schema, target_table, enabled
		FROM oos.event_mappings
		WHERE enabled = true
		ORDER BY name
	`
	
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("load event mappings: %w", err)
	}
	defer rows.Close()

	var mappings []EventMapping
	for rows.Next() {
		var m EventMapping
		err := rows.Scan(
			&m.ID, &m.Name,
			&m.SourceSchema, &m.SourceTable, &m.SourceTextField, &m.SourceIDField, &m.NotifyChannel,
			&m.TargetSchema, &m.TargetTable, &m.Enabled,
		)
		if err != nil {
			if DebugStore {
				log.Printf("[event-mapping] ⚠️  scan error: %v", err)
			}
			continue
		}
		mappings = append(mappings, m)
	}

	if DebugStore {
		log.Printf("[event-mapping] ✅ loaded %d mappings", len(mappings))
	}
	return mappings, nil
}

// FindMappingByChannel finds mapping by notify channel
func (s *EventMappingStore) FindMappingByChannel(ctx context.Context, channel string) (*EventMapping, error) {
	query := `
		SELECT id, name, 
		       source_schema, source_table, source_text_field, source_id_field, notify_channel,
		       target_schema, target_table, enabled
		FROM oos.event_mappings
		WHERE notify_channel = $1 AND enabled = true
	`
	
	var mapping EventMapping
	err := s.db.QueryRowContext(ctx, query, channel).Scan(
		&mapping.ID, &mapping.Name,
		&mapping.SourceSchema, &mapping.SourceTable, &mapping.SourceTextField, 
		&mapping.SourceIDField, &mapping.NotifyChannel,
		&mapping.TargetSchema, &mapping.TargetTable, &mapping.Enabled,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no mapping found for channel %s", channel)
		}
		return nil, fmt.Errorf("find mapping by channel %s: %w", channel, err)
	}
	
	return &mapping, nil
}

// GetNotifyChannels returns all notify channels for enabled mappings
func (s *EventMappingStore) GetNotifyChannels(ctx context.Context) ([]string, error) {
	mappings, err := s.LoadEnabledMappings(ctx)
	if err != nil {
		return nil, err
	}
	
	var channels []string
	for _, mapping := range mappings {
		channels = append(channels, mapping.NotifyChannel)
	}
	
	return channels, nil
}

// ValidateMapping verifies that both source and target tables exist and
// carry the columns the event pipeline expects.
//
// This is a read-only check: oosp never issues DDL. Missing tables or
// columns are a seed or migration problem, not a runtime problem oosp
// can fix on its own.
func (s *EventMappingStore) ValidateMapping(ctx context.Context, mapping EventMapping) error {
	// Source table must carry both the ID and the text field.
	srcFound, err := s.columnsPresent(ctx,
		mapping.SourceSchema, mapping.SourceTable,
		mapping.SourceIDField, mapping.SourceTextField,
	)
	if err != nil {
		return fmt.Errorf("validate source table: %w", err)
	}
	if len(srcFound) < 2 {
		return fmt.Errorf("source table %s.%s missing required columns %s, %s (found: %v)",
			mapping.SourceSchema, mapping.SourceTable,
			mapping.SourceTextField, mapping.SourceIDField, srcFound)
	}

	// Target table must exist — we don't validate every column, only that
	// the embedding column is present, since that's the one the pipeline
	// writes to on every event.
	tgtFound, err := s.columnsPresent(ctx,
		mapping.TargetSchema, mapping.TargetTable,
		"embedding",
	)
	if err != nil {
		return fmt.Errorf("validate target table: %w", err)
	}
	if len(tgtFound) == 0 {
		return fmt.Errorf("target table %s.%s missing (run oos-demo --seed)",
			mapping.TargetSchema, mapping.TargetTable)
	}

	return nil
}

// columnsPresent returns the subset of wanted columns that actually exist
// on the given table. Used by ValidateMapping for read-only schema checks.
func (s *EventMappingStore) columnsPresent(ctx context.Context, schema, table string, wanted ...string) ([]string, error) {
	if len(wanted) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(wanted))
	args := make([]any, 0, len(wanted)+2)
	args = append(args, schema, table)
	for i, col := range wanted {
		placeholders[i] = fmt.Sprintf("$%d", i+3)
		args = append(args, col)
	}

	query := fmt.Sprintf(`
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		  AND column_name IN (%s)
	`, strings.Join(placeholders, ", "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var found []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			continue
		}
		found = append(found, col)
	}
	return found, nil
}

// UpsertVector inserts/updates a vector in the target table
func (s *EventMappingStore) UpsertVector(ctx context.Context, mapping EventMapping, sourceID, streamID, eventType, textContent string, metadata map[string]any, embedding []float32) error {
	tableName := fmt.Sprintf("%s.%s", mapping.TargetSchema, mapping.TargetTable)
	
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	
	vectorStr := formatVectorForSQL(embedding)
	
	query := fmt.Sprintf(`
		INSERT INTO %s (source_id, stream_id, event_type, text_content, metadata, embedding)
		VALUES ($1, $2, $3, $4, $5, $6::vector)
		ON CONFLICT (source_id) DO UPDATE SET
			stream_id = EXCLUDED.stream_id,
			event_type = EXCLUDED.event_type, 
			text_content = EXCLUDED.text_content,
			metadata = EXCLUDED.metadata,
			embedding = EXCLUDED.embedding
	`, tableName)
	
	_, err = s.db.ExecContext(ctx, query, sourceID, streamID, eventType, textContent, metadataJSON, vectorStr)
	if err != nil {
		return fmt.Errorf("upsert vector in %s: %w", tableName, err)
	}
	
	if DebugStore {
		log.Printf("[event-mapping] ✅ upserted vector %s/%s in %s", mapping.Name, sourceID, tableName)
	}
	return nil
}

// SearchVectors performs similarity search in the target vector table
func (s *EventMappingStore) SearchVectors(ctx context.Context, mapping EventMapping, queryVector []float32, streamID string, limit int) ([]VectorSearchResult, error) {
	tableName := fmt.Sprintf("%s.%s", mapping.TargetSchema, mapping.TargetTable)
	vectorStr := formatVectorForSQL(queryVector)
	
	query := fmt.Sprintf(`
		SELECT source_id, stream_id, event_type, text_content, metadata,
		       1 - (embedding <=> $1::vector) AS score
		FROM %s
		WHERE embedding IS NOT NULL
	`, tableName)
	
	args := []any{vectorStr}
	
	if streamID != "" {
		query += ` AND stream_id = $2`
		args = append(args, streamID)
	}
	
	query += fmt.Sprintf(` ORDER BY embedding <=> $1::vector LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search vectors in %s: %w", tableName, err)
	}
	defer rows.Close()
	
	var results []VectorSearchResult
	for rows.Next() {
		var result VectorSearchResult
		var metadataJSON []byte
		
		err := rows.Scan(&result.SourceID, &result.StreamID, &result.EventType, 
			&result.TextContent, &metadataJSON, &result.Score)
		if err != nil {
			continue
		}
		
		if err := json.Unmarshal(metadataJSON, &result.Metadata); err != nil {
			result.Metadata = map[string]any{}
		}
		
		result.MappingName = mapping.Name
		results = append(results, result)
	}
	
	return results, nil
}

// VectorSearchResult represents a vector search hit
type VectorSearchResult struct {
	MappingName string         `json:"mapping_name"`
	SourceID    string         `json:"source_id"`
	StreamID    string         `json:"stream_id"`
	EventType   string         `json:"event_type"`
	TextContent string         `json:"text_content"`
	Metadata    map[string]any `json:"metadata"`
	Score       float64        `json:"score"`
}

// formatVectorForSQL converts float32 slice to PostgreSQL vector format
func formatVectorForSQL(vector []float32) string {
	if len(vector) == 0 {
		return "[]"
	}
	parts := make([]string, len(vector))
	for i, f := range vector {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// GetDistinctStreamIDs returns the distinct stream_id values present in the
// target vector table of the given mapping. This is used by the UI to let
// the user pick an existing stream without hard-coding IDs.
//
// Results are ordered alphabetically. When limit is <= 0 no LIMIT clause is
// applied.
func (s *EventMappingStore) GetDistinctStreamIDs(ctx context.Context, mapping EventMapping, limit int) ([]string, error) {
	tableName := fmt.Sprintf("%s.%s", mapping.TargetSchema, mapping.TargetTable)

	query := fmt.Sprintf(`
		SELECT DISTINCT stream_id
		FROM %s
		WHERE stream_id IS NOT NULL AND stream_id <> ''
		ORDER BY stream_id
	`, tableName)

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list stream ids in %s: %w", tableName, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Close closes the database connection
func (s *EventMappingStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
