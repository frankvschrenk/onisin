-- seed-event-type-schemas.sql
-- Inserts Grammar definitions for all demo event types.
-- Uses ON CONFLICT ... DO UPDATE so it is safe to re-run.

INSERT INTO public.event_type_schemas (event_mapping_id, event_type, source) VALUES

-- ─── police (mapping_id = 1) ──────────────────────────────────────────

(1, 'EinsatzAusgeloest',
'EventType "EinsatzAusgeloest" {
  required text:              string  -- Free-text description of the incident
  required adresse:           string  -- Full address of the incident location
  required einsatz_nr:        string  -- Internal incident number (e.g. E-2024-8821)
  optional betroffene_gaeste: number  -- Number of people affected
}'),

(1, 'TatortGesichert',
'EventType "TatortGesichert" {
  required text:    string  -- Description of scene findings
  required bereich: string  -- Area / location within the scene
  optional schaden: string  -- Type and extent of damage observed
}'),

(1, 'SpurSichergestellt',
'EventType "SpurSichergestellt" {
  required text:     string  -- Description of the secured trace
  required spur_typ: string  -- Type of trace (e.g. Reifenabdruck, Werkzeugabdruck, Federn)
  required fundort:  string  -- Location where the trace was found
  optional analyse:  string  -- Note on analysis or lab submission
}'),

(1, 'ZeugenAussageAufgenommen',
'EventType "ZeugenAussageAufgenommen" {
  required text:     string  -- Content of the witness statement
  required zeuge_id: string  -- Witness ID (e.g. Z-001)
  required relation: string  -- Relationship to the case (e.g. Nachbar, Passant, Sicherheitsdienst)
  optional anflugrichtung: string  -- Observed direction of movement
}'),

(1, 'TaeterBeschreibung',
'EventType "TaeterBeschreibung" {
  required text:          string  -- Free-text description of the suspects
  required anzahl_taeter: number  -- Number of suspects
  optional spezies:       string  -- Species (for animal-related incidents)
  optional herkunft:      string  -- Origin of the suspects
  optional fahndungsmerkmal: string  -- Notable identifying feature
}'),

(1, 'VerhandlungAufgenommen',
'EventType "VerhandlungAufgenommen" {
  required text:                string  -- Description of the negotiation situation
  required verhandlungsfuehrer: string  -- Name of the lead negotiator
  required forderung:           string  -- Demands made by suspects
  optional frist:               string  -- Deadline (e.g. 12:00)
  optional drohung:             string  -- Threatened consequence if demands unmet
}'),

(1, 'FreilassungErfolgt',
'EventType "FreilassungErfolgt" {
  required text:        string  -- Description of the resolution
  required freilassung: string  -- How the situation was resolved
  required verletzte:   number  -- Number of injured persons (0 = none)
  optional letzter_gast: string  -- Name of last affected person
  optional sachschaden:  string  -- Description of any property damage
}'),

(1, 'FahrzeugSichergestellt',
'EventType "FahrzeugSichergestellt" {
  required text:             string  -- Description of the secured vehicle
  required fahrzeug:         string  -- Vehicle designation
  optional ausbruchsursache: string  -- Root cause of the incident
  optional kennzeichen:      string  -- License plate or partial plate
  optional massnahme:        string  -- Follow-up action taken
}'),

-- ─── support (mapping_id = 2) ─────────────────────────────────────────

(2, 'ticket_created',
'EventType "ticket_created" {
  required text:     string  -- Description of the problem
  required priority: string  -- Priority: high | medium | low
  required category: string  -- Category (e.g. shipping, billing, network)
  optional tracking:       string  -- Shipment tracking number
  optional amount:         string  -- Amount affected (e.g. 29.99)
  optional affected_users: number  -- Number of affected users
}'),

(2, 'ticket_updated',
'EventType "ticket_updated" {
  required text:   string  -- Description of the change
  required status: string  -- New status: open | in_progress | resolved | closed
  optional agent:  string  -- Handling agent name
  optional note:   string  -- Internal note on the change
}'),

(2, 'ticket_resolved',
'EventType "ticket_resolved" {
  required text:        string  -- Description of the resolution
  required resolution:  string  -- How the issue was resolved
  required resolved_by: string  -- Name of the resolver
  optional refund:      string  -- Amount refunded (if applicable)
}'),

-- ─── warehouse (mapping_id = 3) ───────────────────────────────────────

(3, 'LieferungEingegangen',
'EventType "LieferungEingegangen" {
  required text:          string  -- Description of the incoming delivery
  required lieferant:     string  -- Supplier name
  required lieferschein:  string  -- Delivery note number
  required anzahl_pakete: number  -- Number of packages received
  optional lagerplatz:    string  -- Assigned storage location
  optional bemerkung:     string  -- Remarks or damage notes
}'),

(3, 'WarenEntnommen',
'EventType "WarenEntnommen" {
  required text:          string  -- Description of the withdrawal
  required artikel_nr:    string  -- Article number
  required menge:         number  -- Quantity withdrawn
  required entnommen_von: string  -- Name of person withdrawing
  optional auftrag_nr:    string  -- Related order number
  optional lagerplatz:    string  -- Storage location of withdrawal
}'),

(3, 'InventurAbgeschlossen',
'EventType "InventurAbgeschlossen" {
  required text:         string  -- Summary of the inventory
  required bereich:      string  -- Area or shelf inventoried
  required gezaehlt_von: string  -- Conducted by
  required differenz:    number  -- Difference from target stock (0 = none)
  optional kommentar:    string  -- Notes on discrepancies
}')

ON CONFLICT (event_mapping_id, event_type)
DO UPDATE SET source = EXCLUDED.source;
