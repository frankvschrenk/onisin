-- migrate-tags.sql
-- Adds tag support to event_type_grammar and event_streams.
--
-- event_type_grammar.tags  — jsonb array of context tags this type
--                            may be used in, e.g. ["Einbruch", "HaeuslicheGewalt"]
--                            Empty array = usable in all contexts.
--
-- event_streams.tag        — single tag identifying the context of
--                            this stream, e.g. "Einbruch".
--                            NULL = no tag filter applied.

ALTER TABLE public.event_type_grammar
    ADD COLUMN IF NOT EXISTS tags jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE public.event_streams
    ADD COLUMN IF NOT EXISTS tag varchar(200) NULL;

-- Seed tags for all demo event types
UPDATE public.event_type_grammar SET tags = '["Einbruch","HaeuslicheGewalt","Schlaegerei","Betrug"]'::jsonb
WHERE name = 'EinsatzAusgeloest';

UPDATE public.event_type_grammar SET tags = '["Einbruch","HaeuslicheGewalt","Schlaegerei"]'::jsonb
WHERE name = 'TatortGesichert';

UPDATE public.event_type_grammar SET tags = '["Einbruch","HaeuslicheGewalt"]'::jsonb
WHERE name = 'SpurSichergestellt';

UPDATE public.event_type_grammar SET tags = '["Einbruch","HaeuslicheGewalt","Ladendiebstahl","Schlaegerei"]'::jsonb
WHERE name = 'ZeugenAussageAufgenommen';

UPDATE public.event_type_grammar SET tags = '["Einbruch","HaeuslicheGewalt","Ladendiebstahl","Schlaegerei","Betrug"]'::jsonb
WHERE name = 'TaeterBeschreibung';

UPDATE public.event_type_grammar SET tags = '["Einbruch","Geiselnahme"]'::jsonb
WHERE name = 'VerhandlungAufgenommen';

UPDATE public.event_type_grammar SET tags = '["Einbruch","Geiselnahme"]'::jsonb
WHERE name = 'FreilassungErfolgt';

UPDATE public.event_type_grammar SET tags = '["Einbruch"]'::jsonb
WHERE name = 'FahrzeugSichergestellt';

UPDATE public.event_type_grammar SET tags = '["Billing","Shipping","Technical","Account"]'::jsonb
WHERE name = 'ticket_created';

UPDATE public.event_type_grammar SET tags = '["Billing","Shipping","Technical","Account"]'::jsonb
WHERE name = 'ticket_updated';

UPDATE public.event_type_grammar SET tags = '["Billing","Shipping","Technical","Account"]'::jsonb
WHERE name = 'ticket_resolved';

UPDATE public.event_type_grammar SET tags = '["Eingang","Ausgang","Inventur"]'::jsonb
WHERE name = 'LieferungEingegangen';

UPDATE public.event_type_grammar SET tags = '["Ausgang","Produktion"]'::jsonb
WHERE name = 'WarenEntnommen';

UPDATE public.event_type_grammar SET tags = '["Inventur"]'::jsonb
WHERE name = 'InventurAbgeschlossen';

-- Seed tags for demo streams
UPDATE public.event_streams SET tag = 'Einbruch'
WHERE stream = 'fall-2024-0042';

UPDATE public.event_streams SET tag = 'Geiselnahme'
WHERE stream = 'fall-2024-0080';
