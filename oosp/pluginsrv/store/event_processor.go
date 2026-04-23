package store

// event_processor.go — Generic Event Processing Pipeline für oosp
//
// Architecture:
//   PostgreSQL NOTIFY → ProcessEventNotification() → Embedding → Vector Storage
//
// Flow:
//   1. LISTEN auf alle konfigurierten notify_channels
//   2. Event empfangen → Mapping finden → Source-Daten laden
//   3. Text extrahieren → Embedding generieren  
//   4. Vector in domain-spezifische Tabelle schreiben

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	_ "github.com/lib/pq"
)

// EventProcessor handles generic event processing pipeline
type EventProcessor struct {
	mappingStore *EventMappingStore
	embedStore   EmbedStore
	dsn          string
}

// NewEventProcessor creates a new event processor
func NewEventProcessor(mappingStore *EventMappingStore, embedStore EmbedStore, dsn string) *EventProcessor {
	return &EventProcessor{
		mappingStore: mappingStore,
		embedStore:   embedStore,
		dsn:          dsn,
	}
}

// ProcessEventNotification processes a NOTIFY message from PostgreSQL
func (p *EventProcessor) ProcessEventNotification(ctx context.Context, channel string, payload string) error {
	// Find mapping for this channel
	mapping, err := p.mappingStore.FindMappingByChannel(ctx, channel)
	if err != nil {
		return fmt.Errorf("process event %s: %w", channel, err)
	}
	
	if DebugStore {
		log.Printf("[event-processor] processing %s event: %s", mapping.Name, channel)
	}
	
	// Parse notification payload
	var eventData map[string]any
	if err := json.Unmarshal([]byte(payload), &eventData); err != nil {
		return fmt.Errorf("parse event payload %s: %w", channel, err)
	}
	
	// Extract required fields
	sourceID, textContent, err := p.extractEventData(eventData, *mapping)
	if err != nil {
		return fmt.Errorf("extract event data %s: %w", channel, err)
	}
	
	if textContent == "" {
		if DebugStore {
			log.Printf("[event-processor] ⏭️  %s/%s: empty text, skipping", mapping.Name, sourceID)
		}
		return nil
	}
	
	// Generate embedding
	embedding, err := p.embedStore.Embed(ctx, textContent)
	if err != nil {
		return fmt.Errorf("generate embedding %s/%s: %w", mapping.Name, sourceID, err)
	}
	
	// Extract metadata
	streamID := p.getStringField(eventData, "stream", "")
	eventType := p.getStringField(eventData, "event_type", "unknown")
	
	// Build metadata from remaining fields
	metadata := make(map[string]any)
	for k, v := range eventData {
		if k != mapping.SourceIDField && k != mapping.SourceTextField && k != "stream" && k != "event_type" {
			metadata[k] = v
		}
	}
	
	// Store vector
	err = p.mappingStore.UpsertVector(ctx, *mapping, sourceID, streamID, eventType, textContent, metadata, embedding)
	if err != nil {
		return fmt.Errorf("store vector %s/%s: %w", mapping.Name, sourceID, err)
	}
	
	if DebugStore {
		log.Printf("[event-processor] ✅ %s/%s embedded and stored", mapping.Name, sourceID)
	}
	return nil
}

// ProcessUnprocessedEvents processes any unprocessed events on startup
func (p *EventProcessor) ProcessUnprocessedEvents(ctx context.Context) error {
	mappings, err := p.mappingStore.LoadEnabledMappings(ctx)
	if err != nil {
		return fmt.Errorf("load mappings for backfill: %w", err)
	}
	
	db, err := sql.Open("postgres", p.dsn)
	if err != nil {
		return fmt.Errorf("open db for backfill: %w", err)
	}
	defer db.Close()
	
	totalProcessed := 0
	for _, mapping := range mappings {
		processed, err := p.processUnprocessedForMapping(ctx, db, mapping)
		if err != nil {
			log.Printf("[event-processor] ⚠️  backfill %s: %v", mapping.Name, err)
			continue
		}
		totalProcessed += processed
	}
	
	if totalProcessed > 0 {
		log.Printf("[event-processor] ✅ backfilled %d unprocessed events", totalProcessed)
	}
	
	return nil
}

// processUnprocessedForMapping processes unprocessed events for a specific mapping
func (p *EventProcessor) processUnprocessedForMapping(ctx context.Context, db *sql.DB, mapping EventMapping) (int, error) {
	// Check if source table has a 'processed' column
	hasProcessedColumn, err := p.hasProcessedColumn(ctx, db, mapping.SourceSchema, mapping.SourceTable)
	if err != nil {
		return 0, fmt.Errorf("check processed column: %w", err)
	}
	
	if !hasProcessedColumn {
		if DebugStore {
			log.Printf("[event-processor] ⏭️  %s.%s has no 'processed' column, skipping backfill", 
				mapping.SourceSchema, mapping.SourceTable)
		}
		return 0, nil
	}
	
	// Find unprocessed events
	tableName := fmt.Sprintf("%s.%s", mapping.SourceSchema, mapping.SourceTable)
	query := fmt.Sprintf(`
		SELECT %s, %s, stream, event_type, payload
		FROM %s
		WHERE processed = false
		ORDER BY %s
		LIMIT 1000
	`, mapping.SourceIDField, mapping.SourceTextField, tableName, mapping.SourceIDField)
	
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("query unprocessed events: %w", err)
	}
	defer rows.Close()
	
	processed := 0
	for rows.Next() {
		var sourceID, textContent, stream, eventType string
		var payloadJSON []byte
		
		if err := rows.Scan(&sourceID, &textContent, &stream, &eventType, &payloadJSON); err != nil {
			if DebugStore {
				log.Printf("[event-processor] ⚠️  scan error: %v", err)
			}
			continue
		}
		
		// Process this event
		if err := p.processBackfillEvent(ctx, mapping, sourceID, textContent, stream, eventType, payloadJSON); err != nil {
			if DebugStore {
				log.Printf("[event-processor] ⚠️  backfill %s/%s: %v", mapping.Name, sourceID, err)
			}
			continue
		}
		
		// Mark as processed
		updateQuery := fmt.Sprintf(`UPDATE %s SET processed = true WHERE %s = $1`, tableName, mapping.SourceIDField)
		if _, err := db.ExecContext(ctx, updateQuery, sourceID); err != nil {
			if DebugStore {
				log.Printf("[event-processor] ⚠️  mark processed %s/%s: %v", mapping.Name, sourceID, err)
			}
		}
		
		processed++
	}
	
	if processed > 0 && DebugStore {
		log.Printf("[event-processor] ✅ %s: backfilled %d events", mapping.Name, processed)
	}
	
	return processed, nil
}

// processBackfillEvent processes a single event during backfill
func (p *EventProcessor) processBackfillEvent(ctx context.Context, mapping EventMapping, sourceID, textContent, stream, eventType string, payloadJSON []byte) error {
	if textContent == "" {
		return nil // Skip empty text
	}
	
	// Generate embedding
	embedding, err := p.embedStore.Embed(ctx, textContent)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}
	
	// Parse payload for metadata
	var metadata map[string]any
	if len(payloadJSON) > 0 {
		if err := json.Unmarshal(payloadJSON, &metadata); err != nil {
			metadata = map[string]any{}
		}
	} else {
		metadata = map[string]any{}
	}
	
	// Store vector
	return p.mappingStore.UpsertVector(ctx, mapping, sourceID, stream, eventType, textContent, metadata, embedding)
}

// hasProcessedColumn checks if a table has a 'processed' boolean column
func (p *EventProcessor) hasProcessedColumn(ctx context.Context, db *sql.DB, schema, table string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = $2 
			  AND column_name = 'processed'
			  AND data_type = 'boolean'
		)
	`
	
	var exists bool
	err := db.QueryRowContext(ctx, query, schema, table).Scan(&exists)
	return exists, err
}

// extractEventData extracts sourceID and textContent from event data
func (p *EventProcessor) extractEventData(eventData map[string]any, mapping EventMapping) (string, string, error) {
	sourceID := p.getStringField(eventData, mapping.SourceIDField, "")
	if sourceID == "" {
		return "", "", fmt.Errorf("missing %s field", mapping.SourceIDField)
	}
	
	textContent := p.getStringField(eventData, mapping.SourceTextField, "")
	
	return sourceID, textContent, nil
}

// getStringField safely extracts a string field from event data
func (p *EventProcessor) getStringField(data map[string]any, field string, defaultValue string) string {
	if val, ok := data[field]; ok {
		if str, ok := val.(string); ok {
			return str
		}
		// Handle numeric IDs
		if num, ok := val.(float64); ok {
			return fmt.Sprintf("%.0f", num)
		}
		if num, ok := val.(int64); ok {
			return fmt.Sprintf("%d", num)
		}
	}
	return defaultValue
}

// ValidateAllMappings validates all configured mappings
func (p *EventProcessor) ValidateAllMappings(ctx context.Context) error {
	mappings, err := p.mappingStore.LoadEnabledMappings(ctx)
	if err != nil {
		return fmt.Errorf("load mappings for validation: %w", err)
	}
	
	var errors []string
	for _, mapping := range mappings {
		if err := p.mappingStore.ValidateMapping(ctx, mapping); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", mapping.Name, err))
			continue
		}
		if DebugStore {
			log.Printf("[event-processor] ✅ mapping %s validated", mapping.Name)
		}
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("mapping validation errors:\n%s", strings.Join(errors, "\n"))
	}
	
	return nil
}
