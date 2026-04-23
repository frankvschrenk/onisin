package cluster

import (
	"context"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu       sync.RWMutex
	data     map[string]memEntry
	watchers []memWatcher
	locks    map[string]bool
}

type memEntry struct {
	value   string
	expires time.Time 
}

type memWatcher struct {
	prefix string
	ch     chan Event
}

func NewMemoryStore() *MemoryStore {
	s := &MemoryStore{
		data:  make(map[string]memEntry),
		locks: make(map[string]bool),
	}
	go s.expireLoop()
	return s
}

func (s *MemoryStore) Get(_ context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.data[key]
	if !ok {
		return "", &ErrNotFound{Key: key}
	}
	if !entry.expires.IsZero() && time.Now().After(entry.expires) {
		return "", &ErrNotFound{Key: key}
	}
	return entry.value, nil
}

func (s *MemoryStore) Set(_ context.Context, key, value string, ttlSeconds int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := memEntry{value: value}
	if ttlSeconds > 0 {
		entry.expires = time.Now().Add(time.Duration(ttlSeconds) * time.Second)
	}
	s.data[key] = entry
	s.notify(Event{Type: EventPut, Key: key, Value: value})
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
	s.notify(Event{Type: EventDelete, Key: key})
	return nil
}

func (s *MemoryStore) Watch(ctx context.Context, prefix string) (<-chan Event, error) {
	ch := make(chan Event, 64)

	s.mu.Lock()
	s.watchers = append(s.watchers, memWatcher{prefix: prefix, ch: ch})
	s.mu.Unlock()

	// Watcher entfernen wenn ctx abgebrochen
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, w := range s.watchers {
			if w.ch == ch {
				s.watchers = append(s.watchers[:i], s.watchers[i+1:]...)
				close(ch)
				return
			}
		}
	}()

	return ch, nil
}

func (s *MemoryStore) List(_ context.Context, prefix string) ([]KeyValue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []KeyValue
	for k, v := range s.data {
		if strings.HasPrefix(k, prefix) {
			if v.expires.IsZero() || time.Now().Before(v.expires) {
				result = append(result, KeyValue{Key: k, Value: v.value})
			}
		}
	}
	return result, nil
}

func (s *MemoryStore) Lock(_ context.Context, resource string, _ int64) (LockHandle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.locks[resource] {
		return nil, &ErrNotFound{Key: "lock bereits vergeben: " + resource}
	}
	s.locks[resource] = true
	return &memLock{store: s, resource: resource}, nil
}

func (s *MemoryStore) Close() error {
	return nil
}

// notify sendet ein Event an alle passenden Watcher (muss unter Lock aufgerufen werden).
func (s *MemoryStore) notify(ev Event) {
	for _, w := range s.watchers {
		if strings.HasPrefix(ev.Key, w.prefix) {
			select {
			case w.ch <- ev:
			default: // Watcher zu langsam — überspringen
			}
		}
	}
}

// expireLoop räumt abgelaufene Keys periodisch auf.
func (s *MemoryStore) expireLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for k, v := range s.data {
			if !v.expires.IsZero() && now.After(v.expires) {
				delete(s.data, k)
				s.notify(Event{Type: EventDelete, Key: k})
			}
		}
		s.mu.Unlock()
	}
}

type memLock struct {
	store    *MemoryStore
	resource string
}

func (l *memLock) Unlock(_ context.Context) error {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	delete(l.store.locks, l.resource)
	return nil
}
