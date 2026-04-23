package store


import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/lib/pq"
)

type PGVectorStore struct {
	db *sql.DB
}

func NewPGVectorStore(dsn string) (*PGVectorStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("pgvector: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("pgvector: ping: %w", err)
	}
	return &PGVectorStore{db: db}, nil
}

func (s *PGVectorStore) Close() {
	s.db.Close()
}

func (s *PGVectorStore) EnsureCollection(_ context.Context, _ string, _ uint64) error {
	return nil
}

func (s *PGVectorStore) Upsert(ctx context.Context, _ string, point VectorPoint) error {
	eventID := point.Payload["event_id"]
	if eventID == "" {
		eventID = fmt.Sprintf("%s-%d", point.Payload["stream"], point.ID)
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE oos.events SET embedding = $1 WHERE id = $2
	`, vectorToString(point.Vector), eventID)
	return err
}

func (s *PGVectorStore) Search(ctx context.Context, _ string, vector []float32, filter map[string]string, n uint64) ([]VectorMatch, error) {
	args := []any{vectorToString(vector)}
	where := "WHERE embedding IS NOT NULL"

	if streamID, ok := filter["stream"]; ok && streamID != "" {
		args = append(args, streamID)
		where += fmt.Sprintf(" AND stream_id = $%d", len(args))
	}

	query := fmt.Sprintf(`
		SELECT id, stream_id, event_type, data,
		       1 - (embedding <=> $1::vector) AS score
		FROM oos.events %s
		ORDER BY embedding <=> $1::vector
		LIMIT %d
	`, where, n)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("pgvector: search: %w", err)
	}
	defer rows.Close()

	var matches []VectorMatch
	var pos uint64
	for rows.Next() {
		var id, streamID, evType string
		var dataJSON []byte
		var score float32

		if err := rows.Scan(&id, &streamID, &evType, &dataJSON, &score); err != nil {
			return nil, err
		}
		pos++
		matches = append(matches, VectorMatch{
			ID:    pos,
			Score: score,
			Payload: map[string]string{
				"event_id":   id,
				"stream":     streamID,
				"event_type": evType,
				"text":       extractTextFromJSON(dataJSON),
			},
		})
	}
	return matches, rows.Err()
}

func vectorToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func extractTextFromJSON(data []byte) string {
	s := string(data)
	key := `"text":"`
	idx := strings.Index(s, key)
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(key):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}
