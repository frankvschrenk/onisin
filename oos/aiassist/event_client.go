package aiassist

// event_client.go — HTTP client for the oosp event endpoints.
//
// Wraps the three REST calls needed by the event chat mode:
//
//   GET  /event/mappings           — list configured event mappings
//   GET  /event/streams?mapping=x  — distinct stream IDs for a mapping
//   POST /event/search             — vector search (embed + pgvector)
//
// All calls go through helper.OOSP which is the shared resty client.
// Methods return parsed Go structs, not raw JSON, so callers don't have
// to repeat the unmarshal boilerplate.

import (
	"encoding/json"
	"fmt"
	"net/url"

	"onisin.com/oos/helper"
)

// EventMapping mirrors the subset of oosp's mapping record the UI needs.
type EventMapping struct {
	Name           string `json:"name"`
	SourceSchema   string `json:"source_schema"`
	SourceTable    string `json:"source_table"`
	TargetSchema   string `json:"target_schema"`
	TargetTable    string `json:"target_table"`
	Enabled        bool   `json:"enabled"`
	ListenerActive bool   `json:"listener_active"`
}

// EventHit is a single vector-search result from the server.
type EventHit struct {
	MappingName string         `json:"mapping_name"`
	SourceID    string         `json:"source_id"`
	StreamID    string         `json:"stream_id"`
	EventType   string         `json:"event_type"`
	TextContent string         `json:"text_content"`
	Metadata    map[string]any `json:"metadata"`
	Score       float64        `json:"score"`
}

// FetchEventMappings loads the list of enabled mappings from oosp.
func FetchEventMappings() ([]EventMapping, error) {
	if helper.OOSP == nil {
		return nil, fmt.Errorf("oosp not connected")
	}
	raw, err := helper.OOSP.Get("/event/mappings")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Mappings []EventMapping `json:"mappings"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("parse event mappings: %w", err)
	}
	return resp.Mappings, nil
}

// FetchEventStreams returns the distinct stream IDs for a mapping.
// An empty mapping name merges streams from all enabled mappings.
func FetchEventStreams(mapping string, limit int) ([]string, error) {
	if helper.OOSP == nil {
		return nil, fmt.Errorf("oosp not connected")
	}
	q := url.Values{}
	if mapping != "" {
		q.Set("mapping", mapping)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/event/streams"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}

	raw, err := helper.OOSP.Get(path)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Streams []string `json:"streams"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("parse event streams: %w", err)
	}
	return resp.Streams, nil
}

// EventSearchRequest is the payload for POST /event/search.
type EventSearchRequest struct {
	Mapping  string `json:"mapping"`
	Query    string `json:"query"`
	StreamID string `json:"stream_id,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// SearchEvents runs a vector search on oosp and returns the hits.
func SearchEvents(req EventSearchRequest) ([]EventHit, error) {
	if helper.OOSP == nil {
		return nil, fmt.Errorf("oosp not connected")
	}
	raw, err := helper.OOSP.Post("/event/search", req)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Results []EventHit `json:"results"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("parse event search: %w", err)
	}
	return resp.Results, nil
}
