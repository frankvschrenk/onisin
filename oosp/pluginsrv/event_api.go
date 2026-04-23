package pluginsrv

// event_api.go — API endpoints for the generic event system

import (
	"context"
	"fmt"
	
	"onisin.com/oosp/pluginsrv/store"  // MISSING IMPORT FIXED!
)

// EventSearch performs vector search across a specific event mapping
func EventSearch(mappingName, query, streamID string, limit int) (any, error) {
	if activeEventSystem == nil {
		return nil, fmt.Errorf("event system not initialized")
	}
	if activeEmbed == nil {
		return nil, fmt.Errorf("embed store not available")
	}
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}
	
	ctx := context.Background()
	
	// Find mapping by name
	mappings, err := activeEventSystem.mappingStore.LoadEnabledMappings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load mappings: %w", err)
	}
	
	var targetMapping *store.EventMapping
	for _, mapping := range mappings {
		if mapping.Name == mappingName {
			targetMapping = &mapping
			break
		}
	}
	
	if targetMapping == nil {
		return nil, fmt.Errorf("mapping %s not found", mappingName)
	}
	
	// Generate query embedding
	queryVector, err := activeEmbed.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	
	// Set default limit
	if limit <= 0 {
		limit = 10
	}
	
	// Perform vector search
	results, err := activeEventSystem.mappingStore.SearchVectors(ctx, *targetMapping, queryVector, streamID, limit)
	if err != nil {
		return nil, fmt.Errorf("search vectors: %w", err)
	}
	
	return map[string]any{
		"mapping":     mappingName,
		"query":       query,
		"stream_id":   streamID,
		"limit":       limit,
		"results":     results,
		"result_count": len(results),
	}, nil
}

// EventStreams returns the distinct stream IDs present in the target vector
// table of the given mapping. Used by the chat UI so the user can pick an
// existing case file without having to memorise IDs.
//
// When mappingName is empty, streams from all enabled mappings are merged.
func EventStreams(mappingName string, limit int) (any, error) {
	if activeEventSystem == nil {
		return nil, fmt.Errorf("event system not initialized")
	}

	ctx := context.Background()
	mappings, err := activeEventSystem.mappingStore.LoadEnabledMappings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load mappings: %w", err)
	}

	// Collect streams from the requested mapping (or all of them).
	seen := map[string]bool{}
	var streams []string
	for _, mapping := range mappings {
		if mappingName != "" && mapping.Name != mappingName {
			continue
		}
		ids, err := activeEventSystem.mappingStore.GetDistinctStreamIDs(ctx, mapping, limit)
		if err != nil {
			// Skip mappings whose target table is not ready yet — the UI
			// should not fail just because one mapping is mis-configured.
			continue
		}
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				streams = append(streams, id)
			}
		}
	}

	return map[string]any{
		"mapping": mappingName,
		"streams": streams,
		"count":   len(streams),
	}, nil
}

// EventMappings returns all configured event mappings
func EventMappings() (any, error) {
	if activeEventSystem == nil {
		return map[string]any{
			"mappings": []any{},
			"message":  "event system not initialized",
		}, nil
	}
	
	ctx := context.Background()
	mappings, err := activeEventSystem.mappingStore.LoadEnabledMappings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load mappings: %w", err)
	}
	
	// Get active listener channels for status
	activeChannels := activeEventSystem.listener.GetActiveChannels()
	channelStatus := make(map[string]bool)
	for _, channel := range activeChannels {
		channelStatus[channel] = true
	}
	
	// Enrich mappings with listener status
	enrichedMappings := make([]map[string]any, len(mappings))
	for i, mapping := range mappings {
		enrichedMappings[i] = map[string]any{
			"id":                 mapping.ID,
			"name":               mapping.Name,
			"source_schema":      mapping.SourceSchema,
			"source_table":       mapping.SourceTable,
			"source_text_field":  mapping.SourceTextField,
			"source_id_field":    mapping.SourceIDField,
			"notify_channel":     mapping.NotifyChannel,
			"target_schema":      mapping.TargetSchema,
			"target_table":       mapping.TargetTable,
			"enabled":            mapping.Enabled,
			"listener_active":    channelStatus[mapping.NotifyChannel],
		}
	}
	
	return map[string]any{
		"mappings":        enrichedMappings,
		"active_channels": activeChannels,
		"total_mappings":  len(mappings),
	}, nil
}
