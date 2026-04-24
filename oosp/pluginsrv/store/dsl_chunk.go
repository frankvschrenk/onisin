package store

// dsl_chunk.go — oos.oos_dsl_schema chunk rendering.
//
// Two shapes of chunks live in the same table, distinguished by kind:
//
//  1. kind='element' — one chunk per XSD complexType that appears as a
//     UIElement (box, section, field, tabs, accordion, table, …). Each
//     chunk teaches the LLM which attributes an element accepts, which
//     children are legal, and shows a tiny rendered example. Generated
//     once at oosp startup from the grammar served out of oos.config
//     (namespace "schema.dsl").
//
//  2. kind='pattern' — one chunk per oos.dsl row. Each chunk describes
//     a real, seed-approved screen: its id, title, which top-level
//     elements it uses, which fields/widgets appear, and which bind
//     paths. Regenerated on pg_notify via the dsl_notify trigger.
//
// Both kinds are embedded into the same vector space so the retriever
// can pull a mix of grammar and concrete usage for one query. IDs are
// prefixed with the kind ("element:field", "pattern:person_detail")
// so LIKE-based filtering works without touching the kind column.
//
// Design note on element chunks: the XSD is parsed with encoding/xml
// against a minimal schema-aware struct. We intentionally don't import
// a full XSD reflection library — the grammar only uses a handful of
// xs:* features (complexType with attributes and either xs:sequence,
// xs:group, xs:choice), so a dedicated parser tailored to this file
// is cheaper and more legible than pulling in a generic XSD crate.

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	base "onisin.com/oos-dsl-base/base"
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

// BuildDSLElementChunks parses a dsl.xsd document and returns one chunk
// per complexType that represents a UIElement.
//
// The root <screen> is included too, even though it isn't in the
// UIElement group — it's the most important element and the LLM will
// be asked about it first.
//
// Types used purely as containers for inline elements (Column under
// Table, Option under Choices, Span under RichText, Tab under Tabs,
// AccordionItem under Accordion, TreeNode under Tree) are included as
// their own chunks: the LLM often needs to emit them in isolation.
//
// Chunks are sorted by ID so backfill output is deterministic.
func BuildDSLElementChunks(xsdText string) ([]DSLChunk, error) {
	var schema xsdSchema
	if err := xml.Unmarshal([]byte(xsdText), &schema); err != nil {
		return nil, fmt.Errorf("xsd parse: %w", err)
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
		chunk := renderElementChunk(ct, attrGroupIndex, enumIndex)
		chunks = append(chunks, DSLChunk{
			ID:   "element:" + xmlNameForComplexType(ct.Name),
			Kind: "element",
			Text: chunk,
		})
	}

	sort.Slice(chunks, func(i, j int) bool { return chunks[i].ID < chunks[j].ID })
	return chunks, nil
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
// Layout mirrors the CTX chunks: one fact per line, stable order.
func renderElementChunk(
	ct xsdComplexType,
	attrGroups map[string]xsdAttributeGroup,
	enums map[string][]string,
) string {
	var sb strings.Builder
	tag := xmlNameForComplexType(ct.Name)

	fmt.Fprintf(&sb, "Element: <%s>\n", tag)
	fmt.Fprintf(&sb, "Purpose: %s\n", elementPurpose(tag))

	attrs := flattenAttributes(ct, attrGroups)
	if len(attrs) > 0 {
		sb.WriteString("Attributes:\n")
		for _, a := range attrs {
			fmt.Fprintf(&sb, "  - %s%s%s\n", a.Name, requiredTag(a.Use), attrTypeHint(a.Type, enums))
		}
	}

	children := describeChildren(ct)
	if children != "" {
		fmt.Fprintf(&sb, "Children: %s\n", children)
	}

	if ex := elementExample(tag); ex != "" {
		fmt.Fprintf(&sb, "Example: %s\n", ex)
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

// elementPurpose is a short, human-authored description of what each
// element is for. Authored inline rather than harvested from the XSD
// comments because the comments aren't surfaced by encoding/xml and
// embedding-quality summaries are worth the small duplication.
func elementPurpose(tag string) string {
	switch tag {
	case "screen":
		return "Top-level screen. Every DSL document has exactly one."
	case "box":
		return "Horizontal or vertical stack of children. Primary layout primitive."
	case "grid":
		return "Fixed-column grid with configurable gap."
	case "gridwrap":
		return "Flow layout — children wrap based on min-width."
	case "border":
		return "Five-slot layout (top/bottom/left/right/center) driven by the child's position attribute."
	case "center":
		return "Centers its single child."
	case "stack":
		return "Z-stacks children on top of each other."
	case "tabs":
		return "Tab container — children are <tab>."
	case "tab":
		return "One tab inside <tabs>. Carries its own UI elements."
	case "section":
		return "Grouping container with an optional header label. cols+gap turn <field> children into a grid row."
	case "card":
		return "Framed panel with a title/subtitle pair."
	case "accordion":
		return "Collapsible sections — children are <accordion-item>."
	case "accordion-item":
		return "One foldable panel inside <accordion>. open=true makes it start expanded."
	case "form":
		return "Fyne widget.Form — label-left / input-right rows."
	case "field":
		return "Labelled input. The widget attribute picks entry/textarea/choices/radio/check/slider/progress. Default is entry."
	case "entry":
		return "Single-line text input."
	case "textarea":
		return "Multi-line text input with word wrap."
	case "choices":
		return "Single-select dropdown. options= names a meta source, or <option> children give inline values."
	case "check":
		return "Single checkbox. The label appears to the right of the box."
	case "radio":
		return "Radio group. orient=horizontal lays out across instead of down."
	case "option":
		return "One option inside <choices> or <radio>. Text is the label, value attribute is the underlying value."
	case "slider":
		return "Numeric slider with min/max/step."
	case "progress":
		return "Read-only progress bar driven by bind= or value=."
	case "label":
		return "Plain text label. style=bold/italic/mono for emphasis."
	case "button":
		return "Clickable button. action= names the event, style= picks an icon."
	case "hyperlink":
		return "Clickable link opening an external URL."
	case "icon":
		return "Theme-icon from the Fyne icon set."
	case "richtext":
		return "Formatted text. Either markdown=true with raw markdown, or <span> children with style= for inline emphasis."
	case "span":
		return "Inline segment inside <richtext>. style= picks heading, bold, italic, mono, codeblock, etc."
	case "sep":
		return "Horizontal separator line."
	case "table":
		return "Data table — rows from bind=, columns declared as <column> children."
	case "column":
		return "One column inside <table>. field= picks the row key, width= the pixel width."
	case "list":
		return "Single-column list bound to a JSON array."
	case "tree":
		return "Hierarchical tree — children are <node> elements with parent= pointers."
	case "node":
		return "One node inside <tree>. parent= points at the node's parent id (empty for roots)."
	case "toolbar":
		return "Top-level action bar — holds <button> and <sep> children."
	}
	return ""
}

// elementExample returns a tiny, copy-pasteable snippet for the element.
// Not every element needs one — the purpose line plus the attribute list
// is often enough. Examples are reserved for elements where the shape
// is tricky (field's widget attribute, richtext's two modes, ...).
func elementExample(tag string) string {
	switch tag {
	case "screen":
		return `<screen id="person_detail" title="Person" save="true" delete="true">…</screen>`
	case "section":
		return `<section label="Address" cols="2" gap="4" p="3">…</section>`
	case "field":
		return `<field label="Role" bind="person.role" widget="choices" options="roles"/>`
	case "table":
		return `<table bind="rows" action="on_select"><column field="id" label="ID" width="60"/>…</table>`
	case "tabs":
		return `<tabs p="2"><tab label="Main">…</tab><tab label="More">…</tab></tabs>`
	case "accordion":
		return `<accordion p="3"><accordion-item label="Notes" open="true">…</accordion-item></accordion>`
	case "richtext":
		return `<richtext><span style="heading">Title</span><span>body</span></richtext>`
	case "toolbar":
		return `<toolbar><button action="on_new" style="add" label="New"/><sep/></toolbar>`
	}
	return ""
}

// ── Pattern chunks (from oos.dsl rows) ────────────────────────────────────────

// BuildDSLPatternChunk renders a single pattern chunk from a seeded DSL
// screen. Returns nil if the XML cannot be parsed — the caller logs and
// moves on, since a broken screen is a seed problem, not a chunk problem.
//
// The chunk captures:
//   - id and title (top-level discovery signals)
//   - top-level structural elements actually used
//   - every <field>/<column>/<check>/<textarea>/<choices>/<radio>/<slider>
//     binding, so the LLM can match intent to concrete bind paths
//   - actions and buttons, which are how the screen behaves
//
// Layout mirrors the element chunks: one fact per line, stable order.
func BuildDSLPatternChunk(screenID, xmlText string) (*DSLChunk, error) {
	root, err := base.Parse(strings.NewReader(xmlText))
	if err != nil {
		return nil, fmt.Errorf("dsl %q parse: %w", screenID, err)
	}
	if root == nil || root.Type != base.NodeScreen {
		return nil, fmt.Errorf("dsl %q: root is not <screen>", screenID)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Screen: %s\n", screenID)
	if title := root.Attr("title", ""); title != "" {
		fmt.Fprintf(&sb, "Title: %s\n", title)
	}
	if label := root.Attr("label-color", ""); label != "" {
		fmt.Fprintf(&sb, "Label color: %s\n", label)
	}
	flags := screenFlags(root)
	if flags != "" {
		fmt.Fprintf(&sb, "Flags: %s\n", flags)
	}

	dslPatternStructure(&sb, root)
	dslPatternBindings(&sb, root)
	dslPatternActions(&sb, root)

	return &DSLChunk{
		ID:   "pattern:" + screenID,
		Kind: "pattern",
		Text: sb.String(),
	}, nil
}

// screenFlags collects the save/delete/exit boolean attributes from the
// screen root and returns them as a compact comma-separated list so the
// LLM can see at a glance what chrome the host app renders around the
// screen.
func screenFlags(root *base.Node) string {
	var flags []string
	for _, name := range []string{"save", "delete", "exit"} {
		if root.AttrBool(name) {
			flags = append(flags, name)
		}
	}
	return strings.Join(flags, ", ")
}

// dslPatternStructure emits the sequence of direct children of the
// screen, without recursing. That sequence is the signature of the
// screen's overall shape — "toolbar, box, sep, tabs" is recognisably
// the detail pattern; "toolbar, table" is the list pattern.
func dslPatternStructure(sb *strings.Builder, root *base.Node) {
	if len(root.Children) == 0 {
		return
	}
	parts := make([]string, 0, len(root.Children))
	for _, c := range root.Children {
		parts = append(parts, string(c.Type))
	}
	fmt.Fprintf(sb, "Structure: %s\n", strings.Join(parts, " → "))
}

// dslPatternBindings walks the whole tree and emits one line per
// element that carries a bind= attribute. Lines are kept compact:
// kind, bind path, optional label, optional widget/format.
//
// Sorted by bind path so the output is deterministic even if the DSL
// reorders its fields, which keeps embeddings stable.
func dslPatternBindings(sb *strings.Builder, root *base.Node) {
	type bindLine struct {
		kind, bind, label, widget string
	}
	var lines []bindLine

	var walk func(n *base.Node)
	walk = func(n *base.Node) {
		bind := n.Attr("bind", "")
		if bind != "" {
			lines = append(lines, bindLine{
				kind:   string(n.Type),
				bind:   bind,
				label:  n.Attr("label", ""),
				widget: n.Attr("widget", ""),
			})
		}
		// <column> uses field= instead of bind= but carries the same
		// "this column reads row[field]" meaning. Include it so the
		// LLM can match table columns to underlying field names.
		if n.Type == base.NodeColumn {
			if f := n.Attr("field", ""); f != "" {
				lines = append(lines, bindLine{
					kind:  "column",
					bind:  f,
					label: n.Attr("label", ""),
				})
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)

	if len(lines) == 0 {
		return
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].bind < lines[j].bind })

	sb.WriteString("Bindings:\n")
	for _, l := range lines {
		extra := ""
		if l.widget != "" {
			extra = " widget=" + l.widget
		}
		if l.label != "" {
			fmt.Fprintf(sb, "  - %s %s (%s)%s\n", l.kind, l.bind, l.label, extra)
		} else {
			fmt.Fprintf(sb, "  - %s %s%s\n", l.kind, l.bind, extra)
		}
	}
}

// dslPatternActions emits every button's (action, style, label) so the
// LLM knows what events a screen can produce. Independent from the
// bindings pass because buttons don't carry bind=.
func dslPatternActions(sb *strings.Builder, root *base.Node) {
	type actionLine struct {
		action, style, label string
	}
	var lines []actionLine

	var walk func(n *base.Node)
	walk = func(n *base.Node) {
		if n.Type == base.NodeButton {
			if act := n.Attr("action", ""); act != "" {
				lines = append(lines, actionLine{
					action: act,
					style:  n.Attr("style", ""),
					label:  n.Attr("label", ""),
				})
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)

	if len(lines) == 0 {
		return
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].action < lines[j].action })

	sb.WriteString("Actions:\n")
	for _, l := range lines {
		extras := make([]string, 0, 2)
		if l.style != "" {
			extras = append(extras, "style="+l.style)
		}
		if l.label != "" {
			extras = append(extras, "label="+l.label)
		}
		if len(extras) > 0 {
			fmt.Fprintf(sb, "  - %s (%s)\n", l.action, strings.Join(extras, ", "))
		} else {
			fmt.Fprintf(sb, "  - %s\n", l.action)
		}
	}
}
