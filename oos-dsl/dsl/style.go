// style.go — Tailwind-ähnliches Spacing und Label-Farben für die DSL.
//
// Alle Container-Elemente (screen, section, field, box, grid, card, ...) verstehen:
//
//   Padding (Innenabstand):
//     p="4"    → 16px rundum
//     px="2"   → 8px links+rechts
//     py="3"   → 12px oben+unten
//     pt/pr/pb/pl="N"  → einzelne Seiten
//
//   Margin (Außenabstand — in Fyne technisch identisch mit Padding eines Wrapper-Containers):
//     m="4"    → 16px rundum
//     mx="2"   → 8px links+rechts
//     my="3"   → 12px oben+unten
//     mt/mr/mb/ml="N"  → einzelne Seiten
//
//   Gap (Abstand zwischen Grid-Zellen):
//     gap="4"  → 16px (nur für <section cols> und <grid>)
//
//   Label-Farben (vererbbar: screen → section → field):
//     label-color="primary"   schwarz/weiss (Fyne ForegroundColor)
//     label-color="muted"     grau (DisabledColor)
//     label-color="error"     rot
//     label-color="success"   grün
//     label-color="warning"   gelb/orange
//
// 1 unit = 4px (wie Tailwind).
package dsl

import (
	"image/color"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// unitToPx wandelt eine Tailwind-Unit in Pixel. 1 unit = 4px.
func unitToPx(unit string, fallback float32) float32 {
	if unit == "" {
		return fallback
	}
	if u, err := strconv.ParseFloat(unit, 32); err == nil {
		return float32(u) * 4
	}
	return fallback
}

// RenderContext — vererbbare Stil-Eigenschaften durch den Widget-Baum.
// Locale + Currency werden am <screen> gesetzt und an alle Kind-Widgets vererbt.
type RenderContext struct {
	LabelColor color.Color
	Locale     string // z.B. "de-DE", "en-US"
	Currency   string // ISO 4217, z.B. "EUR", "USD"
}

func DefaultRenderContext() RenderContext {
	return RenderContext{
		LabelColor: theme.ForegroundColor(),
		Locale:     "de-DE",
		Currency:   "EUR",
	}
}

func (rc RenderContext) WithLabelColor(colorName string) RenderContext {
	if colorName == "" {
		return rc
	}
	rc.LabelColor = resolveColor(colorName)
	return rc
}

func (rc RenderContext) WithLocale(locale, currency string) RenderContext {
	if locale != "" {
		rc.Locale = locale
	}
	if currency != "" {
		rc.Currency = currency
	}
	return rc
}

func resolveColor(name string) color.Color {
	switch name {
	case "primary":
		return theme.ForegroundColor()
	case "muted", "disabled":
		return theme.DisabledColor()
	case "error", "danger":
		return theme.ErrorColor()
	case "success":
		return theme.SuccessColor()
	case "warning":
		return theme.WarningColor()
	default:
		return theme.ForegroundColor()
	}
}

// applySpacing liest p/px/py/pt/pr/pb/pl UND m/mx/my/mt/mr/mb/ml vom Node
// und wrапpt das Objekt entsprechend.
//
// Padding und Margin sind in Fyne technisch identisch — beide erzeugen einen
// Wrapper-Container mit Abstand. Der Loomer nutzt die Begriffe wie in CSS:
//   p = Innenabstand (zwischen Border und Inhalt)
//   m = Außenabstand (zwischen Element und Nachbarn)
//
// Priorität: einzelne Seiten > Achsen > rundum (wie Tailwind).
func applySpacing(obj fyne.CanvasObject, node *Node) fyne.CanvasObject {
	obj = applyPaddingAttrs(obj, node)
	obj = applyMarginAttrs(obj, node)
	return obj
}

// applyPaddingAttrs — p/px/py/pt/pr/pb/pl
func applyPaddingAttrs(obj fyne.CanvasObject, node *Node) fyne.CanvasObject {
	p := unitToPx(node.Attr("p", ""), 0)
	px := unitToPx(node.Attr("px", ""), p)
	py := unitToPx(node.Attr("py", ""), p)
	pt := unitToPx(node.Attr("pt", ""), py)
	pb := unitToPx(node.Attr("pb", ""), py)
	pl := unitToPx(node.Attr("pl", ""), px)
	pr := unitToPx(node.Attr("pr", ""), px)

	if pt == 0 && pb == 0 && pl == 0 && pr == 0 {
		return obj
	}
	return container.New(newPaddingLayout(pl, pr, pt, pb), obj)
}

// applyMarginAttrs — m/mx/my/mt/mr/mb/ml
func applyMarginAttrs(obj fyne.CanvasObject, node *Node) fyne.CanvasObject {
	m := unitToPx(node.Attr("m", ""), 0)
	mx := unitToPx(node.Attr("mx", ""), m)
	my := unitToPx(node.Attr("my", ""), m)
	mt := unitToPx(node.Attr("mt", ""), my)
	mb := unitToPx(node.Attr("mb", ""), my)
	ml := unitToPx(node.Attr("ml", ""), mx)
	mr := unitToPx(node.Attr("mr", ""), mx)

	if mt == 0 && mb == 0 && ml == 0 && mr == 0 {
		return obj
	}
	return container.New(newPaddingLayout(ml, mr, mt, mb), obj)
}

// makeGapGrid — Grid mit Gap zwischen Zellen.
func makeGapGrid(cols int, gap float32, children []fyne.CanvasObject) fyne.CanvasObject {
	if gap <= 0 {
		return container.NewGridWithColumns(cols, children...)
	}
	return container.New(newGapGridLayout(cols, gap), children...)
}
