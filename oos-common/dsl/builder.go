package dsl

import (
	"strings"
)

// Action repräsentiert eine erlaubte Operation auf einem Context.
type Action string

const (
	ActionRead   Action = "read"
	ActionWrite  Action = "write"
	ActionDelete Action = "delete"
)

// OOSAst ist der vollständige Abstract Syntax Tree.
// Ein einziger Aufruf von oosp_ast liefert alles was ein AI-Client braucht:
// Contexts, Felder, Meta-Quellen, Relationen, Prompts, Tools.
type OOSAst struct {
	Version  int          `json:"version"`
	Sources  []SourceAst  `json:"sources,omitempty"`
	Contexts []ContextAst `json:"contexts"`
	Locales  []LocaleAst  `json:"locales,omitempty"`
	Prompts  []PromptAst  `json:"global_prompts,omitempty"`
	Tools    []ToolAst    `json:"tools,omitempty"`
}

// ToolAst beschreibt ein verfügbares MCP Tool.
type ToolAst struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Params      []ToolParamAst `json:"params,omitempty"`
}

// ToolParamAst beschreibt einen Parameter eines MCP Tools.
type ToolParamAst struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

type SourceAst struct {
	Name string `json:"name"`
	Type string `json:"type"`
	DSN  string `json:"dsn,omitempty"`
	URL  string `json:"url,omitempty"`
}

// ContextAst beschreibt einen Context.
//
// Die KI liest Metas um zu wissen welche Referenztabellen es gibt.
// Sie baut daraus einen einzigen GraphQL-Request:
//
//	query {
//	  person_detail(id:1) { firstname country city }
//	  meta_countries { value label }
//	  meta_cities    { value label }
//	}
type ContextAst struct {
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Source      string          `json:"source"`
	DSN         string          `json:"dsn,omitempty"`
	Save        string          `json:"save,omitempty"`
	View        string          `json:"view,omitempty"`
	Locale      string          `json:"locale,omitempty"`
	ListFields  []string        `json:"list_fields,omitempty"`
	GQLQuery    string          `json:"gql_query"`
	GQLMutation string          `json:"gql_mutation,omitempty"`
	QueryableBy []string        `json:"queryable_by,omitempty"`
	Fields      []FieldAst      `json:"fields"`
	Metas       []MetaAst       `json:"metas,omitempty"`
	Relations   []RelationAst   `json:"relations,omitempty"`
	Navigates   []NavigateAst   `json:"navigate,omitempty"`
	Actions     []ActionAst     `json:"actions,omitempty"`
	Logics      []LogicAst      `json:"logic,omitempty"`
	Prompts     []PromptAst     `json:"prompts,omitempty"`
	Permissions []PermissionAst `json:"permissions,omitempty"`
}

func (c *ContextAst) AllowedActions(role string) ([]Action, bool) {
	if len(c.Permissions) == 0 {
		return nil, false
	}
	for _, p := range c.Permissions {
		if p.Role == role {
			return p.Actions, true
		}
	}
	return []Action{}, true
}

func (c *ContextAst) IsAllowed(role string, action Action) bool {
	actions, hasPermissions := c.AllowedActions(role)
	if !hasPermissions {
		return true
	}
	for _, a := range actions {
		if a == action {
			return true
		}
	}
	return false
}

// FieldAst beschreibt ein Feld.
//
// MetaRef verweist auf einen MetaAst — die KI weiß damit:
// dieses Feld hat Options → meta_<MetaRef> mitabfragen.
//
// Examples sind optionale Filter-Beispiele die im RAG-Chunk erscheinen.
// Sie werden aus dem XML <field>-Kind <example op value>comment</example>
// uebernommen und vom schema_chunk Renderer konsumiert.
type FieldAst struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Header     string            `json:"header,omitempty"`
	Readonly   bool              `json:"readonly,omitempty"`
	Filterable bool              `json:"filterable,omitempty"`
	Format     string            `json:"format,omitempty"`
	MetaRef    string            `json:"meta_ref,omitempty"` // z.B. "countries" → MetaAst.Name
	Examples   []FieldExampleAst `json:"examples,omitempty"`
}

// FieldExampleAst ist ein per-Field Filter-Beispiel.
//
// Op matcht den GraphQL-Filter-Operator (eq, like, gt, lt).
// Value ist der Beispielwert — Quoting (Strings vs. Zahlen) uebernimmt
// der Chunk-Renderer typbasiert.
// Comment ist ein kurzer Freitext-Kommentar fuer den Chunk.
type FieldExampleAst struct {
	Op      string `json:"op"`
	Value   string `json:"value"`
	Comment string `json:"comment,omitempty"`
}

// MetaAst beschreibt eine Referenztabelle für Options-Felder.
//
// Synthetist schreibt im CTX:
//
//	<meta name="countries" table="country" value="code" label="name" dsn="demo"/>
//
// oosp registriert dafür eine GraphQL-Query "meta_countries".
// Das Envelope das oosp an oos liefert:
//
//	{
//	  "content": { "firstname": "Frank", "country": "de" },
//	  "meta": {
//	    "countries": [{"value":"de","label":"Deutschland"}, ...],
//	    "cities":    [{"value":"koeln","label":"Köln"}, ...]
//	  }
//	}
type MetaAst struct {
	Name     string `json:"name"`               // interner Name, z.B. "countries"
	GQLQuery string `json:"gql_query"`          // GraphQL-Name, z.B. "meta_countries"
	Table    string `json:"table"`              // DB-Tabelle, z.B. "country"
	Value    string `json:"value"`              // Wert-Spalte, z.B. "code"
	Label    string `json:"label"`              // Anzeige-Spalte, z.B. "name"
	DSN      string `json:"dsn"`                // DSN-Name
	OrderBy  string `json:"order_by,omitempty"` // Sortierung, z.B. "name"
}

type PermissionAst struct {
	Role    string   `json:"role"`
	Actions []Action `json:"actions"`
}

type RelationAst struct {
	Name        string `json:"name"`
	ToContext   string `json:"context"`
	Source      string `json:"source,omitempty"`
	Type        string `json:"type"`
	BindLocal   string `json:"bind_local"`
	BindForeign string `json:"bind_foreign"`
}

type NavigateAst struct {
	Event       string `json:"event"`
	ToContext   string `json:"to"`
	BindLocal   string `json:"bind_local,omitempty"`
	BindForeign string `json:"bind_foreign,omitempty"`
}

// ActionAst beschreibt eine Aktion ohne Fensterwechsel (save, delete).
// Confirm enthält den Bestätigungstext für den Dialog — leer bedeutet kein Dialog.
type ActionAst struct {
	Event   string `json:"event"`
	Type    string `json:"type"`    // "save" | "delete"
	Confirm string `json:"confirm,omitempty"`
}

type LogicAst struct {
	Field string         `json:"field"`
	Rules []LogicRuleAst `json:"rules"`
}

type LogicRuleAst struct {
	Condition string `json:"condition"`
	Class     string `json:"class,omitempty"`
	Style     string `json:"style,omitempty"`
}

type PromptAst struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type LocaleAst struct {
	Name     string `json:"name"`
	Language string `json:"language"`
	Currency string `json:"currency"`
}

// ── AST Builder ───────────────────────────────────────────────────────────────

func BuildAST(files []*DSLFile) *OOSAst {
	tree := &OOSAst{Version: 2}

	for _, f := range files {
		if f.CTX == nil {
			continue
		}
		ctx := f.CTX

		if ctx.AI != nil {
			for _, p := range ctx.AI.Prompts {
				tree.Prompts = append(tree.Prompts, PromptAst{
					Name: p.Name,
					Text: strings.TrimSpace(p.Text),
				})
			}
		}

		for _, s := range ctx.Sources {
			tree.Sources = append(tree.Sources, SourceAst{
				Name: s.Name,
				Type: s.Type,
				DSN:  s.DSN,
				URL:  s.URL,
			})
		}

		if ctx.Backbone != nil {
			for _, d := range ctx.Backbone.DSNs {
				src := SourceAst{Name: d.Name, Type: d.Type, DSN: d.Path}
				if d.Type == "plugin" {
					src.URL = d.URL
					if d.Path != "" {
						src.URL = d.Path
					}
					src.DSN = ""
				}
				tree.Sources = append(tree.Sources, src)
			}
		}

		for _, l := range ctx.Locales {
			tree.Locales = append(tree.Locales, LocaleAst{
				Name:     l.Name,
				Language: l.Language,
				Currency: l.Currency,
			})
		}

		for _, c := range ctx.Contexts {
			tree.Contexts = append(tree.Contexts, buildContext(c))
		}
	}

	return tree
}

func buildContext(c XMLContext) ContextAst {
	gqlName := strings.ReplaceAll(c.Name, ".", "_")
	ctx := ContextAst{
		Name:       c.Name,
		Kind:       c.Kind,
		Source:     c.Source,
		DSN:        c.DSN,
		Save:       c.Save,
		View:       c.View,
		Locale:     c.Locale,
		GQLQuery:   gqlName,
		ListFields: splitList(c.ListFields),
	}

	if c.Kind == "entity" {
		ctx.GQLMutation = "update_" + gqlName
	}

	for _, f := range c.Fields {
		ctx.Fields = append(ctx.Fields, FieldAst{
			Name:       f.Name,
			Type:       f.Type,
			Header:     f.Header,
			Format:     f.Format,
			Readonly:   f.Readonly,
			Filterable: f.Filterable,
			MetaRef:    f.MetaRef,
			Examples:   buildFieldExamples(f.Examples),
		})
	}

	ctx.QueryableBy = buildQueryableBy(ctx.Fields)

	for _, m := range c.Metas {
		dsn := m.DSN
		if dsn == "" {
			dsn = c.DSN
		}
		ctx.Metas = append(ctx.Metas, MetaAst{
			Name:     m.Name,
			GQLQuery: "meta_" + m.Name,
			Table:    m.Table,
			Value:    m.Value,
			Label:    m.Label,
			DSN:      dsn,
			OrderBy:  m.OrderBy,
		})
	}

	for _, r := range c.Relations {
		local, foreign := parseBind(r.Bind)
		ctx.Relations = append(ctx.Relations, RelationAst{
			Name:        r.Name,
			ToContext:   r.Context,
			Source:      r.Source,
			Type:        r.Type,
			BindLocal:   local,
			BindForeign: foreign,
		})
	}

	for _, nav := range c.Navigates {
		local, foreign := parseBind(nav.Bind)
		ctx.Navigates = append(ctx.Navigates, NavigateAst{
			Event:       nav.Event,
			ToContext:   nav.To,
			BindLocal:   local,
			BindForeign: foreign,
		})
	}

	for _, a := range c.Actions {
		ctx.Actions = append(ctx.Actions, ActionAst{
			Event:   a.Event,
			Type:    a.Type,
			Confirm: a.Confirm,
		})
	}

	for _, l := range c.Logics {
		la := LogicAst{Field: l.Field}
		for _, r := range l.Rules {
			la.Rules = append(la.Rules, LogicRuleAst{
				Condition: r.Condition,
				Class:     r.Class,
				Style:     r.Style,
			})
		}
		ctx.Logics = append(ctx.Logics, la)
	}

	for _, p := range c.AIPrompts {
		ctx.Prompts = append(ctx.Prompts, PromptAst{
			Name: p.Name,
			Text: strings.TrimSpace(p.Text),
		})
	}

	for _, perm := range c.Permissions {
		ctx.Permissions = append(ctx.Permissions, PermissionAst{
			Role:    perm.Role,
			Actions: parseActions(perm.Actions),
		})
	}

	return ctx
}

// ── Hilfsfunktionen ───────────────────────────────────────────────────────────

// buildFieldExamples mappt die XML <example>-Kinder auf die AST-Form.
// Entries mit leerem Op oder Value werden uebersprungen — sie waeren
// fuer den Chunk-Renderer nicht konsumierbar.
func buildFieldExamples(examples []XMLFieldExample) []FieldExampleAst {
	if len(examples) == 0 {
		return nil
	}
	out := make([]FieldExampleAst, 0, len(examples))
	for _, ex := range examples {
		if ex.Op == "" || ex.Value == "" {
			continue
		}
		out = append(out, FieldExampleAst{
			Op:      ex.Op,
			Value:   ex.Value,
			Comment: strings.TrimSpace(ex.Comment),
		})
	}
	return out
}

func parseActions(raw string) []Action {
	var actions []Action
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		switch Action(part) {
		case ActionRead, ActionWrite, ActionDelete:
			actions = append(actions, Action(part))
		}
	}
	return actions
}

func buildQueryableBy(fields []FieldAst) []string {
	args := []string{"id"}
	for _, f := range fields {
		if !f.Filterable {
			continue
		}
		args = append(args, f.Name)
		if f.Type == "int" || f.Type == "float" {
			args = append(args, f.Name+"_gt", f.Name+"_lt")
		}
		if f.Type == "string" || f.Type == "" {
			args = append(args, f.Name+"_like")
		}
	}
	return args
}

func splitList(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func parseBind(bind string) (string, string) {
	parts := strings.SplitN(bind, "->", 2)
	if len(parts) != 2 {
		return bind, ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

// OOSPTools gibt alle verfügbaren OOSP MCP Tools zurück.
func OOSPTools() []ToolAst {
	return []ToolAst{
		{
			Name:        "oosp_ast",
			Description: "Returns the OOS Abstract Syntax Tree — contexts, fields, metas, relations, prompts and tools. Load this first.",
		},
		{
			Name:        "oosp_me",
			Description: "Returns the current user's groups and role.",
		},
		{
			Name:        "oosp_query",
			Description: "Executes a GraphQL query and returns JSON with content + meta sections.",
			Params: []ToolParamAst{
				{Name: "context", Type: "string", Required: true, Description: "Context name"},
				{Name: "query", Type: "string", Required: true, Description: "GraphQL query string"},
			},
		},
		{
			Name:        "oosp_mutation",
			Description: "Executes a direct GraphQL mutation.",
			Params: []ToolParamAst{
				{Name: "mutation", Type: "string", Required: true, Description: "GraphQL mutation string"},
			},
		},
		{
			Name:        "oosp_save",
			Description: "Builds and executes a GraphQL mutation from a JSON data map.",
			Params: []ToolParamAst{
				{Name: "context", Type: "string", Required: true, Description: "Context name"},
				{Name: "data", Type: "string", Required: true, Description: "JSON data map"},
			},
		},
		{
			Name:        "oosp_sources",
			Description: "Returns available DSN names on this plugin server.",
		},
	}
}
