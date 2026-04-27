// Package schema also holds the XSD parser that turns the dsl.xsd
// payload into an in-memory Catalog the builder UI can consume.
//
// This is deliberately NOT a general-purpose XSD library. We extract
// only what the builder needs: the list of DSL elements, their
// attributes (with types, enums, optional/required flags), and which
// child elements each element accepts. Anything else in the XSD —
// uniqueness constraints, namespaces, derivations — is ignored.

package schema

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
)

// AttrKind is the coarse classification the properties panel needs to
// pick an editor: a string, a boolean, an integer, or an enumeration.
type AttrKind int

const (
	// AttrString covers every xs:string and any unrecognised xs:* type.
	AttrString AttrKind = iota
	// AttrBool covers xs:boolean.
	AttrBool
	// AttrInt covers xs:integer / xs:positiveInteger / xs:nonNegativeInteger.
	AttrInt
	// AttrEnum covers attributes whose simpleType restricts to a list
	// of xs:enumeration values. Use Enums to render a dropdown.
	AttrEnum
)

// Attr describes a single attribute slot on a DSL element.
type Attr struct {
	Name     string
	Kind     AttrKind
	Required bool
	// Enums is populated only when Kind == AttrEnum.
	Enums []string
}

// Element describes one DSL element type — what attributes it accepts
// and which child elements it permits.
//
// The Category field is a builder-side grouping for the palette only.
// It is derived heuristically from the element's role in the XSD; it
// is not part of the grammar itself.
type Element struct {
	Name       string
	Attrs      []Attr
	// Children is the set of element names this element may contain.
	// Empty for pure leaf widgets (label, sep, icon, …).
	Children []string
	Category Category
}

// Category groups elements in the palette. Pure presentation — does
// not affect validity.
type Category int

const (
	CategoryUnknown Category = iota
	CategoryContainer
	CategoryFormField
	CategoryDisplay
	CategoryCollection
	CategoryAction
	CategoryStructural
)

func (c Category) String() string {
	switch c {
	case CategoryContainer:
		return "Containers and Layout"
	case CategoryFormField:
		return "Form Fields"
	case CategoryDisplay:
		return "Display"
	case CategoryCollection:
		return "Collections"
	case CategoryAction:
		return "Actions"
	case CategoryStructural:
		return "Structural"
	default:
		return "Other"
	}
}

// Catalog is the parsed grammar — one entry per DSL element.
//
// Lookups by element name are case-sensitive and match the XML element
// name (e.g. "section", "accordion-item"), not the XSD complexType
// name ("Section", "AccordionItem").
type Catalog struct {
	// Elements indexed by XML element name.
	Elements map[string]*Element
	// RootName is the single permitted document root, taken from the
	// top-level <xs:element> of the XSD. Always "screen" today.
	RootName string
}

// Get is a nil-safe accessor.
func (c *Catalog) Get(name string) *Element {
	if c == nil || c.Elements == nil {
		return nil
	}
	return c.Elements[name]
}

// Names returns every element name in deterministic order. Used by the
// palette to render the element list.
func (c *Catalog) Names() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.Elements))
	for name := range c.Elements {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ByCategory returns element names grouped and ordered by Category,
// then alphabetically inside each group. The category order is fixed
// (containers first, then form fields, then display, etc.) to keep
// the palette stable across runs.
func (c *Catalog) ByCategory() []CategoryGroup {
	if c == nil {
		return nil
	}
	buckets := map[Category][]string{}
	for name, el := range c.Elements {
		buckets[el.Category] = append(buckets[el.Category], name)
	}
	order := []Category{
		CategoryContainer,
		CategoryFormField,
		CategoryDisplay,
		CategoryCollection,
		CategoryAction,
		CategoryStructural,
		CategoryUnknown,
	}
	out := make([]CategoryGroup, 0, len(order))
	for _, cat := range order {
		names := buckets[cat]
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		out = append(out, CategoryGroup{Category: cat, Names: names})
	}
	return out
}

// CategoryGroup pairs a category with the element names that fell into
// it. Used by the palette renderer.
type CategoryGroup struct {
	Category Category
	Names    []string
}

// ParseCatalog decodes an XSD payload into a Catalog. The parser is
// intentionally strict in shape but tolerant of unknown nodes — any
// XSD construct it does not understand is dropped silently rather
// than failing the whole parse.
func ParseCatalog(xsd []byte) (*Catalog, error) {
	var doc xsdSchema
	if err := xml.Unmarshal(xsd, &doc); err != nil {
		return nil, fmt.Errorf("parse xsd: %w", err)
	}

	// Resolve simpleTypes first — needed to expand enum attribute types.
	simples := map[string][]string{}
	for _, st := range doc.SimpleTypes {
		if st.Name == "" || st.Restriction == nil {
			continue
		}
		var values []string
		for _, e := range st.Restriction.Enumerations {
			values = append(values, e.Value)
		}
		if len(values) > 0 {
			simples[st.Name] = values
		}
	}

	// Resolve attributeGroups so element complexTypes can pull them in.
	attrGroups := map[string][]Attr{}
	for _, ag := range doc.AttributeGroups {
		if ag.Name == "" {
			continue
		}
		attrGroups[ag.Name] = parseAttrs(ag.Attributes, simples)
	}

	// Index complexTypes by name so we can resolve type="X" references.
	typesByName := map[string]*xsdComplexType{}
	for i := range doc.ComplexTypes {
		t := &doc.ComplexTypes[i]
		if t.Name != "" {
			typesByName[t.Name] = t
		}
	}

	// Resolve named groups (e.g. UIElement) once — they map to a list
	// of permissible child element names.
	groups := map[string][]string{}
	for _, g := range doc.Groups {
		if g.Name == "" || g.Choice == nil {
			continue
		}
		var names []string
		for _, e := range g.Choice.Elements {
			if e.Name != "" {
				names = append(names, e.Name)
			}
		}
		groups[g.Name] = names
	}

	cat := &Catalog{
		Elements: map[string]*Element{},
		RootName: "",
	}

	// Top-level <xs:element> entries describe the document roots. In
	// the OOS DSL there is only one (<screen>) but we don't hard-code
	// that — we use the first one we see.
	for _, te := range doc.Elements {
		if cat.RootName == "" {
			cat.RootName = te.Name
		}
		ct := typesByName[te.Type]
		if ct == nil {
			continue
		}
		el := buildElement(te.Name, ct, typesByName, attrGroups, groups, simples)
		cat.Elements[te.Name] = el
	}

	// Every other element is reached transitively from <screen>. Walk
	// the type graph and instantiate one Element per distinct element
	// name we encounter.
	seen := map[string]bool{}
	for name := range cat.Elements {
		seen[name] = true
	}
	pending := append([]string(nil), cat.Names()...)
	for len(pending) > 0 {
		name := pending[0]
		pending = pending[1:]

		el := cat.Elements[name]
		if el == nil {
			continue
		}
		for _, child := range el.Children {
			if seen[child] {
				continue
			}
			seen[child] = true

			// Find the type for this child by looking inside any
			// complexType that declares it. Cheaper alternative: scan
			// every complexType once and record element->type mappings.
			childType := lookupChildType(child, doc.ComplexTypes, doc.Groups)
			ct := typesByName[childType]
			if ct == nil {
				// Unknown type — register a stub so the builder still
				// shows the element in the tree, just without props.
				cat.Elements[child] = &Element{
					Name:     child,
					Category: classify(child),
				}
				continue
			}
			childEl := buildElement(child, ct, typesByName, attrGroups, groups, simples)
			cat.Elements[child] = childEl
			pending = append(pending, child)
		}
	}

	if cat.RootName == "" {
		return nil, fmt.Errorf("xsd has no top-level element")
	}
	return cat, nil
}

// buildElement assembles the Element entry for a single complexType.
func buildElement(
	name string,
	ct *xsdComplexType,
	typesByName map[string]*xsdComplexType,
	attrGroups map[string][]Attr,
	groups map[string][]string,
	simples map[string][]string,
) *Element {
	attrs := parseAttrs(ct.Attributes, simples)

	// Pull in attributes from referenced attributeGroups.
	for _, ref := range ct.AttributeGroups {
		if ref.Ref == "" {
			continue
		}
		if extra, ok := attrGroups[ref.Ref]; ok {
			attrs = append(attrs, extra...)
		}
	}

	// Children come either from a referenced group (e.g. UIElement) or
	// from inline <xs:sequence> child elements (e.g. accordion-item).
	var children []string
	if ct.Sequence != nil {
		for _, e := range ct.Sequence.Elements {
			if e.Name != "" {
				children = append(children, e.Name)
			}
		}
	}
	if ct.Group != nil && ct.Group.Ref != "" {
		if names, ok := groups[ct.Group.Ref]; ok {
			children = append(children, names...)
		}
	}
	if ct.Choice != nil {
		for _, e := range ct.Choice.Elements {
			if e.Name != "" {
				children = append(children, e.Name)
			}
		}
	}

	return &Element{
		Name:     name,
		Attrs:    dedupAttrs(attrs),
		Children: dedupStrings(children),
		Category: classify(name),
	}
}

// parseAttrs converts raw <xs:attribute> nodes into Attr structs.
// Inline <xs:simpleType><xs:restriction>...</xs:restriction></xs:simpleType>
// blocks are honoured here; named simpleType references are resolved
// via the simples map.
func parseAttrs(raw []xsdAttribute, simples map[string][]string) []Attr {
	out := make([]Attr, 0, len(raw))
	for _, a := range raw {
		if a.Name == "" {
			continue
		}
		attr := Attr{
			Name:     a.Name,
			Kind:     classifyType(a.Type),
			Required: a.Use == "required",
		}
		// Inline enum?
		if a.SimpleType != nil && a.SimpleType.Restriction != nil {
			var enums []string
			for _, e := range a.SimpleType.Restriction.Enumerations {
				enums = append(enums, e.Value)
			}
			if len(enums) > 0 {
				attr.Kind = AttrEnum
				attr.Enums = enums
			}
		}
		// Named simpleType reference?
		if attr.Kind != AttrEnum {
			if enums, ok := simples[a.Type]; ok {
				attr.Kind = AttrEnum
				attr.Enums = enums
			}
		}
		out = append(out, attr)
	}
	return out
}

// classifyType maps XSD primitive type names to AttrKind.
func classifyType(t string) AttrKind {
	switch t {
	case "xs:boolean":
		return AttrBool
	case "xs:integer", "xs:positiveInteger", "xs:nonNegativeInteger":
		return AttrInt
	default:
		return AttrString
	}
}

// classify assigns an element to a palette category from its name. The
// rules are cosmetic and intentionally compact — when in doubt the
// element falls into the "Structural" bucket so it still shows up.
func classify(name string) Category {
	switch name {
	case "box", "grid", "gridwrap", "border", "center", "stack",
		"section", "card", "form":
		return CategoryContainer
	case "tabs", "tab", "accordion", "accordion-item":
		return CategoryContainer
	case "field", "entry", "textarea", "choices", "check",
		"radio", "slider", "progress", "option":
		return CategoryFormField
	case "label", "richtext", "icon", "sep", "span":
		return CategoryDisplay
	case "table", "list", "tree", "column", "node":
		return CategoryCollection
	case "button", "hyperlink", "toolbar":
		return CategoryAction
	case "screen":
		return CategoryStructural
	}
	return CategoryUnknown
}

// lookupChildType walks every complexType (and named group) to find
// the type of a child element by its XML name.
func lookupChildType(name string, cts []xsdComplexType, groups []xsdGroup) string {
	for _, ct := range cts {
		if ct.Sequence != nil {
			for _, e := range ct.Sequence.Elements {
				if e.Name == name && e.Type != "" {
					return e.Type
				}
			}
		}
		if ct.Choice != nil {
			for _, e := range ct.Choice.Elements {
				if e.Name == name && e.Type != "" {
					return e.Type
				}
			}
		}
	}
	for _, g := range groups {
		if g.Choice == nil {
			continue
		}
		for _, e := range g.Choice.Elements {
			if e.Name == name && e.Type != "" {
				return e.Type
			}
		}
	}
	return ""
}

// dedupAttrs removes duplicates by attribute name, keeping the first
// occurrence. Spacing and ChildHints groups are referenced from many
// element types, so name collisions are common.
func dedupAttrs(in []Attr) []Attr {
	seen := map[string]bool{}
	out := make([]Attr, 0, len(in))
	for _, a := range in {
		if seen[a.Name] {
			continue
		}
		seen[a.Name] = true
		out = append(out, a)
	}
	return out
}

func dedupStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// suppress unused import warning during incremental development.
var _ = strings.TrimSpace
