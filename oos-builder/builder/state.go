// Package builder holds the in-memory state, mutation API and XML
// round-trip used by the visual DSL builder.
//
// The model is an *base.Node tree (the same node type the renderer
// consumes), wrapped in a Tree value that carries the current
// selection plus a parent index for cheap "where am I?" lookups.
//
// Design choice: the tree is the source of truth. XML is generated
// from the tree on save and parsed into a tree on load. We never run
// the editor against an XML buffer between mouse events, because that
// would lose ordering and whitespace and turn every property change
// into a full re-parse. See session-2026-04-27 in memory for the
// reasoning.
package builder

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	base "onisin.com/oos-dsl-base/base"
)

// Tree is the editable document plus the metadata the UI needs to
// move around it.
//
// The zero value is unusable — always construct via NewTree, NewEmpty
// or LoadXML.
type Tree struct {
	Root     *base.Node
	parents  map[*base.Node]*base.Node
	Selected *base.Node

	// listeners are called whenever the tree mutates so panels can
	// refresh themselves. Add via OnChange.
	listeners []func()
}

// NewEmpty returns a tree with a fresh <screen id="new_screen"/> root
// and that root selected. Useful as the starting state when the
// builder is opened without prior XML.
func NewEmpty(rootID string) *Tree {
	if rootID == "" {
		rootID = "new_screen"
	}
	root := &base.Node{
		Type:  base.NodeScreen,
		Attrs: map[string]string{"id": rootID},
	}
	t := &Tree{Root: root}
	t.rebuildParents()
	t.Selected = root
	return t
}

// LoadXML parses src and returns a tree rooted at the parsed node. An
// empty src is treated as "create a fresh skeleton" — the caller
// usually wants that anyway when opening on a blank document.
func LoadXML(src string, fallbackID string) (*Tree, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return NewEmpty(fallbackID), nil
	}
	root, err := base.Parse(bytes.NewReader([]byte(src)))
	if err != nil {
		return nil, fmt.Errorf("parse dsl: %w", err)
	}
	if root == nil {
		return NewEmpty(fallbackID), nil
	}
	t := &Tree{Root: root}
	t.rebuildParents()
	t.Selected = root
	return t, nil
}

// OnChange registers a callback fired after every mutation. Callbacks
// run on the goroutine that triggered the mutation; UI consumers must
// dispatch back to the Fyne thread themselves (fyne.Do).
func (t *Tree) OnChange(fn func()) {
	t.listeners = append(t.listeners, fn)
}

// notify runs every registered listener. Internal — every mutation
// must call this exactly once at the end.
func (t *Tree) notify() {
	for _, fn := range t.listeners {
		fn()
	}
}

// Parent returns the parent node of n, or nil for the root.
func (t *Tree) Parent(n *base.Node) *base.Node {
	if t == nil || n == nil {
		return nil
	}
	return t.parents[n]
}

// Select sets the selection to n. nil clears the selection. The
// listeners fire so the properties panel and tree highlight refresh.
func (t *Tree) Select(n *base.Node) {
	if t.Selected == n {
		return
	}
	t.Selected = n
	t.notify()
}

// AppendChild adds child as the last child of parent. Both must be
// non-nil; child must not already live in the tree.
//
// Returns true on success. The new child becomes the selection so
// the next property edit operates on it — that is what the user
// expects when they drop a widget from the palette onto a container.
func (t *Tree) AppendChild(parent, child *base.Node) bool {
	if parent == nil || child == nil {
		return false
	}
	parent.Children = append(parent.Children, child)
	t.parents[child] = parent
	t.indexSubtree(child, parent)
	t.Selected = child
	t.notify()
	return true
}

// Remove detaches n from the tree. The root cannot be removed; the
// call is a silent no-op in that case.
//
// If the removed subtree contained the selection, the selection
// shifts to the parent so the UI is never left without a focused
// node.
func (t *Tree) Remove(n *base.Node) {
	if n == nil || n == t.Root {
		return
	}
	parent := t.parents[n]
	if parent == nil {
		return
	}
	for i, c := range parent.Children {
		if c == n {
			parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
			break
		}
	}
	// Selection check before dropping the parent index — isAncestor
	// walks via t.parents, so the subtree must still be indexed.
	selectionGone := t.Selected == n || t.isAncestor(n, t.Selected)
	t.dropSubtree(n)
	if selectionGone {
		t.Selected = parent
	}
	t.notify()
}

// MoveBefore relocates n so it sits immediately before sibling. n and
// sibling must share a parent and neither may be the root.
//
// Used for the tree panel's drag-reorder. Returns true on success.
func (t *Tree) MoveBefore(n, sibling *base.Node) bool {
	if n == nil || sibling == nil || n == sibling {
		return false
	}
	parent := t.parents[n]
	if parent == nil || t.parents[sibling] != parent {
		return false
	}
	// Detach n.
	for i, c := range parent.Children {
		if c == n {
			parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
			break
		}
	}
	// Reinsert at sibling's index.
	for i, c := range parent.Children {
		if c == sibling {
			parent.Children = append(parent.Children[:i], append([]*base.Node{n}, parent.Children[i:]...)...)
			t.notify()
			return true
		}
	}
	// sibling vanished — append to keep the tree sane.
	parent.Children = append(parent.Children, n)
	t.notify()
	return true
}

// SetAttr writes a key/value pair on n. Empty value deletes the
// attribute (mirrors how the renderer treats absent vs empty
// attributes — most are absent-or-set).
func (t *Tree) SetAttr(n *base.Node, key, value string) {
	if n == nil || key == "" {
		return
	}
	if n.Attrs == nil {
		n.Attrs = map[string]string{}
	}
	if value == "" {
		delete(n.Attrs, key)
	} else {
		n.Attrs[key] = value
	}
	t.notify()
}

// rebuildParents walks the tree and rebuilds the parent index from
// scratch. Cheap (O(n)) and safer than maintaining the index across
// every external Children mutation.
func (t *Tree) rebuildParents() {
	t.parents = map[*base.Node]*base.Node{}
	t.indexSubtree(t.Root, nil)
}

func (t *Tree) indexSubtree(n, parent *base.Node) {
	if n == nil {
		return
	}
	t.parents[n] = parent
	for _, c := range n.Children {
		t.indexSubtree(c, n)
	}
}

func (t *Tree) dropSubtree(n *base.Node) {
	if n == nil {
		return
	}
	delete(t.parents, n)
	for _, c := range n.Children {
		t.dropSubtree(c)
	}
}

func (t *Tree) isAncestor(a, b *base.Node) bool {
	for cur := b; cur != nil; cur = t.parents[cur] {
		if cur == a {
			return true
		}
	}
	return false
}

// MarshalXML serialises the tree into a pretty-printed XML document
// with a leading prolog. Output is stable: attributes for the same
// node always appear in the same order so diffing two saves of the
// same tree is meaningful.
func (t *Tree) MarshalXML() (string, error) {
	if t == nil || t.Root == nil {
		return "", fmt.Errorf("empty tree")
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	if err := writeNode(&buf, t.Root, 0); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// writeNode emits one node and its descendants with indent-aware
// pretty printing. Standard library xml.Encoder is not used here
// because it has no opinion on attribute order and re-quotes every
// attribute with double quotes regardless of input — both of which
// would produce noisy diffs against hand-written *.dsl.xml files.
func writeNode(w io.Writer, n *base.Node, depth int) error {
	if n == nil {
		return nil
	}
	indent := strings.Repeat("  ", depth)
	tag := nodeTagName(n)
	if tag == "" {
		// Unknown node type — skip silently.
		return nil
	}

	if _, err := fmt.Fprintf(w, "%s<%s", indent, tag); err != nil {
		return err
	}
	for _, k := range sortedAttrKeys(n.Attrs) {
		v := n.Attrs[k]
		if _, err := fmt.Fprintf(w, ` %s=%q`, k, v); err != nil {
			return err
		}
	}

	hasChildren := len(n.Children) > 0
	hasText := strings.TrimSpace(n.Text) != ""

	if !hasChildren && !hasText {
		_, err := fmt.Fprint(w, "/>\n")
		return err
	}

	if _, err := fmt.Fprint(w, ">"); err != nil {
		return err
	}
	if hasText && !hasChildren {
		_, err := fmt.Fprintf(w, "%s</%s>\n", xmlEscape(n.Text), tag)
		return err
	}
	if _, err := fmt.Fprint(w, "\n"); err != nil {
		return err
	}
	for _, c := range n.Children {
		if err := writeNode(w, c, depth+1); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "%s</%s>\n", indent, tag)
	return err
}

// xmlEscape produces minimally-escaped attribute/text content. Encoder
// equivalents are too aggressive (escape every quote variant); the
// renderer is happy with the basic five.
func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return r.Replace(s)
}

// nodeTagName returns the XML tag for n. NodeType is directly the tag
// string (see oos-dsl-base/base/node.go), so the cast is enough for
// known types. For NodeUnknown we fall back to the original XMLName
// captured during parsing — important for round-trips where the input
// contained a tag the parser did not recognise.
func nodeTagName(n *base.Node) string {
	if n.Type != base.NodeUnknown {
		return string(n.Type)
	}
	if n.XMLName.Local != "" {
		return n.XMLName.Local
	}
	return ""
}

func sortedAttrKeys(attrs map[string]string) []string {
	if len(attrs) == 0 {
		return nil
	}
	// Stable, conventional order: id and label first, then everything
	// else alphabetically. Mirrors how humans hand-write the DSL.
	priority := []string{"id", "label", "title", "bind"}
	seen := map[string]bool{}
	out := make([]string, 0, len(attrs))
	for _, k := range priority {
		if _, ok := attrs[k]; ok {
			out = append(out, k)
			seen[k] = true
		}
	}
	rest := make([]string, 0, len(attrs))
	for k := range attrs {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	// Simple insertion sort to avoid pulling in sort for so few items.
	for i := 1; i < len(rest); i++ {
		for j := i; j > 0 && rest[j-1] > rest[j]; j-- {
			rest[j-1], rest[j] = rest[j], rest[j-1]
		}
	}
	return append(out, rest...)
}
