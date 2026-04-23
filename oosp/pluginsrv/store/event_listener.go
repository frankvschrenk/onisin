package store

// event_listener.go — Generic PostgreSQL LISTEN/NOTIFY für Event-basierte Embeddings
//
// Ersetzt den hardcoded pg.Listener aus oosai mit einem generischen System:
//   - Auto-Discovery aller Event-Mappings beim Start
//   - Dynamic LISTEN auf alle notify_channels  
//   - Generic Event Processing Pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/lib/pq"
	_ "github.com/lib/pq"
)

// EventListener handles PostgreSQL NOTIFY events for generic event processing
type EventListener struct {
	dsn             string
	processor       *EventProcessor
	mappingStore    *EventMappingStore
	listener        *pq.Listener
	channels        []string
}

// NewEventListener creates a new generic event listener
func NewEventListener(dsn string, processor *EventProcessor, mappingStore *EventMappingStore) *EventListener {
	return &EventListener{
		dsn:          dsn,
		processor:    processor,
		mappingStore: mappingStore,
	}
}

// Start begins listening for event notifications
func (l *EventListener) Start(ctx context.Context) error {
	// Discover all notify channels from mappings
	channels, err := l.mappingStore.GetNotifyChannels(ctx)
	if err != nil {
		return fmt.Errorf("get notify channels: %w", err)
	}
	
	if len(channels) == 0 {
		log.Println("[event-listener] ⚠️  no event mappings configured, listener idle")
		// Still block on context to keep server running
		<-ctx.Done()
		return nil
	}
	
	l.channels = channels
	
	// Setup PostgreSQL listener with error callback
	reportErr := func(ev pq.ListenerEventType, err error) {
		if err != nil {
			log.Printf("[event-listener] connection error: %v", err)
		}
	}
	
	l.listener = pq.NewListener(l.dsn, 5*time.Second, time.Minute, reportErr)
	
	// Listen on all discovered channels
	for _, channel := range l.channels {
		if err := l.listener.Listen(channel); err != nil {
			l.listener.Close()
			return fmt.Errorf("listen on %s: %w", channel, err)
		}
		log.Printf("[event-listener] ✅ listening on %s", channel)
	}
	
	// Process any unprocessed events on startup
	if err := l.processor.ProcessUnprocessedEvents(ctx); err != nil {
		log.Printf("[event-listener] ⚠️  backfill: %v", err)
	}
	
	// Main event loop
	return l.eventLoop(ctx)
}

// eventLoop runs the main NOTIFY processing loop
func (l *EventListener) eventLoop(ctx context.Context) error {
	defer l.listener.Close()
	
	for {
		select {
		case <-ctx.Done():
			log.Println("[event-listener] shutting down")
			return nil
			
		case notification := <-l.listener.Notify:
			if notification == nil {
				continue // Connection reconnect event
			}
			
			if err := l.handleNotification(ctx, notification); err != nil {
				log.Printf("[event-listener] ❌ %s: %v", notification.Channel, err)
			}
			
		case <-time.After(90 * time.Second):
			// Periodic ping to keep connection alive
			if err := l.listener.Ping(); err != nil {
				log.Printf("[event-listener] ping error: %v", err)
			}
		}
	}
}

// handleNotification processes a single NOTIFY message
func (l *EventListener) handleNotification(ctx context.Context, n *pq.Notification) error {
	if DebugStore {
		log.Printf("[event-listener] received %s: %s", n.Channel, truncatePayload(n.Extra))
	}
	
	// Validate payload is JSON
	if !json.Valid([]byte(n.Extra)) {
		return fmt.Errorf("invalid JSON payload")
	}
	
	// Process the event
	return l.processor.ProcessEventNotification(ctx, n.Channel, n.Extra)
}

// RefreshChannels reloads event mappings and updates listened channels
func (l *EventListener) RefreshChannels(ctx context.Context) error {
	newChannels, err := l.mappingStore.GetNotifyChannels(ctx)
	if err != nil {
		return fmt.Errorf("refresh channels: %w", err)
	}
	
	// Find channels to add/remove
	toAdd := difference(newChannels, l.channels)
	toRemove := difference(l.channels, newChannels)
	
	// Add new channels
	for _, channel := range toAdd {
		if err := l.listener.Listen(channel); err != nil {
			log.Printf("[event-listener] ⚠️  failed to add channel %s: %v", channel, err)
			continue
		}
		log.Printf("[event-listener] ✅ added channel %s", channel)
	}
	
	// Remove old channels
	for _, channel := range toRemove {
		if err := l.listener.Unlisten(channel); err != nil {
			log.Printf("[event-listener] ⚠️  failed to remove channel %s: %v", channel, err)
			continue
		}
		log.Printf("[event-listener] ✅ removed channel %s", channel)
	}
	
	l.channels = newChannels
	return nil
}

// GetActiveChannels returns currently listened channels
func (l *EventListener) GetActiveChannels() []string {
	return append([]string(nil), l.channels...)
}

// Stop gracefully stops the listener
func (l *EventListener) Stop() {
	if l.listener != nil {
		l.listener.Close()
		l.listener = nil
	}
}

// difference returns elements in a that are not in b
func difference(a, b []string) []string {
	mb := make(map[string]bool, len(b))
	for _, x := range b {
		mb[x] = true
	}
	
	var diff []string
	for _, x := range a {
		if !mb[x] {
			diff = append(diff, x)
		}
	}
	return diff
}

// truncatePayload truncates long payloads for logging
func truncatePayload(payload string) string {
	if len(payload) <= 200 {
		return payload
	}
	return payload[:200] + "..."
}
