package cluster

import "context"

type Store interface {
	Get(ctx context.Context, key string) (string, error)

	Set(ctx context.Context, key, value string, ttlSeconds int64) error

	Delete(ctx context.Context, key string) error

	Watch(ctx context.Context, prefix string) (<-chan Event, error)

	List(ctx context.Context, prefix string) ([]KeyValue, error)

	Lock(ctx context.Context, resource string, ttlSeconds int64) (LockHandle, error)

	Close() error
}

type Event struct {
	Type  EventType
	Key   string
	Value string 
}

type EventType string

const (
	EventPut    EventType = "PUT"
	EventDelete EventType = "DELETE"
)

type KeyValue struct {
	Key   string
	Value string
}

type LockHandle interface {
	Unlock(ctx context.Context) error
}

const (
	PrefixPresence = "presence/"
	PrefixCalls    = "calls/"
	PrefixShared   = "shared/"
	PrefixLocks    = "locks/"
)

type ErrNotFound struct {
	Key string
}

func (e *ErrNotFound) Error() string {
	return "cluster: key not found: " + e.Key
}

func IsNotFound(err error) bool {
	_, ok := err.(*ErrNotFound)
	return ok
}
