package theme

// fyne_theme.go — fyne.Theme Implementierungen für OOSTheme.
//
// GlobalFyneTheme: ein fyne.Theme für den gesamten Screen.
//   Wendet Variant (dark/light) + globale Sizes an.
//   Verwendet keine Widget-spezifischen Overrides — die werden via
//   container.NewThemeOverride pro Widget-Typ angewendet (späterer Schritt).
//
// WidgetFyneTheme: ein fyne.Theme für einen einzelnen Widget-Typ.
//   Wird vom Builder via container.NewThemeOverride verwendet.

import (
	"image/color"

	"fyne.io/fyne/v2"
	fyneTheme "fyne.io/fyne/v2/theme"
)

// ── GlobalFyneTheme ───────────────────────────────────────────────────────────

// GlobalFyneTheme implementiert fyne.Theme für einen gesamten OOSTheme.
// Wendet Variante + globale Größen an.
// Widget-spezifische Farben werden hier NICHT angewendet —
// das übernimmt WidgetFyneTheme via container.NewThemeOverride.
type GlobalFyneTheme struct {
	xtheme *OOSTheme
	base   fyne.Theme
}

// NewGlobalFyneTheme erstellt ein fyne.Theme aus einem OOSTheme.
// Kann direkt an fyne.App.Settings().SetTheme() übergeben werden,
// oder als Basis für container.NewThemeOverride.
func NewGlobalFyneTheme(xtheme *OOSTheme) fyne.Theme {
	return &GlobalFyneTheme{
		xtheme: xtheme,
		base:   fyneTheme.DefaultTheme(),
	}
}

func (t *GlobalFyneTheme) variant() fyne.ThemeVariant {
	if t.xtheme.Variant == "light" {
		return fyneTheme.VariantLight
	}
	return fyneTheme.VariantDark
}

// Color resolves a Fyne colour slot against the OOS theme.
//
// The OOS theme has no dedicated "global" colour block — globals are
// lifted from the widgets whose role naturally defines them. The
// mapping is conservative: only slots that really are app-wide
// (background, foreground, primary, separator, overlay) are overridden;
// widget-local colours (input backgrounds, button faces, table headers)
// are still applied per-widget via WidgetFyneTheme + NewThemeOverride.
//
// Any slot that the theme does not cover falls through to the Fyne
// default for the selected variant, so unstyled parts of the app keep
// a sensible look.
func (t *GlobalFyneTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	variant := t.variant()

	pick := func(hex string) color.Color {
		c := ParseHex(hex)
		if c == color.Transparent {
			return t.base.Color(name, variant)
		}
		return c
	}

	switch name {
	case fyneTheme.ColorNameBackground:
		if w := t.widget(KindForm); w != nil && w.Background != "" {
			return pick(w.Background)
		}
	case fyneTheme.ColorNameForeground:
		if w := t.widget(KindLabel); w != nil && w.Foreground != "" {
			return pick(w.Foreground)
		}
	case fyneTheme.ColorNamePrimary,
		fyneTheme.ColorNameFocus:
		if w := t.widget(KindButton); w != nil && w.Primary != "" {
			return pick(w.Primary)
		}
	case fyneTheme.ColorNameSelection:
		// Selection is rendered as a filled rectangle under the row/cell.
		// The full-opacity brand colour drowns the text; Fyne's default
		// uses ~30% alpha of the primary, we match that convention so
		// the ink foreground still reads through.
		if w := t.widget(KindButton); w != nil && w.Primary != "" {
			c := ParseHex(w.Primary)
			if nrgba, ok := c.(color.NRGBA); ok {
				nrgba.A = 0x4d // 30%
				return nrgba
			}
		}
	case fyneTheme.ColorNameOverlayBackground,
		fyneTheme.ColorNameMenuBackground:
		if w := t.widget(KindCard); w != nil && w.Background != "" {
			return pick(w.Background)
		}
	case fyneTheme.ColorNameInputBackground:
		// Inputs need to visibly sit on top of the card they live in.
		// Fall back to the entry widget's background, which is set
		// one elevation step higher than the card surface.
		if w := t.widget(KindEntry); w != nil && w.Background != "" {
			return pick(w.Background)
		}
	case fyneTheme.ColorNameButton:
		if w := t.widget(KindButton); w != nil && w.Background != "" {
			return pick(w.Background)
		}
	case fyneTheme.ColorNameHeaderBackground:
		if w := t.widget(KindTable); w != nil && w.Header != "" {
			return pick(w.Header)
		}
	case fyneTheme.ColorNameSeparator,
		fyneTheme.ColorNameInputBorder:
		if w := t.widget(KindCard); w != nil && w.Border != "" {
			return pick(w.Border)
		}
	case fyneTheme.ColorNameHover:
		// Light tint of the brand colour for button hover feedback.
		if w := t.widget(KindButton); w != nil && w.Primary != "" {
			c := ParseHex(w.Primary)
			if nrgba, ok := c.(color.NRGBA); ok {
				nrgba.A = 0x20
				return nrgba
			}
		}
	}

	return t.base.Color(name, variant)
}

// widget returns the WidgetTheme for kind, or nil if none is present.
func (t *GlobalFyneTheme) widget(kind WidgetKind) *WidgetTheme {
	for i := range t.xtheme.Widgets {
		if t.xtheme.Widgets[i].Kind == kind {
			return &t.xtheme.Widgets[i]
		}
	}
	return nil
}

func (t *GlobalFyneTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t *GlobalFyneTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t *GlobalFyneTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case fyneTheme.SizeNameText:
		if v := ParseFloat32(t.xtheme.Sizes.Text); v > 0 {
			return v
		}
	case fyneTheme.SizeNamePadding:
		if v := ParseFloat32(t.xtheme.Sizes.Padding); v > 0 {
			return v
		}
	case fyneTheme.SizeNameInnerPadding:
		if v := ParseFloat32(t.xtheme.Sizes.InnerPadding); v > 0 {
			return v
		}
	}
	return t.base.Size(name)
}

// ── WidgetFyneTheme ───────────────────────────────────────────────────────────

// WidgetFyneTheme implementiert fyne.Theme für einen einzelnen Widget-Typ.
// Wird via container.NewThemeOverride pro Widget angewendet.
type WidgetFyneTheme struct {
	wt      *WidgetTheme
	variant fyne.ThemeVariant
	base    fyne.Theme
}

// NewWidgetFyneTheme erstellt ein fyne.Theme aus einem WidgetTheme.
func NewWidgetFyneTheme(wt *WidgetTheme, variant string) fyne.Theme {
	v := fyneTheme.VariantDark
	if variant == "light" {
		v = fyneTheme.VariantLight
	}
	return &WidgetFyneTheme{
		wt:      wt,
		variant: v,
		base:    fyneTheme.DefaultTheme(),
	}
}

func (t *WidgetFyneTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	bg := ParseHex(t.wt.Background)
	fg := ParseHex(t.wt.Foreground)
	primary := ParseHex(t.wt.Primary)
	border := ParseHex(t.wt.Border)
	header := ParseHex(t.wt.Header)

	switch name {
	// Background → trifft je nach Widget-Typ verschiedene Color-Namen
	case fyneTheme.ColorNameBackground,
		fyneTheme.ColorNameButton,          // Button-Hintergrund
		fyneTheme.ColorNameInputBackground, // Entry/TextArea-Hintergrund
		fyneTheme.ColorNameMenuBackground,  // Choices/Select-Hintergrund
		fyneTheme.ColorNameHeaderBackground: // Table-Header
		if bg != color.Transparent {
			return bg
		}
		// Header hat eigenen Wert
		if name == fyneTheme.ColorNameHeaderBackground && header != color.Transparent {
			return header
		}
	case fyneTheme.ColorNameForeground,
		fyneTheme.ColorNamePlaceHolder:
		if fg != color.Transparent {
			return fg
		}
	case fyneTheme.ColorNamePrimary,
		fyneTheme.ColorNameFocus:
		if primary != color.Transparent {
			return primary
		}
	case fyneTheme.ColorNameSelection:
		// Match the global convention: full-opacity primary would
		// drown the text under the selected row; 30% alpha keeps
		// the ink legible.
		if primary != color.Transparent {
			if nrgba, ok := primary.(color.NRGBA); ok {
				nrgba.A = 0x4d
				return nrgba
			}
		}
	case fyneTheme.ColorNameInputBorder,
		fyneTheme.ColorNameSeparator:
		if border != color.Transparent {
			return border
		}
	}
	return t.base.Color(name, t.variant)
}

func (t *WidgetFyneTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t *WidgetFyneTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t *WidgetFyneTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case fyneTheme.SizeNameText:
		if v := ParseFloat32(t.wt.TextSize); v > 0 {
			return v
		}
	case fyneTheme.SizeNamePadding:
		if v := ParseFloat32(t.wt.Padding); v > 0 {
			return v
		}
	case fyneTheme.SizeNameInputRadius:
		if v := ParseFloat32(t.wt.Radius); v > 0 {
			return v
		}
	}
	return t.base.Size(name)
}
