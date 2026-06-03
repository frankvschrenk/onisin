-- seed-event-type-grammar.sql
-- Seeds the event_type_grammar library with field instance definitions.
--
-- Each source is an instance of the event-schema DSL:
--   <EventTypeName> {
--     required <field>: <type>
--     optional <field>: <type>
--   }
--
-- The Langium grammar (event-schema.langium) in the package validates
-- these instances. The Admin edits them in oosd's Event Types panel.
-- oos uses them to drive completions and form stubs in the stream editor.

DELETE FROM public.event_type_grammar;

INSERT INTO public.event_type_grammar (name, source) VALUES

('EinsatzAusgeloest', $s$EinsatzAusgeloest {
  required text:              string
  required adresse:           string
  required einsatz_nr:        string
  optional betroffene_gaeste: number
}$s$),

('TatortGesichert', $s$TatortGesichert {
  required text:    string
  required bereich: string
  optional schaden: string
}$s$),

('SpurSichergestellt', $s$SpurSichergestellt {
  required text:     string
  required spur_typ: string
  required fundort:  string
  optional analyse:  string
}$s$),

('ZeugenAussageAufgenommen', $s$ZeugenAussageAufgenommen {
  required text:           string
  required zeuge_id:       string
  required relation:       string
  optional anflugrichtung: string
}$s$),

('TaeterBeschreibung', $s$TaeterBeschreibung {
  required text:             string
  required anzahl_taeter:    number
  optional spezies:          string
  optional herkunft:         string
  optional fahndungsmerkmal: string
}$s$),

('VerhandlungAufgenommen', $s$VerhandlungAufgenommen {
  required text:                string
  required verhandlungsfuehrer: string
  required forderung:           string
  optional frist:               string
  optional drohung:             string
}$s$),

('FreilassungErfolgt', $s$FreilassungErfolgt {
  required text:         string
  required freilassung:  string
  required verletzte:    number
  optional letzter_gast: string
  optional sachschaden:  string
}$s$),

('FahrzeugSichergestellt', $s$FahrzeugSichergestellt {
  required text:             string
  required fahrzeug:         string
  optional ausbruchsursache: string
  optional kennzeichen:      string
  optional massnahme:        string
}$s$),

('ticket_created', $s$ticket_created {
  required text:           string
  required priority:       string
  required category:       string
  optional tracking:       string
  optional amount:         string
  optional affected_users: number
}$s$),

('ticket_updated', $s$ticket_updated {
  required text:   string
  required status: string
  optional agent:  string
  optional note:   string
}$s$),

('ticket_resolved', $s$ticket_resolved {
  required text:        string
  required resolution:  string
  required resolved_by: string
  optional refund:      string
}$s$),

('LieferungEingegangen', $s$LieferungEingegangen {
  required text:          string
  required lieferant:     string
  required lieferschein:  string
  required anzahl_pakete: number
  optional lagerplatz:    string
  optional bemerkung:     string
}$s$),

('WarenEntnommen', $s$WarenEntnommen {
  required text:          string
  required artikel_nr:    string
  required menge:         number
  required entnommen_von: string
  optional auftrag_nr:    string
  optional lagerplatz:    string
}$s$),

('InventurAbgeschlossen', $s$InventurAbgeschlossen {
  required text:         string
  required bereich:      string
  required gezaehlt_von: string
  required differenz:    number
  optional kommentar:    string
}$s$)

ON CONFLICT (name) DO UPDATE SET source = EXCLUDED.source;

-- Assignments stored as event_types jsonb[] in event_mappings.
-- Updated via demo-schema.sql ON CONFLICT DO UPDATE.
