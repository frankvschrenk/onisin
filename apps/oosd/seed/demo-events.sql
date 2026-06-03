-- demo-events.sql — Demo events for the police and support pipelines.
--
-- Run after demo-schema.sql + demo-data.sql. Inserts:
--   * fall-2024-0042 — Munich burglary case (6 events)
--   * fall-2024-0080 — Augsburg parrot escape (7 events)
--   * 3 support tickets across customer + IT streams
--
-- Each INSERT into public.police_incidents fires the
-- police_incidents_notify trigger; oosp picks the row up and writes
-- an embedding into public.police_embeddings. Same for the support
-- mappings.
--
-- Idempotent: TRUNCATE first so re-running starts from a clean slate.

TRUNCATE public.police_embeddings, public.police_incidents
    RESTART IDENTITY CASCADE;
TRUNCATE public.support_embeddings, public.support_tickets
    RESTART IDENTITY CASCADE;
TRUNCATE public.event_streams CASCADE;

-- ─── Stream master rows ──────────────────────────────────────────────
--
-- Inserted before the events themselves so the foreign key from
-- police_incidents.stream / support_tickets.stream resolves.

INSERT INTO public.event_streams (stream, description, event_mapping_id, tag) VALUES
('fall-2024-0042', 'Einbruch Industriestrasse München',        1, 'Einbruch'),
('fall-2024-0080', 'Papageien-Ausbruch Cafe Kolibri Augsburg', 1, 'Geiselnahme'),
('customer-12345', 'Kunde 12345 — Lieferung verspätet',        2, 'Shipping'),
('customer-67890', 'Kunde 67890 — Doppelte Abrechnung',        2, 'Billing'),
('department-IT',  'IT-Abteilung — VPN-Störung',              2, 'Technical');

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
 '{"spur_typ":"Werkzeugabdruck","fundort":"Tuerrahmen Osteingang"}');

-- ─── Fall 2024-0080 — Augsburg zoo parrot escape ─────────────────────

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

-- ─── Support tickets ─────────────────────────────────────────────────

INSERT INTO public.support_tickets (stream, event_type, text, payload) VALUES
('customer-12345', 'ticket_created',
 'Order has not arrived for 10 days. Tracking number: DE123456789. Customer is very unhappy.',
 '{"priority":"high","category":"shipping","tracking":"DE123456789"}'),

('customer-67890', 'ticket_created',
 'Invoice miscalculated. Charged twice for the premium account. Customer requests a refund.',
 '{"priority":"medium","category":"billing","amount":"29.99"}'),

('department-IT', 'ticket_created',
 'VPN connection broken since the latest Windows update. Multiple users affected.',
 '{"priority":"high","category":"network","affected_users":15}');


-- ─── Validate FKs once data is consistent ────────────────────────────
--
-- demo-schema.sql installed the FKs as NOT VALID so that retro-fitting
-- a populated database works. After the TRUNCATE + reseed above the
-- data is consistent again, so we promote the FKs to fully valid.
-- VALIDATE CONSTRAINT is idempotent — running it on an already-valid
-- constraint is a no-op.

ALTER TABLE public.police_incidents
    VALIDATE CONSTRAINT police_incidents_stream_fkey;
ALTER TABLE public.support_tickets
    VALIDATE CONSTRAINT support_tickets_stream_fkey;
