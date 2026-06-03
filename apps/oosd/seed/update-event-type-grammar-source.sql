-- update-event-type-grammar-source.sql
-- Replaces placeholder source with real Langium grammar definitions.
-- Each event type has its own self-contained grammar.
-- ON CONFLICT DO UPDATE so this is safe to re-run.

UPDATE public.event_type_grammar SET source =
'grammar EinsatzAusgeloest

entry EinsatzAusgeloestModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'EinsatzAusgeloest';

UPDATE public.event_type_grammar SET source =
'grammar TatortGesichert

entry TatortGesichertModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'TatortGesichert';

UPDATE public.event_type_grammar SET source =
'grammar SpurSichergestellt

entry SpurSichergestelltModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'SpurSichergestellt';

UPDATE public.event_type_grammar SET source =
'grammar ZeugenAussageAufgenommen

entry ZeugenAussageAufgenommenModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'ZeugenAussageAufgenommen';

UPDATE public.event_type_grammar SET source =
'grammar TaeterBeschreibung

entry TaeterBeschreibungModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'TaeterBeschreibung';

UPDATE public.event_type_grammar SET source =
'grammar VerhandlungAufgenommen

entry VerhandlungAufgenommenModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'VerhandlungAufgenommen';

UPDATE public.event_type_grammar SET source =
'grammar FreilassungErfolgt

entry FreilassungErfolgtModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'FreilassungErfolgt';

UPDATE public.event_type_grammar SET source =
'grammar FahrzeugSichergestellt

entry FahrzeugSichergestelltModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'FahrzeugSichergestellt';

UPDATE public.event_type_grammar SET source =
'grammar ticket_created

entry ticket_createdModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'ticket_created';

UPDATE public.event_type_grammar SET source =
'grammar ticket_updated

entry ticket_updatedModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'ticket_updated';

UPDATE public.event_type_grammar SET source =
'grammar ticket_resolved

entry ticket_resolvedModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'ticket_resolved';

UPDATE public.event_type_grammar SET source =
'grammar LieferungEingegangen

entry LieferungEingegangenModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'LieferungEingegangen';

UPDATE public.event_type_grammar SET source =
'grammar WarenEntnommen

entry WarenEntnommenModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'WarenEntnommen';

UPDATE public.event_type_grammar SET source =
'grammar InventurAbgeschlossen

entry InventurAbgeschlossenModel:
    ''EventType'' name=STRING ''{''
        fields+=FieldDecl*
    ''}'';

FieldDecl:
    ( {infer RequiredField} ''required''
    | {infer OptionalField} ''optional'' )
    fieldName=ID '':'' type=FieldType;

FieldType returns string:
    ''string'' | ''number'' | ''boolean'';

terminal ID:     /[_a-zA-Z][a-zA-Z0-9_]*/;
terminal STRING: /"(\\.|[^"\\])*"/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
'
WHERE name = 'InventurAbgeschlossen';
