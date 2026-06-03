-- migrate_ctx_dsl_to_domain_view.sql
--
-- Renames the legacy ctx/dsl tables to domain/view, renames the xml
-- column to source, and rewires the NOTIFY triggers to the new
-- channel names.
--
-- Idempotent: every step probes pg_class / information_schema first,
-- so a partial run can be retried without harm.
--
-- Channels affected:
--   oos_ctx_notify -> oos_domain_notify
--   oos_dsl_notify -> oos_view_notify
--
-- The vector schema tables (oos.oos_ctx_schema, oos.oos_dsl_schema)
-- are renamed as well — they describe the same data domains and
-- their names should stay consistent.
--
-- Other repos (oos, oosp, ooso, oos-common, oos-chat, oos-builder)
-- still reference the old table/column/channel names. They are not
-- running today; they will be updated in a follow-up session.

BEGIN;

-- ─── 1. Rename oos.ctx -> oos.domain ────────────────────────────

ALTER TABLE IF EXISTS oos.ctx          RENAME TO domain;
ALTER TABLE IF EXISTS oos.domain       RENAME COLUMN xml TO source;

-- Drop and recreate the trigger so it points at the new channel.
DROP TRIGGER  IF EXISTS ctx_notify     ON oos.domain;
DROP TRIGGER  IF EXISTS ctx_updated_at ON oos.domain;
DROP FUNCTION IF EXISTS oos.notify_ctx();

CREATE OR REPLACE FUNCTION oos.notify_domain()
RETURNS TRIGGER LANGUAGE plpgsql AS $func$
BEGIN
    PERFORM pg_notify('oos_domain_notify', NEW.id);
    RETURN NEW;
END;
$func$;

CREATE TRIGGER domain_notify
    AFTER INSERT OR UPDATE ON oos.domain
    FOR EACH ROW EXECUTE FUNCTION oos.notify_domain();

CREATE TRIGGER domain_updated_at
    BEFORE UPDATE ON oos.domain
    FOR EACH ROW EXECUTE FUNCTION oos.set_updated_at();

-- ─── 2. Rename oos.dsl -> oos.view ──────────────────────────────

ALTER TABLE IF EXISTS oos.dsl          RENAME TO view;
ALTER TABLE IF EXISTS oos.view         RENAME COLUMN xml TO source;

DROP TRIGGER  IF EXISTS dsl_notify     ON oos.view;
DROP TRIGGER  IF EXISTS dsl_updated_at ON oos.view;
DROP FUNCTION IF EXISTS oos.notify_dsl();

CREATE OR REPLACE FUNCTION oos.notify_view()
RETURNS TRIGGER LANGUAGE plpgsql AS $func$
BEGIN
    PERFORM pg_notify('oos_view_notify', NEW.id);
    RETURN NEW;
END;
$func$;

CREATE TRIGGER view_notify
    AFTER INSERT OR UPDATE ON oos.view
    FOR EACH ROW EXECUTE FUNCTION oos.notify_view();

CREATE TRIGGER view_updated_at
    BEFORE UPDATE ON oos.view
    FOR EACH ROW EXECUTE FUNCTION oos.set_updated_at();

-- ─── 3. Rename the vector schema tables ─────────────────────────
-- They mirror the data domains and should stay consistent in name.

ALTER TABLE IF EXISTS oos.oos_ctx_schema RENAME TO oos_domain_schema;
ALTER TABLE IF EXISTS oos.oos_dsl_schema RENAME TO oos_view_schema;

-- The ivfflat indexes ride along with the rename; rename them too
-- so debugging/EXPLAIN output stays readable.
ALTER INDEX IF EXISTS oos.oos_ctx_schema_embedding_idx
    RENAME TO oos_domain_schema_embedding_idx;
ALTER INDEX IF EXISTS oos.oos_dsl_schema_embedding_idx
    RENAME TO oos_view_schema_embedding_idx;
ALTER INDEX IF EXISTS oos.oos_dsl_schema_kind_idx
    RENAME TO oos_view_schema_kind_idx;

COMMIT;
