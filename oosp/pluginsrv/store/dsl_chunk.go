package store

// dsl_chunk.go — oos.oos_dsl_schema chunk rendering.
//
// One chunk per DSL element, stored with kind='element'. A chunk
// combines two sources:
//
//  1. Grammar (dsl.xsd)        — attribute names, required/optional,
//                                 enum values, legal children. These
//                                 are hard facts the Fyne renderer
//                                 enforces.
//  2. Enrichment (dsl-enrichment.xml) — German aliases, intent, a
//                                 copy-pasteable example and AI hints.
//                                 The soft layer that bridges natural-
//                                 language UI requests to DSL shape.
//
// Both documents live in oos.oos_dsl_meta (rows 'grammar' and
// 'enrichment') and are refreshed by --seed-internal. oosp listens on
// pg_notify 'oos_dsl_meta' and rebuilds every element chunk on any
// change.
//
// kind='pattern' is reserved for future combination-level chunks
// (full-screen skeletons, form templates) but is not populated today.
// The agent loop in oosp uses multiple element retrievals instead.
//
// Chunk IDs are prefixed with the kind ("element:field") so LIKE-based
// filtering works without touching the kind column.
//
// Design note on the XSD parser: encoding/xml against a minimal
// schema-aware struct. We intentionally don't import a full XSD
// reflection library — the grammar only uses a handful of xs:*
// features (complexType with attributes and either xs:sequence,
// xs:group, xs:choice), so a dedicated parser tailored to this file
// is cheaper and more legible than pulling in a generic XSD crate.

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
)

// ── Public types ──────────────────────────────────────────────────────────────

// DSLChunk holds one DSL schema chunk ready for embedding.
// ID carries the kind as a prefix ("element:field", "pattern:person_detail").
type DSLChunk struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // "element" | "pattern"
	Text string `json:"chunk"`
}

// ── Element chunks (from XSD) ─────────────────────────────────────────────────

// xsdSchema is a minimal unmarshalling target for dsl.xsd.
//
// We only read the handful of features the grammar actually uses:
//   - xs:element (the top-level root declaration and inside xs:group/choice)
//   - xs:complexType (named types — the element chunks we emit)
//   - xs:attribute (direct and through attributeGroup references)
//   - xs:attributeGroup (named groups that complexTypes reference)
//   - xs:group (named content groups — our UIElement)
//   - xs:sequence / xs:choice (both are flattened to "children allowed")
//
// xs:simpleType restrictions (enums) are resolved separately in
// resolveEnum to enrich the "attributes" line with concrete allowed
// values, which is much more useful to the LLM than a bare type name.
type xsdSchema struct {
	XMLName         xml.Name            `xml:"schema"`
	Elements        []xsdTopElement     `xml:"element"`
	ComplexTypes    []xsdComplexType    `xml:"complexType"`
	SimpleTypes     []xsdSimpleType     `xml:"simpleType"`
	AttributeGroups []xsdAttributeGroup `xml:"attributeGroup"`
	Groups          []xsdGroup          `xml:"group"`
}

type xsdTopElement struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

type xsdComplexType struct {
	Name       string             `xml:"name,attr"`
	Mixed      string             `xml:"mixed,attr"`
	Sequence   *xsdContentModel   `xml:"sequence"`
	Choice     *xsdContentModel   `xml:"choice"`
	Group      *xsdGroupRef       `xml:"group"`
	Attributes []xsdAttribute     `xml:"attribute"`
	AttrGroups []xsdAttrGroupRef  `xml:"attributeGroup"`
}

type xsdContentModel struct {
	Elements []xsdInlineElement `xml:"element"`
}

type xsdInlineElement struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

type xsdGroupRef struct {
	Ref string `xml:"ref,attr"`
}

type xsdAttribute struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
	Use  string `xml:"use,attr"`
}

type xsdAttrGroupRef struct {
	Ref string `xml:"ref,attr"`
}

type xsdAttributeGroup struct {
	Name       string         `xml:"name,attr"`
	Attributes []xsdAttribute `xml:"attribute"`
}

type xsdGroup struct {
	Name   string           `xml:"name,attr"`
	Choice *xsdContentModel `xml:"choice"`
}

type xsdSimpleType struct {
	Name        string         `xml:"name,attr"`
	Restriction xsdRestriction `xml:"restriction"`
}

type xsdRestriction struct {
	Base         string            `xml:"base,attr"`
	Enumerations []xsdEnumeration  `xml:"enumeration"`
}

type xsdEnumeration struct {
	Value string `xml:"value,attr"`
}

// enrichmentDoc is the unmarshalling target for dsl-enrichment.xml.
type enrichmentDoc struct {
	XMLName  xml.Name             `xml:"dsl-enrichment"`
	Elements []enrichmentElement  `xml:"element"`
}

// enrichmentElement carries the semantic layer for one DSL element.
// All fields are optional — missing sections are simply skipped in
// the rendered chunk.
type enrichmentElement struct {
	Name    string   `xml:"name,attr"`
	Aliases []string `xml:"alias"`
	Intent  string   `xml:"intent"`
	Example string   `xml:"example"`
	Hints   []string `xml:"hint"`
}

// BuildDSLElementChunks parses dsl.xsd + dsl-enrichment.xml and returns
// one chunk per DSL element. The enrichment document may be empty
// ("") — the chunks then carry only grammar facts, which is still
// useful, just less effective at bridging natural-language intent.
//
// The root <screen> is included even though it isn't in the UIElement
// group — it's the most important element and the LLM will be asked
// about it first.
//
// Types used only as containers for inline elements (Column under
// Table, Option under Choices, Span under RichText, Tab under Tabs,
// AccordionItem under Accordion, TreeNode under Tree) are included as
// their own chunks: the LLM often needs to emit them in isolation.
//
// Chunks are sorted by ID so output is deterministic across rebuilds.
func BuildDSLElementChunks(xsdText, enrichmentText string) ([]DSLChunk, error) {
	var schema xsdSchema
	if err := xml.Unmarshal([]byte(xsdText), &schema); err != nil {
		return nil, fmt.Errorf("xsd parse: %w", err)
	}

	enrichmentIndex, err := indexEnrichment(enrichmentText)
	if err != nil {
		return nil, fmt.Errorf("enrichment parse: %w", err)
	}

	enumIndex := indexEnums(schema.SimpleTypes)
	attrGroupIndex := indexAttributeGroups(schema.AttributeGroups)
	uiElementNames := collectUIElementNames(schema.Groups)
	// Known root element plus every UIElement + a small set of always-
	// relevant sub-types the grammar uses as building blocks. The set
	// is assembled from the XSD itself; hard-coding would drift.
	targets := buildElementTargetSet(schema.ComplexTypes, uiElementNames)

	var chunks []DSLChunk
	for _, ct := range schema.ComplexTypes {
		if !targets[ct.Name] {
			continue
		}
		tag := xmlNameForComplexType(ct.Name)
		chunk := renderElementChunk(ct, attrGroupIndex, enumIndex, enrichmentIndex[tag])
		chunks = append(chunks, DSLChunk{
			ID:   "element:" + tag,
			Kind: "element",
			Text: chunk,
		})
	}

	sort.Slice(chunks, func(i, j int) bool { return chunks[i].ID < chunks[j].ID })
	return chunks, nil
}

// indexEnrichment parses the enrichment XML and returns a map from
// element tag name ("section", "field", ...) to its enrichment block.
// An empty input is not an error — it yields an empty map.
func indexEnrichment(enrichmentText string) (map[string]enrichmentElement, error) {
	out := make(map[string]enrichmentElement)
	if strings.TrimSpace(enrichmentText) == "" {
		return out, nil
	}
	var doc enrichmentDoc
	if err := xml.Unmarshal([]byte(enrichmentText), &doc); err != nil {
		return nil, err
	}
	for _, e := range doc.Elements {
		if e.Name == "" {
			continue
		}
		out[e.Name] = e
	}
	return out, nil
}

// xmlNameForComplexType maps a complexType name (PascalCase in the XSD)
// back to the XML tag name (lowercase, or hyphenated for AccordionItem).
// The UIElement group is the authoritative source, but for types that
// live outside it we fall back to a simple ToLower — the XSD follows a
// consistent convention where the type name is exactly the tag with an
// initial capital.
func xmlNameForComplexType(typeName string) string {
	switch typeName {
	case "AccordionItem":
		return "accordion-item"
	case "TreeNode":
		return "node"
	case "Screen", "Box", "Grid", "GridWrap", "Border", "Center", "Stack",
		"Tabs", "Tab", "Section", "Card", "Accordion", "Form",
		"Field", "Entry", "TextArea", "Choices", "Check", "Radio",
		"Option", "Slider", "Progress",
		"Label", "Button", "Hyperlink", "Icon", "RichText", "Span", "Sep",
		"Table", "Column", "List", "Tree", "Toolbar":
		return strings.ToLower(typeName)
	}
	return strings.ToLower(typeName)
}

// collectUIElementNames returns the set of xml tag names listed inside
// the UIElement group's choice. Used later to mark those complexTypes
// as the core embeddable elements.
func collectUIElementNames(groups []xsdGroup) map[string]bool {
	out := make(map[string]bool)
	for _, g := range groups {
		if g.Name != "UIElement" || g.Choice == nil {
			continue
		}
		for _, e := range g.Choice.Elements {
			out[e.Name] = true
		}
	}
	return out
}

// buildElementTargetSet picks every complexType we want to emit a chunk
// for: Screen (the root), every type referenced by the UIElement group,
// and the specialised child types (Tab, AccordionItem, Column, Option,
// Span, TreeNode) since those are meaningful on their own.
func buildElementTargetSet(complexTypes []xsdComplexType, uiElementNames map[string]bool) map[string]bool {
	// Map xml tag name → complexType name.
	tagToType := make(map[string]string)
	for _, ct := range complexTypes {
		tagToType[xmlNameForComplexType(ct.Name)] = ct.Name
	}

	out := map[string]bool{"Screen": true}
	for tag := range uiElementNames {
		if typeName, ok := tagToType[tag]; ok {
			out[typeName] = true
		}
	}
	// Child types that are reachable only through a parent's sequence —
	// not in UIElement directly but still important for the LLM to know.
	for _, childTag := range []string{"tab", "accordion-item", "column", "option", "span", "node"} {
		if typeName, ok := tagToType[childTag]; ok {
			out[typeName] = true
		}
	}
	return out
}

// indexEnums builds a map from simpleType name to its enumeration values.
// Non-enum simpleTypes (patterns, plain restrictions) land with an empty
// slice — callers can treat that as "no enum to show".
func indexEnums(simpleTypes []xsdSimpleType) map[string][]string {
	out := make(map[string][]string, len(simpleTypes))
	for _, st := range simpleTypes {
		vals := make([]string, 0, len(st.Restriction.Enumerations))
		for _, e := range st.Restriction.Enumerations {
			vals = append(vals, e.Value)
		}
		out[st.Name] = vals
	}
	return out
}

// indexAttributeGroups returns the attribute groups keyed by name so
// complexTypes that reference them can be flattened during rendering.
func indexAttributeGroups(groups []xsdAttributeGroup) map[string]xsdAttributeGroup {
	out := make(map[string]xsdAttributeGroup, len(groups))
	for _, g := range groups {
		out[g.Name] = g
	}
	return out
}

// renderElementChunk produces the text body for one element chunk.
// Layout follows the CTX chunk shape: one fact per line, stable
// order, sections only emitted when they have content.
//
// Section order:
//   Element:    the XML tag
//   Alias:      German natural-language phrases (enrichment)
//   Intent:     one-sentence description (enrichment)
//   Attributes: XSD-derived list with required/enum/type hints
//   Children:   XSD-derived content model description
//   Example:    copy-pasteable snippet (enrichment)
//   AI hints:   short rules of thumb (enrichment)
//
// Structural sections come from the XSD — those facts are enforced by
// the renderer and must be accurate. Semantic sections come from
// enrichment — those bridge natural-language intent to structure and
// carry the judgment calls the grammar cannot express.
func renderElementChunk(
	ct xsdComplexType,
	attrGroups map[string]xsdAttributeGroup,
	enums map[string][]string,
	enrich enrichmentElement,
) string {
	var sb strings.Builder
	tag := xmlNameForComplexType(ct.Name)

	fmt.Fprintf(&sb, "Element: <%s>\n", tag)

	if len(enrich.Aliases) > 0 {
		fmt.Fprintf(&sb, "Alias: %s\n", strings.Join(enrich.Aliases, ", "))
	}
	if intent := strings.TrimSpace(enrich.Intent); intent != "" {
		fmt.Fprintf(&sb, "Intent: %s\n", intent)
	}

	attrs := flattenAttributes(ct, attrGroups)
	if len(attrs) > 0 {
		sb.WriteString("Attributes:\n")
		for _, a := range attrs {
			fmt.Fprintf(&sb, "  - %s%s%s\n", a.Name, requiredTag(a.Use), attrTypeHint(a.Type, enums))
		}
	}

	if children := describeChildren(ct); children != "" {
		fmt.Fprintf(&sb, "Children: %s\n", children)
	}

	if ex := strings.TrimSpace(enrich.Example); ex != "" {
		sb.WriteString("Example:\n")
		for _, line := range strings.Split(ex, "\n") {
			fmt.Fprintf(&sb, "  %s\n", line)
		}
	}

	if len(enrich.Hints) > 0 {
		sb.WriteString("AI hints:\n")
		for _, h := range enrich.Hints {
			h = strings.TrimSpace(h)
			if h == "" {
				continue
			}
			fmt.Fprintf(&sb, "  - %s\n", h)
		}
	}

	return sb.String()
}

// flattenAttributes merges directly-declared attributes and the attributes
// pulled in via attributeGroup references, preserving declaration order.
// Deduplicates on attribute name — if a complexType shadows a group's
// attribute, the local one wins.
func flattenAttributes(ct xsdComplexType, attrGroups map[string]xsdAttributeGroup) []xsdAttribute {
	seen := make(map[string]bool)
	var out []xsdAttribute

	for _, a := range ct.Attributes {
		if a.Name == "" || seen[a.Name] {
			continue
		}
		seen[a.Name] = true
		out = append(out, a)
	}
	for _, ref := range ct.AttrGroups {
		g, ok := attrGroups[ref.Ref]
		if !ok {
			continue
		}
		for _, a := range g.Attributes {
			if a.Name == "" || seen[a.Name] {
				continue
			}
			seen[a.Name] = true
			out = append(out, a)
		}
	}
	return out
}

// requiredTag returns " (required)" for use="required", empty otherwise.
func requiredTag(use string) string {
	if use == "required" {
		return " (required)"
	}
	return ""
}

// attrTypeHint renders a compact type description for the attributes
// line. Plain xs:* types become bare ("string", "boolean"); named
// simpleTypes with an enum become the enum itself so the LLM sees the
// allowed values without a separate lookup.
func attrTypeHint(typeName string, enums map[string][]string) string {
	if typeName == "" {
		return ""
	}
	// xs:string, xs:boolean, xs:positiveInteger → strip prefix.
	if strings.HasPrefix(typeName, "xs:") {
		return " — " + strings.TrimPrefix(typeName, "xs:")
	}
	if vals, ok := enums[typeName]; ok && len(vals) > 0 {
		return " — one of: " + strings.Join(vals, ", ")
	}
	return " — " + typeName
}

// describeChildren summarises the allowed content of a complexType.
// Three shapes matter for this grammar:
//   - group ref="UIElement" → "any UI element"
//   - sequence with specific elements → list the element names
//   - choice with specific elements → list, noting it's a choice
// A complexType with none of these is a leaf (empty children).
func describeChildren(ct xsdComplexType) string {
	if ct.Group != nil && ct.Group.Ref == "UIElement" {
		return "any UI element (box, section, field, tabs, accordion, table, ...)"
	}
	if ct.Sequence != nil && len(ct.Sequence.Elements) > 0 {
		names := collectChildNames(ct.Sequence.Elements)
		if len(names) == 1 {
			return fmt.Sprintf("only <%s>", names[0])
		}
		return fmt.Sprintf("only %s", joinAngled(names))
	}
	if ct.Choice != nil && len(ct.Choice.Elements) > 0 {
		names := collectChildNames(ct.Choice.Elements)
		return fmt.Sprintf("one of %s", joinAngled(names))
	}
	if strings.EqualFold(ct.Mixed, "true") {
		return "mixed text content"
	}
	return ""
}

// collectChildNames extracts the `name` attribute from a list of inline
// element declarations. Order preserved.
func collectChildNames(elements []xsdInlineElement) []string {
	out := make([]string, 0, len(elements))
	for _, e := range elements {
		if e.Name == "" {
			continue
		}
		out = append(out, e.Name)
	}
	return out
}

// joinAngled renders ["tab", "sep"] as "<tab>, <sep>".
func joinAngled(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = "<" + n + ">"
	}
	return strings.Join(parts, ", ")
}

