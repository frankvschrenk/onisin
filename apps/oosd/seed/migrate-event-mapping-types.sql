-- migrate-event-mapping-types.sql
-- Simplifies the event type assignment model:
--   - DROP event_mapping_types (M:N table no longer needed)
--   - ADD event_types jsonb to event_mappings (array of type names)

DROP TABLE IF EXISTS public.event_mapping_types CASCADE;

ALTER TABLE public.event_mappings
    DROP COLUMN IF EXISTS event_types;

ALTER TABLE public.event_mappings
    ADD COLUMN event_types jsonb NOT NULL DEFAULT '[]'::jsonb;

-- Populate existing mappings with their event types
UPDATE public.event_mappings SET event_types = '["EinsatzAusgeloest","TatortGesichert","SpurSichergestellt","ZeugenAussageAufgenommen","TaeterBeschreibung","VerhandlungAufgenommen","FreilassungErfolgt","FahrzeugSichergestellt"]'::jsonb
WHERE name = 'police';

UPDATE public.event_mappings SET event_types = '["ticket_created","ticket_updated","ticket_resolved"]'::jsonb
WHERE name = 'support';

UPDATE public.event_mappings SET event_types = '["LieferungEingegangen","WarenEntnommen","InventurAbgeschlossen","FreilassungErfolgt"]'::jsonb
WHERE name = 'warehouse';
