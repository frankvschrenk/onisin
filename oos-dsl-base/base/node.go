// Package oosdsl — x-DSL Node-Baum und XML-Parser.
//
// Dieses Modul (onisin.com/oos-dsl) hat ausschliesslich Stdlib-Dependencies.
// Es kann von ooso, oos und dem Fyne-Renderer importiert werden ohne
// transitive UI-Bibliotheken mitzuziehen.
//
// Verwendung:
//
//	f, _ := os.Open("person-detail.dsl.xml")
//	root, _ := oosdsl.Parse(f)
//	title := root.Attr("title", "")
package base

import (
	"encoding/xml"
	"strconv"
)

// NodeType identifiziert den Typ eines DSL-Knotens.
type NodeType string

const (
	// Containers & Layout
	NodeScreen   NodeType = "screen"
	NodeBox      NodeType = "box"
	NodeGrid     NodeType = "grid"
	NodeGridWrap NodeType = "gridwrap"
	NodeBorder   NodeType = "border"
	NodeCenter   NodeType = "center"
	NodeStack    NodeType = "stack"
	NodeTabs     NodeType = "tabs"
	NodeTab      NodeType = "tab"
	NodeSection  NodeType = "section"
	NodeField    NodeType = "field"

	// Standard Widgets
	NodeLabel    NodeType = "label"
	NodeButton   NodeType = "button"
	NodeEntry    NodeType = "entry"
	NodeTextArea NodeType = "textarea"
	NodeChoices  NodeType = "choices"
	NodeCheck    NodeType = "check"
	NodeRadio    NodeType = "radio"
	NodeOption   NodeType = "option"
	NodeProgress NodeType = "progress"
	NodeToolbar  NodeType = "toolbar"
	NodeSep      NodeType = "sep"
	NodeCard     NodeType = "card"
	NodeForm     NodeType = "form"

	// Erweiterte Widgets
	NodeAccordion     NodeType = "accordion"
	NodeAccordionItem NodeType = "accordion-item"
	NodeSlider        NodeType = "slider"
	NodeHyperlink     NodeType = "hyperlink"
	NodeIcon          NodeType = "icon"
	NodeRichText      NodeType = "richtext"
	NodeSpan          NodeType = "span"

	// Collections
	NodeTable  NodeType = "table"
	NodeColumn NodeType = "column"
	NodeList   NodeType = "list"
	NodeTree   NodeType = "tree"
	NodeNode   NodeType = "node"

	NodeUnknown NodeType = "unknown"
)

// Node ist ein Knoten im x-DSL Baum.
//
// JSON-Tags (kompakt) für Speicherung in PostgreSQL JSONB (oos_dsl Tabelle):
//
//	t = type, a = attrs, x = text, c = children
//
// XMLName wird bei JSON nicht benötigt — Type reicht als Identifier.
type Node struct {
	XMLName  xml.Name          `json:"-"`
	Type     NodeType          `json:"t"`
	Attrs    map[string]string `json:"a,omitempty"`
	Text     string            `json:"x,omitempty"`
	Children []*Node           `json:"c,omitempty"`
}

// Attr gibt den Wert eines Attributs zurück, oder den Fallback.
func (n *Node) Attr(key, fallback string) string {
	if v, ok := n.Attrs[key]; ok {
		return v
	}
	return fallback
}

// AttrBool gibt true zurück wenn das Attribut "true" oder "1" ist.
func (n *Node) AttrBool(key string) bool {
	v := n.Attrs[key]
	return v == "true" || v == "1"
}

// AttrInt gibt den int-Wert eines Attributs zurück, oder den Fallback.
func (n *Node) AttrInt(key string, fallback int) int {
	if v, ok := n.Attrs[key]; ok {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

// AttrFloat gibt den float64-Wert eines Attributs zurück, oder den Fallback.
func (n *Node) AttrFloat(key string, fallback float64) float64 {
	if v, ok := n.Attrs[key]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
