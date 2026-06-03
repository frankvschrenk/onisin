-- demo-schema-pipeline.sql
-- Pipeline demo schema: document store and pipeline registry.
-- Idempotent: all statements use IF NOT EXISTS.

-- Document store: text + embedding side by side.
-- embedding column is added only when pgvector is available.
CREATE TABLE IF NOT EXISTS public.pipeline_documents (
    id           SERIAL PRIMARY KEY,
    fall_nr      VARCHAR(20)  NOT NULL UNIQUE,
    kategorie    VARCHAR(50)  NOT NULL,
    titel        VARCHAR(200) NOT NULL,
    inhalt       TEXT         NOT NULL,
    embedding    vector(1024),
    quelle       VARCHAR(10)  NOT NULL DEFAULT 'db',   -- 'db' or 's3'
    s3_key       VARCHAR(500),                          -- set when quelle='s3'
    erstellt_am  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS pipeline_documents_kategorie_idx
    ON public.pipeline_documents (kategorie);

CREATE INDEX IF NOT EXISTS pipeline_documents_quelle_idx
    ON public.pipeline_documents (quelle);

-- Pipeline registry: stores .pipeline DSL source.
CREATE TABLE IF NOT EXISTS public.pipelines (
    name          VARCHAR(100) PRIMARY KEY,
    source        TEXT         NOT NULL,
    erstellt_am   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    geaendert_am  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
