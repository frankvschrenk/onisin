-- demo-schema.sql — Public application schema (base tables only).
--
-- Run after internal.sql. Creates:
--   * Reference lookups (country, city, role, department, ...)
--   * Application tables: public.person, public.note
--   * Stream master table: public.event_streams
--   * Event type grammar library: public.event_type_grammar
--   * event_mappings.event_types column
--
-- Event source tables (police_incidents, support_tickets) and their
-- embeddings + mappings are created separately via:
--   "Install police event source" → demo-schema-police.sql
--   "Install support event source" → demo-schema-support.sql
--
-- Idempotent. Safe to run multiple times.

-- ─── Public schema and shared trigger function ───────────────────────

CREATE SCHEMA IF NOT EXISTS public;
CREATE EXTENSION IF NOT EXISTS vector;

CREATE OR REPLACE FUNCTION public.set_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $func$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$func$;

-- ─── Reference lookups ───────────────────────────────────────────────

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

-- ─── Application tables ──────────────────────────────────────────────

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

ALTER TABLE public.note DROP CONSTRAINT IF EXISTS note_person_id_fkey;
ALTER TABLE public.note ADD CONSTRAINT note_person_id_fkey
    FOREIGN KEY (person_id) REFERENCES public.person(id) ON DELETE CASCADE;

-- ─── Stream master table ─────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS public.event_streams (
    stream           varchar(200) PRIMARY KEY,
    description      text         NOT NULL,
    event_mapping_id int4         REFERENCES public.event_mappings(id),
    tag              varchar(200) NULL,
    created_at       timestamptz  NOT NULL DEFAULT now()
);

ALTER TABLE public.event_streams
    ADD COLUMN IF NOT EXISTS tag varchar(200) NULL;

-- ─── Event type grammar library ──────────────────────────────────────

CREATE TABLE IF NOT EXISTS public.event_type_grammar (
    id           serial4      NOT NULL,
    name         varchar(200) NOT NULL,
    source       text         NOT NULL DEFAULT '',
    tags         jsonb        NOT NULL DEFAULT '[]'::jsonb,
    close_policy varchar(50)  NULL,
    created_at   timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT event_type_grammar_pkey     PRIMARY KEY (id),
    CONSTRAINT event_type_grammar_name_key UNIQUE (name)
);

ALTER TABLE public.event_type_grammar
    ADD COLUMN IF NOT EXISTS tags jsonb NOT NULL DEFAULT '[]'::jsonb;

-- close_policy on the EventType overrides the mapping-level default.
-- NULL = inherit from event_mappings.close_policy. See internal.sql
-- for the allowed values.
ALTER TABLE public.event_type_grammar
    ADD COLUMN IF NOT EXISTS close_policy varchar(50) NULL;

ALTER TABLE public.event_type_grammar
    DROP CONSTRAINT IF EXISTS event_type_grammar_close_policy_check;
ALTER TABLE public.event_type_grammar
    ADD CONSTRAINT event_type_grammar_close_policy_check
    CHECK (close_policy IS NULL OR close_policy IN ('manual', 'auto_on_insert'));

CREATE INDEX IF NOT EXISTS event_type_grammar_name_idx
    ON public.event_type_grammar (name);

-- ─── event_mappings extension ─────────────────────────────────────────

ALTER TABLE public.event_mappings
    ADD COLUMN IF NOT EXISTS event_types jsonb NOT NULL DEFAULT '[]'::jsonb;
