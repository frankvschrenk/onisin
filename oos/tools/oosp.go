package tools

// oosp.go — OOSP proxy calls used by the AI tool handlers.
//
// Each function maps directly to an oosp_* server-side tool.
// Results are returned as raw strings for the caller to interpret.

import (
	"fmt"

	"onisin.com/oos/helper"
)

// oospCall invokes a named OOSP tool with the given string arguments.
// Returns an error when OOSP is not connected.
func oospCall(tool string, args map[string]string) (string, error) {
	if helper.OOSP == nil {
		return "", fmt.Errorf("OOSP not connected")
	}
	return helper.OOSP.Call(tool, args)
}

// OOSPQuery executes a GraphQL query via OOSP and returns the raw JSON response.
func OOSPQuery(contextName, query string) (string, error) {
	return oospCall("oosp_query", map[string]string{
		"context": contextName,
		"query":   query,
	})
}

// OOSPSave persists data for the given context via OOSP.
func OOSPSave(contextName, dataJSON string) (string, error) {
	return oospCall("oosp_save", map[string]string{
		"context": contextName,
		"data":    dataJSON,
	})
}

// OOSPMutation executes a GraphQL mutation via OOSP.
func OOSPMutation(mutation string) (string, error) {
	return oospCall("oosp_mutation", map[string]string{
		"mutation": mutation,
	})
}

// OOSPEmbed generates an embedding vector for the given text via OOSP.
func OOSPEmbed(text string) (string, error) {
	return oospCall("oosp_embed", map[string]string{
		"text": text,
	})
}

// OOSPVectorUpsert writes a vector point to the vector store via OOSP.
func OOSPVectorUpsert(collection, id, vector, payload string) (string, error) {
	return oospCall("oosp_vector_upsert", map[string]string{
		"collection": collection,
		"id":         id,
		"vector":     vector,
		"payload":    payload,
	})
}

// OOSPVectorSearch performs a similarity search via OOSP.
func OOSPVectorSearch(collection, vector, filter, n string) (string, error) {
	return oospCall("oosp_vector_search", map[string]string{
		"collection": collection,
		"vector":     vector,
		"filter":     filter,
		"n":          n,
	})
}

// OOSPStreamAppend appends an event to the event stream via OOSP.
func OOSPStreamAppend(stream, eventType, data string) (string, error) {
	return oospCall("oosp_stream_append", map[string]string{
		"stream":     stream,
		"event_type": eventType,
		"data":       data,
	})
}

// OOSPStreamRead reads events from a stream starting at the given position via OOSP.
func OOSPStreamRead(stream, fromPosition string) (string, error) {
	return oospCall("oosp_stream_read", map[string]string{
		"stream":        stream,
		"from_position": fromPosition,
	})
}

// OOSPDsl fetches a rendered DSL screen envelope from OOSP.
func OOSPDsl(id, content string) (string, error) {
	return oospCall("oosp_dsl", map[string]string{
		"id":      id,
		"content": content,
	})
}

// OOSPAISchema fetches AI reference content from oosp.ai.
func OOSPAISchema(id string) (string, error) {
	return oospCall("oosp_ai_schema", map[string]string{
		"id": id,
	})
}
