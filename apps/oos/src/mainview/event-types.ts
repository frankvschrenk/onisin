// event-types.ts \u2014 type surface for the event subsystem, webview-side.
//
// Relocated from the retired bun/event-client.ts. Only the types are kept:
// the old module's rpcCall helpers used the Bun NATS client, which no longer
// exists. The event panels (NewEventPanel, EventResultPanel, MappingPicker,
// StreamManagerPanel, StreamPicker) import only these shapes. The data path
// behind them (oosai event subjects) is not yet migrated \u2014 see rpc.ts,
// where the event methods are honest 'not yet migrated' stubs.

export interface EventMapping {
	id:                number;
	name:              string;
	source_schema:     string;
	source_table:      string;
	source_text_field: string;
	source_id_field:   string;
	notify_channel:    string;
	target_schema:     string;
	target_table:      string;
	enabled:           boolean;
	listener_active:   boolean;
}

export interface EventHit {
	mappingName: string;
	sourceId:    string;
	streamId:    string;
	eventType:   string;
	textContent: string;
	metadata:    Record<string, unknown>;
	score:       number;
}

export interface StreamSummary {
	mapping:     string;
	stream:      string;
	description: string;
	eventCount:  number;
}
