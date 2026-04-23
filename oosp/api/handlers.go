package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"onisin.com/oos-common/dsl"
)

// Event System Handlers

// eventSearch performs vector search across a specific event mapping
func (h *handler) eventSearch(c echo.Context) error {
	var req struct {
		Mapping  string `json:"mapping"`
		Query    string `json:"query"`
		StreamID string `json:"stream_id"`
		Limit    int    `json:"limit"`
	}

	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid request")
	}

	if req.Mapping == "" {
		return errJSON(c, http.StatusBadRequest, "mapping required")
	}
	if req.Query == "" {
		return errJSON(c, http.StatusBadRequest, "query required")
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}

	if h.svc.EventSearch == nil {
		return errJSON(c, http.StatusServiceUnavailable, "event search not available")
	}

	results, err := h.svc.EventSearch(req.Mapping, req.Query, req.StreamID, req.Limit)
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, results)
}

// eventStreams returns the distinct stream IDs present in the given mapping.
// Used by the chat UI so the user can pick an existing case file.
func (h *handler) eventStreams(c echo.Context) error {
	mapping := c.QueryParam("mapping")
	limit := 0 // 0 means: no limit
	if raw := c.QueryParam("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}

	if h.svc.EventStreams == nil {
		return errJSON(c, http.StatusServiceUnavailable, "event streams not available")
	}

	result, err := h.svc.EventStreams(mapping, limit)
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

// eventMappings returns all configured event mappings
func (h *handler) eventMappings(c echo.Context) error {
	if h.svc.EventMappings == nil {
		return errJSON(c, http.StatusServiceUnavailable, "event mappings not available")
	}

	mappings, err := h.svc.EventMappings()
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, mappings)
}

// Existing handlers continue below...

func (h *handler) health(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "healthy"})
}

func (h *handler) ast(c echo.Context) error {
	groups := groupsFromCtx(c)
	if h.svc.GetAST == nil {
		return errJSON(c, http.StatusServiceUnavailable, "store not available")
	}
	ast, raw, found := h.svc.GetAST(groups)
	if !found {
		return errJSON(c, http.StatusNotFound, "no AST for groups")
	}
	return c.JSON(http.StatusOK, map[string]any{"ast": ast, "raw": raw})
}

func (h *handler) query(c echo.Context) error {
	var req struct {
		Query string `json:"query"`
	}
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid request")
	}
	if h.svc.ExecuteQuery == nil {
		return errJSON(c, http.StatusServiceUnavailable, "store not available")
	}
	result, err := h.svc.ExecuteQuery(req.Query)
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

// save accepts an oos_save call and persists the payload via the service layer.
//
// Wire format: the oos client's generic RPC speaks map[string]string, so the
// save payload arrives as a JSON-encoded string under `data`, not as a nested
// object. We accept both shapes — a JSON string (the client's canonical form)
// and a direct map (for future callers that speak structured JSON directly) —
// and normalise to a map before handing off. Same pattern as the /dsl handler.
//
// Permissions: save is always a "write" action against the named context.
// The permission check happens before the payload is passed to the
// executor, so a role without write permission never reaches the
// resolver layer. The check is authoritative — the client may or may
// not have pre-filtered, but the server cannot trust that.
func (h *handler) save(c echo.Context) error {
	var req struct {
		Context string          `json:"context"`
		Data    json.RawMessage `json:"data"`
	}
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid request")
	}
	if h.svc.ExecuteSave == nil {
		return errJSON(c, http.StatusServiceUnavailable, "store not available")
	}
	if req.Context == "" {
		return errJSON(c, http.StatusBadRequest, "context required")
	}

	if err := h.assertActionAllowed(c, req.Context, dsl.ActionWrite); err != nil {
		return err
	}

	data, err := decodeSaveData(req.Data)
	if err != nil {
		return errJSON(c, http.StatusBadRequest, err.Error())
	}
	if len(data) == 0 {
		return errJSON(c, http.StatusBadRequest, "data required")
	}

	result, err := h.svc.ExecuteSave(req.Context, data)
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

// decodeSaveData turns the `data` field of a /save request into a map.
//
// Two input shapes are accepted:
//   - a JSON string carrying an object: "{\"firstname\":\"Anna\"}"
//   - a JSON object directly:           {"firstname":"Anna"}
//
// Anything else is a client bug and surfaces as a 400.
func decodeSaveData(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	// Case 1: the value is a JSON string — unquote it first, then parse
	// the contained JSON object.
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if asString == "" {
			return nil, nil
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(asString), &m); err != nil {
			return nil, errInvalidSaveData(err)
		}
		return m, nil
	}

	// Case 2: the value is a JSON object — parse directly.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, errInvalidSaveData(err)
	}
	return m, nil
}

func errInvalidSaveData(cause error) error {
	return echo.NewHTTPError(http.StatusBadRequest,
		"data is not a JSON object: "+cause.Error())
}

// mutation executes a raw GraphQL mutation string against the active schema.
//
// The mutation is sent by the client's delete / refresh / future write paths
// as `{"mutation": "mutation { delete_person_detail(id: 2) }"}`. We parse
// out the action and context from the first operation and gate on the
// caller's role before forwarding to the executor. Mutations that address
// a context the role is not allowed to touch are rejected with 403 — the
// resolver never runs.
//
// Single-operation limitation: we only inspect the first operation in
// the mutation string. Today the client always sends one; if that ever
// changes (batched deletes etc.) this parser needs to be extended.
func (h *handler) mutation(c echo.Context) error {
	var req struct {
		Mutation string `json:"mutation"`
	}
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid request")
	}
	if req.Mutation == "" {
		return errJSON(c, http.StatusBadRequest, "mutation required")
	}
	if h.svc.ExecuteMutation == nil {
		return errJSON(c, http.StatusServiceUnavailable, "mutations not available")
	}

	action, contextName, ok := parseMutationActionAndContext(req.Mutation)
	if !ok {
		// Could not recognise the shape. Refuse: accepting unparseable
		// mutations would bypass the permission gate entirely.
		return errJSON(c, http.StatusBadRequest,
			"mutation does not match the expected insert_/update_/delete_<context> form")
	}
	if err := h.assertActionAllowed(c, contextName, action); err != nil {
		return err
	}

	result, err := h.svc.ExecuteMutation(req.Mutation)
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

// mutationFieldPattern matches the first field call inside a mutation
// document: it captures the operation prefix (insert / update / delete)
// and the context name that follows.
//
// Examples it recognises:
//
//	mutation { delete_person_detail(id: 2) }           -> delete, person_detail
//	mutation { update_note_detail(id: 5, title: "x") } -> update, note_detail
//	mutation { insert_person_detail(firstname: "A") }  -> insert, person_detail
//
// The regex is anchored loosely on purpose: whitespace, newlines and
// the optional "mutation" keyword are all tolerated. A fragment like
// "mutation Name { delete_foo(...) }" also matches because we look for
// the field call, not the document header.
var mutationFieldPattern = regexp.MustCompile(
	`(?is)\b(insert|update|delete)_([a-z][a-z0-9_]*)\s*\(`,
)

// parseMutationActionAndContext extracts the action and context name from
// a GraphQL mutation string. Returns ok=false when no recognised field
// call is found — the caller should then refuse the request rather than
// execute an unclassifiable mutation.
//
// The action is mapped to the permission vocabulary: delete -> delete,
// insert/update -> write. That is the granularity our permission matrix
// speaks; GraphQL's three CRUD operations collapse to two permission
// actions on purpose, because "insert" and "update" are the same power
// from a security point of view.
func parseMutationActionAndContext(stmt string) (dsl.Action, string, bool) {
	m := mutationFieldPattern.FindStringSubmatch(stmt)
	if len(m) != 3 {
		return "", "", false
	}
	op := strings.ToLower(m[1])
	ctxName := m[2]

	switch op {
	case "delete":
		return dsl.ActionDelete, ctxName, true
	case "insert", "update":
		return dsl.ActionWrite, ctxName, true
	default:
		return "", "", false
	}
}

// assertActionAllowed resolves the caller's role from the X-OOS-Group
// header, loads the AST for that role, and checks whether the role is
// allowed to perform the given action on contextName. Returns nil when
// permitted; otherwise it returns an echo.HTTPError so Echo's framework
// aborts the handler chain and writes exactly one response.
//
// On missing AST or unknown context we fail closed — the request is
// refused. The logic behind that: a client that asks for a context the
// server has no record of is either out of date or malicious, and in
// either case we should not execute the write.
//
// Why HTTPError and not errJSON: errJSON writes the response body via
// c.JSON and returns nil. A nil return is "all good" to Echo, so the
// handler would continue running past the gate and write a second
// response — a 403 followed by a 200 success. Returning an HTTPError
// signals Echo to stop and emit the error response itself, which is
// the only path that leaves exactly one response on the wire.
func (h *handler) assertActionAllowed(c echo.Context, contextName string, action dsl.Action) error {
	if h.svc.GetAST == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "store not available")
	}

	groups := groupsFromCtx(c)
	ast, role, ok := h.svc.GetAST(groups)
	if !ok || ast == nil {
		return echo.NewHTTPError(http.StatusForbidden,
			fmt.Sprintf("no AST for groups %v", groups))
	}

	for i := range ast.Contexts {
		ctx := &ast.Contexts[i]
		if ctx.Name != contextName {
			continue
		}
		if ctx.IsAllowed(role, action) {
			return nil
		}
		return echo.NewHTTPError(http.StatusForbidden,
			fmt.Sprintf("role %q is not allowed to %s on %q", role, action, contextName))
	}

	return echo.NewHTTPError(http.StatusNotFound,
		fmt.Sprintf("context %q not found", contextName))
}

func (h *handler) theme(c echo.Context) error {
	if h.svc.GetTheme == nil {
		return errJSON(c, http.StatusServiceUnavailable, "theme not available")
	}
	theme, err := h.svc.GetTheme()
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}
	// Response key is "xml" to match the other ctx endpoints; the client
	// parses this generically rather than keying on the ctx id.
	return c.JSON(http.StatusOK, map[string]string{"xml": theme})
}

func (h *handler) dsl(c echo.Context) error {
	// The oos client sends `content` as a JSON-encoded string because its
	// generic RPC layer only speaks map[string]string. We accept that shape
	// here and unmarshal into the map the service layer expects.
	var req struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid request")
	}
	if h.svc.GetEnvelope == nil {
		return errJSON(c, http.StatusServiceUnavailable, "envelope not available")
	}

	var content map[string]interface{}
	if req.Content != "" {
		if err := json.Unmarshal([]byte(req.Content), &content); err != nil {
			return errJSON(c, http.StatusBadRequest,
				"content is not valid JSON: "+err.Error())
		}
	}

	result, err := h.svc.GetEnvelope(req.ID, content)
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

func (h *handler) embed(c echo.Context) error {
	var req struct {
		Text string `json:"text"`
	}
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid request")
	}
	if h.svc.Embed == nil {
		return errJSON(c, http.StatusServiceUnavailable, "embedding not available")
	}
	result, err := h.svc.Embed(req.Text)
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"embedding": result})
}

func (h *handler) vectorUpsert(c echo.Context) error {
	var req struct {
		Collection string            `json:"collection"`
		ID         uint64            `json:"id"`
		Vector     []float32         `json:"vector"`
		Payload    map[string]string `json:"payload"`
	}
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid request")
	}
	if h.svc.VectorUpsert == nil {
		return errJSON(c, http.StatusServiceUnavailable, "vector not available")
	}
	err := h.svc.VectorUpsert(req.Collection, req.ID, req.Vector, req.Payload)
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handler) vectorSearch(c echo.Context) error {
	var req struct {
		Collection string            `json:"collection"`
		Vector     []float32         `json:"vector"`
		Filter     map[string]string `json:"filter"`
		N          uint64            `json:"n"`
	}
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid request")
	}
	if h.svc.VectorSearch == nil {
		return errJSON(c, http.StatusServiceUnavailable, "vector not available")
	}
	result, err := h.svc.VectorSearch(req.Collection, req.Vector, req.Filter, req.N)
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

func (h *handler) schemaAll(c echo.Context) error {
	if h.svc.SchemaAll == nil {
		return errJSON(c, http.StatusServiceUnavailable, "schema not available")
	}
	result, err := h.svc.SchemaAll()
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

func (h *handler) schemaSearch(c echo.Context) error {
	var req struct {
		Query string `json:"query"`
		N     int    `json:"n"`
	}
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid request")
	}
	if req.Query == "" {
		return errJSON(c, http.StatusBadRequest, "query required")
	}
	if req.N <= 0 {
		req.N = 3
	}
	if h.svc.SchemaSearch == nil {
		return errJSON(c, http.StatusServiceUnavailable, "schema search not available")
	}
	results, err := h.svc.SchemaSearch(req.Query, req.N)
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, results)
}
