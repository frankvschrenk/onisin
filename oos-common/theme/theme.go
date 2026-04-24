package theme

// theme.go — OOS Widget-Theme.
//
// Speicherformat: XML in oos.ctx[oos.theme]
//
//	<oos-theme variant="dark">
//	  <sizes text="14" padding="4" inner-padding="8"/>
//	  <colors warning="#d97706" error="#b91c1c" success="#16a34a"/>
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

// ── GlobalColors ──────────────────────────────────────────────────────────────

// GlobalColors are semantic colours that are not tied to a specific widget
// type. They drive Fyne's Warning/Error/Success/Hyperlink importance slots,
// so a widget.Button with Importance=WarningImportance (or any RichText with
// a hyperlink) picks up the themed value instead of Fyne's default.
//
// All fields are optional; an empty value falls through to the Fyne default
// for the active variant.
type GlobalColors struct {
	Warning   string
	Error     string
	Success   string
	Hyperlink string
}

// IsEmpty reports whether no global colour is set.
func (c GlobalColors) IsEmpty() bool {
	return c.Warning == "" && c.Error == "" && c.Success == "" && c.Hyperlink == ""
}

// ── OOSTheme ──────────────────────────────────────────────────────────────────

type OOSTheme struct {
	Variant string
	Sizes   GlobalSizes
	Colors  GlobalColors
	Widgets []WidgetTheme
}

// Palette constants — shared between the light and dark default themes
// and mirrored in the onisin.com landing page so the desktop client,
// the website and the documentation read as one product.
//
// The naming is deliberately neutral (brand / accent / ink / paper)
// so a single source of truth can feed both themes; the variant picks
// which shade of "paper" or "ink" a given widget renders against.
const (
	// Brand — deep indigo for primary actions, focus, selection.
	paletteBrand     = "#1e3a8a"
	paletteBrandSoft = "#3b5bdb"

	// Accent — warm amber for highlights and call-outs.
	paletteAccent     = "#d97706"
	paletteAccentSoft = "#fef3c7"

	// Semantic colours — used by GlobalColors for Fyne's importance slots.
	//
	// Warning is the accent amber on purpose: it's loud enough to catch
	// the eye on a destructive-confirm button and stays in-family with
	// the progress-bar fill, so the user reads a consistent "something
	// to notice" signal across the product.
	paletteWarning   = paletteAccent
	paletteError     = "#b91c1c"
	paletteSuccess   = "#16a34a"
	paletteHyperlink = "#1e40af"

	// Ink — text colours, darkest to faintest.
	paletteInk      = "#1f2937"
	paletteInkSoft  = "#4b5563"
	paletteInkFaint = "#6b7280"

	// Paper — light variant backgrounds.
	paperBg     = "#fafaf7"
	paperSoft   = "#f3f1ea"
	paperCard   = "#ffffff"
	paperRule   = "#e5e1d6"
	paperHeader = "#eceae1"

	// Slate — dark variant backgrounds.
	//
	// Five visible layers with enough contrast between them that the
	// shell, the cards and the surfaces on top of cards all read as
	// distinct without the user squinting. Warmer than pure zinc so
	// the look stays consistent with the warm paper of the light
	// variant (same hue family, just inverted brightness).
	//
	//   bg       shell background, window body
	//   soft     toolbars, sidebars, section headers
	//   card     forms, tables, lists — clearly lighter than bg
	//   elevated inputs on top of a card — clearly lighter than card
	//   header   table header band
	//   rule     dividers and input borders — clearly lighter still
	//
	// Luminance steps are 8–10 points apart so adjacent layers never
	// blur into each other, even on dim laptop panels.
	slateBg       = "#15171c"
	slateSoft     = "#1f2229"
	slateCard     = "#2a2f38"
	slateElevated = "#363c47"
	slateHeader   = "#3a4150"
	slateRule     = "#485163"

	// Inverted ink — readable on slate.
	slateInk      = "#e5e7eb"
	slateInkSoft  = "#b8bfcc"
	slateInkFaint = "#8892a0"
)

// DefaultTheme returns the built-in theme for the given variant
// ("light" or "dark"). Any other value falls back to "light".
//
// The theme is fully populated — every WidgetKind gets colours,
// radius and padding that match the onisin.com palette. This is the
// theme shown on first launch when oos.theme is empty and the one
// the desktop client falls back to if oosp cannot serve a theme.
func DefaultTheme(variant string) *OOSTheme {
	if variant == "dark" {
		return defaultDarkTheme()
	}
	return defaultLightTheme()
}

// defaultLightTheme — warm paper background, indigo brand, amber accent.
func defaultLightTheme() *OOSTheme {
	radius := "8"
	t := &OOSTheme{
		Variant: "light",
		Sizes: GlobalSizes{
			Text:         "14",
			Padding:      "6",
			InnerPadding: "10",
		},
		Colors: GlobalColors{
			Warning:   paletteWarning,
			Error:     paletteError,
			Success:   paletteSuccess,
			Hyperlink: paletteHyperlink,
		},
	}
	add := func(w WidgetTheme) { t.Widgets = append(t.Widgets, w) }

	add(WidgetTheme{Kind: KindButton, Background: paperCard, Foreground: paperCard, Primary: paletteBrand, Border: paletteBrand, Radius: radius})
	add(WidgetTheme{Kind: KindEntry, Background: paperCard, Foreground: paletteInk, Border: paperRule, Primary: paletteBrand, Radius: radius})
	add(WidgetTheme{Kind: KindTextArea, Background: paperCard, Foreground: paletteInk, Border: paperRule, Primary: paletteBrand, Radius: radius})
	add(WidgetTheme{Kind: KindLabel, Foreground: paletteInk})
	add(WidgetTheme{Kind: KindCard, Background: paperCard, Foreground: paletteInk, Border: paperRule, Radius: radius})
	add(WidgetTheme{Kind: KindSection, Background: paperSoft, Foreground: paletteInkSoft, Border: paperRule})
	add(WidgetTheme{Kind: KindForm, Background: paperBg, Foreground: paletteInk})
	add(WidgetTheme{Kind: KindTable, Background: paperCard, Foreground: paletteInk, Header: paperHeader, Border: paperRule})
	add(WidgetTheme{Kind: KindList, Background: paperCard, Foreground: paletteInk, Border: paperRule, Primary: paletteBrand})
	add(WidgetTheme{Kind: KindToolbar, Background: paperSoft, Foreground: paletteInkSoft, Border: paperRule})
	add(WidgetTheme{Kind: KindCheck, Foreground: paletteInk, Primary: paletteBrand})
	add(WidgetTheme{Kind: KindRadio, Foreground: paletteInk, Primary: paletteBrand})
	add(WidgetTheme{Kind: KindChoices, Background: paperCard, Foreground: paletteInk, Border: paperRule, Primary: paletteBrand, Radius: radius})
	add(WidgetTheme{Kind: KindSlider, Foreground: paletteInkSoft, Primary: paletteBrand})
	add(WidgetTheme{Kind: KindProgress, Foreground: paperRule, Primary: paletteAccent})

	return t
}

// defaultDarkTheme — slate backgrounds, same indigo brand for continuity.
func defaultDarkTheme() *OOSTheme {
	radius := "8"
	t := &OOSTheme{
		Variant: "dark",
		Sizes: GlobalSizes{
			Text:         "14",
			Padding:      "6",
			InnerPadding: "10",
		},
		Colors: GlobalColors{
			Warning:   paletteWarning,
			Error:     paletteError,
			Success:   paletteSuccess,
			Hyperlink: paletteHyperlink,
		},
	}
	add := func(w WidgetTheme) { t.Widgets = append(t.Widgets, w) }

	add(WidgetTheme{Kind: KindButton, Background: slateElevated, Foreground: "#ffffff", Primary: paletteBrandSoft, Border: paletteBrandSoft, Radius: radius})
	add(WidgetTheme{Kind: KindEntry, Background: slateElevated, Foreground: slateInk, Border: slateRule, Primary: paletteBrandSoft, Radius: radius})
	add(WidgetTheme{Kind: KindTextArea, Background: slateElevated, Foreground: slateInk, Border: slateRule, Primary: paletteBrandSoft, Radius: radius})
	add(WidgetTheme{Kind: KindLabel, Foreground: slateInk})
	add(WidgetTheme{Kind: KindCard, Background: slateCard, Foreground: slateInk, Border: slateRule, Radius: radius})
	add(WidgetTheme{Kind: KindSection, Background: slateSoft, Foreground: slateInkSoft, Border: slateRule})
	add(WidgetTheme{Kind: KindForm, Background: slateBg, Foreground: slateInk})
	add(WidgetTheme{Kind: KindTable, Background: slateCard, Foreground: slateInk, Header: slateHeader, Border: slateRule})
	add(WidgetTheme{Kind: KindList, Background: slateCard, Foreground: slateInk, Border: slateRule, Primary: paletteBrandSoft})
	add(WidgetTheme{Kind: KindToolbar, Background: slateSoft, Foreground: slateInkSoft, Border: slateRule})
	add(WidgetTheme{Kind: KindCheck, Foreground: slateInk, Primary: paletteBrandSoft})
	add(WidgetTheme{Kind: KindRadio, Foreground: slateInk, Primary: paletteBrandSoft})
	add(WidgetTheme{Kind: KindChoices, Background: slateElevated, Foreground: slateInk, Border: slateRule, Primary: paletteBrandSoft, Radius: radius})
	add(WidgetTheme{Kind: KindSlider, Foreground: slateInkFaint, Primary: paletteBrandSoft})
	add(WidgetTheme{Kind: KindProgress, Foreground: slateRule, Primary: paletteAccent})

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

	// <colors> — semantic global colours (warning / error / success / hyperlink)
	if !t.Colors.IsEmpty() {
		colors := root.CreateElement("colors")
		setAttrIfNotEmpty(colors, "warning", t.Colors.Warning)
		setAttrIfNotEmpty(colors, "error", t.Colors.Error)
		setAttrIfNotEmpty(colors, "success", t.Colors.Success)
		setAttrIfNotEmpty(colors, "hyperlink", t.Colors.Hyperlink)
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

	// <colors>
	if colors := root.SelectElement("colors"); colors != nil {
		t.Colors = GlobalColors{
			Warning:   colors.SelectAttrValue("warning", ""),
			Error:     colors.SelectAttrValue("error", ""),
			Success:   colors.SelectAttrValue("success", ""),
			Hyperlink: colors.SelectAttrValue("hyperlink", ""),
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
