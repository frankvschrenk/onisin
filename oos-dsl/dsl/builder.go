// builder.go — *Node Baum → Fyne Widgets.
// fmtValue → fmtvalue.go | sectionTitle → section_title.go | Layouts → layouts.go
// Neue Widgets (Accordion, Slider, Hyperlink, Icon, RichText) → widgets_new.go
package dsl

import (
	"encoding/json"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type ActionFunc func(screenID, action string, state *State)

// Builder wandelt einen *Node-Baum in Fyne-Widgets um.
// focus ist ein shared Pointer — alle withCtx-Kopien schreiben ins selbe Ziel,
// so dass main.go nach dem Build das Fokus-Widget findet.
type Builder struct {
	state         *State
	screenID      string
	onAction      ActionFunc
	ctx           RenderContext
	focus         *fyne.Focusable // shared zwischen allen Builder-Instanzen
	themeProvider ThemeProvider   // optional — nil = kein Widget-Theme
}

func NewBuilder(state *State, screenID string, onAction ActionFunc) *Builder {
	var f fyne.Focusable
	return &Builder{
		state:    state,
		screenID: screenID,
		onAction: onAction,
		ctx:      DefaultRenderContext(),
		focus:    &f,
	}
}

// SetThemeProvider setzt den ThemeProvider für Widget-spezifische Themes.
// Muss vor Build() aufgerufen werden.
func (b *Builder) SetThemeProvider(tp ThemeProvider) {
	b.themeProvider = tp
}

// applyTheme wrапpt ein Widget mit container.NewThemeOverride wenn ein
// ThemeProvider gesetzt ist und ein Theme für diesen Widget-Typ vorhanden ist.
func (b *Builder) applyTheme(obj fyne.CanvasObject, kind string) fyne.CanvasObject {
	if b.themeProvider == nil {
		return obj
	}
	t := b.themeProvider.ThemeFor(kind)
	if t == nil {
		return obj
	}
	return container.NewThemeOverride(obj, t)
}

// FocusTarget gibt das erste Widget zurück das focus="true" hatte, oder nil.
func (b *Builder) FocusTarget() fyne.Focusable {
	if b.focus == nil {
		return nil
	}
	return *b.focus
}

// setFocus speichert das erste Fokus-Widget — alle anderen werden ignoriert.
func (b *Builder) setFocus(w fyne.Focusable) {
	if b.focus != nil && *b.focus == nil {
		*b.focus = w
	}
}

// withCtx gibt eine Kopie des Builders mit neuem RenderContext zurück.
// Der focus-Pointer wird geteilt — alle Kopien schreiben ins selbe Ziel.
func (b *Builder) withCtx(rc RenderContext) *Builder {
	return &Builder{
		state:         b.state,
		screenID:      b.screenID,
		onAction:      b.onAction,
		ctx:           rc,
		focus:         b.focus,
		themeProvider: b.themeProvider, // weitergeben an Kind-Builder
	}
}

// Build dispatcht einen Node an die zuständige build*-Funktion.
func (b *Builder) Build(node *Node) fyne.CanvasObject {
	switch node.Type {
	// --- Container & Layout ---
	case NodeScreen:
		return b.buildScreen(node)
	case NodeBox:
		return b.buildBox(node)
	case NodeGrid:
		return b.buildGrid(node)
	case NodeGridWrap:
		return b.buildGridWrap(node)
	case NodeBorder:
		return b.buildBorder(node)
	case NodeCenter:
		return b.buildCenter(node)
	case NodeStack:
		return b.buildStack(node)
	case NodeTabs:
		return b.buildTabs(node)
	case NodeCard:
		return b.buildCard(node)
	case NodeSection:
		return b.buildSection(node)
	case NodeField:
		return b.buildField(node)

	// --- Standard Widgets ---
	case NodeForm:
		return b.buildForm(node)
	case NodeLabel:
		return b.buildLabel(node)
	case NodeButton:
		return b.buildButton(node)
	case NodeEntry:
		return b.buildEntry(node)
	case NodeTextArea:
		return b.buildTextArea(node)
	case NodeChoices:
		return b.buildChoices(node)
	case NodeCheck:
		return b.buildCheck(node)
	case NodeRadio:
		return b.buildRadio(node)
	case NodeProgress:
		return b.buildProgress(node)
	case NodeToolbar:
		return b.buildToolbar(node)
	case NodeSep:
		return widget.NewSeparator()

	// --- Neue Widgets (implementiert in widgets_new.go) ---
	case NodeAccordion:
		return b.buildAccordion(node)
	case NodeSlider:
		return b.buildSlider(node)
	case NodeHyperlink:
		return b.buildHyperlink(node)
	case NodeIcon:
		return b.buildIcon(node)
	case NodeRichText:
		return b.buildRichText(node)

	// --- Collections ---
	case NodeTable:
		return b.buildTable(node)
	case NodeList:
		return b.buildList(node)
	case NodeTree:
		return b.buildTree(node)

	default:
		return widget.NewLabel("? " + string(node.Type))
	}
}

// ============================================================================
// Containers & Layout
// ============================================================================

func (b *Builder) buildScreen(node *Node) fyne.CanvasObject {
	// Screen-weite Format-Defaults: cur + locale
	screenCtx := b.ctx.
		WithLabelColor(node.Attr("label-color", "")).
		WithLocale(node.Attr("locale", ""), node.Attr("cur", ""))
	child := b.withCtx(screenCtx)
	expandIdx := -1
	for i, c := range node.Children {
		if c.Type == NodeTable || c.Type == NodeList || c.Type == NodeTree {
			expandIdx = i
			break
		}
	}
	if expandIdx < 0 {
		return child.vbox(node.Children)
	}
	top := child.vbox(node.Children[:expandIdx])
	center := child.Build(node.Children[expandIdx])
	bottom := child.vbox(node.Children[expandIdx+1:])
	return container.NewBorder(top, bottom, nil, nil, center)
}

func (b *Builder) buildBox(node *Node) fyne.CanvasObject {
	var obj fyne.CanvasObject
	if node.Attr("orient", "vertical") == "horizontal" {
		// expand="true" auf einem Kind → dieses Kind bekommt den gesamten
		// restlichen Platz (via container.NewBorder). Ohne expand: NewHBox.
		expandIdx := -1
		for i, c := range node.Children {
			if c.AttrBool("expand") {
				expandIdx = i
				break
			}
		}
		if expandIdx >= 0 {
			// Alle Kinder vor dem expand-Kind → left-side, danach → right-side
			// Einfachste Variante: alles links vom expand-Kind als HBox, expand-Kind im Center
			var left []fyne.CanvasObject
			for _, c := range node.Children[:expandIdx] {
				left = append(left, b.Build(c))
			}
			center := b.Build(node.Children[expandIdx])
			var leftObj fyne.CanvasObject
			if len(left) == 1 {
				leftObj = left[0]
			} else if len(left) > 1 {
				leftObj = container.NewHBox(left...)
			}
			obj = container.NewBorder(nil, nil, leftObj, nil, center)
		} else {
			children := b.buildChildren(node)
			obj = alignH(children, node.Attr("align", "left"))
		}
	} else {
		children := b.buildChildren(node)
		obj = container.NewVBox(children...)
	}
	return applySpacing(obj, node)
}

func (b *Builder) buildGrid(node *Node) fyne.CanvasObject {
	cols, _ := strconv.Atoi(node.Attr("cols", "2"))
	gap := unitToPx(node.Attr("gap", ""), 0)
	return applySpacing(makeGapGrid(cols, gap, b.buildChildren(node)), node)
}

func (b *Builder) buildGridWrap(node *Node) fyne.CanvasObject {
	minW, _ := strconv.ParseFloat(node.Attr("minw", "200"), 32)
	return applySpacing(
		container.NewGridWrap(fyne.NewSize(float32(minW), 0), b.buildChildren(node)...),
		node,
	)
}

func (b *Builder) buildBorder(node *Node) fyne.CanvasObject {
	slots := map[string]fyne.CanvasObject{
		"top": nil, "bottom": nil, "left": nil, "right": nil, "center": nil,
	}
	for _, child := range node.Children {
		pos := child.Attr("position", "center")
		obj := b.Build(child)
		if pos == "left" || pos == "right" {
			obj = container.NewPadded(obj)
		}
		slots[pos] = obj
	}
	border := container.NewBorder(
		slots["top"], slots["bottom"], slots["left"], slots["right"],
		slots["center"],
	)
	minH, _ := strconv.ParseFloat(node.Attr("minheight", "120"), 32)
	return container.NewStack(newMinHeightSpacer(float32(minH)), border)
}

func (b *Builder) buildCenter(node *Node) fyne.CanvasObject {
	if len(node.Children) == 0 {
		return widget.NewLabel("")
	}
	return container.NewCenter(b.Build(node.Children[0]))
}

func (b *Builder) buildStack(node *Node) fyne.CanvasObject {
	return container.NewStack(b.buildChildren(node)...)
}

func (b *Builder) buildTabs(node *Node) fyne.CanvasObject {
	var items []*container.TabItem
	for _, child := range node.Children {
		if child.Type != NodeTab {
			continue
		}
		label := child.Attr("label", "Tab")
		var content fyne.CanvasObject
		if len(child.Children) == 1 {
			content = b.Build(child.Children[0])
		} else {
			content = b.vbox(child.Children)
		}
		items = append(items, container.NewTabItem(label, content))
	}
	return container.NewAppTabs(items...)
}

func (b *Builder) buildCard(node *Node) fyne.CanvasObject {
	var content fyne.CanvasObject
	if len(node.Children) > 0 {
		content = b.vbox(node.Children)
	}
	return b.applyTheme(widget.NewCard(node.Attr("title", ""), node.Attr("subtitle", ""), content), "card")
}

// ============================================================================
// Section + Field
// ============================================================================

func (b *Builder) buildSection(node *Node) fyne.CanvasObject {
	child := b.withCtx(b.ctx.WithLabelColor(node.Attr("label-color", "")))
	cols := node.AttrInt("cols", 1)
	gap := unitToPx(node.Attr("gap", ""), 0)

	var rows []fyne.CanvasObject
	if label := node.Attr("label", ""); label != "" {
		rows = append(rows, sectionTitle(label, child.ctx.LabelColor))
	}

	var fieldBuf []fyne.CanvasObject
	flush := func() {
		if len(fieldBuf) == 0 {
			return
		}
		if cols <= 1 {
			rows = append(rows, container.NewVBox(fieldBuf...))
		} else {
			rows = append(rows, makeGapGrid(cols, gap, fieldBuf))
		}
		fieldBuf = nil
	}

	for _, c := range node.Children {
		if c.Type == NodeField {
			fieldBuf = append(fieldBuf, child.buildField(c))
		} else {
			flush()
			rows = append(rows, child.Build(c))
		}
	}
	flush()

	return b.applyTheme(applySpacing(container.NewVBox(rows...), node), "section")
}

// buildField — Label oben + Widget darunter.
// focus="true" → Widget bekommt initialen Fokus beim Öffnen des Screens.
// widget="entry|textarea|choices|radio|check|slider" (default: entry)
func (b *Builder) buildField(node *Node) fyne.CanvasObject {
	col := b.ctx.WithLabelColor(node.Attr("label-color", "")).LabelColor
	lbl := canvas.NewText(node.Attr("label", ""), col)
	lbl.TextSize = 11

	var w fyne.CanvasObject
	switch node.Attr("widget", "entry") {
	case "textarea":
		w = b.buildTextArea(node)
	case "choices":
		w = b.buildChoices(node)
	case "radio":
		w = b.buildRadio(node)
	case "check":
		w = b.buildCheck(node)
	case "slider":
		w = b.buildSlider(node)
	default:
		w = b.buildEntry(node)
	}

	if node.AttrBool("focus") {
		if f, ok := w.(fyne.Focusable); ok {
			b.setFocus(f)
		}
	}

	return applySpacing(container.NewVBox(lbl, w), node)
}

// ============================================================================
// Standard Widgets
// ============================================================================

func (b *Builder) buildForm(node *Node) fyne.CanvasObject {
	form := widget.NewForm()
	for _, child := range node.Children {
		if child.Type == NodeSep {
			form.Append("", widget.NewSeparator())
			continue
		}
		form.Append(child.Attr("label", ""), b.Build(child))
	}
	return b.applyTheme(applySpacing(form, node), "form")
}

func (b *Builder) buildLabel(node *Node) fyne.CanvasObject {
	lbl := widget.NewLabel(node.Attr("text", node.Text))
	switch node.Attr("style", "") {
	case "bold":
		lbl.TextStyle = fyne.TextStyle{Bold: true}
	case "italic":
		lbl.TextStyle = fyne.TextStyle{Italic: true}
	case "mono":
		lbl.TextStyle = fyne.TextStyle{Monospace: true}
	}
	if node.Attr("wrap", "false") == "true" {
		lbl.Wrapping = fyne.TextWrapWord
	} else {
		lbl.Wrapping = fyne.TextTruncate
	}
	return b.applyTheme(lbl, "label")
}

func (b *Builder) buildButton(node *Node) fyne.CanvasObject {
	label := node.Attr("label", "OK")
	action := node.Attr("action", "click")
	btn := widget.NewButtonWithIcon(label, buttonIcon(node.Attr("style", "")), func() {
		if b.onAction != nil {
			b.onAction(b.screenID, action, b.state)
		}
	})
	return b.applyTheme(btn, "button")
}

func (b *Builder) buildEntry(node *Node) fyne.CanvasObject {
	entry := widget.NewEntry()
	entry.SetPlaceHolder(node.Attr("placeholder", ""))
	readonly := node.AttrBool("readonly")
	if readonly {
		entry.Disable()
	}
	if bp := node.Attr("bind", ""); bp != "" {
		raw := b.state.Get(bp)
		if raw != "" {
			if readonly {
				entry.SetText(FormatDisplay(raw, node.Attr("format", ""), b.ctx))
			} else {
				entry.SetText(raw)
			}
		}
		entry.OnChanged = func(v string) { b.state.Set(bp, v) }
	}
	return b.applyTheme(entry, "entry")
}

func (b *Builder) buildTextArea(node *Node) fyne.CanvasObject {
	entry := widget.NewMultiLineEntry()
	entry.SetPlaceHolder(node.Attr("placeholder", ""))
	entry.Wrapping = fyne.TextWrapWord
	if node.AttrBool("readonly") {
		entry.Disable()
	}
	if bp := node.Attr("bind", ""); bp != "" {
		if v := b.state.Get(bp); v != "" {
			entry.SetText(v)
		}
		entry.OnChanged = func(v string) { b.state.Set(bp, v) }
	}
	return b.applyTheme(entry, "textarea")
}

func (b *Builder) buildChoices(node *Node) fyne.CanvasObject {
	bp := node.Attr("bind", "")
	v2l, l2v, labels := b.resolveOptions(node)
	sel := widget.NewSelect(labels, func(lbl string) {
		if bp == "" {
			return
		}
		if val, ok := l2v[lbl]; ok {
			b.state.Set(bp, val)
		} else {
			b.state.Set(bp, lbl)
		}
	})
	if bp != "" {
		if cur := b.state.Get(bp); cur != "" {
			if lbl, ok := v2l[cur]; ok {
				sel.SetSelected(lbl)
			} else {
				sel.SetSelected(cur)
			}
		}
	}
	return b.applyTheme(sel, "choices")
}

func (b *Builder) buildCheck(node *Node) fyne.CanvasObject {
	bp := node.Attr("bind", "")
	check := widget.NewCheck(node.Attr("label", ""), func(checked bool) {
		if bp == "" {
			return
		}
		if checked {
			b.state.Set(bp, "true")
		} else {
			b.state.Set(bp, "false")
		}
	})
	if bp != "" {
		check.SetChecked(b.state.Get(bp) == "true")
	}
	return b.applyTheme(check, "check")
}

func (b *Builder) buildRadio(node *Node) fyne.CanvasObject {
	bp := node.Attr("bind", "")
	v2l, l2v, labels := b.resolveOptions(node)

	radio := widget.NewRadioGroup(labels, func(lbl string) {
		if bp == "" {
			return
		}
		if val, ok := l2v[lbl]; ok {
			b.state.Set(bp, val)
		} else {
			b.state.Set(bp, lbl)
		}
	})

	if node.Attr("orient", "") == "horizontal" {
		radio.Horizontal = true
	}

	if bp != "" {
		if cur := b.state.Get(bp); cur != "" {
			if lbl, ok := v2l[cur]; ok {
				radio.SetSelected(lbl)
				radio.Refresh()
			}
		}
	}
	return b.applyTheme(radio, "radio")
}

func (b *Builder) buildProgress(node *Node) fyne.CanvasObject {
	bar := widget.NewProgressBar()
	val := ""
	if bp := node.Attr("bind", ""); bp != "" {
		val = b.state.Get(bp)
	}
	if val == "" {
		val = node.Attr("value", "")
	}
	if val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			bar.SetValue(f)
		}
	}
	return b.applyTheme(bar, "progress")
}

func (b *Builder) buildToolbar(node *Node) fyne.CanvasObject {
	var items []widget.ToolbarItem
	for _, child := range node.Children {
		switch child.Type {
		case NodeButton:
			act := child.Attr("action", "click")
			items = append(items, widget.NewToolbarAction(
				buttonIcon(child.Attr("style", "")),
				func() {
					if b.onAction != nil {
						b.onAction(b.screenID, act, b.state)
					}
				},
			))
		case NodeSep:
			items = append(items, widget.NewToolbarSeparator())
		}
	}
	return b.applyTheme(widget.NewToolbar(items...), "toolbar")
}

// ============================================================================
// Collections
// ============================================================================

func (b *Builder) buildTable(node *Node) fyne.CanvasObject {
	type col struct {
		label, field, format string
		width                float32
	}
	var cols []col
	for _, child := range node.Children {
		if child.Type == NodeColumn {
			w, _ := strconv.ParseFloat(child.Attr("width", "200"), 32)
			cols = append(cols, col{
				label:  child.Attr("label", child.Attr("field", "?")),
				field:  child.Attr("field", ""),
				format: child.Attr("format", ""),
				width:  float32(w),
			})
		}
	}
	if len(cols) == 0 {
		return widget.NewLabel("table: keine Spalten")
	}
	rows := loadTableRows(b.state.Get(node.Attr("bind", "")))
	action := node.Attr("action", "select")
	ctx := b.ctx // RenderContext für Formatierung

	t := widget.NewTable(
		func() (int, int) { return len(rows) + 1, len(cols) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			lbl := obj.(*widget.Label)
			if id.Row == 0 {
				lbl.TextStyle = fyne.TextStyle{Bold: true}
				lbl.SetText(cols[id.Col].label)
				return
			}
			lbl.TextStyle = fyne.TextStyle{}
			if r := id.Row - 1; r < len(rows) {
				raw := rows[r][cols[id.Col].field]
				lbl.SetText(FormatDisplay(raw, cols[id.Col].format, ctx))
			}
		},
	)
	t.OnSelected = func(id widget.TableCellID) {
		if id.Row == 0 {
			return
		}
		if r := id.Row - 1; r < len(rows) {
			for k, v := range rows[r] {
				b.state.Set("selected."+k, v)
			}
			if b.onAction != nil {
				b.onAction(b.screenID, action, b.state)
			}
		}
	}
	for i, c := range cols {
		t.SetColumnWidth(i, c.width)
	}
	return t
}

func (b *Builder) buildList(node *Node) fyne.CanvasObject {
	rows := loadTableRows(b.state.Get(node.Attr("bind", "")))
	field := node.Attr("field", "")
	action := node.Attr("action", "select")

	list := widget.NewList(
		func() int { return len(rows) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			lbl := obj.(*widget.Label)
			if id >= len(rows) {
				return
			}
			if field != "" {
				lbl.SetText(rows[id][field])
			} else {
				for _, v := range rows[id] {
					lbl.SetText(v)
					break
				}
			}
		},
	)
	list.OnSelected = func(id widget.ListItemID) {
		if id >= len(rows) {
			return
		}
		for k, v := range rows[id] {
			b.state.Set("selected."+k, v)
		}
		if b.onAction != nil {
			b.onAction(b.screenID, action, b.state)
		}
	}
	return list
}

func (b *Builder) buildTree(node *Node) fyne.CanvasObject {
	action := node.Attr("action", "select")
	childMap := make(map[string][]string)
	labelMap := make(map[string]string)
	for _, child := range node.Children {
		if child.Type == NodeNode {
			id := child.Attr("id", child.Text)
			parent := child.Attr("parent", "")
			labelMap[id] = child.Attr("label", child.Text)
			childMap[parent] = append(childMap[parent], id)
		}
	}
	tree := widget.NewTree(
		func(id widget.TreeNodeID) []widget.TreeNodeID { return childMap[id] },
		func(id widget.TreeNodeID) bool { return len(childMap[id]) > 0 },
		func(bool) fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TreeNodeID, _ bool, obj fyne.CanvasObject) {
			obj.(*widget.Label).SetText(labelMap[id])
		},
	)
	tree.OnSelected = func(id widget.TreeNodeID) {
		b.state.Set("selected.id", id)
		b.state.Set("selected.label", labelMap[id])
		if b.onAction != nil {
			b.onAction(b.screenID, action, b.state)
		}
	}
	return tree
}

// ============================================================================
// Hilfsfunktionen
// ============================================================================

func (b *Builder) resolveOptions(node *Node) (v2l, l2v map[string]string, labels []string) {
	v2l = make(map[string]string)
	l2v = make(map[string]string)
	if key := node.Attr("options", ""); key != "" {
		for _, e := range b.state.GetOptions(key) {
			labels = append(labels, e.Label)
			v2l[e.Value] = e.Label
			l2v[e.Label] = e.Value
		}
	}
	if len(labels) == 0 {
		for _, child := range node.Children {
			if child.Type != NodeOption {
				continue
			}
			val := child.Attr("value", child.Text)
			lbl := child.Text
			if lbl == "" {
				lbl = val
			}
			labels = append(labels, lbl)
			v2l[val] = lbl
			l2v[lbl] = val
		}
	}
	return
}

func (b *Builder) buildChildren(node *Node) []fyne.CanvasObject {
	out := make([]fyne.CanvasObject, 0, len(node.Children))
	for _, child := range node.Children {
		out = append(out, b.Build(child))
	}
	return out
}

// vbox stacks the given nodes vertically and never returns nil — an
// empty input still yields a valid empty *fyne.Container so callers
// can hand the result straight to layouts (Stack, Border, VScroll)
// without a nil-check. Returning nil here used to crash the renderer
// when an empty <screen/> skeleton was previewed: VScroll wrapped a
// nil content, and the first Refresh dereferenced inside Fyne's
// scrollContainerRenderer.Layout.
func (b *Builder) vbox(nodes []*Node) fyne.CanvasObject {
	children := make([]fyne.CanvasObject, 0, len(nodes))
	for _, n := range nodes {
		if obj := b.Build(n); obj != nil {
			children = append(children, obj)
		}
	}
	return container.NewVBox(children...)
}

func withPadding(obj fyne.CanvasObject, p string) fyne.CanvasObject {
	switch p {
	case "all":
		return container.NewPadded(obj)
	case "top":
		return container.NewVBox(layout.NewSpacer(), obj)
	case "bottom":
		return container.NewVBox(obj, layout.NewSpacer())
	case "vertical":
		return container.NewVBox(layout.NewSpacer(), obj, layout.NewSpacer())
	case "horizontal":
		return container.NewHBox(layout.NewSpacer(), obj, layout.NewSpacer())
	}
	return obj
}

func alignH(children []fyne.CanvasObject, align string) fyne.CanvasObject {
	switch align {
	case "right":
		return container.NewHBox(append([]fyne.CanvasObject{layout.NewSpacer()}, children...)...)
	case "center":
		all := []fyne.CanvasObject{layout.NewSpacer()}
		all = append(all, children...)
		return container.NewHBox(append(all, layout.NewSpacer())...)
	}
	return container.NewHBox(children...)
}

func loadTableRows(jsonStr string) []map[string]string {
	if jsonStr == "" {
		return nil
	}
	var raw []map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil
	}
	rows := make([]map[string]string, 0, len(raw))
	for _, item := range raw {
		row := make(map[string]string, len(item))
		for k, v := range item {
			row[k] = fmtValue(v)
		}
		rows = append(rows, row)
	}
	return rows
}
