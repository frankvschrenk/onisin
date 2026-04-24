package seed

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
	"onisin.com/oos-demo/db"
)

// Options controls the seed operation.
//
// DSN points at the target database. AdminDSN points at the built-in
// "postgres" maintenance database and is used for bootstrap operations
// that cannot run against the target itself (CREATE DATABASE). Database
// is the target database name and must match the dbname in DSN.
type Options struct {
	DSN      string
	AdminDSN string
	Database string
	AppUsers map[string]string
}

// Internal installs everything oosp itself needs to start: creates the
// target database if missing, then installs the oos schema, roles and
// permission grants. Safe to run multiple times — all statements are
// idempotent.
//
// The embedded IAM is stateless and needs no seeding; demo users come
// from demo.toml at runtime, not from the database.
func Internal(opts Options) error {
	if err := db.Setup(db.SetupOptions{
		DSN:      opts.DSN,
		AdminDSN: opts.AdminDSN,
		Database: opts.Database,
		AppUsers: opts.AppUsers,
	}); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	return nil
}

// Demo installs the public schema and all demo data: application tables,
// reference lookups, sample persons/notes, the police/support event demo
// and the event mappings that wire them to oosp's generic pipeline.
//
// Requires Internal() to have been run first (the oos schema and roles
// must exist). Writes directly to PostgreSQL — oosp does not need to be
// running, and ideally is not: running Demo before starting oosp ensures
// the event_mappings table is populated when oosp's listener boots, so
// no mappings are missed.
func Demo(opts Options) error {
	conn, err := sql.Open("postgres", opts.DSN)
	if err != nil {
		return fmt.Errorf("db connect: %w", err)
	}
	defer conn.Close()

	if err := conn.Ping(); err != nil {
		return fmt.Errorf("db ping: %w", err)
	}

	steps := []struct {
		name string
		fn   func() error
	}{
		{"public schema",  func() error { return seedPublicSchema(conn) }},
		{"public grants",  func() error { return seedPublicGrants(conn) }},
		{"CTX files",      func() error { return seedCTX(conn) }},
		{"themes",         func() error { return seedThemes(conn) }},
		{"schemas",        func() error { return seedSchemas(conn) }},
		{"DSL files",      func() error { return seedDSL(conn) }},
		{"reference data", func() error { return seedRefTables(conn) }},
		{"persons",        func() error { return seedPersons(conn) }},
		{"notes",          func() error { return seedNotes(conn) }},
		{"event demo",     func() error { return seedEventSystem(conn) }},
		{"event data",     func() error { return seedPoliceEvents(conn) }},
	}

	for _, step := range steps {
		if err := step.fn(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}

	return nil
}

// seedPublicSchema creates the application tables in the public schema:
// reference lookups (country, city, role, ...), person, note.
//
// The public schema itself is (re)created first — standard PostgreSQL
// ships it by default, but operators can drop it. Without public the
// first CREATE TABLE below would fail with 3F000.
//
// pgvector is re-enabled here as well. It is normally installed by
// Internal(), but dropping public takes the extension with it and the
// event vector tables below depend on vector columns.
//
// The police/support event tables for the demo are created by
// seedEventSystem below.
func seedPublicSchema(conn *sql.DB) error {
	_, err := conn.Exec(`
		CREATE SCHEMA IF NOT EXISTS public;
		CREATE EXTENSION IF NOT EXISTS vector;
		CREATE EXTENSION IF NOT EXISTS hstore;

		CREATE OR REPLACE FUNCTION public.set_updated_at()
		RETURNS TRIGGER LANGUAGE plpgsql AS $func$
		BEGIN NEW.updated_at = now(); RETURN NEW; END;
		$func$;

		CREATE TABLE IF NOT EXISTS public.country (
			code varchar(10)  PRIMARY KEY,
			name varchar(100) NOT NULL
		);

		CREATE TABLE IF NOT EXISTS public.city (
			id           serial4      PRIMARY KEY,
			name         varchar(100) NOT NULL,
			country_code varchar(10)  NOT NULL REFERENCES public.country(code)
		);
		CREATE INDEX IF NOT EXISTS city_country_idx ON public.city (country_code);

		CREATE TABLE IF NOT EXISTS public.role (
			key   varchar(50)  PRIMARY KEY,
			label varchar(100) NOT NULL
		);

		CREATE TABLE IF NOT EXISTS public.department (
			key   varchar(50)  PRIMARY KEY,
			label varchar(100) NOT NULL
		);

		CREATE TABLE IF NOT EXISTS public.employment_type (
			key   varchar(20)  PRIMARY KEY,
			label varchar(100) NOT NULL
		);

		CREATE TABLE IF NOT EXISTS public.notify_channel (
			key   varchar(20)  PRIMARY KEY,
			label varchar(100) NOT NULL
		);

		CREATE TABLE IF NOT EXISTS public.language (
			code varchar(10)  PRIMARY KEY,
			name varchar(100) NOT NULL
		);

		CREATE TABLE IF NOT EXISTS public.person (
			id               serial4      NOT NULL,
			uuid             uuid         NOT NULL DEFAULT gen_random_uuid(),
			source           varchar(100) NULL,
			created_at       timestamptz  NOT NULL DEFAULT now(),
			updated_at       timestamptz  NOT NULL DEFAULT now(),
			title            varchar(20)  NULL,
			firstname        varchar(100) NOT NULL,
			lastname         varchar(100) NOT NULL,
			age              int4         NULL,
			net_worth        float8       NULL,
			role             varchar(50)  NULL REFERENCES public.role(key),
			department       varchar(50)  NULL REFERENCES public.department(key),
			employment       varchar(20)  NULL REFERENCES public.employment_type(key),
			active           bool         NOT NULL DEFAULT true,
			profile_complete float4       NOT NULL DEFAULT 0,
			street           varchar(100) NULL,
			zip              varchar(10)  NULL,
			city             varchar(100) NULL,
			country          varchar(10)  NULL REFERENCES public.country(code),
			email            varchar(100) NULL,
			phone            varchar(20)  NULL,
			mobile           varchar(20)  NULL,
			linkedin         varchar(200) NULL,
			notify_channel   varchar(20)  NULL REFERENCES public.notify_channel(key),
			notify_email     bool         NOT NULL DEFAULT true,
			notify_push      bool         NOT NULL DEFAULT true,
			notify_sms       bool         NOT NULL DEFAULT false,
			notify_weekly    bool         NOT NULL DEFAULT false,
			language         varchar(10)  NULL REFERENCES public.language(code),
			font_size        int4         NOT NULL DEFAULT 14,
			notes            text         NULL,
			CONSTRAINT person_pkey PRIMARY KEY (id)
		);
		CREATE INDEX IF NOT EXISTS person_lastname_idx ON public.person (lastname);
		CREATE INDEX IF NOT EXISTS person_city_idx     ON public.person (city);
		CREATE INDEX IF NOT EXISTS person_active_idx   ON public.person (active);

		DROP TRIGGER IF EXISTS person_updated_at ON public.person;
		CREATE TRIGGER person_updated_at
			BEFORE UPDATE ON public.person
			FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

		CREATE TABLE IF NOT EXISTS public.note (
			id         serial4      NOT NULL,
			person_id  int4         NOT NULL,
			created_at timestamptz  NOT NULL DEFAULT now(),
			title      varchar(200) NOT NULL,
			body       text         NULL,
			CONSTRAINT note_pkey PRIMARY KEY (id)
		);

		ALTER TABLE public.note
			DROP CONSTRAINT IF EXISTS note_person_id_fkey;
		ALTER TABLE public.note
			ADD CONSTRAINT note_person_id_fkey
			FOREIGN KEY (person_id) REFERENCES public.person(id)
			ON DELETE CASCADE;
	`)
	return err
}

// seedPublicGrants gives oosp and ooso DML access to the public schema.
//
// The generic event pipeline runs as oosp: it reads application event
// tables, marks rows processed, and writes into the configured vector
// target tables. Sequence privileges are granted explicitly so serial
// columns can be written to.
func seedPublicGrants(conn *sql.DB) error {
	for _, user := range []string{"oosp", "ooso"} {
		_, err := conn.Exec(fmt.Sprintf(`
			DO $grant$ BEGIN
				IF EXISTS (SELECT FROM pg_roles WHERE rolname = '%s') THEN
					GRANT USAGE ON SCHEMA public TO %s;
					GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %s;
					GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %s;
					ALTER DEFAULT PRIVILEGES IN SCHEMA public
						GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s;
					ALTER DEFAULT PRIVILEGES IN SCHEMA public
						GRANT USAGE, SELECT ON SEQUENCES TO %s;
				END IF;
			END $grant$;
		`, user, user, user, user, user, user))
		if err != nil {
			return fmt.Errorf("grant %s on public: %w", user, err)
		}
	}
	return nil
}

// seedEventSystem creates the demo event tables in public and registers
// two example mappings in oos.event_mappings.
//
// Schema layout:
//   oos.event_mappings        — oosp-internal registry (already created
//                               by db.Setup as part of the oos schema)
//   public.police_incidents   — source events for the police demo
//   public.police_embeddings  — vector target for the police demo
//   public.support_tickets    — source events for the support demo
//   public.support_embeddings — vector target for the support demo
//
// Demo tables live in public because they model application data, not
// oosp infrastructure.
func seedEventSystem(conn *sql.DB) error {
	setupSQL := `
-- ================================================================
-- OOS Generic Event Processing System - Demo Setup
-- ================================================================

-- 1. Event Mapping Registry (oos schema, oosp-internal)
CREATE TABLE IF NOT EXISTS oos.event_mappings (
    id serial PRIMARY KEY,
    name varchar(100) UNIQUE NOT NULL,

    -- Source (event table)
    source_schema varchar(100) NOT NULL,
    source_table varchar(100) NOT NULL,
    source_text_field varchar(100) NOT NULL,
    source_id_field varchar(100) DEFAULT 'id' NOT NULL,
    notify_channel varchar(100) NOT NULL,

    -- Target (vector table)
    target_schema varchar(100) NOT NULL,
    target_table varchar(100) NOT NULL,

    enabled boolean DEFAULT true NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,

    UNIQUE(source_schema, source_table),
    UNIQUE(notify_channel)
);

CREATE INDEX IF NOT EXISTS event_mappings_enabled_idx
    ON oos.event_mappings (enabled) WHERE enabled = true;

-- 2. Police demo — source table in public
CREATE TABLE IF NOT EXISTS public.police_incidents (
    id serial PRIMARY KEY,
    stream varchar(200) NOT NULL,
    event_type varchar(200) NOT NULL,
    text text NOT NULL,
    payload jsonb DEFAULT '{}' NOT NULL,
    processed boolean DEFAULT false NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
);

CREATE OR REPLACE FUNCTION public.notify_police_incidents()
RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
BEGIN
    PERFORM pg_notify('police_incidents_notify', row_to_json(NEW)::text);
    RETURN NEW;
END;
$fn$;

DROP TRIGGER IF EXISTS police_incidents_notify ON public.police_incidents;
CREATE TRIGGER police_incidents_notify
    AFTER INSERT ON public.police_incidents
    FOR EACH ROW EXECUTE FUNCTION public.notify_police_incidents();

-- 3. Police demo — vector target in public
CREATE TABLE IF NOT EXISTS public.police_embeddings (
    id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
    source_id text NOT NULL,
    stream_id text NOT NULL,
    event_type text NOT NULL,
    text_content text NOT NULL,
    metadata jsonb DEFAULT '{}' NOT NULL,
    embedding vector(384),
    created_at timestamptz DEFAULT now() NOT NULL,

    UNIQUE(source_id)
);

CREATE INDEX IF NOT EXISTS police_embeddings_stream_idx
    ON public.police_embeddings (stream_id, created_at);
CREATE INDEX IF NOT EXISTS police_embeddings_vector_idx
    ON public.police_embeddings USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- 4. Support demo — source table in public
CREATE TABLE IF NOT EXISTS public.support_tickets (
    id serial PRIMARY KEY,
    stream varchar(200) NOT NULL,
    event_type varchar(200) NOT NULL,
    text text NOT NULL,
    payload jsonb DEFAULT '{}' NOT NULL,
    processed boolean DEFAULT false NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
);

CREATE OR REPLACE FUNCTION public.notify_support_tickets()
RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
BEGIN
    PERFORM pg_notify('support_tickets_notify', row_to_json(NEW)::text);
    RETURN NEW;
END;
$fn$;

DROP TRIGGER IF EXISTS support_tickets_notify ON public.support_tickets;
CREATE TRIGGER support_tickets_notify
    AFTER INSERT ON public.support_tickets
    FOR EACH ROW EXECUTE FUNCTION public.notify_support_tickets();

-- 5. Support demo — vector target in public
CREATE TABLE IF NOT EXISTS public.support_embeddings (
    id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
    source_id text NOT NULL,
    stream_id text NOT NULL,
    event_type text NOT NULL,
    text_content text NOT NULL,
    metadata jsonb DEFAULT '{}' NOT NULL,
    embedding vector(384),
    created_at timestamptz DEFAULT now() NOT NULL,

    UNIQUE(source_id)
);

CREATE INDEX IF NOT EXISTS support_embeddings_stream_idx
    ON public.support_embeddings (stream_id, created_at);
CREATE INDEX IF NOT EXISTS support_embeddings_vector_idx
    ON public.support_embeddings USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- 6. Register both demo mappings
INSERT INTO oos.event_mappings
    (name, source_schema, source_table, source_text_field, source_id_field, notify_channel, target_schema, target_table, enabled)
VALUES
    ('police',  'public', 'police_incidents', 'text', 'id', 'police_incidents_notify', 'public', 'police_embeddings',  true),
    ('support', 'public', 'support_tickets',  'text', 'id', 'support_tickets_notify',  'public', 'support_embeddings', true)
ON CONFLICT (name) DO UPDATE SET
    source_schema     = EXCLUDED.source_schema,
    source_table      = EXCLUDED.source_table,
    source_text_field = EXCLUDED.source_text_field,
    source_id_field   = EXCLUDED.source_id_field,
    notify_channel    = EXCLUDED.notify_channel,
    target_schema     = EXCLUDED.target_schema,
    target_table      = EXCLUDED.target_table,
    enabled           = EXCLUDED.enabled;
`

	if _, err := conn.Exec(setupSQL); err != nil {
		return fmt.Errorf("event system setup: %w", err)
	}

	return nil
}

// seedPoliceEvents loads the demo police case events into public.police_incidents
// and a handful of support tickets into public.support_tickets.
func seedPoliceEvents(conn *sql.DB) error {
	const insertPolice = `
		INSERT INTO public.police_incidents (stream, event_type, text, payload)
		VALUES ($1, $2, $3, $4::jsonb)
	`

	for _, event := range Fall0042Events {
		if _, err := conn.Exec(insertPolice, event.Stream, event.EventType, event.Text, event.Payload); err != nil {
			return fmt.Errorf("insert police event: %w", err)
		}
	}

	for _, event := range Fall0080Events {
		if _, err := conn.Exec(insertPolice, event.Stream, event.EventType, event.Text, event.Payload); err != nil {
			return fmt.Errorf("insert police event: %w", err)
		}
	}

	// A few support tickets so the support mapping has something to embed.
	supportEvents := []PoliceEvent{
		{
			Stream:    "customer-12345",
			EventType: "ticket_created",
			Text:      "Order has not arrived for 10 days. Tracking number: DE123456789. Customer is very unhappy.",
			Payload:   `{"priority": "high", "category": "shipping", "tracking": "DE123456789"}`,
		},
		{
			Stream:    "customer-67890",
			EventType: "ticket_created",
			Text:      "Invoice miscalculated. Charged twice for the premium account. Customer requests a refund.",
			Payload:   `{"priority": "medium", "category": "billing", "amount": "29.99"}`,
		},
		{
			Stream:    "department-IT",
			EventType: "ticket_created",
			Text:      "VPN connection broken since the latest Windows update. Multiple users affected.",
			Payload:   `{"priority": "high", "category": "network", "affected_users": 15}`,
		},
	}

	const insertSupport = `
		INSERT INTO public.support_tickets (stream, event_type, text, payload)
		VALUES ($1, $2, $3, $4::jsonb)
	`

	for _, event := range supportEvents {
		if _, err := conn.Exec(insertSupport, event.Stream, event.EventType, event.Text, event.Payload); err != nil {
			return fmt.Errorf("insert support event: %w", err)
		}
	}

	return nil
}


