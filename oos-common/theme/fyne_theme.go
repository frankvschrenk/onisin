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

func (t *GlobalFyneTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return t.base.Color(name, t.variant())
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
		fyneTheme.ColorNameFocus,
		fyneTheme.ColorNameSelection:
		if primary != color.Transparent {
			return primary
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
