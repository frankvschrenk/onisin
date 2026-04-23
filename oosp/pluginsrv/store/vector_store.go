package store

import "context"

type VectorPoint struct {
	ID         uint64
	Vector     []float32
	Payload    map[string]string
}

type VectorMatch struct {
	ID      uint64
	Score   float32
	Payload map[string]string
}

type VectorStore interface {
	EnsureCollection(ctx context.Context, collection string, dim uint64) error

	Upsert(ctx context.Context, collection string, point VectorPoint) error

	Search(ctx context.Context, collection string, vector []float32, filter map[string]string, n uint64) ([]VectorMatch, error)

	Close()
}
