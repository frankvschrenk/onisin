// LSP type definitions covering just the fields oosfs reads from gopls.
// The spec is large; keeping this file minimal makes it obvious which
// parts of the protocol this client actually depends on.

package gopls

// Position is a zero-based line/character pair. Note LSP counts
// characters in UTF-16 code units, but ASCII/BMP content makes this
// indistinguishable from byte offsets for typical Go code. Callers that
// pass non-ASCII columns should be aware of the discrepancy.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range spans two positions. End is exclusive per the LSP spec.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location pairs a URI with a Range — the shape of definition and
// references results.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Hover is the result shape for textDocument/hover. The Contents field
// is either a MarkupContent object or a legacy string/array; the wrapper
// normalizes all forms into a single {kind, value} pair.
type Hover struct {
	Contents HoverContents `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// HoverContents is a normalized representation: whatever gopls sends,
// we expose a single Value string. Callers only need to render it, so
// collapsing the variants here keeps the tool handler clean.
type HoverContents struct {
	Kind  string `json:"kind,omitempty"` // "markdown" or "plaintext"
	Value string `json:"value"`
}

// DocumentSymbol is the hierarchical form of documentSymbol. gopls
// emits this when hierarchicalDocumentSymbolSupport=true, which we set
// during initialize.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// Diagnostic matches publishDiagnostics payloads. Severity uses LSP's
// convention: 1=Error, 2=Warning, 3=Information, 4=Hint.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Code     any    `json:"code,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// PublishDiagnosticsParams is the notification envelope for
// textDocument/publishDiagnostics.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     int          `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// SymbolKindName names the numeric LSP SymbolKind enum values we
// encounter in documentSymbol output from gopls. The map is only for
// rendering; unknown kinds fall back to "unknown".
var SymbolKindName = map[int]string{
	1:  "file",
	2:  "module",
	3:  "namespace",
	4:  "package",
	5:  "class",
	6:  "method",
	7:  "property",
	8:  "field",
	9:  "constructor",
	10: "enum",
	11: "interface",
	12: "function",
	13: "variable",
	14: "constant",
	15: "string",
	16: "number",
	17: "boolean",
	18: "array",
	19: "object",
	20: "key",
	21: "null",
	22: "enum_member",
	23: "struct",
	24: "event",
	25: "operator",
	26: "type_parameter",
}

// SeverityName renders a Diagnostic.Severity as an LLM-friendly string.
func SeverityName(sev int) string {
	switch sev {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return "unknown"
	}
}
