package theme

// adapter.go — Adapter der OOSTheme als dsl.ThemeProvider verfügbar macht.
//
// Da fyne_tmp/dsl kein oos-common importieren kann (Zirkel),
// definiert dsl nur das Interface ThemeProvider.
// Dieser Adapter implementiert es und liegt in oos-common.
//
// Verwendung in ooso/gui:
//   adapter := theme.NewAdapter(xtheme)
//   builder.SetThemeProvider(adapter)

import "fyne.io/fyne/v2"

// Adapter implementiert das dsl.ThemeProvider Interface.
// Gibt für jeden Widget-Typ-Namen das passende fyne.Theme zurück.
type Adapter struct {
	xtheme *OOSTheme
}

// NewAdapter erstellt einen neuen ThemeProvider-Adapter.
func NewAdapter(xtheme *OOSTheme) *Adapter {
	return &Adapter{xtheme: xtheme}
}

// ThemeFor gibt das fyne.Theme für einen Widget-Typ zurück.
// Gibt nil zurück wenn der WidgetTheme leer ist (kein Override).
func (a *Adapter) ThemeFor(widgetKind string) fyne.Theme {
	wt := a.xtheme.ForWidget(WidgetKind(widgetKind))
	if wt == nil || wt.IsEmpty() {
		return nil
	}
	return NewWidgetFyneTheme(wt, a.xtheme.Variant)
}
