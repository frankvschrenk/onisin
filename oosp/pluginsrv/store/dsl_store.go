package store

// dsl_store.go — oos.oos_dsl_schema read/write operations.
//
// DSLSchemaStore manages DSL grammar chunks (elements) and DSL pattern
// chunks (one per seeded screen) in a single table. The table is keyed
// by a kind-prefixed id:
//
//   element:field
//   element:section
//   pattern:person_detail
//   pattern:note_list
//
// Element chunks come from the XSD served out of oos.config (namespace
// "schema.dsl"). They are regenerated on every backfill so a changed
// grammar is picked up without restarting oosp — backfill is cheap
// because the XSD is ~20 KB and produces 25–30 chunks.
//
// Pattern chunks come from oos.dsl. They are re-rendered on the fly
// whenever the dsl_notify trigger fires. The ContextStore already knows
// how to fetch a raw DSL row by id (GetDSL), so this store only needs
// a DB handle for the oos_dsl_schema table itself.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/lib/pq"
)

// DSLSchemaStore handles persistence of DSL schema chunks and embeddings.
type DSLSchemaStore struct {
	db       *sql.DB
	ctxStore ContextStore
	embed    EmbedStore
}

// NewDSLSchemaStore creates a DSLSchemaStore backed by the given
// database, context store (for DSL XML fetching) and embed store.
func NewDSLSchemaStore(db *sql.DB, ctxStore ContextStore, embed EmbedStore) *DSLSchemaStore {
	return &DSLSchemaStore{db: db, ctxStore: ctxStore, embed: embed}
}

// ── Processing ────────────────────────────────────────────────────────────────

// ProcessElements regenerates every element chunk from the XSD stored
// in oos.config under namespace "schema.dsl". Safe to call on every
// startup; upsert makes it idempotent.
//
// A missing XSD row is not an error — it just means the seed has not
// run yet. Callers will retry on the next backfill.
func (s *DSLSchemaStore) ProcessElements() error {
	xsd, found, err := s.ctxStore.GetConfigXML("schema.dsl")
	if err != nil {
		return fmt.Errorf("fetch schema.dsl: %w", err)
	}
	if !found {
		log.Println("[dsl-schema] schema.dsl not in oos.config — skipping element chunks")
		return nil
	}

	chunks, err := BuildDSLElementChunks(xsd)
	if err != nil {
		return fmt.Errorf("element chunks: %w", err)
	}
	for _, chunk := range chunks {
		if err := s.upsertChunk(chunk); err != nil {
			log.Printf("[dsl-schema] ❌ %s: %v", chunk.ID, err)
			continue
		}
		log.Printf("[dsl-schema] ✅ %s", chunk.ID)
	}
	return nil
}

// ProcessPattern fetches the DSL row for the given screen id and
// regenerates its pattern chunk.
//
// A missing row is not an error: oos.dsl rows can be deleted between
// the trigger firing and the listener handling the notification. In
// that case we silently drop the chunk too — the table should never
// carry a pattern for a screen that no longer exists.
func (s *DSLSchemaStore) ProcessPattern(screenID string) error {
	xmlStr, found, err := s.ctxStore.GetDSL(screenID)
	if err != nil {
		return fmt.Errorf("fetch dsl %q: %w", screenID, err)
	}
	if !found {
		return s.deleteChunk("pattern:" + screenID)
	}

	chunk, err := BuildDSLPatternChunk(screenID, xmlStr)
	if err != nil {
		return fmt.Errorf("pattern chunk %q: %w", screenID, err)
	}
	if chunk == nil {
		return nil
	}
	if err := s.upsertChunk(*chunk); err != nil {
		return fmt.Errorf("upsert %q: %w", chunk.ID, err)
	}
	log.Printf("[dsl-schema] ✅ %s", chunk.ID)
	return nil
}

// Backfill regenerates every element chunk from the XSD and every
// pattern chunk for rows present in oos.dsl. Safe to call on every
// startup — both paths are upsert-based.
//
// A missing oos.dsl (pre-seed state) is logged and treated as a no-op,
// matching SchemaStore.Backfill's behaviour.
func (s *DSLSchemaStore) Backfill() error {
	if err := s.ProcessElements(); err != nil {
		log.Printf("[dsl-schema] elements backfill: %v", err)
	}

	rows, err := s.db.Query(`SELECT id FROM oos.dsl ORDER BY id`)
	if err != nil {
		log.Printf("[dsl-schema] patterns backfill skipped (oos.dsl not ready): %v", err)
		return nil
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		if err := s.ProcessPattern(id); err != nil {
			log.Printf("[dsl-schema] backfill pattern %s: %v", id, err)
		}
		count++
	}
	if count > 0 {
		log.Printf("[dsl-schema] backfill: %d screens processed", count)
	}
	return nil
}

// ── Retrieval ────────────────────────────────────────────────────────────────

// All returns every chunk ordered by id. Used for debugging and for
// small-context full-dump prompts.
func (s *DSLSchemaStore) All() ([]DSLChunk, error) {
	rows, err := s.db.Query(`
		SELECT id, kind, chunk
		FROM oos.oos_dsl_schema
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("dsl-schema all: %w", err)
	}
	defer rows.Close()

	var out []DSLChunk
	for rows.Next() {
		var c DSLChunk
		if err := rows.Scan(&c.ID, &c.Kind, &c.Text); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Search returns the top n chunks most similar to the query embedding.
// kindFilter, when non-empty, restricts results to that kind — useful
// when the caller knows it wants only elements or only patterns.
func (s *DSLSchemaStore) Search(ctx context.Context, query, kindFilter string, n int) ([]DSLChunk, error) {
	vector, err := s.embed.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("dsl-schema search embed: %w", err)
	}

	sqlQ := `
		SELECT id, kind, chunk
		FROM oos.oos_dsl_schema
		WHERE embedding IS NOT NULL
	`
	args := []any{vectorToString(vector), n}
	if kindFilter != "" {
		sqlQ += ` AND kind = $3`
		args = []any{vectorToString(vector), n, kindFilter}
	}
	sqlQ += `
		ORDER BY embedding <=> $1::vector
		LIMIT $2
	`

	rows, err := s.db.QueryContext(ctx, sqlQ, args...)
	if err != nil {
		return nil, fmt.Errorf("dsl-schema search query: %w", err)
	}
	defer rows.Close()

	var out []DSLChunk
	for rows.Next() {
		var c DSLChunk
		if err := rows.Scan(&c.ID, &c.Kind, &c.Text); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ── DB helpers ────────────────────────────────────────────────────────────────

// upsertChunk embeds a chunk's text and writes it into oos.oos_dsl_schema.
// Embedding is performed every time: the chunk content may have changed
// even when the id is the same (seed update, XSD revision).
func (s *DSLSchemaStore) upsertChunk(chunk DSLChunk) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vector, err := s.embed.Embed(ctx, chunk.Text)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO oos.oos_dsl_schema (id, kind, chunk, embedding, updated_at)
		VALUES ($1, $2, $3, $4::vector, now())
		ON CONFLICT (id) DO UPDATE
			SET kind = $2, chunk = $3, embedding = $4::vector, updated_at = now()
	`, chunk.ID, chunk.Kind, chunk.Text, vectorToString(vector))
	return err
}

// deleteChunk removes a row by id. Used when a pattern's source DSL row
// has been deleted — we don't want orphaned chunks polluting retrieval.
func (s *DSLSchemaStore) deleteChunk(id string) error {
	_, err := s.db.Exec(`DELETE FROM oos.oos_dsl_schema WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete %q: %w", id, err)
	}
	return nil
}

// ── Listener ──────────────────────────────────────────────────────────────────

// ListenForDSLChanges blocks and listens on 'oos_dsl_notify'. Each
// notification triggers a re-render of the affected pattern chunk.
// Call in a goroutine. Stops when ctx is cancelled.
//
// Element chunks don't have their own notify channel: the XSD changes
// only when a developer edits dsl.xsd and runs --seed, which restarts
// oosp via make/dev workflow anyway. Backfill at startup catches any
// XSD drift that happened while oosp was down.
func (s *DSLSchemaStore) ListenForDSLChanges(ctx context.Context, dsn string) {
	for {
		if err := s.listenLoop(ctx, dsn); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[dsl-schema] listener error: %v — reconnecting in 5s", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

// listenLoop opens a dedicated pq listener connection and processes events.
// Mirror of schema_store.listenLoop — same retry/keepalive semantics so
// operators see consistent behaviour across the two listeners.
func (s *DSLSchemaStore) listenLoop(ctx context.Context, dsn string) error {
	listener := pq.NewListener(dsn,
		2*time.Second, 30*time.Second,
		func(ev pq.ListenerEventType, err error) {
			if err != nil {
				log.Printf("[dsl-schema] listener event %d: %v", ev, err)
			}
		},
	)
	if err := listener.Listen("oos_dsl_notify"); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	log.Println("[dsl-schema] ✅ listening on oos_dsl_notify")

	for {
		select {
		case <-ctx.Done():
			return nil

		case n, ok := <-listener.Notify:
			if !ok {
				return fmt.Errorf("listener channel closed")
			}
			if n == nil {
				continue // keepalive ping
			}
			s.handleNotify(n.Extra)

		case <-time.After(60 * time.Second):
			if err := listener.Ping(); err != nil {
				return fmt.Errorf("ping: %w", err)
			}
		}
	}
}

// handleNotify processes a single oos_dsl_notify payload.
// Payload is the plain dsl id, e.g. "person_detail".
func (s *DSLSchemaStore) handleNotify(dslID string) {
	if dslID == "" {
		return
	}
	log.Printf("[dsl-schema] notify: processing dsl %q", dslID)
	if err := s.ProcessPattern(dslID); err != nil {
		log.Printf("[dsl-schema] notify %s: %v", dslID, err)
	}
}
