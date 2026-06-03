-- demo-events-police.sql — Demo events for the police pipeline only.
--
-- Inserts two cases:
--   fall-2024-0042 — Munich burglary (6 events)
--   fall-2024-0080 — Augsburg parrot hostage situation (7 events)
--
-- Idempotent: TRUNCATEs police data and streams first.
-- Run after demo-schema.sql and demo-data.sql.

TRUNCATE public.police_embeddings RESTART IDENTITY CASCADE;
TRUNCATE public.police_incidents  RESTART IDENTITY CASCADE;

DELETE FROM public.event_streams WHERE event_mapping_id = 1;

-- ─── Streams ──────────────────────────────────────────────────────────

INSERT INTO public.event_streams (stream, description, event_mapping_id, tag) VALUES
('fall-2024-0042', 'Einbruch Industriestrasse München',        1, 'Einbruch'),
('fall-2024-0080', 'Papageien-Ausbruch Cafe Kolibri Augsburg', 1, 'Geiselnahme')
ON CONFLICT (stream) DO UPDATE SET
    description      = EXCLUDED.description,
    event_mapping_id = EXCLUDED.event_mapping_id,
    tag              = EXCLUDED.tag;

-- ─── Fall 2024-0042 — Burglary at Industriestrasse Munich ────────────

INSERT INTO public.police_incidents (stream, event_type, text, payload) VALUES
('fall-2024-0042', 'EinsatzAusgeloest',
 'Alarm ausgeloest durch Einbruchmeldeanlage um 02:00 Uhr. Zwei Streifenwagen entsandt.',
 '{"adresse":"Industriestrasse 17, 80939 Muenchen","einsatz_nr":"E-2024-8821"}'),

('fall-2024-0042', 'TatortGesichert',
 'Osteingang aufgebrochen. Hebelspuren am Tuerrahmen. Glasscherben innen. Kein Taeter vor Ort.',
 '{"bereich":"Lagerhalle Osteingang"}'),

('fall-2024-0042', 'SpurSichergestellt',
 'Reifenabdruck auf unbefestigtem Untergrund. Breite ca. 225mm, typisch fuer Transporter.',
 '{"spur_typ":"Reifenabdruck","fundort":"Parkplatz Suedseite"}'),

('fall-2024-0042', 'ZeugenAussageAufgenommen',
 'Nachbar Rudolf K. hoerte gegen 02:15 Uhr Motorengeraeusche. Sah weissen Transporter ohne Aufschrift. Fuhr ca. 02:45 Uhr Richtung Nordausfahrt.',
 '{"zeuge_id":"Z-001","relation":"Nachbar"}'),

('fall-2024-0042', 'ZeugenAussageAufgenommen',
 'Sicherheitsdienstmitarbeiter Thomas W. sah weissen Transporter mit getoenten Scheiben. Teilkennzeichen: M-?? 4??.',
 '{"zeuge_id":"Z-002","relation":"Sicherheitsdienst"}'),

('fall-2024-0042', 'SpurSichergestellt',
 'Hebelwerkzeug hinterliess Abdruck am Tuerrahmen. Breite ca. 30mm. Kriminaltechniker angefordert.',
 '{"spur_typ":"Werkzeugabdruck","fundort":"Tuerrahmen Osteingang"}'),

('fall-2024-0042', 'TaeterBeschreibung',
 'Drei unbekannte Maenner. Alle vermummt, dunkle Jacken. Einer ca. 185cm, kraeftige Statur. Flucht Richtung Autobahnauffahrt Nord.',
 '{"anzahl_taeter":3,"fahndungsmerkmal":"dunkle Jacken, Vermummung"}'),

('fall-2024-0042', 'FahrzeugSichergestellt',
 'Weisser Transporter Typ Mercedes Sprinter, Kennzeichen teilweise lesbar M-AX. Fahrzeug in einer Seitenstrasse sichergestellt.',
 '{"fahrzeug":"Mercedes Sprinter weiss","kennzeichen":"M-AX ???","massnahme":"Spurensicherung KTU"}');

-- ─── Fall 2024-0080 — Augsburg parrot hostage situation ──────────────

INSERT INTO public.police_incidents (stream, event_type, text, payload) VALUES
('fall-2024-0080', 'EinsatzAusgeloest',
 'Notruf um 09:14 Uhr. Zwei ausgebuexte Graupapageien aus dem Augsburger Zoo haben die Theke des Cafe Kolibri in der Maximilianstrasse 42 besetzt. Vier Gaeste und eine Baeckerin kommen nicht mehr an ihre Croissants.',
 '{"adresse":"Maximilianstrasse 42, 86150 Augsburg","einsatz_nr":"E-2024-3301","betroffene_gaeste":4}'),

('fall-2024-0080', 'TaeterBeschreibung',
 'Taeter 1: Graupapagei, ca. 33cm, Rufname Coco, auffaelliger roter Schwanz, ruft unablaessig Freiheit. Taeter 2: Graupapagei, ca. 31cm, Rufname Pepe, etwas kleiner, imitiert Kaffeemaschine und Handyklingeln.',
 '{"anzahl_taeter":2,"spezies":"Psittacus erithacus","herkunft":"Zoo Augsburg Voliere 7"}'),

('fall-2024-0080', 'VerhandlungAufgenommen',
 'Tierpfleger Bernd Moeller vom Augsburger Zoo uebernimmt die Verhandlung. Taeter fordern Sonnenblumenkerne und ungestoerten Zugang zur Croissant-Auslage. Forderungsfrist bis 12:00 Uhr, sonst drohen weitere Imitationen der Kaffeemaschine.',
 '{"verhandlungsfuehrer":"Tierpfleger Bernd Moeller","forderung":"Sonnenblumenkerne + Croissants","frist":"12:00"}'),

('fall-2024-0080', 'ZeugenAussageAufgenommen',
 'Passantin Maria S. beobachtete die beiden Papageien beim Anflug aus Richtung Stadtpark. Coco landete zuerst auf der Espressomaschine, Pepe folgte ueber das offene Oberlicht.',
 '{"zeuge_id":"Z-001","relation":"Passantin","anflugrichtung":"Stadtpark"}'),

('fall-2024-0080', 'SpurSichergestellt',
 'Mehrere graue Schwanzfedern und ein angeknabbertes Butter-Croissant sichergestellt. Federn zur DNA-Abgleichung an die Zoo-Tieraerztin uebergeben.',
 '{"spur_typ":"Federn, Croissant","fundort":"Theke und Oberlicht Cafe Kolibri"}'),

('fall-2024-0080', 'FreilassungErfolgt',
 'Um 15:42 Uhr lassen sich beide Papageien mit Sonnenblumenkernen in die mitgebrachte Transportbox locken. Keine Verletzten. Alle Gaeste erhalten ihre Croissants, das Haus uebernimmt die Rechnung.',
 '{"freilassung":"ohne Widerstand","verletzte":0,"letzter_gast":"Dr. Susanne Kramer"}'),

('fall-2024-0080', 'FahrzeugSichergestellt',
 'Transportfahrzeug im Einsatz: Lastenrad des Augsburger Zoos mit gruener Transportbox. Ausbruchsursache: Volierentuer nach Reinigung nicht korrekt verriegelt. Tierpflegeplan wird ueberarbeitet.',
 '{"fahrzeug":"Lastenrad Zoo Augsburg","ausbruchsursache":"Voliere 7 nicht verriegelt"}');

-- Demonstrate closed events: mark the final FahrzeugSichergestellt
-- entries of both cases as closed. Showcases the immutability trigger
-- and gives the UI a state to render the lock icon against.
UPDATE public.police_incidents
   SET closed    = true,
       closed_at = now()
 WHERE stream     = 'fall-2024-0042'
   AND event_type = 'FahrzeugSichergestellt';

UPDATE public.police_incidents
   SET closed    = true,
       closed_at = now()
 WHERE stream     = 'fall-2024-0080'
   AND event_type = 'FahrzeugSichergestellt';

ALTER TABLE public.police_incidents
    VALIDATE CONSTRAINT police_incidents_stream_fkey;
