package helper

import (
	"fmt"
	"log"
	"strings"

	"github.com/go-resty/resty/v2"
	"onisin.com/oos-common/dsl"
)

type OOSPClient struct {
	resty *resty.Client
}

var OOSP *OOSPClient

func NewOOSP(baseURL string) *OOSPClient {
	if !strings.HasPrefix(baseURL, "http") {
		baseURL = "http://" + baseURL
	}
	r := resty.New().
		SetBaseURL(baseURL).
		SetHeader("Content-Type", "application/json")
	return &OOSPClient{resty: r}
}

// SetActiveGroup sets the X-OOS-Group header that oosp uses to resolve
// the caller's role.
//
// The client owns this decision because the client is where the JWT is
// decoded — the server has no independent way to know which group the
// user belongs to until we wire up full JWT validation on the server.
// Until then, this header is the single source of truth per request.
//
// Passing an empty string removes the header, which causes oosp to fall
// back to its default (currently oos-admin). That's a convenience for
// tests and bootstrap — never call this with "" in production flows.
func (c *OOSPClient) SetActiveGroup(group string) {
	if c == nil || c.resty == nil {
		return
	}
	if group == "" {
		c.resty.Header.Del("X-OOS-Group")
		return
	}
	c.resty.SetHeader("X-OOS-Group", group)
	log.Printf("[oosp] active group: %s", group)
}

func (c *OOSPClient) Post(path string, body any) (string, error) {
	resp, err := c.resty.R().SetBody(body).Post(path)
	if err != nil {
		return "", fmt.Errorf("oosp %s: %w", path, err)
	}
	if resp.IsError() {
		return "", fmt.Errorf("oosp %s: %s", path, resp.String())
	}
	return resp.String(), nil
}

func (c *OOSPClient) Get(path string) (string, error) {
	resp, err := c.resty.R().Get(path)
	if err != nil {
		return "", fmt.Errorf("oosp %s: %w", path, err)
	}
	if resp.IsError() {
		return "", fmt.Errorf("oosp %s: %s", path, resp.String())
	}
	return resp.String(), nil
}

func (c *OOSPClient) Call(tool string, args map[string]string) (string, error) {
	switch tool {
	case "oosp_ast":
		return c.Get("/ast")
	case "oosp_query":
		return c.Post("/query", args)
	case "oosp_save":
		return c.Post("/save", args)
	case "oosp_mutation":
		return c.Post("/mutation", args)
	case "oosp_embed":
		return c.Post("/embed", map[string]string{"text": args["text"]})
	case "oosp_vector_search":
		return c.Post("/vector/search", args)
	case "oosp_vector_upsert":
		return c.Post("/vector/upsert", args)
	case "oosp_stream_append":
		return c.Post("/stream/append", args)
	case "oosp_stream_read":
		return c.Get("/stream/" + args["stream"])
	case "oosp_dsl":
		return c.Post("/dsl", map[string]string{
			"id":      args["id"],
			"content": args["content"],
		})
	case "oosp_load_theme":
		raw, err := c.Get("/theme")
		if err != nil {
			return "", err
		}
		var result struct {
			XML string `json:"xml"`
		}
		if err := parseJSON(raw, &result); err != nil {
			return raw, nil // fallback: raw zurückgeben
		}
		return result.XML, nil
	case "oosp_ai_schema":
		if id := args["id"]; id != "" {
			return c.Get("/ai-schema/" + id)
		}
		return c.Get("/ai-schema")
	case "oosp_schema_search":
		return c.Post("/schema/search", args)
	default:
		return "", fmt.Errorf("unbekanntes oosp tool: %s", tool)
	}
}

func (c *OOSPClient) FetchAST() (*dsl.OOSAst, string, error) {
	raw, err := c.Get("/ast")
	if err != nil {
		return nil, "", err
	}

	var result struct {
		AST  dsl.OOSAst `json:"ast"`
		Role string     `json:"role"`
	}
	if err := parseJSON(raw, &result); err != nil {
		return nil, "", fmt.Errorf("ast parse: %w", err)
	}
	return &result.AST, result.Role, nil
}

func (c *OOSPClient) IsAlive() bool {
	resp, err := c.resty.R().Get("/health")
	return err == nil && resp.IsSuccess()
}

func ConnectOOSP(baseURL string) bool {
	client := NewOOSP(baseURL)
	if client.IsAlive() {
		OOSP = client
		log.Printf("[oosp] ✅ REST → %s", baseURL)
		return true
	}
	log.Printf("[oosp] ⚠️  nicht erreichbar: %s", baseURL)
	return false
}
