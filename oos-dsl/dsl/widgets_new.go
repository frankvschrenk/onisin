// widgets_new.go — Neue DSL-Widgets: Accordion, Slider, Hyperlink, Icon, RichText.
// Alle build*-Funktionen folgen dem gleichen Muster wie in builder.go.
package dsl

import (
	"net/url"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// ============================================================================
// Accordion
// ============================================================================

// buildAccordion — aufklappbare Abschnitte.
//
// DSL-Beispiel:
//
//	<accordion>
//	  <accordion-item label="Allgemein" open="true">
//	    <entry label="Name" bind="person.name"/>
//	  </accordion-item>
//	  <accordion-item label="Details">
//	    <label text="Weitere Informationen..."/>
//	  </accordion-item>
//	</accordion>
func (b *Builder) buildAccordion(node *Node) fyne.CanvasObject {
	var items []*widget.AccordionItem
	var openIndex int = -1

	for i, child := range node.Children {
		if child.Type != NodeAccordionItem {
			continue
		}
		label := child.Attr("label", "Abschnitt")
		var content fyne.CanvasObject
		if len(child.Children) == 1 {
			content = b.Build(child.Children[0])
		} else {
			content = b.vbox(child.Children)
		}
		items = append(items, widget.NewAccordionItem(label, content))
		if child.AttrBool("open") {
			openIndex = i
		}
	}

	acc := widget.NewAccordion(items...)
	if openIndex >= 0 && openIndex < len(items) {
		acc.Open(openIndex)
	}
	return applySpacing(acc, node)
}

// ============================================================================
// Slider
// ============================================================================

// buildSlider — Schieberegler mit optionalem Datenbinding.
//
// DSL-Beispiel:
//
//	<slider min="0" max="100" step="5" bind="person.satisfaction" orient="horizontal"/>
//
// Attribute:
//
//	min="0"              Minimalwert (default: 0)
//	max="100"            Maximalwert (default: 100)
//	step="1"             Schrittweite (default: 1)
//	bind="..."           Datenpfad im State (lesen + schreiben)
//	orient="horizontal"  Orientierung: horizontal (default) oder vertical
func (b *Builder) buildSlider(node *Node) fyne.CanvasObject {
	min := node.AttrFloat("min", 0)
	max := node.AttrFloat("max", 100)
	step := node.AttrFloat("step", 1)
	bp := node.Attr("bind", "")

	s := widget.NewSlider(min, max)
	s.Step = step

	if node.Attr("orient", "horizontal") == "vertical" {
		s.Orientation = widget.Vertical
	}

	// Aktuellen Wert aus State laden
	if bp != "" {
		if raw := b.state.Get(bp); raw != "" {
			if f, err := strconv.ParseFloat(raw, 64); err == nil {
				s.SetValue(f)
			}
		}
		s.OnChanged = func(v float64) {
			b.state.Set(bp, strconv.FormatFloat(v, 'f', -1, 64))
		}
	}

	return b.applyTheme(applySpacing(s, node), "slider")
}

// ============================================================================
// Hyperlink
// ============================================================================

// buildHyperlink — klickbarer Link.
//
// DSL-Beispiel:
//
//	<hyperlink text="Fyne Dokumentation" url="https://docs.fyne.io"/>
//
// Attribute:
//
//	text="..."   Linktext (Pflichtfeld, Fallback: URL)
//	url="..."    Ziel-URL (Pflichtfeld)
func (b *Builder) buildHyperlink(node *Node) fyne.CanvasObject {
	rawURL := node.Attr("url", "#")
	text := node.Attr("text", rawURL)

	parsed, err := url.Parse(rawURL)
	if err != nil {
		// Ungültige URL — als plain Label anzeigen damit die App nicht abstürzt
		return widget.NewLabel(text)
	}

	link := widget.NewHyperlink(text, parsed)
	return applySpacing(link, node)
}

// ============================================================================
// Icon
// ============================================================================

// buildIcon — Theme-Icon aus dem Fyne-Icon-Set.
//
// DSL-Beispiel:
//
//	<icon name="home" size="32"/>
//	<icon name="account" size="48"/>
//
// Attribute:
//
//	name="..."   Icon-Name aus https://docs.fyne.io/explore/icons/
//	             (z.B. "home", "search", "settings", "account", "mail")
//	size="32"    Pixelgröße (default: 32)
func (b *Builder) buildIcon(node *Node) fyne.CanvasObject {
	name := node.Attr("name", "")
	size := float32(node.AttrFloat("size", 32))

	res := themeIconByName(name)
	icon := widget.NewIcon(res)
	icon.Resize(fyne.NewSize(size, size))

	return applySpacing(container.NewCenter(icon), node)
}

// ============================================================================
// RichText
// ============================================================================

// buildRichText — formatierter Text mit Segmenten.
//
// Modus 1 — Markdown (empfohlen für einfache Fälle):
//
//	<richtext markdown="true">
//	  # Überschrift
//	  **Fett** und *kursiv* und `Code`.
//	</richtext>
//
// Modus 2 — explizite Segmente über Kinder:
//
//	<richtext>
//	  <span style="heading">Überschrift</span>
//	  <span style="bold">Fett</span>
//	  <span style="italic">Kursiv</span>
//	  <span style="mono">Code</span>
//	  <span>Normaler Text</span>
//	</richtext>
func (b *Builder) buildRichText(node *Node) fyne.CanvasObject {
	// Modus 1: Markdown
	if node.AttrBool("markdown") {
		rt := widget.NewRichTextFromMarkdown(node.Text)
		rt.Wrapping = fyne.TextWrapWord
		return applySpacing(rt, node)
	}

	// Modus 2: Segmente aus Kinder-Nodes
	segments := buildRichTextSegments(node)
	rt := widget.NewRichText(segments...)
	rt.Wrapping = fyne.TextWrapWord
	return applySpacing(rt, node)
}

// buildRichTextSegments wandelt <span style="..."> Kinder in RichText-Segmente.
func buildRichTextSegments(node *Node) []widget.RichTextSegment {
	// Wenn kein Kinder aber Text vorhanden → als einfachen Paragraph
	if len(node.Children) == 0 && node.Text != "" {
		return []widget.RichTextSegment{
			&widget.TextSegment{
				Text:  node.Text,
				Style: widget.RichTextStyleParagraph,
			},
		}
	}

	segments := make([]widget.RichTextSegment, 0, len(node.Children))
	for _, child := range node.Children {
		if child.Type != NodeSpan {
			continue
		}
		style := spanStyle(child.Attr("style", ""))
		segments = append(segments, &widget.TextSegment{
			Text:  child.Text,
			Style: style,
		})
	}
	return segments
}

// spanStyle — <span style="..."> → RichTextStyle
func spanStyle(name string) widget.RichTextStyle {
	switch name {
	case "heading":
		return widget.RichTextStyleHeading
	case "subheading":
		return widget.RichTextStyleSubHeading
	case "bold", "strong":
		return widget.RichTextStyleStrong
	case "italic", "emphasis", "em":
		return widget.RichTextStyleEmphasis
	case "mono", "code":
		return widget.RichTextStyleCodeInline
	case "codeblock":
		return widget.RichTextStyleCodeBlock
	default:
		return widget.RichTextStyleParagraph
	}
}
