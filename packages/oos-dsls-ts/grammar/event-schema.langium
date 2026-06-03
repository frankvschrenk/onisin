// event-schema.langium — Grammar for the onisin event-schema DSL.
//
// An EventType schema defines the structure of a single event type
// within an event mapping (e.g. police, support, warehouse).
// It acts as a corset for the New Event tab: the user can only enter
// fields that the Grammar allows, and required fields must be present.
//
// Each file contains exactly one EventType declaration.
// File extension: .event-schema
//
// Grammar shape:
//
//   EventType "EinsatzAusgeloest" {
//     required text:        string  -- Free-text description
//     required adresse:     string  -- Full address
//     required einsatz_nr:  string  -- Internal number (e.g. E-2024-8821)
//     optional betroffene_gaeste: number  -- Number of people affected
//   }

grammar EventSchema

entry EventSchemaModel:
    eventType=EventTypeDecl;

EventTypeDecl:
    'EventType' name=STRING '{'
        fields+=FieldDecl*
    '}';

FieldDecl:
    required=Required name=ID ':' type=FieldType;

Required returns boolean:
    'required' | 'optional';

FieldType returns string:
    'string' | 'number' | 'boolean' | 'date' | 'datetime';

// ─── Terminals ──────────────────────────────────────────────────

terminal ID: /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;

hidden terminal WS: /\s+/;
hidden terminal ML_COMMENT: /\/\*[\s\S]*?\*\//;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
