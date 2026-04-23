package store

// new_stores.go — factory for the vector store backend.
//
// Backend selection:
//   OOSP_VECTOR_BACKEND = pg   (default — pgvector)

import "fmt"

// NewVectorStore creates a VectorStore for the selected backend.
func NewVectorStore(backend, addr string) (VectorStore, error) {
	switch backend {
	case "pg", "pgvector", "":
		if addr == "" {
			return nil, fmt.Errorf("vector store: OOSP_DSN missing")
		}
		return NewPGVectorStore(addr)
	default:
		return nil, fmt.Errorf("unknown vector backend: %q (supported: pg)", backend)
	}
}
