package store

// dsl_store.go — oos.oos_dsl_schema read/write operations.
//
// DSLSchemaStore builds one element chunk per DSL element by combining
// the grammar (dsl.xsd) with the enrichment (dsl-enrichment.xml). Both
// sources live in oos.oos_dsl_meta under the namespaces 'grammar' and
// 'enrichment' respectively. The resulting chunks live in
// oos.oos_dsl_schema with kind='element' and IDs like "element:field".
//
// Rebuilds happen in two situations:
//   - On startup via Backfill — catches any seed change that happened
//     while oosp was down.
//   - On pg_notify 'oos_dsl_meta' via the listener — catches live
//     edits made by re-running --seed-internal against a hot oosp.
//
// Pattern chunks (kind='pattern') are reserved for future
// combination-level snippets and are not populated today. The agent
// loop composes full screens from multiple element retrievals
// instead.

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
// database, context store (for DSL meta XML fetching) and embed store.
func NewDSLSchemaStore(db *sql.DB, ctxStore ContextStore, embed EmbedStore) *DSLSchemaStore {
	return &DSLSchemaStore{db: db, ctxStore: ctxStore, embed: embed}
}

// ── Processing ────────────────────────────────────────────────────────────────

// RebuildElements regenerates every element chunk from the grammar and
// enrichment documents in oos.oos_dsl_meta. Safe to call on every
// startup and on every notify; upsert makes it idempotent.
//
// A missing grammar row is not an error — it means the seed has not
// run yet. A missing enrichment row is logged and the chunks are built
// with grammar facts only, which is a degraded but still useful state.
func (s *DSLSchemaStore) RebuildElements() error {
	xsd, found, err := s.ctxStore.GetDSLMeta("grammar")
	if err != nil {
		return fmt.Errorf("fetch dsl grammar: %w", err)
	}
	if !found {
		log.Println("[dsl-schema] oos.oos_dsl_meta['grammar'] not seeded yet — skipping element rebuild")
		return nil
	}

	enrichment, found, err := s.ctxStore.GetDSLMeta("enrichment")
	if err != nil {
		return fmt.Errorf("fetch dsl enrichment: %w", err)
	}
	if !found {
		log.Println("[dsl-schema] oos.oos_dsl_meta['enrichment'] not seeded — chunks will lack aliases/intent")
		enrichment = ""
	}

	chunks, err := BuildDSLElementChunks(xsd, enrichment)
	if err != nil {
		return fmt.Errorf("element chunks: %w", err)
	}

	// Sweep stale rows: anything not produced by the current grammar
	// (renamed elements, removed pattern chunks from the previous
	// regime) would otherwise stay in the table with a now-orphaned
	// id. The new ids are upserted right after, so this is safe.
	if err := s.purgeStale(chunks); err != nil {
		log.Printf("[dsl-schema] purge stale: %v", err)
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

// purgeStale removes rows from oos.oos_dsl_schema whose id is not in
// the current set. Catches renamed/removed elements and the legacy
// 'pattern:*' rows from the pre-meta regime.
func (s *DSLSchemaStore) purgeStale(current []DSLChunk) error {
	if len(current) == 0 {
		return nil
	}
	ids := make([]string, 0, len(current))
	for _, c := range current {
		ids = append(ids, c.ID)
	}
	_, err := s.db.Exec(
		`DELETE FROM oos.oos_dsl_schema WHERE id <> ALL($1)`,
		pq.Array(ids),
	)
	if err != nil {
		return fmt.Errorf("purge: %w", err)
	}
	return nil
}

// Backfill regenerates every element chunk. Called on oosp startup.
func (s *DSLSchemaStore) Backfill() error {
	return s.RebuildElements()
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

// ── Listener ──────────────────────────────────────────────────────────────────

// ListenForDSLChanges blocks and listens on 'oos_dsl_meta'. Each
// notification triggers a full rebuild of every element chunk. Element
// chunks are cross-cutting — a change to one enrichment entry can
// affect the whole batch's ordering or wording — so we always rebuild
// the full set rather than trying to target a single chunk.
//
// Call in a goroutine. Stops when ctx is cancelled.
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

// listenLoop opens a dedicated pq listener connection and processes
// events. Mirror of schema_store.listenLoop — same retry/keepalive
// semantics so operators see consistent behaviour across the two
// listeners.
func (s *DSLSchemaStore) listenLoop(ctx context.Context, dsn string) error {
	listener := pq.NewListener(dsn,
		2*time.Second, 30*time.Second,
		func(ev pq.ListenerEventType, err error) {
			if err != nil {
				log.Printf("[dsl-schema] listener event %d: %v", ev, err)
			}
		},
	)
	if err := listener.Listen("oos_dsl_meta"); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	log.Println("[dsl-schema] ✅ listening on oos_dsl_meta")

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

// handleNotify processes a single oos_dsl_meta payload. Payload is the
// namespace that changed ('grammar' or 'enrichment'); we always do a
// full rebuild regardless.
func (s *DSLSchemaStore) handleNotify(namespace string) {
	log.Printf("[dsl-schema] notify: %q changed, rebuilding element chunks", namespace)
	if err := s.RebuildElements(); err != nil {
		log.Printf("[dsl-schema] rebuild after notify %s: %v", namespace, err)
	}
}
