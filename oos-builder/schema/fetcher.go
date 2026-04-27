// Package schema fetches the DSL grammar from oosp and turns it into
// an in-memory Element catalog the builder UI consumes.
//
// The grammar is the dsl.xsd document held in oos.oos_dsl_meta under
// the namespace "grammar". oosp serves it as application/xml from
// GET /dsl/meta?ns=grammar. Keeping the XSD as the single source of
// truth means the builder, the live renderer and the LLM retrieval
// pipeline all agree on what an element is and what attributes it
// accepts — no compiled-in copy that can drift.
package schema

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Fetcher pulls grammar/enrichment payloads from an oosp instance.
//
// One Fetcher is meant to live for the lifetime of a builder window
// (or longer). Concurrent calls are safe because the underlying
// http.Client is.
type Fetcher struct {
	baseURL string
	client  *http.Client
}

// NewFetcher returns a Fetcher rooted at baseURL (e.g. "http://localhost:9100").
// A trailing slash on baseURL is tolerated.
func NewFetcher(baseURL string) *Fetcher {
	return &Fetcher{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// FetchGrammar returns the dsl.xsd payload as raw XML bytes. A 404
// from oosp is surfaced as a typed error so callers can distinguish
// "not seeded" from network problems.
func (f *Fetcher) FetchGrammar(ctx context.Context) ([]byte, error) {
	return f.fetchMeta(ctx, "grammar")
}

// FetchEnrichment returns the enrichment XML payload as raw bytes.
// Optional from the builder's point of view — used today only for
// element tooltips.
func (f *Fetcher) FetchEnrichment(ctx context.Context) ([]byte, error) {
	return f.fetchMeta(ctx, "enrichment")
}

// ErrNotSeeded indicates oosp answered 404 — the row exists in code
// but has not been written to oos.oos_dsl_meta yet (typically because
// --seed-internal has not been run).
var ErrNotSeeded = fmt.Errorf("dsl meta namespace not seeded")

func (f *Fetcher) fetchMeta(ctx context.Context, namespace string) ([]byte, error) {
	u, err := url.Parse(f.baseURL)
	if err != nil {
		return nil, fmt.Errorf("base url: %w", err)
	}
	u.Path = "/dsl/meta"
	q := u.Query()
	q.Set("ns", namespace)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/xml")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", namespace, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrNotSeeded, namespace)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oosp returned %d for ns=%s", resp.StatusCode, namespace)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}
