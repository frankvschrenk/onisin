-- demo-schema-support.sql — DDL for the support event source.
--
-- Creates support_tickets source table and support_embeddings vector
-- target table. Run as part of "Install support event source".
-- Idempotent — safe to run multiple times.

CREATE TABLE IF NOT EXISTS public.support_tickets (
    id         serial       PRIMARY KEY,
    stream     varchar(200) NOT NULL REFERENCES public.event_streams(stream),
    event_type varchar(200) NOT NULL,
    text       text         NOT NULL,
    payload    jsonb        NOT NULL DEFAULT '{}',
    processed  boolean      NOT NULL DEFAULT false,
    closed     boolean      NOT NULL DEFAULT false,
    closed_at  timestamptz  NULL,
    created_at timestamptz  NOT NULL DEFAULT now()
);

ALTER TABLE public.support_tickets
    DROP CONSTRAINT IF EXISTS support_tickets_stream_fkey;
ALTER TABLE public.support_tickets
    ADD CONSTRAINT support_tickets_stream_fkey
    FOREIGN KEY (stream) REFERENCES public.event_streams(stream);

-- closed/closed_at lifecycle columns. The trigger below enforces
-- immutability once closed=true. See internal.sql for the function.
ALTER TABLE public.support_tickets
    ADD COLUMN IF NOT EXISTS closed    boolean     NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS closed_at timestamptz NULL;

DROP TRIGGER IF EXISTS support_tickets_prevent_modify_closed
    ON public.support_tickets;
CREATE TRIGGER support_tickets_prevent_modify_closed
    BEFORE UPDATE OR DELETE ON public.support_tickets
    FOR EACH ROW EXECUTE FUNCTION public.prevent_modify_closed_event();

CREATE TABLE IF NOT EXISTS public.support_embeddings (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id    text        NOT NULL UNIQUE,
    stream_id    text        NOT NULL,
    event_type   text        NOT NULL,
    text_content text        NOT NULL,
    metadata     jsonb       NOT NULL DEFAULT '{}',
    embedding    vector(1024),
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS support_embeddings_stream_idx
    ON public.support_embeddings (stream_id, created_at);
CREATE INDEX IF NOT EXISTS support_embeddings_vector_idx
    ON public.support_embeddings USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

-- Register support mapping
INSERT INTO public.event_mappings
    (name, source_schema, source_table, source_text_field, source_id_field,
     notify_channel, target_schema, target_table, enabled, close_policy,
     event_types)
VALUES
    ('support', 'public', 'support_tickets', 'text', 'id',
     'support_tickets_notify', 'public', 'support_embeddings', true, 'manual',
     '["ticket_created","ticket_updated","ticket_resolved"]')
ON CONFLICT (name) DO UPDATE SET
    source_schema     = EXCLUDED.source_schema,
    source_table      = EXCLUDED.source_table,
    source_text_field = EXCLUDED.source_text_field,
    source_id_field   = EXCLUDED.source_id_field,
    notify_channel    = EXCLUDED.notify_channel,
    target_schema     = EXCLUDED.target_schema,
    target_table      = EXCLUDED.target_table,
    enabled           = EXCLUDED.enabled,
    close_policy      = EXCLUDED.close_policy,
    event_types       = EXCLUDED.event_types;
