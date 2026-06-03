-- migrate-event-type-grammar.sql
-- Replaces event_type_schemas with event_type_grammar (mapping-independent)
-- and adds event_mapping_types M:N table.

-- Drop old table (CASCADE removes dependent objects)
DROP TABLE IF EXISTS public.event_type_schemas CASCADE;

-- Grammar library — one row per event type, mapping-independent
CREATE TABLE IF NOT EXISTS public.event_type_grammar (
    id         serial4      NOT NULL,
    name       varchar(200) NOT NULL,
    source     text         NOT NULL DEFAULT '',
    created_at timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT event_type_grammar_pkey     PRIMARY KEY (id),
    CONSTRAINT event_type_grammar_name_key UNIQUE (name)
);

CREATE INDEX IF NOT EXISTS event_type_grammar_name_idx
    ON public.event_type_grammar (name);

-- M:N link: which event types belong to which mapping
CREATE TABLE IF NOT EXISTS public.event_mapping_types (
    id               serial4 NOT NULL,
    event_mapping_id int4    NOT NULL
        REFERENCES public.event_mappings(id) ON DELETE CASCADE,
    event_type_id    int4    NOT NULL
        REFERENCES public.event_type_grammar(id) ON DELETE CASCADE,
    CONSTRAINT event_mapping_types_pkey            PRIMARY KEY (id),
    CONSTRAINT event_mapping_types_mapping_type_key
        UNIQUE (event_mapping_id, event_type_id)
);

CREATE INDEX IF NOT EXISTS event_mapping_types_mapping_idx
    ON public.event_mapping_types (event_mapping_id);
CREATE INDEX IF NOT EXISTS event_mapping_types_type_idx
    ON public.event_mapping_types (event_type_id);
