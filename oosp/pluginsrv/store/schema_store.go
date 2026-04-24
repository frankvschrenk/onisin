package store

// schema_store.go — oos.oos_ctx_schema read/write operations.
//
// SchemaStore manages the CTX schema chunks table used for AI context injection.
// On startup oosp processes all existing oos.ctx rows (backfill).
// While running it reacts to pg_notify 'oos_ctx_notify' events to keep the
// table current whenever the seed or a future editor updates oos.ctx.
//
// The chunk text is rendered from the already-parsed ContextAst carried by
// the ContextStore, not from the raw XML — that way the chunk can never
// drift from what the GraphQL schema sees. See schema_chunk.go for the
// rendering itself.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/lib/pq"
)

// SchemaStore handles persistence of CTX schema chunks and their embeddings.
type SchemaStore struct {
	db       *sql.DB
	ctxStore ContextStore
	embed    EmbedStore
	onChange func() // called after every successful CTX notify
}

// NewSchemaStore creates a SchemaStore backed by the given database,
// context store and embed store. The context store is used to translate
// an oos.ctx row id into the corresponding ContextAst slice.
func NewSchemaStore(db *sql.DB, ctxStore ContextStore, embed EmbedStore) *SchemaStore {
	return &SchemaStore{db: db, ctxStore: ctxStore, embed: embed}
}

// SetOnChange registers a callback that fires after every CTX notify is processed.
// Used by oosp to rebuild the AST and GraphQL schema after --seed.
func (s *SchemaStore) SetOnChange(fn func()) {
	s.onChange = fn
}

// ProcessCTX fetches the ContextAst slice for ctxID via the context store,
// renders one chunk per context and upserts each into oos.oos_ctx_schema.
//
// Rows that don't describe contexts (e.g. global.conf, groups) return an
// empty slice and are quietly skipped: their content is either wired
// directly into the LLM system prompt (global prompts) or is operational
// metadata the retriever has no use for. Dumping raw XML into oos_ctx_schema
// would only pollute the vector space with low-quality embeddings that
// never help and sometimes hurt retrieval accuracy.
func (s *SchemaStore) ProcessCTX(ctxID string) error {
	contexts, err := s.ctxStore.ContextsByCTXID(ctxID)
	if err != nil {
		return fmt.Errorf("ctx %q: %w", ctxID, err)
	}
	if len(contexts) == 0 {
		return nil
	}

	chunks := BuildSchemaChunks(contexts)
	for _, chunk := range chunks {
		if err := s.upsertChunk(chunk); err != nil {
			log.Printf("[schema] ❌ %s: %v", chunk.ContextName, err)
			continue
		}
		log.Printf("[schema] ✅ %s", chunk.ContextName)
	}
	return nil
}

// Backfill reads all rows from oos.ctx and processes any that are missing
// from oos.oos_ctx_schema. Safe to call on every startup.
// Returns nil if oos.ctx or oos.oos_ctx_schema do not exist yet (pre-seed state).
//
// The query targets ctx rows whose id is not represented in oos_ctx_schema. Since
// one ctx row can map to multiple context chunks (e.g. person → person_list,
// person_detail) the LIKE clause catches both exact and prefixed matches.
func (s *SchemaStore) Backfill() error {
	rows, err := s.db.Query(`
		SELECT c.id
		FROM oos.ctx c
		WHERE NOT EXISTS (
			SELECT 1 FROM oos.oos_ctx_schema os
			WHERE os.context_name = c.id
			   OR os.context_name LIKE c.id || '_%'
		)
		ORDER BY c.id
	`)
	if err != nil {
		// Tables do not exist yet — schema has not been seeded. Not an error.
		log.Printf("[schema] backfill skipped (tables not ready): %v", err)
		return nil
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		if err := s.ProcessCTX(id); err != nil {
			log.Printf("[schema] backfill %s: %v", id, err)
		}
		count++
	}
	if count > 0 {
		log.Printf("[schema] backfill: %d ctx files processed", count)
	}
	return nil
}

// All returns all schema chunks ordered by context_name.
// Does not require an embedding — used for full schema injection at startup.
func (s *SchemaStore) All() ([]SchemaChunk, error) {
	rows, err := s.db.Query(`
		SELECT context_name, chunk
		FROM oos.oos_ctx_schema
		ORDER BY context_name
	`)
	if err != nil {
		return nil, fmt.Errorf("schema all: %w", err)
	}
	defer rows.Close()

	var results []SchemaChunk
	for rows.Next() {
		var c SchemaChunk
		if err := rows.Scan(&c.ContextName, &c.Text); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, rows.Err()
}

// Search returns the top n schema chunks most similar to the query embedding.
func (s *SchemaStore) Search(ctx context.Context, query string, n int) ([]SchemaChunk, error) {
	vector, err := s.embed.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("schema search embed: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT context_name, chunk
		FROM oos.oos_ctx_schema
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $2
	`, vectorToString(vector), n)
	if err != nil {
		return nil, fmt.Errorf("schema search query: %w", err)
	}
	defer rows.Close()

	var results []SchemaChunk
	for rows.Next() {
		var c SchemaChunk
		if err := rows.Scan(&c.ContextName, &c.Text); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, rows.Err()
}

// upsertChunk embeds a single chunk and writes it to oos.oos_ctx_schema.
func (s *SchemaStore) upsertChunk(chunk SchemaChunk) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vector, err := s.embed.Embed(ctx, chunk.Text)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO oos.oos_ctx_schema (context_name, chunk, embedding, updated_at)
		VALUES ($1, $2, $3::vector, now())
		ON CONFLICT (context_name)
		DO UPDATE SET chunk = $2, embedding = $3::vector, updated_at = now()
	`, chunk.ContextName, chunk.Text, vectorToString(vector))
	return err
}

// vectorToString is defined in pg_vector.go — shared within the store package.

// ListenForCTXChanges blocks and listens on the 'oos_ctx_notify' PostgreSQL
// channel. For every notification it re-processes the affected CTX row.
// Call in a goroutine. Stops when ctx is cancelled.
func (s *SchemaStore) ListenForCTXChanges(ctx context.Context, dsn string) {
	for {
		if err := s.listenLoop(ctx, dsn); err != nil {
			if ctx.Err() != nil {
				return // cancelled — clean shutdown
			}
			log.Printf("[schema] listener error: %v — reconnecting in 5s", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

// listenLoop opens a dedicated pq listener connection and processes events.
func (s *SchemaStore) listenLoop(ctx context.Context, dsn string) error {
	listener := pq.NewListener(dsn,
		2*time.Second, 30*time.Second,
		func(ev pq.ListenerEventType, err error) {
			if err != nil {
				log.Printf("[schema] listener event %d: %v", ev, err)
			}
		},
	)
	if err := listener.Listen("oos_ctx_notify"); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	log.Println("[schema] ✅ listening on oos_ctx_notify")

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
			// Keepalive — ping the connection
			if err := listener.Ping(); err != nil {
				return fmt.Errorf("ping: %w", err)
			}
		}
	}
}

// handleNotify processes a single oos_ctx_notify payload.
// Payload is the plain ctx id, e.g. "person".
//
// Order matters here: the AST is reloaded first (via the onChange callback
// in the caller's wiring) so that ContextsByCTXID sees fresh data; then
// ProcessCTX renders new chunks off the refreshed AST.
func (s *SchemaStore) handleNotify(ctxID string) {
	if ctxID == "" {
		return
	}
	log.Printf("[schema] notify: processing ctx %q", ctxID)

	// onChange reloads the AST in the context store and rebuilds the
	// GraphQL schema. We call it before ProcessCTX so the ContextAst
	// slice ProcessCTX consumes reflects the new row.
	if s.onChange != nil {
		s.onChange()
	}

	if err := s.ProcessCTX(ctxID); err != nil {
		log.Printf("[schema] notify %s: %v", ctxID, err)
	}
}
