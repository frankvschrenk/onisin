-- reset-event-type-grammar-to-instances.sql
-- Replaces Langium grammar source with concrete field instances.
-- The Langium grammar (event-schema.langium) lives in the package;
-- source here is an instance of that grammar per event type.

UPDATE public.event_type_grammar SET source =
$s$EinsatzAusgeloest {
  required text:              string
  required adresse:           string
  required einsatz_nr:        string
  optional betroffene_gaeste: number
}$s$
WHERE name = 'EinsatzAusgeloest';

UPDATE public.event_type_grammar SET source =
$s$TatortGesichert {
  required text:    string
  required bereich: string
  optional schaden: string
}$s$
WHERE name = 'TatortGesichert';

UPDATE public.event_type_grammar SET source =
$s$SpurSichergestellt {
  required text:     string
  required spur_typ: string
  required fundort:  string
  optional analyse:  string
}$s$
WHERE name = 'SpurSichergestellt';

UPDATE public.event_type_grammar SET source =
$s$ZeugenAussageAufgenommen {
  required text:            string
  required zeuge_id:        string
  required relation:        string
  optional anflugrichtung:  string
}$s$
WHERE name = 'ZeugenAussageAufgenommen';

UPDATE public.event_type_grammar SET source =
$s$TaeterBeschreibung {
  required text:             string
  required anzahl_taeter:    number
  optional spezies:          string
  optional herkunft:         string
  optional fahndungsmerkmal: string
}$s$
WHERE name = 'TaeterBeschreibung';

UPDATE public.event_type_grammar SET source =
$s$VerhandlungAufgenommen {
  required text:                string
  required verhandlungsfuehrer: string
  required forderung:           string
  optional frist:               string
  optional drohung:             string
}$s$
WHERE name = 'VerhandlungAufgenommen';

UPDATE public.event_type_grammar SET source =
$s$FreilassungErfolgt {
  required text:         string
  required freilassung:  string
  required verletzte:    number
  optional letzter_gast: string
  optional sachschaden:  string
}$s$
WHERE name = 'FreilassungErfolgt';

UPDATE public.event_type_grammar SET source =
$s$FahrzeugSichergestellt {
  required text:             string
  required fahrzeug:         string
  optional ausbruchsursache: string
  optional kennzeichen:      string
  optional massnahme:        string
}$s$
WHERE name = 'FahrzeugSichergestellt';

UPDATE public.event_type_grammar SET source =
$s$ticket_created {
  required text:           string
  required priority:       string
  required category:       string
  optional tracking:       string
  optional amount:         string
  optional affected_users: number
}$s$
WHERE name = 'ticket_created';

UPDATE public.event_type_grammar SET source =
$s$ticket_updated {
  required text:   string
  required status: string
  optional agent:  string
  optional note:   string
}$s$
WHERE name = 'ticket_updated';

UPDATE public.event_type_grammar SET source =
$s$ticket_resolved {
  required text:        string
  required resolution:  string
  required resolved_by: string
  optional refund:      string
}$s$
WHERE name = 'ticket_resolved';

UPDATE public.event_type_grammar SET source =
$s$LieferungEingegangen {
  required text:          string
  required lieferant:     string
  required lieferschein:  string
  required anzahl_pakete: number
  optional lagerplatz:    string
  optional bemerkung:     string
}$s$
WHERE name = 'LieferungEingegangen';

UPDATE public.event_type_grammar SET source =
$s$WarenEntnommen {
  required text:          string
  required artikel_nr:    string
  required menge:         number
  required entnommen_von: string
  optional auftrag_nr:    string
  optional lagerplatz:    string
}$s$
WHERE name = 'WarenEntnommen';

UPDATE public.event_type_grammar SET source =
$s$InventurAbgeschlossen {
  required text:         string
  required bereich:      string
  required gezaehlt_von: string
  required differenz:    number
  optional kommentar:    string
}$s$
WHERE name = 'InventurAbgeschlossen';
