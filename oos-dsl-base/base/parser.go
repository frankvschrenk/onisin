package base

// parser.go — XML → *Node Baum.
//
// Liest eine x-DSL Datei (*.dsl.xml) und gibt den vollständigen Node-Baum zurück.
// Unbekannte Tags werden als NodeUnknown markiert — kein Fehler, kein Panic.

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// ParseBytes parst ein XML-Byte-Slice und gibt den vollständigen Node-Baum zurück.
// Praktisch wenn der Aufrufer das Roh-XML bereits im Speicher hat (z.B. für RawXML).
func ParseBytes(data []byte) (*Node, error) {
	return Parse(bytes.NewReader(data))
}

// Parse liest einen XML-Stream und gibt den vollständigen Node-Baum zurück.
func Parse(r io.Reader) (*Node, error) {
	dec := xml.NewDecoder(r)
	var root *Node
	var stack []*Node

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("oos-dsl parse: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			node := &Node{
				XMLName: t.Name,
				Type:    resolveType(t.Name.Local),
				Attrs:   extractAttrs(t.Attr),
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			}
			stack = append(stack, node)
			if root == nil {
				root = node
			}

		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}

		case xml.CharData:
			if text := strings.TrimSpace(string(t)); text != "" && len(stack) > 0 {
				stack[len(stack)-1].Text = text
			}
		}
	}

	if root == nil {
		return nil, fmt.Errorf("oos-dsl parse: leeres Dokument")
	}
	return root, nil
}

// resolveType wandelt einen XML-Tag-Namen in einen NodeType um.
// Unbekannte Tags werden als NodeUnknown markiert.
func resolveType(tag string) NodeType {
	switch NodeType(tag) {
	case NodeScreen,
		NodeBox, NodeGrid, NodeGridWrap, NodeBorder, NodeCenter, NodeStack,
		NodeTabs, NodeTab,
		NodeSection, NodeField,
		NodeLabel, NodeButton, NodeEntry, NodeTextArea,
		NodeChoices, NodeCheck, NodeRadio, NodeOption,
		NodeProgress, NodeToolbar, NodeSep, NodeCard, NodeForm,
		NodeAccordion, NodeAccordionItem,
		NodeSlider, NodeHyperlink, NodeIcon,
		NodeRichText, NodeSpan,
		NodeTable, NodeColumn, NodeList, NodeTree, NodeNode:
		return NodeType(tag)
	}
	return NodeUnknown
}

// extractAttrs wandelt XML-Attribute in eine map um.
func extractAttrs(xmlAttrs []xml.Attr) map[string]string {
	if len(xmlAttrs) == 0 {
		return nil
	}
	out := make(map[string]string, len(xmlAttrs))
	for _, a := range xmlAttrs {
		out[a.Name.Local] = a.Value
	}
	return out
}
