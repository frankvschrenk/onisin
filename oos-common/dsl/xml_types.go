package dsl

import "encoding/xml"

// CTXFile ist die geparste ctx.xml Datei.
// Der Synthetist schreibt diese Datei — sie beschreibt die Domäne für KI und oosp.
type CTXFile struct {
	XMLName  xml.Name     `xml:"oos"`
	Contexts []XMLContext `xml:"context"`
	Sources  []XMLSource  `xml:"source"`
	Locales  []XMLLocale  `xml:"locale"`
	AI       *XMLAIGlobal `xml:"ai"`
	Backbone *XMLBackbone `xml:"backbone"`
}

// XMLContext beschreibt einen einzelnen Context (collection oder entity).
// Metas: Referenztabellen für Options-Felder (Länder, Städte, Rollen usw.)
type XMLContext struct {
	Name        string          `xml:"name,attr"`
	Kind        string          `xml:"kind,attr"`           // "collection" | "entity"
	Source      string          `xml:"source,attr"`         // Tabellen-Name in der DB
	DSN         string          `xml:"dsn,attr"`            // DSN-Name, z.B. "demo"
	Save        string          `xml:"save,attr,omitempty"`
	View        string          `xml:"view,attr,omitempty"` // veraltet, nicht mehr genutzt
	Locale      string          `xml:"locale,attr,omitempty"`
	ListFields  string          `xml:"list_fields"`
	Fields      []XMLField      `xml:"field"`
	Metas       []XMLMeta       `xml:"meta"`      // Referenztabellen für Options
	Logics      []XMLLogic      `xml:"logic"`
	Navigates   []XMLNavigate   `xml:"navigate"`
	Actions     []XMLAction     `xml:"action"`
	Relations   []XMLRelation   `xml:"relation"`
	AIPrompts   []XMLAIPrompt   `xml:"ai"`
	Permissions []XMLPermission `xml:"permission"`
}

// XMLField beschreibt ein Feld in einem Context.
//
// MetaRef verweist auf einen XMLMeta-Eintrag desselben Context.
// Die KI weiß damit: dieses Feld hat Optionen — meta_<MetaRef> mitabfragen.
//
// Examples (<example op="..." value="...">comment</example>) sind optionale
// Filter-Beispiele die im Schema-Chunk fuer das RAG erscheinen. Sie werden
// ausschliesslich fuer die Chunk-Generierung benutzt und haben keinen Einfluss
// auf das GraphQL-Schema.
//
// Beispiel:
//
//	<field name="country" type="string" meta="countries"/>
type XMLField struct {
	Name       string          `xml:"name,attr"`
	Type       string          `xml:"type,attr"`
	Header     string          `xml:"header,attr,omitempty"`   // veraltet, gehört in DSL
	Format     string          `xml:"format,attr,omitempty"`   // veraltet, gehört in DSL
	Readonly   bool            `xml:"readonly,attr,omitempty"`
	Filterable bool            `xml:"filterable,attr,omitempty"`
	MetaRef    string          `xml:"meta,attr,omitempty"` // Verweis auf XMLMeta.Name
	Examples   []XMLFieldExample `xml:"example"`
}

// XMLFieldExample ist ein per-Field Filter-Beispiel.
//
// Op matcht den GraphQL-Filter-Operator (eq, like, gt, lt).
// Value ist der Beispielwert — Quoting (Strings vs. Zahlen) uebernimmt
// der Chunk-Renderer typbasiert.
// Der Element-Inhalt ist ein kurzer Kommentar fuer den Chunk, z.B.
//
//	<example op="like" value="Anna">Vorname enthaelt Anna</example>
type XMLFieldExample struct {
	Op      string `xml:"op,attr"`
	Value   string `xml:"value,attr"`
	Comment string `xml:",chardata"`
}

// XMLMeta beschreibt eine Referenztabelle für Options-Felder.
//
// oosp registriert für jeden Meta-Eintrag eine eigene GraphQL-Query (meta_<n>).
// So kann die KI Stammdaten + alle Options in einem einzigen GraphQL-Request abfragen.
//
// Beispiel CTX:
//
//	<meta name="countries" table="country" value="code" label="name" dsn="demo"/>
//	<meta name="cities"    table="city"    value="id"   label="name" dsn="demo"/>
//
// Ergibt GraphQL-Queries: meta_countries, meta_cities
// Die KI fragt alles in einem Request:
//
//	query {
//	  person_detail(id:1) { firstname country city }
//	  meta_countries { value label }
//	  meta_cities    { value label }
//	}
type XMLMeta struct {
	Name    string `xml:"name,attr"`              // interner Name, z.B. "countries"
	Table   string `xml:"table,attr"`             // DB-Tabelle, z.B. "country"
	Value   string `xml:"value,attr"`             // Wert-Spalte, z.B. "code"
	Label   string `xml:"label,attr"`             // Anzeige-Spalte, z.B. "name"
	DSN     string `xml:"dsn,attr"`               // DSN (kann vom Context abweichen)
	OrderBy string `xml:"order_by,attr,omitempty"` // Sortierung, z.B. "name"
}

// XMLPermission steuert Rollen-Berechtigungen auf einem Context.
// actions: komma-getrennt — read, write, delete
type XMLPermission struct {
	Role    string `xml:"role,attr"`
	Actions string `xml:"actions,attr"`
}

type XMLLogic struct {
	Field string    `xml:"field,attr"`
	Rules []XMLRule `xml:"rule"`
}

type XMLRule struct {
	Condition string `xml:"condition,attr"`
	Class     string `xml:"class,attr,omitempty"`
	Style     string `xml:"style,attr,omitempty"`
}

type XMLNavigate struct {
	Event string `xml:"event,attr"`
	To    string `xml:"to,attr"`
	Bind  string `xml:"bind,attr,omitempty"`
}

// XMLAction beschreibt eine Aktion die KEIN Fensterwechsel ist.
// type: save | delete
// confirm: optionaler Bestätigungstext — erscheint als Dialog vor der Ausführung.
type XMLAction struct {
	Event   string `xml:"event,attr"`
	Type    string `xml:"type,attr"`
	Confirm string `xml:"confirm,attr,omitempty"`
}

type XMLRelation struct {
	Name    string `xml:"name,attr"`
	Context string `xml:"context,attr"`
	Source  string `xml:"source,attr,omitempty"`
	Type    string `xml:"type,attr"`
	Bind    string `xml:"bind,attr"`
}

type XMLAIPrompt struct {
	Name string `xml:"name,attr"`
	Text string `xml:",chardata"`
}

type XMLAIGlobal struct {
	Prompts []XMLAIPrompt `xml:"prompt"`
}

type XMLSource struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
	DSN  string `xml:"dsn,attr,omitempty"`
	URL  string `xml:"url,attr,omitempty"`
}

type XMLLocale struct {
	Name     string `xml:"name,attr"`
	Language string `xml:"language,attr"`
	Currency string `xml:"currency,attr"`
}

type XMLBackbone struct {
	Name string   `xml:"name,attr"`
	DSNs []XMLDSN `xml:"dsn"`
}

type XMLDSN struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
	Path string `xml:"path,attr"`
	URL  string `xml:"url,attr,omitempty"`
}
