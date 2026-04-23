// Tool: postgres — pg_query, pg_exec, pg_reset
//
// Connects to a PostgreSQL instance for local development work against
// the onisin demo database. Credentials are read from demo.toml, which
// sits at the repository root (one of the allowed roots). When no
// demo.toml is found, none of the pg_* tools are registered — the
// capability is simply absent rather than present-but-broken.
//
// Uses the superuser DSN ([postgresql] user/password) so that the full
// DDL repertoire is available: CREATE/DROP DATABASE, CREATE EXTENSION,
// CREATE ROLE, plus everything the app users already can do. oos-demo's
// own app users (oosp, ooso) intentionally can't run DDL; for a dev
// tool that helps reset and re-seed databases that's the wrong scope.

package tools

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"onisin.com/oosfs/internal/roots"
)

// pgConfig mirrors the subset of demo.toml the pg_* tools consume.
// Only the [postgresql] block is relevant; everything else in demo.toml
// is ignored.
type pgConfig struct {
	PostgreSQL struct {
		Port     int    `toml:"port"`
		Database string `toml:"database"`
		User     string `toml:"user"`
		Password string `toml:"password"`
	} `toml:"postgresql"`

	// sourceFile is the path demo.toml was loaded from. Handy for
	// diagnostics and for logging which file the tool is operating
	// against.
	sourceFile string
}

// adminDSNs returns DSN candidates to try in order, for connecting to
// the application database as the superuser. Postgres.app and a lot of
// local dev setups run with pg_hba trust authentication, which rejects
// any password the client sends — so we fall back to a passwordless
// DSN if the first attempt fails. This mirrors what oos-demo does in
// Config.PostgresDSN.
func (c *pgConfig) adminDSNs() []string {
	withPw := fmt.Sprintf(
		"host=localhost port=%d user=%s password=%s dbname=%s sslmode=disable",
		c.PostgreSQL.Port, c.PostgreSQL.User, c.PostgreSQL.Password, c.PostgreSQL.Database,
	)
	noPw := fmt.Sprintf(
		"host=localhost port=%d user=%s dbname=%s sslmode=disable",
		c.PostgreSQL.Port, c.PostgreSQL.User, c.PostgreSQL.Database,
	)
	if c.PostgreSQL.Password == "" {
		return []string{noPw}
	}
	return []string{withPw, noPw}
}

// maintenanceDSNs is the equivalent of adminDSNs but pointed at the
// postgres maintenance database — needed for CREATE/DROP DATABASE.
func (c *pgConfig) maintenanceDSNs() []string {
	withPw := fmt.Sprintf(
		"host=localhost port=%d user=%s password=%s dbname=postgres sslmode=disable",
		c.PostgreSQL.Port, c.PostgreSQL.User, c.PostgreSQL.Password,
	)
	noPw := fmt.Sprintf(
		"host=localhost port=%d user=%s dbname=postgres sslmode=disable",
		c.PostgreSQL.Port, c.PostgreSQL.User,
	)
	if c.PostgreSQL.Password == "" {
		return []string{noPw}
	}
	return []string{withPw, noPw}
}

// findDemoTOML looks for demo.toml in each of the allowed roots. The
// first hit wins — in practice there is only ever one. Returns the
// loaded config or nil when no demo.toml exists anywhere.
func findDemoTOML(reg *roots.Registry, logger *slog.Logger) *pgConfig {
	for _, root := range reg.All() {
		candidate := filepath.Join(root, "demo.toml")
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		var cfg pgConfig
		if _, err := toml.DecodeFile(candidate, &cfg); err != nil {
			logger.Warn("demo.toml decode failed", "path", candidate, "err", err)
			continue
		}
		if cfg.PostgreSQL.Database == "" || cfg.PostgreSQL.User == "" {
			logger.Warn("demo.toml missing [postgresql] database/user", "path", candidate)
			continue
		}
		cfg.sourceFile = candidate
		return &cfg
	}
	return nil
}

// registerPostgres wires the pg_query, pg_exec and pg_reset tools into
// the MCP server, but only if a demo.toml is present.
func registerPostgres(s *server.MCPServer, ctx *handlerCtx) {
	cfg := findDemoTOML(ctx.reg, ctx.logger)
	if cfg == nil {
		ctx.logger.Info("postgres tools not registered — no demo.toml in any allowed root")
		return
	}
	ctx.logger.Info("postgres tools enabled",
		"source", cfg.sourceFile,
		"database", cfg.PostgreSQL.Database,
		"user", cfg.PostgreSQL.User,
	)
	pgCtx := &pgHandlerCtx{handlerCtx: ctx, cfg: cfg}

	queryTool := mcp.NewTool("pg_query",
		mcp.WithDescription(
			"Run a SELECT (or any row-returning statement) against the demo database "+
				"configured in demo.toml. Returns rows as JSON objects with Postgres "+
				"types mapped naturally (int, float, string, bool, null, ISO timestamp). "+
				"Row count is capped at 1000 by default; set limit=0 for no cap.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("pg query")),
		mcp.WithString("sql", mcp.Required(), mcp.Description("SQL to execute (must return rows)")),
		mcp.WithNumber("limit", mcp.Description("Cap rows returned (default 1000, 0 = unlimited)")),
	)
	s.AddTool(queryTool, pgCtx.handleQuery)

	execTool := mcp.NewTool("pg_exec",
		mcp.WithDescription(
			"Run a non-row-returning statement against the demo database: INSERT, "+
				"UPDATE, DELETE, or DDL such as CREATE/DROP/ALTER. Returns rows_affected. "+
				"Runs as the [postgresql] superuser from demo.toml — has full DDL power.",
		),
		mcp.WithToolAnnotation(destructiveAnnotations("pg exec", true)),
		mcp.WithString("sql", mcp.Required(), mcp.Description("SQL to execute")),
	)
	s.AddTool(execTool, pgCtx.handleExec)

	resetTool := mcp.NewTool("pg_reset",
		mcp.WithDescription(
			"Drop and recreate the demo database configured in demo.toml. "+
				"Terminates all other sessions on it first, so this works even when "+
				"oos-demo or oosp had connections open. After a reset, the database "+
				"is empty — run `oos-demo --seed-internal` and `oos-demo --seed-demo` "+
				"to refill it.",
		),
		mcp.WithToolAnnotation(destructiveAnnotations("pg reset", true)),
	)
	s.AddTool(resetTool, pgCtx.handleReset)
}

// pgHandlerCtx bundles handlerCtx with the Postgres config so handlers
// don't have to re-resolve demo.toml on every call.
type pgHandlerCtx struct {
	*handlerCtx
	cfg *pgConfig
}

// openAdmin opens a short-lived connection to the application database,
// trying the password and passwordless DSN variants in turn.
func (c *pgHandlerCtx) openAdmin(ctx context.Context) (*sql.DB, error) {
	return openPG(ctx, c.cfg.adminDSNs())
}

// openMaintenance opens a short-lived connection to the postgres
// maintenance database (for CREATE/DROP DATABASE).
func (c *pgHandlerCtx) openMaintenance(ctx context.Context) (*sql.DB, error) {
	return openPG(ctx, c.cfg.maintenanceDSNs())
}

// openPG tries each candidate DSN with a short ping; the first one that
// succeeds wins. Returns the last error if none work — the caller gets
// a concrete failure rather than a generic "connection refused" when
// neither trust nor password auth is configured.
func openPG(ctx context.Context, dsns []string) (*sql.DB, error) {
	var lastErr error
	for _, dsn := range dsns {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			lastErr = fmt.Errorf("open: %w", err)
			continue
		}
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = db.PingContext(pingCtx)
		cancel()
		if err == nil {
			return db, nil
		}
		_ = db.Close()
		lastErr = fmt.Errorf("ping: %w", err)
	}
	return nil, lastErr
}

func (c *pgHandlerCtx) handleQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sqlText, err := req.RequireString("sql")
	if err != nil {
		return c.errResult("pg_query", err), nil
	}
	limit := 1000
	if raw, ok := req.GetArguments()["limit"]; ok {
		if n, ok := raw.(float64); ok {
			limit = int(n)
		}
	}

	db, err := c.openAdmin(ctx)
	if err != nil {
		return c.errResult("pg_query", err), nil
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, sqlText)
	if err != nil {
		return c.errResult("pg_query", err), nil
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return c.errResult("pg_query", err), nil
	}

	out := make([]map[string]any, 0)
	truncated := false
	for rows.Next() {
		if limit > 0 && len(out) >= limit {
			truncated = true
			break
		}
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return c.errResult("pg_query", err), nil
		}
		row := make(map[string]any, len(cols))
		for i, name := range cols {
			row[name] = jsonFriendly(raw[i])
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return c.errResult("pg_query", err), nil
	}

	return jsonResult(map[string]any{
		"columns":   cols,
		"rows":      out,
		"row_count": len(out),
		"truncated": truncated,
	}), nil
}

func (c *pgHandlerCtx) handleExec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sqlText, err := req.RequireString("sql")
	if err != nil {
		return c.errResult("pg_exec", err), nil
	}

	db, err := c.openAdmin(ctx)
	if err != nil {
		return c.errResult("pg_exec", err), nil
	}
	defer db.Close()

	res, err := db.ExecContext(ctx, sqlText)
	if err != nil {
		return c.errResult("pg_exec", err), nil
	}

	// RowsAffected is only meaningful for DML; DDL typically returns 0
	// with no error. Report whatever the driver gives us.
	affected, _ := res.RowsAffected()
	return jsonResult(map[string]any{
		"rows_affected": affected,
	}), nil
}

func (c *pgHandlerCtx) handleReset(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	target := c.cfg.PostgreSQL.Database

	db, err := c.openMaintenance(ctx)
	if err != nil {
		return c.errResult("pg_reset", err), nil
	}
	defer db.Close()

	// Step 1: terminate every other backend connected to the target db,
	// otherwise DROP DATABASE will refuse the drop. pg_terminate_backend
	// is safe for the postgres superuser to call.
	const terminateSQL = `
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = $1 AND pid <> pg_backend_pid()`
	if _, err := db.ExecContext(ctx, terminateSQL, target); err != nil {
		return c.errResult("pg_reset: terminate sessions", err), nil
	}

	// Step 2: drop and recreate. Use fmt.Sprintf with %q to quote the
	// identifier safely; database names cannot be passed as bound
	// parameters in DDL.
	dropSQL := fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdent(target))
	if _, err := db.ExecContext(ctx, dropSQL); err != nil {
		return c.errResult("pg_reset: drop", err), nil
	}
	createSQL := fmt.Sprintf("CREATE DATABASE %s", quoteIdent(target))
	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return c.errResult("pg_reset: create", err), nil
	}

	return jsonResult(map[string]any{
		"database":  target,
		"action":    "dropped and recreated",
		"next_step": "run oos-demo --seed-internal, then --seed-demo",
	}), nil
}

// quoteIdent wraps a Postgres identifier in double quotes and escapes
// embedded double quotes. Used for DDL where parameter binding is
// unavailable; inputs come only from our own demo.toml so injection
// is not a real risk, but quoting is still correct.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// jsonFriendly converts a scanned Postgres value into something that
// marshals cleanly via encoding/json. The default sql.Scan result for
// unknown types is []byte, which json would otherwise render as
// base64 — usually not what a reader wants.
func jsonFriendly(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case float64:
		// Postgres numeric/float can land here; keep NaN/Inf out of JSON.
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fmt.Sprintf("%v", x)
		}
		return x
	default:
		return x
	}
}

