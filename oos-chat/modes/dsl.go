package modes

// dsl.go — ready-to-use Mode for screen-building chats.
//
// Bundles dsl_schema_search (the only tool a screen builder needs
// today) with a system prompt that tells the LLM how to operate:
// retrieve once per concept, then emit the final <screen> XML in a
// single message. The host supplies an OnXML callback that receives
// the assistant's reply, extracts the XML and forwards it to the
// editor / preview / wherever it needs to go.
//
// The mode is intentionally tiny so hosts that want a different
// flavour (e.g. extra tools that read the current editor buffer for
// in-place edits) can copy this file and adapt.

import (
	"strings"

	"github.com/cloudwego/eino/components/tool"

	"onisin.com/oos-chat/chat"
	"onisin.com/oos-chat/tools"
)

// DSLConfig configures the screen-building Mode.
type DSLConfig struct {
	// OOSPBaseURL points at oosp's REST root, e.g. http://localhost:9100
	OOSPBaseURL string
	// Group sets X-OOS-Group on tool calls; empty falls back to the
	// server default (oos-admin in dev).
	Group string
	// OnXML receives the extracted <screen> XML once the assistant
	// finishes a turn. The host typically pushes this into a code
	// editor or live preview. Returning an error surfaces a red
	// bubble in the chat without aborting the conversation.
	OnXML func(xml string) error
}

// NewDSLMode builds a chat.Mode that lets the LLM compose <screen>
// fragments via dsl_schema_search and forwards the resulting XML to
// cfg.OnXML.
func NewDSLMode(cfg DSLConfig) chat.Mode {
	return &dslMode{
		cfg: cfg,
		tools: []tool.BaseTool{
			tools.NewDSLSchemaSearch(tools.DSLSchemaSearchConfig{
				BaseURL: cfg.OOSPBaseURL,
				Group:   cfg.Group,
			}),
		},
	}
}

type dslMode struct {
	cfg   DSLConfig
	tools []tool.BaseTool
}

func (m *dslMode) Name() string             { return "DSL" }
func (m *dslMode) Tools() []tool.BaseTool   { return m.tools }

func (m *dslMode) SystemPrompt() string {
	return `Du bist ein OOS-Screen-Designer. Aufgabe: aus einer deutschen
Layout-Beschreibung des Benutzers ein vollständiges *.dsl.xml-Dokument
bauen, das im Fyne-Renderer von OOS direkt darstellbar ist.

Vorgehen pro Auftrag:
  1. Ermittle die einzelnen Layout-Konzepte (z.B. "zwei Felder
     nebeneinander", "Reiter", "Tabelle", "Dropdown").
  2. Rufe dsl_schema_search für jedes Konzept einmal auf.
  3. Setze daraus das vollständige <screen>-Dokument zusammen.
  4. Gib NUR das fertige XML als finale Antwort zurück — kein
     Vor- oder Nachspann, kein Markdown-Codeblock-Fence, keine
     Erklärung. Beginne mit <?xml ... ?> oder direkt mit <screen.

Wichtig:
  - Verwende ausschließlich Elemente und Attribute, die von
    dsl_schema_search bekannt sind. Erfinde keine.
  - Auch wenn ein Beispiel im Chunk Platzhalter zeigt (z.B.
    bind="person.firstname"), passe sie an den Auftrag an
    (z.B. bind="customer.first_name").
  - Jeder <field> braucht ein label-Attribut UND ein bind-Attribut.
  - Der <screen>-Wurzelknoten braucht ein id-Attribut, das der
    Editor zuweisen wird; wähle einen sprechenden Standardwert,
    der Benutzer kann ihn anschließend anpassen.`
}

func (m *dslMode) OnAssistantMessage(text string) error {
	if m.cfg.OnXML == nil {
		return nil
	}
	xml := extractXML(text)
	if xml == "" {
		return nil // nichts Verwertbares — Bubble bleibt sichtbar.
	}
	return m.cfg.OnXML(xml)
}

// extractXML pulls the first <screen>...</screen> block out of an
// assistant message. Models occasionally wrap their answer in
// markdown fences or add a sentence of explanation despite the
// system-prompt instruction; this strips both.
func extractXML(text string) string {
	s := text

	// Strip markdown fences if present.
	if strings.Contains(s, "```") {
		parts := strings.Split(s, "```")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			// fenced blocks come labelled (xml, html, ...) on their
			// first line; strip whatever the label is.
			if strings.Contains(p, "<screen") {
				if nl := strings.IndexByte(p, '\n'); nl >= 0 && !strings.HasPrefix(strings.TrimSpace(p[:nl]), "<") {
					p = p[nl+1:]
				}
				s = p
				break
			}
		}
	}

	// Trim everything before <?xml or <screen.
	if idx := strings.Index(s, "<?xml"); idx >= 0 {
		s = s[idx:]
	} else if idx := strings.Index(s, "<screen"); idx >= 0 {
		s = s[idx:]
	} else {
		return ""
	}

	// Trim everything after </screen>.
	if idx := strings.LastIndex(s, "</screen>"); idx >= 0 {
		s = s[:idx+len("</screen>")]
	}
	return strings.TrimSpace(s)
}
