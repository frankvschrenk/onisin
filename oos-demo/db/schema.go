package db

// schema.go — PostgreSQL internal setup for OOS.
//
// Creates only what oosp itself requires to start:
//   - Extensions: pgvector, hstore
//   - Roles:      oosp, ooso
//   - Schema oos:
//       oos.ctx             — CTX definition files (*.ctx.xml)
//       oos.dsl             — DSL screen definition files (*.dsl.xml)
//       oos.config          — hstore key-value store
//       oos.oos_schema      — CTX schema chunks + embeddings for AI prompt injection
//       oos.event_mappings  — registry for the generic event → vector pipeline
//
// Application tables (public.person, public.note, ...) and demo tables
// (public.police_*, public.support_*) live in seed/demo.go and are
// installed separately via `oos-demo --seed-demo`.
//
// All statements are idempotent — safe to run multiple times.
//
// Note on dollar quoting: lib/pq does not cope well with $ inside multi-
// statement Exec calls. Every PL/pgSQL function below uses a unique $func$
// delimiter instead.

import (
	"database/sql"
	"fmt"
	"regexp"

	_ "github.com/lib/pq"
)

// identRE validates SQL identifiers used in DDL that lib/pq cannot
// parameterise (database and schema names). Matches the common subset
// of PostgreSQL identifiers: starts with a letter or underscore,
// continues with letters, digits or underscores.
var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// SetupOptions controls the Setup operation.
//
// DSN points at the target database. AdminDSN points at the built-in
// "postgres" maintenance database — used only when the target database
// does not yet exist, because CREATE DATABASE cannot run against a
// database that is not there. Database is the target database name and
// must match the dbname in DSN.
type SetupOptions struct {
	DSN      string
	AdminDSN string
	Database string
	AppUsers map[string]string
}

// Setup installs the oosp-internal schema. Safe to call on every start.
//
// If the target database does not exist yet, it is created via the
// admin DSN first. After that, all work happens against the target.
//
// This intentionally does NOT touch the public schema — application and
// demo tables are installed by seed.Demo().
func Setup(opts SetupOptions) error {
	if err := ensureDatabase(opts.AdminDSN, opts.Database); err != nil {
		return fmt.Errorf("database: %w", err)
	}

	db, err := sql.Open("postgres", opts.DSN)
	if err != nil {
		return fmt.Errorf("db connect: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("db ping: %w", err)
	}

	steps := []struct {
		name string
		fn   func(*sql.DB) error
	}{
		{"public schema", setupPublicSchema},
		{"extensions",    setupExtensions},
		{"roles",         func(db *sql.DB) error { return setupRoles(db, opts.AppUsers) }},
		{"oos schema",    setupOOSSchema},
		{"oos tables",    setupOOSTables},
		{"oos grants",    setupOOSGrants},
	}

	for _, step := range steps {
		if err := step.fn(db); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}

	return nil
}

// ensureDatabase creates the target database if it does not yet exist.
//
// PostgreSQL has no "CREATE DATABASE IF NOT EXISTS" syntax, so existence
// is probed via pg_database first. The call runs against the admin DSN
// (pointing at the "postgres" maintenance database); CREATE DATABASE
// cannot run against a database that does not exist yet.
//
// The database name is validated against a strict identifier regex
// before interpolation — lib/pq does not parameterise DDL identifiers.
func ensureDatabase(adminDSN, database string) error {
	if !identRE.MatchString(database) {
		return fmt.Errorf("invalid database name %q", database)
	}

	admin, err := sql.Open("postgres", adminDSN)
	if err != nil {
		return fmt.Errorf("admin connect: %w", err)
	}
	defer admin.Close()

	if err := admin.Ping(); err != nil {
		return fmt.Errorf("admin ping: %w", err)
	}

	var exists bool
	err = admin.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`,
		database,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	if exists {
		return nil
	}

	if _, err := admin.Exec(fmt.Sprintf("CREATE DATABASE %s", database)); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	return nil
}

// setupPublicSchema ensures the public schema exists.
//
// Standard PostgreSQL ships with public already present, but operators
// can drop it (and some hardened setups do). pgvector installs into
// public by default, so CREATE EXTENSION below fails with 3F000 if it
// is missing. Recreating it is safe and idempotent.
func setupPublicSchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE SCHEMA IF NOT EXISTS public;`)
	return err
}

// setupExtensions enables pgvector and hstore.
func setupExtensions(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE EXTENSION IF NOT EXISTS vector;
		CREATE EXTENSION IF NOT EXISTS hstore;
	`)
	return err
}

// setupRoles creates the application database roles (idempotent via DO block).
func setupRoles(db *sql.DB, appUsers map[string]string) error {
	for user, password := range appUsers {
		_, err := db.Exec(fmt.Sprintf(`
			DO $role$ BEGIN
				IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '%s') THEN
					CREATE ROLE %s LOGIN PASSWORD '%s';
				END IF;
			END $role$;
		`, user, user, password))
		if err != nil {
			return fmt.Errorf("role %s: %w", user, err)
		}
	}
	return nil
}

// setupOOSSchema creates the oos schema.
func setupOOSSchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE SCHEMA IF NOT EXISTS oos;`)
	return err
}

// setupOOSTables creates all oos.* tables, functions and triggers.
func setupOOSTables(db *sql.DB) error {
	_, err := db.Exec(`
		-- updated_at trigger helper
		CREATE OR REPLACE FUNCTION oos.set_updated_at()
		RETURNS TRIGGER LANGUAGE plpgsql AS $func$
		BEGIN NEW.updated_at = now(); RETURN NEW; END;
		$func$;

		-- oos.ctx — CTX definition files (*.ctx.xml)
		CREATE TABLE IF NOT EXISTS oos.ctx (
			id         varchar(200) PRIMARY KEY,
			xml        text         NOT NULL,
			updated_at timestamptz  NOT NULL DEFAULT now()
		);

		-- oos.dsl — DSL screen definition files (*.dsl.xml)
		CREATE TABLE IF NOT EXISTS oos.dsl (
			id         varchar(200) PRIMARY KEY,
			xml        text         NOT NULL,
			updated_at timestamptz  NOT NULL DEFAULT now()
		);

		-- oos.config — typed configuration store.
		--
		-- Originally a pure hstore key-value store (replaces etcd) but
		-- grown to carry three payload shapes so callers can pick the
		-- one that fits the content:
		--
		--   data  hstore  — flat key-value pairs (namespace settings).
		--   xml   text    — structured documents (themes, prompts).
		--   json  jsonb   — typed structured data with index support.
		--
		-- The row is keyed by namespace, e.g. "oosp", "theme.light",
		-- "theme.dark". Columns are nullable independently; a given
		-- row typically uses one of the three.
		CREATE TABLE IF NOT EXISTS oos.config (
			namespace  text   PRIMARY KEY,
			data       hstore NOT NULL DEFAULT ''::hstore,
			xml        text,
			json       jsonb,
			updated_at timestamptz NOT NULL DEFAULT now()
		);

		-- Additive migration for databases created before the xml/json
		-- columns existed.
		ALTER TABLE oos.config ADD COLUMN IF NOT EXISTS xml        text;
		ALTER TABLE oos.config ADD COLUMN IF NOT EXISTS json       jsonb;
		ALTER TABLE oos.config ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

		DROP TRIGGER IF EXISTS config_updated_at ON oos.config;
		CREATE TRIGGER config_updated_at
			BEFORE UPDATE ON oos.config
			FOR EACH ROW EXECUTE FUNCTION oos.set_updated_at();

		-- oos.oos_schema — CTX schema chunks for AI prompt injection.
		-- One row per context, embedded via granite-embedding (384-dim).
		CREATE TABLE IF NOT EXISTS oos.oos_schema (
			context_name varchar(200) PRIMARY KEY,
			chunk        text         NOT NULL,
			embedding    vector(384),
			updated_at   timestamptz  NOT NULL DEFAULT now()
		);

		CREATE INDEX IF NOT EXISTS oos_schema_embedding_idx
			ON oos.oos_schema USING ivfflat (embedding vector_cosine_ops)
			WITH (lists = 10);

		-- oos.event_mappings — registry for the generic event pipeline.
		-- Each row wires a source table (application events) to a target
		-- vector table that oosp writes embeddings into.
		CREATE TABLE IF NOT EXISTS oos.event_mappings (
			id                serial       PRIMARY KEY,
			name              varchar(100) UNIQUE NOT NULL,
			source_schema     varchar(100) NOT NULL,
			source_table      varchar(100) NOT NULL,
			source_text_field varchar(100) NOT NULL,
			source_id_field   varchar(100) NOT NULL DEFAULT 'id',
			notify_channel    varchar(100) NOT NULL,
			target_schema     varchar(100) NOT NULL,
			target_table      varchar(100) NOT NULL,
			enabled           boolean      NOT NULL DEFAULT true,
			created_at        timestamptz  NOT NULL DEFAULT now(),
			UNIQUE (source_schema, source_table),
			UNIQUE (notify_channel)
		);

		CREATE INDEX IF NOT EXISTS event_mappings_enabled_idx
			ON oos.event_mappings (enabled) WHERE enabled = true;

		-- oosp listens on 'oos_ctx_notify' to rebuild the schema chunks
		-- and their embeddings. Only the id is sent — oosp fetches the XML
		-- itself. This avoids the 8000-byte pg_notify payload limit.
		CREATE OR REPLACE FUNCTION oos.notify_ctx()
		RETURNS TRIGGER LANGUAGE plpgsql AS $func$
		BEGIN
			PERFORM pg_notify('oos_ctx_notify', NEW.id);
			RETURN NEW;
		END;
		$func$;

		DROP TRIGGER IF EXISTS ctx_notify ON oos.ctx;
		CREATE TRIGGER ctx_notify
			AFTER INSERT OR UPDATE ON oos.ctx
			FOR EACH ROW EXECUTE FUNCTION oos.notify_ctx();

		-- updated_at triggers for ctx and dsl
		DROP TRIGGER IF EXISTS ctx_updated_at ON oos.ctx;
		CREATE TRIGGER ctx_updated_at
			BEFORE UPDATE ON oos.ctx
			FOR EACH ROW EXECUTE FUNCTION oos.set_updated_at();

		DROP TRIGGER IF EXISTS dsl_updated_at ON oos.dsl;
		CREATE TRIGGER dsl_updated_at
			BEFORE UPDATE ON oos.dsl
			FOR EACH ROW EXECUTE FUNCTION oos.set_updated_at();
	`)
	return err
}

// setupOOSGrants grants oosp and ooso access to the oos schema.
func setupOOSGrants(db *sql.DB) error {
	for _, user := range []string{"oosp", "ooso"} {
		_, err := db.Exec(fmt.Sprintf(`
			DO $grant$ BEGIN
				IF EXISTS (SELECT FROM pg_roles WHERE rolname = '%s') THEN
					GRANT USAGE ON SCHEMA oos TO %s;
					GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA oos TO %s;
					GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA oos TO %s;
					ALTER DEFAULT PRIVILEGES IN SCHEMA oos
						GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s;
					ALTER DEFAULT PRIVILEGES IN SCHEMA oos
						GRANT USAGE, SELECT ON SEQUENCES TO %s;
				END IF;
			END $grant$;
		`, user, user, user, user, user, user))
		if err != nil {
			return fmt.Errorf("grant %s on oos: %w", user, err)
		}
	}
	return nil
}


