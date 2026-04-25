package tools

// dsl_schema.go — DSL element retrieval tool against oosp.
//
// Wraps POST /dsl/schema/search behind the eino BaseTool contract so
// any chat Mode can hand it to the LLM. Each call returns the top n
// element chunks ranked by cosine similarity over
// granite-embedding(query). The chunks include German aliases,
// intent, attributes, valid children, an example and AI hints —
// enough material for the LLM to emit the matching DSL fragment.
//
// The tool is transport-agnostic at construction time: callers pass
// the oosp base URL and an optional X-OOS-Group header. We make our
// own *http.Client rather than depending on oos's helper package, so
// ooso (which has no helper.OOSP) can use the tool directly.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// DSLSchemaSearchConfig wires a DSLSchemaSearch tool to a specific
// oosp endpoint. Group is used as the X-OOS-Group header so server-
// side permission checks can scope the answer; empty falls back to
// the server default.
type DSLSchemaSearchConfig struct {
	BaseURL string        // e.g. http://localhost:9100
	Group   string        // optional X-OOS-Group header value
	Timeout time.Duration // optional, defaults to 30s
}

// DSLChunk is the response shape of POST /dsl/schema/search. Fields
// match oos.oos_dsl_schema columns minus the embedding.
type DSLChunk struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Chunk string `json:"chunk"`
}

// NewDSLSchemaSearch returns an eino BaseTool the LLM can call to
// retrieve DSL element grounding by natural-language layout intent.
//
// Description is German because the queries the model crafts come
// from German user input. The LLM is encouraged to call this once
// per layout concept (one search for "two fields side by side",
// another for "tabs", another for "dropdown") and then assemble the
// final <screen> XML from the retrieved fragments.
func NewDSLSchemaSearch(cfg DSLSchemaSearchConfig) tool.BaseTool {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	client := &http.Client{Timeout: cfg.Timeout}

	return &dslSchemaTool{
		cfg:    cfg,
		client: client,
	}
}

type dslSchemaTool struct {
	cfg    DSLSchemaSearchConfig
	client *http.Client
}

func (t *dslSchemaTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "dsl_schema_search",
		Desc: "Sucht DSL-Element-Grammatik anhand einer natürlichsprachigen Layout-Absicht. " +
			"Gibt einen oder mehrere Element-Chunks zurück (jeweils mit XML-Tag, deutschen " +
			"Aliasen, Intent, Attribut-Liste mit erlaubten Werten, gültigen Kindern, " +
			"copy-paste-Beispiel und AI-Hints). " +
			"Diese Funktion einmal pro Layout-Konzept aufrufen, das im Auftrag des Benutzers " +
			"vorkommt – z.B. einmal für 'zwei Felder nebeneinander', einmal für 'Reiter', " +
			"einmal für 'Dropdown'. Anschließend das vollständige <screen>-XML aus den " +
			"abgerufenen Fragmenten zusammensetzen.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "Layout-Absicht in natürlicher Sprache, z.B. 'zwei Felder nebeneinander', 'Reiter mit verschiedenen Seiten', 'Datentabelle mit Spalten'",
				Required: true,
			},
			"n": {
				Type: schema.String,
				Desc: "Anzahl der zurückgegebenen Chunks (Default 3).",
			},
		}),
	}, nil
}

func (t *dslSchemaTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	var params struct {
		Query string `json:"query"`
		N     any    `json:"n"`
	}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", fmt.Errorf("dsl_schema_search: invalid args: %w", err)
	}
	if params.Query == "" {
		return "", fmt.Errorf("dsl_schema_search: query required")
	}
	n := 3
	switch v := params.N.(type) {
	case float64:
		n = int(v)
	case string:
		if v != "" {
			fmt.Sscanf(v, "%d", &n) //nolint:errcheck
		}
	}
	if n <= 0 {
		n = 3
	}

	reqBody, _ := json.Marshal(map[string]any{"query": params.Query, "n": n})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.cfg.BaseURL+"/dsl/schema/search", bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("dsl_schema_search: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if t.cfg.Group != "" {
		req.Header.Set("X-OOS-Group", t.cfg.Group)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("dsl_schema_search: oosp call: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("dsl_schema_search: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("dsl_schema_search: oosp status %d: %s", resp.StatusCode, string(body))
	}

	var chunks []DSLChunk
	if err := json.Unmarshal(body, &chunks); err != nil {
		// Surface the raw response — it may still contain useful
		// information for the model to act on.
		return string(body), nil
	}
	if len(chunks) == 0 {
		return "(keine DSL-Element-Chunks gefunden)", nil
	}

	var sb strings.Builder
	for i, c := range chunks {
		if i > 0 {
			sb.WriteString("\n---\n")
		}
		sb.WriteString(c.Chunk)
	}
	return sb.String(), nil
}
