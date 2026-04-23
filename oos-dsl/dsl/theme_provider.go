package dsl

// theme_provider.go — Interface damit der Builder ein OOSTheme anwenden kann
// ohne direkte Abhängigkeit auf oos-common/theme.
//
// Verwendung:
//   builder := dsl.NewBuilder(state, screenID, onAction)
//   builder.SetThemeProvider(myThemeProvider)
//
// Der ThemeProvider gibt für einen Widget-Typ-Namen ein fyne.Theme zurück.
// Gibt nil zurück → kein Override, Default-Theme wird verwendet.

import "fyne.io/fyne/v2"

// ThemeProvider liefert ein fyne.Theme für einen Widget-Typ.
// Implementiert von oos-common/theme.OOSTheme via Adapter.
type ThemeProvider interface {
	// ThemeFor gibt das fyne.Theme für einen Widget-Typ zurück.
	// widgetKind entspricht den NodeType-Konstanten: "button", "entry", etc.
	// Gibt nil zurück wenn kein Override vorhanden.
	ThemeFor(widgetKind string) fyne.Theme
}
