-- demo-schema-police.sql — DDL for the police event source.
--
-- Creates police_incidents source table and police_embeddings vector
-- target table. Run as part of "Install police event source".
-- Idempotent — safe to run multiple times.

CREATE TABLE IF NOT EXISTS public.police_incidents (
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

ALTER TABLE public.police_incidents
    DROP CONSTRAINT IF EXISTS police_incidents_stream_fkey;
ALTER TABLE public.police_incidents
    ADD CONSTRAINT police_incidents_stream_fkey
    FOREIGN KEY (stream) REFERENCES public.event_streams(stream);

-- closed/closed_at lifecycle columns. The trigger below enforces
-- immutability once closed=true. See internal.sql for the function.
ALTER TABLE public.police_incidents
    ADD COLUMN IF NOT EXISTS closed    boolean     NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS closed_at timestamptz NULL;

DROP TRIGGER IF EXISTS police_incidents_prevent_modify_closed
    ON public.police_incidents;
CREATE TRIGGER police_incidents_prevent_modify_closed
    BEFORE UPDATE OR DELETE ON public.police_incidents
    FOR EACH ROW EXECUTE FUNCTION public.prevent_modify_closed_event();

CREATE TABLE IF NOT EXISTS public.police_embeddings (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id    text        NOT NULL UNIQUE,
    stream_id    text        NOT NULL,
    event_type   text        NOT NULL,
    text_content text        NOT NULL,
    metadata     jsonb       NOT NULL DEFAULT '{}',
    embedding    vector(1024),
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS police_embeddings_stream_idx
    ON public.police_embeddings (stream_id, created_at);
CREATE INDEX IF NOT EXISTS police_embeddings_vector_idx
    ON public.police_embeddings USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

-- Register police mapping
INSERT INTO public.event_mappings
    (name, source_schema, source_table, source_text_field, source_id_field,
     notify_channel, target_schema, target_table, enabled, close_policy,
     event_types)
VALUES
    ('police', 'public', 'police_incidents', 'text', 'id',
     'police_incidents_notify', 'public', 'police_embeddings', true, 'manual',
     '["EinsatzAusgeloest","TatortGesichert","SpurSichergestellt","ZeugenAussageAufgenommen","TaeterBeschreibung","VerhandlungAufgenommen","FreilassungErfolgt","FahrzeugSichergestellt"]')
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
