// Tool: memory — memory_write, memory_search, memory_list, memory_delete
//
// Claude's long-term memory across sessions. Lives in its own database
// ("claude") separate from the onisin demo DB so the two never mingle.
// Credentials are borrowed from demo.toml (same Postgres instance, same
// superuser) but the database name is hardcoded to "claude".
//
// Schema is bootstrapped lazily: the first memory_write / memory_search
// call ensures the database exists, enables pgvector, and creates the
// memory table. No separate seed step.
//
// Each row captures a single self-contained unit of understanding
// Claude produced while working: a design decision, a pattern, a fact
// about the codebase, an open question, or a pitfall. Embeddings are
// computed via Ollama's OpenAI-compatible /v1/embeddings endpoint with
// granite-embedding:latest — the same model oosp uses for its CTX/DSL
// retrieval stores, so the embedding space is consistent.
//
// When Ollama is unreachable, memory_write stores the row with
// embedding = NULL. memory_search then reports how many rows have an
// embedding so the caller can see whether search is actually effective
// or just showing a random slice of the table.
//
// kind values:
//
//	decision       a design choice and the reasoning behind it
//	pattern        a recurring structural/code idiom in the project
//	fact           a piece of project state worth remembering
//	open_question  something unresolved that needs follow-up
//	pitfall        a trap Claude hit or wants to avoid next time

package tools

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// memoryDatabase is the fixed database name for Claude's memory store.
// Lives in the same Postgres instance as onisin but isolated by DB
// boundary so DROP DATABASE onisin or pg_reset can never lose memory.
const memoryDatabase = "claude"

// memoryEmbedModel is the Ollama model used for embeddings. Matches
// what oosp uses in its CTX/DSL retrieval stores so vectors are
// comparable — Claude's memory and the project's own retrieval stores
// sit in the same 384-dim space.
const memoryEmbedModel = "granite-embedding:latest"

// memoryEmbedBaseURL is the OpenAI-compatible endpoint Ollama exposes.
// Hardcoded for now — the rare case of a remote Ollama deployment can
// be added later as an env var if needed.
const memoryEmbedBaseURL = "http://localhost:11434/v1"

// memoryValidKinds enumerates the allowed values of the kind column.
// Enforced both by a DB CHECK constraint and by the Go handlers so bad
// input fails fast with a readable error rather than a SQL violation.
var memoryValidKinds = map[string]bool{
	"decision":      true,
	"pattern":       true,
	"fact":          true,
	"open_question": true,
	"pitfall":       true,
}

// registerMemory wires the memory_* tools into the MCP server.
//
// Requires demo.toml for Postgres connection info, same as the pg_*
// tools. Without it, memory tools are silently absent.
func registerMemory(s *server.MCPServer, ctx *handlerCtx) {
	cfg := findDemoTOML(ctx.reg, ctx.logger)
	if cfg == nil {
		ctx.logger.Info("memory tools not registered — no demo.toml in any allowed root")
		return
	}
	ctx.logger.Info("memory tools enabled",
		"database", memoryDatabase,
		"embed_model", memoryEmbedModel,
	)
	mc := &memoryCtx{handlerCtx: ctx, cfg: cfg}

	writeTool := mcp.NewTool("memory_write",
		mcp.WithDescription(
			"Record a unit of understanding into Claude's long-term memory "+
				"so a future session can retrieve it. Use this while working "+
				"to save design decisions, patterns, facts, open questions "+
				"and pitfalls — NOT verbatim commands from the user. Each "+
				"memory is embedded and later searchable by meaning. "+
				"kind must be one of: decision, pattern, fact, open_question, pitfall. "+
				"topic is a short handle (<= 200 chars). content is the body (markdown ok, "+
				"no length limit).",
		),
		mcp.WithToolAnnotation(writeAnnotations("memory write")),
		mcp.WithString("kind", mcp.Required(),
			mcp.Description("One of: decision, pattern, fact, open_question, pitfall")),
		mcp.WithString("topic", mcp.Required(),
			mcp.Description("Short handle, <= 200 chars")),
		mcp.WithString("content", mcp.Required(),
			mcp.Description("The memory body — markdown is fine")),
	)
	s.AddTool(writeTool, mc.handleWrite)

	searchTool := mcp.NewTool("memory_search",
		mcp.WithDescription(
			"Semantic search across Claude's long-term memory. Returns the top n "+
				"most similar memories to the query, ranked by cosine distance over "+
				"the embeddings. Optionally filter by kind (decision, pattern, fact, "+
				"open_question, pitfall). Use this at the start of a session — or "+
				"whenever the topic shifts — to recall relevant context instead of "+
				"re-reading prose handovers. Always favour memory_search over asking "+
				"the user what was decided before.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("memory search")),
		mcp.WithString("query", mcp.Required(),
			mcp.Description("Natural-language query — the meaning, not exact keywords")),
		mcp.WithString("kind",
			mcp.Description("Optional filter: one of decision, pattern, fact, open_question, pitfall")),
		mcp.WithNumber("n",
			mcp.Description("Number of results to return (default 5, max 20)")),
	)
	s.AddTool(searchTool, mc.handleSearch)

	listTool := mcp.NewTool("memory_list",
		mcp.WithDescription(
			"List memories in reverse chronological order. For browsing and "+
				"debugging — semantic search is usually the better entry point. "+
				"Optional kind filter. Default limit 20, max 100.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("memory list")),
		mcp.WithString("kind",
			mcp.Description("Optional filter: one of decision, pattern, fact, open_question, pitfall")),
		mcp.WithNumber("n",
			mcp.Description("Number of results to return (default 20, max 100)")),
	)
	s.AddTool(listTool, mc.handleList)

	deleteTool := mcp.NewTool("memory_delete",
		mcp.WithDescription(
			"Delete a memory by id. Use sparingly — memories become useful over "+
				"time, and deletion is irreversible. Prefer memory_write with an "+
				"updated/corrected version when a memory has gone stale; old and "+
				"new rows both stay searchable until you are sure the old one is "+
				"no longer relevant.",
		),
		mcp.WithToolAnnotation(destructiveAnnotations("memory delete", true)),
		mcp.WithNumber("id", mcp.Required(),
			mcp.Description("Row id as returned by memory_write or memory_search")),
	)
	s.AddTool(deleteTool, mc.handleDelete)
}

// memoryCtx bundles handlerCtx with the Postgres config so handlers
// don't have to re-resolve demo.toml on every call.
type memoryCtx struct {
	*handlerCtx
	cfg *pgConfig
}

// memoryDSNs returns DSN candidates for the claude database, with and
// without password so we cope with pg_hba trust auth the same way the
// pg_* tools do. Mirrors pgConfig.adminDSNs but swaps the dbname.
func (c *memoryCtx) memoryDSNs() []string {
	withPw := fmt.Sprintf(
		"host=localhost port=%d user=%s password=%s dbname=%s sslmode=disable",
		c.cfg.PostgreSQL.Port, c.cfg.PostgreSQL.User, c.cfg.PostgreSQL.Password, memoryDatabase,
	)
	noPw := fmt.Sprintf(
		"host=localhost port=%d user=%s dbname=%s sslmode=disable",
		c.cfg.PostgreSQL.Port, c.cfg.PostgreSQL.User, memoryDatabase,
	)
	if c.cfg.PostgreSQL.Password == "" {
		return []string{noPw}
	}
	return []string{withPw, noPw}
}

// openMemory opens a connection to the claude database, creating the
// database and schema on first use.
//
// Two-phase ensure: first try to connect directly, which succeeds
// after the initial bootstrap. If that fails with "database does not
// exist", fall back to the maintenance DSN, CREATE DATABASE, then
// reconnect. The schema (extensions + table) is idempotent so running
// it on every open is cheap and keeps the code branch-free.
func (c *memoryCtx) openMemory(ctx context.Context) (*sql.DB, error) {
	db, err := openPG(ctx, c.memoryDSNs())
	if err != nil {
		// Most likely "database does not exist" on first use. Create it.
		if createErr := c.createMemoryDatabase(ctx); createErr != nil {
			return nil, fmt.Errorf("ensure claude db: %w (original: %v)", createErr, err)
		}
		db, err = openPG(ctx, c.memoryDSNs())
		if err != nil {
			return nil, fmt.Errorf("reconnect to claude db: %w", err)
		}
	}
	if err := c.ensureMemorySchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ensure memory schema: %w", err)
	}
	return db, nil
}

// createMemoryDatabase connects to the postgres maintenance db and
// issues CREATE DATABASE. Harmless if the database already exists.
func (c *memoryCtx) createMemoryDatabase(ctx context.Context) error {
	admin, err := openPG(ctx, c.cfg.maintenanceDSNs())
	if err != nil {
		return fmt.Errorf("maintenance connect: %w", err)
	}
	defer admin.Close()

	var exists bool
	err = admin.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`,
		memoryDatabase,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	if exists {
		return nil
	}

	// Database name is a constant — identifier quoting is a correctness
	// habit, not a safety measure.
	_, err = admin.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", quoteIdent(memoryDatabase)))
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	c.logger.Info("memory: created database", "database", memoryDatabase)
	return nil
}

// ensureMemorySchema enables pgvector and creates the memory table.
// All statements are idempotent — safe to run on every connection open.
func (c *memoryCtx) ensureMemorySchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE EXTENSION IF NOT EXISTS vector;

		CREATE TABLE IF NOT EXISTS memory (
			id         bigserial    PRIMARY KEY,
			kind       varchar(20)  NOT NULL CHECK (kind IN (
				'decision', 'pattern', 'fact', 'open_question', 'pitfall'
			)),
			topic      varchar(200) NOT NULL,
			content    text         NOT NULL,
			embedding  vector(384),
			created_at timestamptz  NOT NULL DEFAULT now(),
			updated_at timestamptz  NOT NULL DEFAULT now()
		);

		CREATE INDEX IF NOT EXISTS memory_embedding_idx
			ON memory USING ivfflat (embedding vector_cosine_ops)
			WITH (lists = 10);

		CREATE INDEX IF NOT EXISTS memory_kind_idx ON memory (kind);
		CREATE INDEX IF NOT EXISTS memory_topic_idx ON memory (topic);

		CREATE OR REPLACE FUNCTION set_updated_at()
		RETURNS TRIGGER LANGUAGE plpgsql AS $func$
		BEGIN NEW.updated_at = now(); RETURN NEW; END;
		$func$;

		DROP TRIGGER IF EXISTS memory_updated_at ON memory;
		CREATE TRIGGER memory_updated_at
			BEFORE UPDATE ON memory
			FOR EACH ROW EXECUTE FUNCTION set_updated_at();
	`)
	return err
}

// embedText calls Ollama's /v1/embeddings endpoint and returns the
// vector. On failure it returns nil and the error — callers decide
// whether to store the row with NULL embedding as graceful fallback.
func (c *memoryCtx) embedText(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}{Model: memoryEmbedModel, Input: text})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		memoryEmbedBaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama unreachable at %s: %w", memoryEmbedBaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama HTTP %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}
	return result.Data[0].Embedding, nil
}

// vectorLiteral renders a float32 slice as a pgvector string literal
// ("[0.1,0.2,...]"). The caller casts with ::vector in the SQL.
func vectorLiteral(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// handleWrite inserts a new memory row. Embedding failures are not
// fatal — the row lands with NULL embedding and a note in the
// response so the caller knows search will miss it until reembedded.
func (c *memoryCtx) handleWrite(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	kind, err := req.RequireString("kind")
	if err != nil {
		return c.errResult("memory_write", err), nil
	}
	if !memoryValidKinds[kind] {
		return c.errResult("memory_write",
			fmt.Errorf("invalid kind %q: expected one of decision, pattern, fact, open_question, pitfall", kind)), nil
	}
	topic, err := req.RequireString("topic")
	if err != nil {
		return c.errResult("memory_write", err), nil
	}
	if len(topic) > 200 {
		return c.errResult("memory_write",
			fmt.Errorf("topic too long (%d chars, max 200)", len(topic))), nil
	}
	content, err := req.RequireString("content")
	if err != nil {
		return c.errResult("memory_write", err), nil
	}

	db, err := c.openMemory(ctx)
	if err != nil {
		return c.errResult("memory_write", err), nil
	}
	defer db.Close()

	// Embed topic + content so search by topic keyword also matches
	// content-only queries and vice versa. Either embedding quality
	// is acceptable; combining them is slightly better than either
	// alone and costs the same one round-trip.
	embedInput := topic + "\n\n" + content
	vector, embedErr := c.embedText(ctx, embedInput)

	var (
		id            int64
		embeddingNote string
	)
	if embedErr != nil {
		embeddingNote = fmt.Sprintf("embedding skipped: %v", embedErr)
		err = db.QueryRowContext(ctx, `
			INSERT INTO memory (kind, topic, content)
			VALUES ($1, $2, $3)
			RETURNING id
		`, kind, topic, content).Scan(&id)
	} else {
		embeddingNote = fmt.Sprintf("embedded with %s (%d dims)", memoryEmbedModel, len(vector))
		err = db.QueryRowContext(ctx, `
			INSERT INTO memory (kind, topic, content, embedding)
			VALUES ($1, $2, $3, $4::vector)
			RETURNING id
		`, kind, topic, content, vectorLiteral(vector)).Scan(&id)
	}
	if err != nil {
		return c.errResult("memory_write", err), nil
	}

	return jsonResult(map[string]any{
		"id":        id,
		"kind":      kind,
		"topic":     topic,
		"embedding": embeddingNote,
	}), nil
}

// handleSearch runs a cosine-distance search over the memory table.
// Without an embedding for the query (Ollama down), falls back to
// ILIKE search over topic+content so the tool still returns something
// useful. The response indicates which mode was used.
func (c *memoryCtx) handleSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return c.errResult("memory_search", err), nil
	}
	kindFilter := ""
	if raw, ok := req.GetArguments()["kind"]; ok {
		if s, ok := raw.(string); ok {
			kindFilter = s
			if kindFilter != "" && !memoryValidKinds[kindFilter] {
				return c.errResult("memory_search",
					fmt.Errorf("invalid kind %q", kindFilter)), nil
			}
		}
	}
	n := 5
	if raw, ok := req.GetArguments()["n"]; ok {
		if f, ok := raw.(float64); ok {
			n = int(f)
		}
	}
	if n < 1 {
		n = 5
	}
	if n > 20 {
		n = 20
	}

	db, err := c.openMemory(ctx)
	if err != nil {
		return c.errResult("memory_search", err), nil
	}
	defer db.Close()

	// Report how many rows have an embedding — if that number is low,
	// the caller knows search coverage is partial.
	var total, embedded int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(embedding) FROM memory`).Scan(&total, &embedded)

	vector, embedErr := c.embedText(ctx, query)
	if embedErr != nil {
		// Fallback: simple case-insensitive substring search.
		return c.fallbackSearch(ctx, db, query, kindFilter, n, total, embedded, embedErr)
	}

	// ivfflat defaults to probes=1 which gives poor recall on small
	// tables. Raising it to the index's list count (10) effectively
	// turns the search into an exhaustive scan — which is what we want
	// as long as the table is small. Revisit once memory grows past a
	// few thousand rows.
	if _, err := db.ExecContext(ctx, "SET ivfflat.probes = 10"); err != nil {
		return c.errResult("memory_search", fmt.Errorf("set probes: %w", err)), nil
	}

	args := []any{vectorLiteral(vector)}
	where := "WHERE embedding IS NOT NULL"
	if kindFilter != "" {
		args = append(args, kindFilter)
		where += fmt.Sprintf(" AND kind = $%d", len(args))
	}
	args = append(args, n)
	sqlText := fmt.Sprintf(`
		SELECT id, kind, topic, content, created_at,
		       1 - (embedding <=> $1::vector) AS similarity
		FROM memory
		%s
		ORDER BY embedding <=> $1::vector
		LIMIT $%d
	`, where, len(args))

	rows, err := db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return c.errResult("memory_search", err), nil
	}
	defer rows.Close()

	results, err := scanMemoryRows(rows, true)
	if err != nil {
		return c.errResult("memory_search", err), nil
	}

	return jsonResult(map[string]any{
		"mode":             "semantic",
		"query":            query,
		"kind_filter":      kindFilter,
		"memory_total":     total,
		"memory_embedded":  embedded,
		"results":          results,
	}), nil
}

// fallbackSearch runs a lexical ILIKE search when embedding is
// unavailable. Ranks by recency — with no similarity score, latest is
// the most honest default.
func (c *memoryCtx) fallbackSearch(
	ctx context.Context, db *sql.DB,
	query, kindFilter string, n, total, embedded int, embedErr error,
) (*mcp.CallToolResult, error) {
	pattern := "%" + query + "%"
	args := []any{pattern}
	where := "WHERE (topic ILIKE $1 OR content ILIKE $1)"
	if kindFilter != "" {
		args = append(args, kindFilter)
		where += fmt.Sprintf(" AND kind = $%d", len(args))
	}
	args = append(args, n)
	sqlText := fmt.Sprintf(`
		SELECT id, kind, topic, content, created_at
		FROM memory
		%s
		ORDER BY created_at DESC
		LIMIT $%d
	`, where, len(args))

	rows, err := db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return c.errResult("memory_search", err), nil
	}
	defer rows.Close()

	results, err := scanMemoryRows(rows, false)
	if err != nil {
		return c.errResult("memory_search", err), nil
	}

	return jsonResult(map[string]any{
		"mode":            "lexical_fallback",
		"reason":          embedErr.Error(),
		"query":           query,
		"kind_filter":     kindFilter,
		"memory_total":    total,
		"memory_embedded": embedded,
		"results":         results,
	}), nil
}

// handleList returns memories ordered by recency.
func (c *memoryCtx) handleList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	kindFilter := ""
	if raw, ok := req.GetArguments()["kind"]; ok {
		if s, ok := raw.(string); ok {
			kindFilter = s
			if kindFilter != "" && !memoryValidKinds[kindFilter] {
				return c.errResult("memory_list",
					fmt.Errorf("invalid kind %q", kindFilter)), nil
			}
		}
	}
	n := 20
	if raw, ok := req.GetArguments()["n"]; ok {
		if f, ok := raw.(float64); ok {
			n = int(f)
		}
	}
	if n < 1 {
		n = 20
	}
	if n > 100 {
		n = 100
	}

	db, err := c.openMemory(ctx)
	if err != nil {
		return c.errResult("memory_list", err), nil
	}
	defer db.Close()

	args := []any{}
	where := ""
	if kindFilter != "" {
		args = append(args, kindFilter)
		where = fmt.Sprintf("WHERE kind = $%d", len(args))
	}
	args = append(args, n)
	sqlText := fmt.Sprintf(`
		SELECT id, kind, topic, content, created_at
		FROM memory
		%s
		ORDER BY created_at DESC
		LIMIT $%d
	`, where, len(args))

	rows, err := db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return c.errResult("memory_list", err), nil
	}
	defer rows.Close()

	results, err := scanMemoryRows(rows, false)
	if err != nil {
		return c.errResult("memory_list", err), nil
	}

	return jsonResult(map[string]any{
		"kind_filter": kindFilter,
		"count":       len(results),
		"results":     results,
	}), nil
}

// handleDelete removes a single row by id.
func (c *memoryCtx) handleDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	raw, ok := req.GetArguments()["id"]
	if !ok {
		return c.errResult("memory_delete", fmt.Errorf("id is required")), nil
	}
	f, ok := raw.(float64)
	if !ok {
		return c.errResult("memory_delete", fmt.Errorf("id must be a number")), nil
	}
	id := int64(f)

	db, err := c.openMemory(ctx)
	if err != nil {
		return c.errResult("memory_delete", err), nil
	}
	defer db.Close()

	res, err := db.ExecContext(ctx, `DELETE FROM memory WHERE id = $1`, id)
	if err != nil {
		return c.errResult("memory_delete", err), nil
	}
	affected, _ := res.RowsAffected()
	return jsonResult(map[string]any{
		"id":      id,
		"deleted": affected == 1,
	}), nil
}

// scanMemoryRows collects the common row shape from search/list queries.
// Include the similarity column only when semantic search ran.
func scanMemoryRows(rows *sql.Rows, withSimilarity bool) ([]map[string]any, error) {
	out := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id         int64
			kind       string
			topic      string
			content    string
			createdAt  time.Time
			similarity float64
		)
		if withSimilarity {
			if err := rows.Scan(&id, &kind, &topic, &content, &createdAt, &similarity); err != nil {
				return nil, err
			}
		} else {
			if err := rows.Scan(&id, &kind, &topic, &content, &createdAt); err != nil {
				return nil, err
			}
		}
		row := map[string]any{
			"id":         id,
			"kind":       kind,
			"topic":      topic,
			"content":    content,
			"created_at": createdAt.UTC().Format(time.RFC3339),
		}
		if withSimilarity {
			row["similarity"] = similarity
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
