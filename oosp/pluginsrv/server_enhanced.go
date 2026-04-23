package pluginsrv

import (
	"context"
	"log"

	_ "github.com/lib/pq"
	"onisin.com/oosp/pluginsrv/store"
)

// eventSystem encapsulates the generic event processing components
type eventSystem struct {
	mappingStore *store.EventMappingStore
	processor    *store.EventProcessor
	listener     *store.EventListener
}

// Extension variable for the event system (not redeclared)
var activeEventSystem *eventSystem

// initEventSystem initializes the generic event processing system
// This extends the base server.go functionality
func initEventSystem() {
	if activeEmbed == nil {
		log.Println("[oosp] event system skipped — no embed store configured")
		return
	}
	if cfg.DSN == "" {
		log.Println("[oosp] event system skipped — no DSN")
		return
	}

	// Initialize event mapping store
	mappingStore, err := store.NewEventMappingStore(cfg.DSN)
	if err != nil {
		log.Printf("[oosp] ⚠️  event mapping store: %v", err)
		return
	}

	// Initialize event processor
	processor := store.NewEventProcessor(mappingStore, activeEmbed, cfg.DSN)

	// Validate all configured mappings
	ctx := context.Background()
	if err := processor.ValidateAllMappings(ctx); err != nil {
		log.Printf("[oosp] ⚠️  event mapping validation: %v", err)
		return
	}

	// Initialize event listener
	listener := store.NewEventListener(cfg.DSN, processor, mappingStore)

	activeEventSystem = &eventSystem{
		mappingStore: mappingStore,
		processor:    processor,
		listener:     listener,
	}

	// Backfill any rows that were inserted while no listener was running.
	// Each source table with a `processed = false` column is scanned and
	// embedded. Runs in the background so REST stays responsive.
	go func() {
		if err := processor.ProcessUnprocessedEvents(context.Background()); err != nil {
			log.Printf("[oosp] ⚠️  event backfill: %v", err)
		}
	}()

	// Start event listener in background
	go func() {
		if err := listener.Start(context.Background()); err != nil {
			log.Printf("[oosp] ❌ event listener: %v", err)
		}
	}()

	log.Println("[oosp] ✅ event system ready")
}

// shutdownEventSystem gracefully shuts down the event system
func shutdownEventSystem() {
	if activeEventSystem != nil {
		activeEventSystem.listener.Stop()
		activeEventSystem.mappingStore.Close()
	}
}

// Note: openDB function removed - already exists in api.go
