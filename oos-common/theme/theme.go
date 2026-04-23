package theme

// theme.go — OOS Widget-Theme.
//
// Speicherformat: XML in oos.ctx[oos.theme]
//
//	<oos-theme variant="dark">
//	  <sizes text="14" padding="4" inner-padding="8"/>
//	  <widget type="button"   primary="#3d6fd4" foreground="#ffffff" radius="6"/>
//	  <widget type="entry"    background="#202023" border="#39393a"/>
//	  <widget type="card"     background="#1e1e24"/>
//	</oos-theme>

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

// ── Widget-Typen ──────────────────────────────────────────────────────────────

type WidgetKind string

const (
	KindButton   WidgetKind = "button"
	KindEntry    WidgetKind = "entry"
	KindTextArea WidgetKind = "textarea"
	KindLabel    WidgetKind = "label"
	KindCard     WidgetKind = "card"
	KindSection  WidgetKind = "section"
	KindTable    WidgetKind = "table"
	KindList     WidgetKind = "list"
	KindToolbar  WidgetKind = "toolbar"
	KindCheck    WidgetKind = "check"
	KindRadio    WidgetKind = "radio"
	KindChoices  WidgetKind = "choices"
	KindSlider   WidgetKind = "slider"
	KindProgress WidgetKind = "progress"
	KindForm     WidgetKind = "form"
)

var AllWidgetKinds = []WidgetKind{
	KindButton, KindEntry, KindTextArea, KindLabel,
	KindCard, KindSection, KindForm,
	KindTable, KindList, KindToolbar,
	KindCheck, KindRadio, KindChoices,
	KindSlider, KindProgress,
}

// ── WidgetTheme ───────────────────────────────────────────────────────────────

type WidgetTheme struct {
	Kind       WidgetKind
	Background string
	Foreground string
	Primary    string
	Border     string
	Header     string // table/list
	TextSize   string
	Radius     string
	Padding    string
}

func (w *WidgetTheme) IsEmpty() bool {
	return w.Background == "" && w.Foreground == "" && w.Primary == "" &&
		w.Border == "" && w.Header == "" &&
		w.TextSize == "" && w.Radius == "" && w.Padding == ""
}

// ── GlobalSizes ───────────────────────────────────────────────────────────────

type GlobalSizes struct {
	Text         string
	Padding      string
	InnerPadding string
}

// ── OOSTheme ──────────────────────────────────────────────────────────────────

type OOSTheme struct {
	Variant string
	Sizes   GlobalSizes
	Widgets []WidgetTheme
}

func DefaultTheme() *OOSTheme {
	t := &OOSTheme{Variant: "dark"}
	for _, k := range AllWidgetKinds {
		t.Widgets = append(t.Widgets, WidgetTheme{Kind: k})
	}
	return t
}

func (t *OOSTheme) ForWidget(kind WidgetKind) *WidgetTheme {
	for i := range t.Widgets {
		if t.Widgets[i].Kind == kind {
			return &t.Widgets[i]
		}
	}
	t.Widgets = append(t.Widgets, WidgetTheme{Kind: kind})
	return &t.Widgets[len(t.Widgets)-1]
}

// ── XML Serialisierung via etree ──────────────────────────────────────────────

// ToXML serialisiert das Theme als sauber formatiertes XML.
// Leere Attribute werden weggelassen, Widgets ohne Overrides erscheinen als
// self-closing Tags: <widget type="button"/>
func (t *OOSTheme) ToXML() (string, error) {
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)

	root := doc.CreateElement("oos-theme")
	root.CreateAttr("variant", t.Variant)

	// <sizes> — nur wenn mindestens ein Wert gesetzt
	if t.Sizes.Text != "" || t.Sizes.Padding != "" || t.Sizes.InnerPadding != "" {
		sizes := root.CreateElement("sizes")
		setAttrIfNotEmpty(sizes, "text", t.Sizes.Text)
		setAttrIfNotEmpty(sizes, "padding", t.Sizes.Padding)
		setAttrIfNotEmpty(sizes, "inner-padding", t.Sizes.InnerPadding)
	}

	// <widget> Einträge
	for _, w := range t.Widgets {
		el := root.CreateElement("widget")
		el.CreateAttr("type", string(w.Kind))
		setAttrIfNotEmpty(el, "background", w.Background)
		setAttrIfNotEmpty(el, "foreground", w.Foreground)
		setAttrIfNotEmpty(el, "primary", w.Primary)
		setAttrIfNotEmpty(el, "border", w.Border)
		setAttrIfNotEmpty(el, "header", w.Header)
		setAttrIfNotEmpty(el, "text-size", w.TextSize)
		setAttrIfNotEmpty(el, "radius", w.Radius)
		setAttrIfNotEmpty(el, "padding", w.Padding)
	}

	doc.Indent(2)
	out, err := doc.WriteToString()
	if err != nil {
		return "", fmt.Errorf("theme toXML: %w", err)
	}
	return out, nil
}

// ParseXML liest ein OOSTheme aus einem XML-String via etree.
func ParseXML(raw string) (*OOSTheme, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromString(raw); err != nil {
		return nil, fmt.Errorf("theme parseXML: %w", err)
	}

	root := doc.SelectElement("oos-theme")
	if root == nil {
		return nil, fmt.Errorf("kein <oos-theme> Element gefunden")
	}

	t := &OOSTheme{
		Variant: root.SelectAttrValue("variant", "dark"),
	}

	// <sizes>
	if sizes := root.SelectElement("sizes"); sizes != nil {
		t.Sizes = GlobalSizes{
			Text:         sizes.SelectAttrValue("text", ""),
			Padding:      sizes.SelectAttrValue("padding", ""),
			InnerPadding: sizes.SelectAttrValue("inner-padding", ""),
		}
	}

	// <widget> Einträge
	existing := make(map[WidgetKind]bool)
	for _, el := range root.SelectElements("widget") {
		kind := WidgetKind(el.SelectAttrValue("type", ""))
		if kind == "" {
			continue
		}
		wt := WidgetTheme{
			Kind:       kind,
			Background: el.SelectAttrValue("background", ""),
			Foreground: el.SelectAttrValue("foreground", ""),
			Primary:    el.SelectAttrValue("primary", ""),
			Border:     el.SelectAttrValue("border", ""),
			Header:     el.SelectAttrValue("header", ""),
			TextSize:   el.SelectAttrValue("text-size", ""),
			Radius:     el.SelectAttrValue("radius", ""),
			Padding:    el.SelectAttrValue("padding", ""),
		}
		t.Widgets = append(t.Widgets, wt)
		existing[kind] = true
	}

	// Fehlende Widget-Typen ergänzen
	for _, k := range AllWidgetKinds {
		if !existing[k] {
			t.Widgets = append(t.Widgets, WidgetTheme{Kind: k})
		}
	}

	return t, nil
}

// ── Hilfsfunktionen ───────────────────────────────────────────────────────────

func setAttrIfNotEmpty(el *etree.Element, key, val string) {
	if val != "" {
		el.CreateAttr(key, val)
	}
}

func ParseHex(s string) color.Color {
	s = strings.TrimPrefix(s, "#")
	if len(s) == 0 {
		return color.Transparent
	}
	if len(s) == 6 {
		s += "ff"
	}
	if len(s) != 8 {
		return color.Transparent
	}
	val, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.Transparent
	}
	return color.NRGBA{
		R: uint8(val >> 24),
		G: uint8(val >> 16),
		B: uint8(val >> 8),
		A: uint8(val),
	}
}

func ToHex(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

func ParseFloat32(s string) float32 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0
	}
	return float32(f)
}
