-- internal.sql — OOS internal schema (oos.*)
--
-- Run once before first start. Creates:
--   * Extensions: pgvector, hstore
--   * Schema oos
--   * Tables:
--       oos.config             — typed configuration store
--       oos.domain             — domain definition source
--       oos.view               — view definition source
--       oos.global_prompt      — global prompt store
--       oos.oos_domain_schema  — domain chunks + embeddings (RAG)
--       oos.oos_view_schema    — view chunks + embeddings (RAG)
--       oos.oos_global_schema  — global prompt chunks + embeddings (RAG)
--       oos.event_mappings     — generic event → vector pipeline registry
--   * Trigger functions and triggers
--
-- Idempotent — every CREATE uses IF NOT EXISTS or OR REPLACE, every
-- trigger is dropped and recreated. Safe to run on every boot.
--
-- This file does NOT create roles or grant access — that needs
-- application-supplied passwords and is handled by the runner in
-- apps/oosd/src/bun/seed/runner.ts.

-- ─── Extensions ──────────────────────────────────────────────────────
--
-- pgvector and hstore must live in a schema that sits on every
-- session's search_path, otherwise type references like vector(1024)
-- fail to resolve. The natural home is public.
--
-- This block survives one specific footgun: someone renames the
-- public schema (e.g. to public_bak) without first moving the
-- extensions out. After such a rename CREATE EXTENSION IF NOT
-- EXISTS is a no-op — the extension already exists, just in the
-- wrong schema — and every later vector(...) reference fails with
-- "type vector does not exist". Re-homing the extension is cheap
-- and idempotent.

CREATE SCHEMA IF NOT EXISTS public;

DO $ext$
DECLARE
    public_oid oid := (SELECT oid FROM pg_namespace WHERE nspname = 'public');
    ext_ns     oid;
BEGIN
    -- vector
    SELECT extnamespace INTO ext_ns FROM pg_extension WHERE extname = 'vector';
    IF ext_ns IS NULL THEN
        EXECUTE 'CREATE EXTENSION vector SCHEMA public';
    ELSIF ext_ns <> public_oid THEN
        EXECUTE 'ALTER EXTENSION vector SET SCHEMA public';
    END IF;

    -- hstore
    SELECT extnamespace INTO ext_ns FROM pg_extension WHERE extname = 'hstore';
    IF ext_ns IS NULL THEN
        EXECUTE 'CREATE EXTENSION hstore SCHEMA public';
    ELSIF ext_ns <> public_oid THEN
        EXECUTE 'ALTER EXTENSION hstore SET SCHEMA public';
    END IF;
END
$ext$;

-- ─── Schema ──────────────────────────────────────────────────────────

CREATE SCHEMA IF NOT EXISTS oos;

-- ─── Trigger function: bump updated_at ───────────────────────────────

CREATE OR REPLACE FUNCTION oos.set_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $func$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$func$;

-- pg_notify functions removed — messaging is now handled by NATS.
-- oosd publishes on oos.domain.changed / oos.view.changed after save.
-- oosai subscribes via nats-client.ts.

-- ─── Trigger function: block modification of closed events ───────────
--
-- Used by per-source-table BEFORE UPDATE OR DELETE triggers in
-- demo-schema-police.sql, demo-schema-support.sql, and any future
-- event source. Lives in public so each source table can attach to
-- it without duplicating the function body.
--
-- The function raises when OLD.closed is true, which blocks every
-- UPDATE and DELETE on a closed row. The UPDATE that sets closed
-- itself from false to true is allowed: at that point OLD.closed is
-- still false. Re-opening (true → false) is therefore impossible.

CREATE OR REPLACE FUNCTION public.prevent_modify_closed_event()
RETURNS TRIGGER LANGUAGE plpgsql AS $func$
BEGIN
    IF OLD.closed = true THEN
        RAISE EXCEPTION 'event % is closed and cannot be %', OLD.id, lower(TG_OP)
            USING ERRCODE = '23514';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$func$;

-- ─── oos.config — typed configuration store ──────────────────────────

CREATE TABLE IF NOT EXISTS oos.config (
    namespace  text          PRIMARY KEY,
    data       hstore        NOT NULL DEFAULT ''::hstore,
    xml        text,
    json       jsonb,
    updated_at timestamptz   NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS config_updated_at ON oos.config;
CREATE TRIGGER config_updated_at
    BEFORE UPDATE ON oos.config
    FOR EACH ROW EXECUTE FUNCTION oos.set_updated_at();

-- ─── oos.domain — domain definition source ───────────────────────────

CREATE TABLE IF NOT EXISTS oos.domain (
    id         varchar(200) PRIMARY KEY,
    source     text         NOT NULL,
    updated_at timestamptz  NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS domain_updated_at ON oos.domain;
CREATE TRIGGER domain_updated_at
    BEFORE UPDATE ON oos.domain
    FOR EACH ROW EXECUTE FUNCTION oos.set_updated_at();

-- domain_notify trigger removed — NATS replaces pg_notify.

-- ─── oos.view — view definition source ───────────────────────────────

CREATE TABLE IF NOT EXISTS oos.view (
    id         varchar(200) PRIMARY KEY,
    source     text         NOT NULL,
    updated_at timestamptz  NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS view_updated_at ON oos.view;
CREATE TRIGGER view_updated_at
    BEFORE UPDATE ON oos.view
    FOR EACH ROW EXECUTE FUNCTION oos.set_updated_at();

-- view_notify trigger removed — NATS replaces pg_notify.

-- ─── oos.global_prompt — global LLM prompt store ─────────────────────

CREATE TABLE IF NOT EXISTS oos.global_prompt (
    name       text         PRIMARY KEY,
    text       text         NOT NULL,
    updated_at timestamptz  NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS global_prompt_updated_at ON oos.global_prompt;
CREATE TRIGGER global_prompt_updated_at
    BEFORE UPDATE ON oos.global_prompt
    FOR EACH ROW EXECUTE FUNCTION oos.set_updated_at();

-- global_prompt_notify trigger removed — NATS replaces pg_notify.

-- ─── oos.oos_domain_schema — domain RAG chunks ───────────────────────

CREATE TABLE IF NOT EXISTS oos.oos_domain_schema (
    context_name varchar(200) PRIMARY KEY,
    chunk        text         NOT NULL,
    embedding    vector(1024),
    updated_at   timestamptz  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS oos_domain_schema_embedding_idx
    ON oos.oos_domain_schema USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 10);

-- ─── oos.oos_view_schema — view RAG chunks ───────────────────────────

CREATE TABLE IF NOT EXISTS oos.oos_view_schema (
    id         varchar(200) PRIMARY KEY,
    kind       varchar(20)  NOT NULL CHECK (kind IN ('element', 'pattern')),
    chunk      text         NOT NULL,
    embedding  vector(1024),
    updated_at timestamptz  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS oos_view_schema_embedding_idx
    ON oos.oos_view_schema USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 10);

CREATE INDEX IF NOT EXISTS oos_view_schema_kind_idx
    ON oos.oos_view_schema (kind);

-- ─── oos.oos_global_schema — global prompt RAG chunks ────────────────

CREATE TABLE IF NOT EXISTS oos.oos_global_schema (
    name       varchar(200) PRIMARY KEY,
    chunk      text         NOT NULL,
    embedding  vector(1024),
    updated_at timestamptz  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS oos_global_schema_embedding_idx
    ON oos.oos_global_schema USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 10);

-- ─── oos.event_mappings — generic event → vector pipeline registry ───

CREATE TABLE IF NOT EXISTS public.event_mappings (
    id                serial       PRIMARY KEY,
    name              varchar(100) NOT NULL UNIQUE,
    source_schema     varchar(100) NOT NULL,
    source_table      varchar(100) NOT NULL,
    source_text_field varchar(100) NOT NULL,
    source_id_field   varchar(100) NOT NULL DEFAULT 'id',
    notify_channel    varchar(100) NOT NULL UNIQUE,
    target_schema     varchar(100) NOT NULL,
    target_table      varchar(100) NOT NULL,
    enabled           boolean      NOT NULL DEFAULT true,
    close_policy      varchar(50)  NOT NULL DEFAULT 'manual',
    created_at        timestamptz  NOT NULL DEFAULT now(),
    UNIQUE (source_schema, source_table)
);

-- close_policy controls when events become immutable. Values:
--   'manual'         — events are mutable until the user (or a cronjob)
--                      sets closed=true. Default; supports collaborative
--                      editing across sessions.
--   'auto_on_insert' — events are inserted with closed=true and can
--                      never be modified. For audit-critical sources.
ALTER TABLE public.event_mappings
    ADD COLUMN IF NOT EXISTS close_policy varchar(50) NOT NULL DEFAULT 'manual';

ALTER TABLE public.event_mappings
    DROP CONSTRAINT IF EXISTS event_mappings_close_policy_check;
ALTER TABLE public.event_mappings
    ADD CONSTRAINT event_mappings_close_policy_check
    CHECK (close_policy IN ('manual', 'auto_on_insert'));

CREATE INDEX IF NOT EXISTS event_mappings_enabled_idx
    ON public.event_mappings (enabled) WHERE enabled = true;
