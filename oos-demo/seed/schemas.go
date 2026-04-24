package seed

// schemas.go — publishes the authoritative DSL grammar, its semantic
// enrichment, and the CTX grammar into the database.
//
// DSL (both grammar and enrichment) lives in oos.oos_dsl_meta. The
// grammar row carries dsl.xsd — the structural contract the Fyne
// renderer validates against. The enrichment row carries aliases,
// intent, examples and AI hints per DSL element — the layer the XSD
// cannot express but the LLM needs to bridge natural-language UI
// requests to DSL structure. oosp reads both rows to build the
// element chunks in oos.oos_dsl_schema.
//
// CTX (ctx.xsd) still lives in oos.config under namespace "schema.ctx"
// because it has no enrichment layer today and no consumer benefits
// from a dedicated table yet. When CTX retrieval grows the same
// grammar+enrichment shape as DSL, CTX will move to its own
// oos.oos_ctx_meta table mirroring this one.
//
// Drift warning: ctxXSD / dslXSD / dslEnrichmentXML below must stay
// byte-identical with /ctx.xsd, /dsl.xsd, /dsl-enrichment.xml at the
// repo root. A md5-based test to enforce this is on the backlog.

import (
	"database/sql"
	"fmt"
)

// seedSchemas upserts the built-in schemas into their respective
// tables and removes pre-migration DSL rows from oos.config.
//
// Idempotent — re-running refreshes every xml payload and bumps
// updated_at via the matching _updated_at trigger.
func seedSchemas(db *sql.DB) error {
	// DSL grammar and enrichment → oos.oos_dsl_meta.
	dslEntries := []struct {
		namespace string
		xml       string
	}{
		{"grammar", dslXSD},
		{"enrichment", dslEnrichmentXML},
	}
	for _, e := range dslEntries {
		if _, err := db.Exec(`
			INSERT INTO oos.oos_dsl_meta (namespace, xml)
			VALUES ($1, $2)
			ON CONFLICT (namespace) DO UPDATE SET xml = $2, updated_at = now()
		`, e.namespace, e.xml); err != nil {
			return fmt.Errorf("upsert oos_dsl_meta %s: %w", e.namespace, err)
		}
	}

	// CTX XSD continues to live in oos.config for now.
	if _, err := db.Exec(`
		INSERT INTO oos.config (namespace, xml)
		VALUES ('schema.ctx', $1)
		ON CONFLICT (namespace) DO UPDATE SET xml = $1, updated_at = now()
	`, ctxXSD); err != nil {
		return fmt.Errorf("upsert schema.ctx: %w", err)
	}

	// Drop the obsolete schema.dsl row from oos.config. Pre-migration
	// databases still carry it; leaving it would be a silent source
	// of drift once the DSL grammar is edited.
	if _, err := db.Exec(`
		DELETE FROM oos.config WHERE namespace = 'schema.dsl'
	`); err != nil {
		return fmt.Errorf("delete legacy schema.dsl from oos.config: %w", err)
	}

	return nil
}

// ctxXSD is the authoritative grammar for *.ctx.xml and *.conf.xml.
// Kept in sync with /ctx.xsd at the repo root — that file remains the
// editable source, this constant is the seed payload.
const ctxXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           elementFormDefault="qualified">

  <!-- =========================================================
       OOS Context Schema (ctx.xsd)
       Validates .ctx.xml and .conf.xml files produced by the
       synthesist or hand-maintained. Served from oos.config
       under namespace "schema.ctx" at runtime.
       ========================================================= -->

  <!-- Root-Element -->
  <xs:element name="oos" type="OOSFile"/>

  <xs:complexType name="OOSFile">
    <xs:sequence>
      <xs:element name="private" type="Private"   minOccurs="0" maxOccurs="1"/>
      <xs:element name="ai"      type="AIGlobal"  minOccurs="0" maxOccurs="1"/>
      <xs:element name="locale"  type="Locale"    minOccurs="0" maxOccurs="unbounded"/>
      <xs:element name="source"  type="Source"    minOccurs="0" maxOccurs="unbounded"/>
      <xs:element name="context" type="Context"   minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
  </xs:complexType>

  <!-- =========================================================
       private — Infrastruktur: Storages und DSNs
       ========================================================= -->

  <xs:complexType name="Private">
    <xs:sequence>
      <xs:element name="storage" type="Storage" minOccurs="0" maxOccurs="unbounded"/>
      <xs:element name="dsn"     type="DSN"     minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
    <xs:attribute name="name" type="xs:string" use="required"/>
  </xs:complexType>

  <xs:complexType name="Storage">
    <xs:attribute name="name" type="xs:string" use="required"/>
    <xs:attribute name="type" type="StorageType" use="required"/>
    <xs:attribute name="base" type="xs:string" use="required"/>
  </xs:complexType>

  <xs:simpleType name="StorageType">
    <xs:restriction base="xs:string">
      <xs:enumeration value="fs"/>
      <xs:enumeration value="emb"/>
      <xs:enumeration value="s3"/>
    </xs:restriction>
  </xs:simpleType>

  <xs:complexType name="DSN">
    <xs:attribute name="name" type="xs:string"  use="required"/>
    <xs:attribute name="type" type="DSNType"    use="required"/>
    <xs:attribute name="path" type="xs:string"  use="required"/>
  </xs:complexType>

  <xs:simpleType name="DSNType">
    <xs:restriction base="xs:string">
      <xs:enumeration value="postgres"/>
      <xs:enumeration value="mysql"/>
      <xs:enumeration value="sqlite"/>
    </xs:restriction>
  </xs:simpleType>

  <!-- =========================================================
       ai — Globale System-Prompts
       ========================================================= -->

  <xs:complexType name="AIGlobal">
    <xs:sequence>
      <xs:element name="prompt" type="AIPrompt" minOccurs="1" maxOccurs="unbounded"/>
    </xs:sequence>
  </xs:complexType>

  <xs:complexType name="AIPrompt">
    <xs:simpleContent>
      <xs:extension base="xs:string">
        <xs:attribute name="name" type="xs:string" use="required"/>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>

  <!-- =========================================================
       locale
       ========================================================= -->

  <xs:complexType name="Locale">
    <xs:attribute name="name"     type="xs:string" use="required"/>
    <xs:attribute name="language" type="xs:string" use="required"/>
    <xs:attribute name="currency" type="xs:string" use="required"/>
  </xs:complexType>

  <!-- =========================================================
       source — Externe Datenquellen
       ========================================================= -->

  <xs:complexType name="Source">
    <xs:attribute name="name" type="xs:string" use="required"/>
    <xs:attribute name="type" type="xs:string" use="required"/>
    <xs:attribute name="dsn"  type="xs:string" use="optional"/>
    <xs:attribute name="url"  type="xs:string" use="optional"/>
  </xs:complexType>

  <!-- =========================================================
       context — Das Herzstück
       Children erscheinen in beliebiger Reihenfolge (xs:choice),
       weil der Synthetist und der Seed sie gemischt produzieren.
       ========================================================= -->

  <xs:complexType name="Context">
    <xs:choice minOccurs="0" maxOccurs="unbounded">
      <xs:element name="list_fields" type="xs:string"    minOccurs="0" maxOccurs="1"/>
      <xs:element name="permission"  type="Permission"   minOccurs="0" maxOccurs="unbounded"/>
      <xs:element name="field"       type="Field"        minOccurs="0" maxOccurs="unbounded"/>
      <xs:element name="meta"        type="Meta"         minOccurs="0" maxOccurs="unbounded"/>
      <xs:element name="logic"       type="Logic"        minOccurs="0" maxOccurs="unbounded"/>
      <xs:element name="navigate"    type="Navigate"     minOccurs="0" maxOccurs="unbounded"/>
      <xs:element name="action"      type="Action"       minOccurs="0" maxOccurs="unbounded"/>
      <xs:element name="relation"    type="Relation"     minOccurs="0" maxOccurs="unbounded"/>
      <xs:element name="ai"          type="AIPrompt"     minOccurs="0" maxOccurs="unbounded"/>
    </xs:choice>
    <xs:attribute name="name"   type="xs:string"    use="required"/>
    <xs:attribute name="kind"   type="ContextKind"  use="required"/>
    <xs:attribute name="source" type="xs:string"    use="required"/>
    <xs:attribute name="dsn"    type="xs:string"    use="optional"/>
    <xs:attribute name="save"   type="xs:string"    use="optional"/>
    <xs:attribute name="view"   type="xs:string"    use="optional"/>
    <xs:attribute name="locale" type="xs:string"    use="optional"/>
  </xs:complexType>

  <xs:simpleType name="ContextKind">
    <xs:restriction base="xs:string">
      <xs:enumeration value="collection"/>
      <xs:enumeration value="entity"/>
    </xs:restriction>
  </xs:simpleType>

  <!-- =========================================================
       permission — Rollen-basierte Rechte auf einem Context
       actions: kommagetrennt, erlaubt: read, write, delete
       ========================================================= -->

  <xs:complexType name="Permission">
    <xs:attribute name="role"    type="xs:string"         use="required"/>
    <xs:attribute name="actions" type="PermissionActions" use="required"/>
  </xs:complexType>

  <!-- Pattern: ein oder mehrere durch Komma (+ optional Whitespace) getrennte
       Tokens aus {read, write, delete}. Validiert z.B. "read", "read,write",
       "read, write, delete". -->
  <xs:simpleType name="PermissionActions">
    <xs:restriction base="xs:string">
      <xs:pattern value="(read|write|delete)(\s*,\s*(read|write|delete))*"/>
    </xs:restriction>
  </xs:simpleType>

  <!-- =========================================================
       field — Kann <example> Kinder tragen fuer Filter-Beispiele
       ========================================================= -->

  <xs:complexType name="Field">
    <xs:sequence>
      <xs:element name="example" type="FieldExample" minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
    <xs:attribute name="name"       type="xs:string"    use="required"/>
    <xs:attribute name="type"       type="FieldType"    use="required"/>
    <xs:attribute name="header"     type="xs:string"    use="optional"/>
    <xs:attribute name="format"     type="xs:string"    use="optional"/>
    <xs:attribute name="readonly"   type="xs:boolean"   use="optional" default="false"/>
    <xs:attribute name="filterable" type="xs:boolean"   use="optional" default="false"/>
    <xs:attribute name="meta"       type="xs:string"    use="optional"/>
  </xs:complexType>

  <xs:simpleType name="FieldType">
    <xs:restriction base="xs:string">
      <xs:enumeration value="int"/>
      <xs:enumeration value="float"/>
      <xs:enumeration value="string"/>
      <xs:enumeration value="text"/>
      <xs:enumeration value="bool"/>
      <xs:enumeration value="date"/>
      <xs:enumeration value="datetime"/>
    </xs:restriction>
  </xs:simpleType>

  <!-- =========================================================
       example — Per-Field Filter-Beispiel fuer das RAG-Chunk.
       op   — GraphQL Filter-Operator (eq, like, gt, lt, ...)
       value — Beispielwert fuer diesen Operator
       Element-Inhalt ist ein kurzer Kommentar fuer den Chunk.
       ========================================================= -->

  <xs:complexType name="FieldExample">
    <xs:simpleContent>
      <xs:extension base="xs:string">
        <xs:attribute name="op"    type="FilterOperator" use="required"/>
        <xs:attribute name="value" type="xs:string"      use="required"/>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>

  <xs:simpleType name="FilterOperator">
    <xs:restriction base="xs:string">
      <xs:enumeration value="eq"/>
      <xs:enumeration value="like"/>
      <xs:enumeration value="gt"/>
      <xs:enumeration value="lt"/>
      <xs:enumeration value="is"/>
      <xs:enumeration value="after"/>
      <xs:enumeration value="before"/>
    </xs:restriction>
  </xs:simpleType>

  <!-- =========================================================
       meta — Referenztabelle fuer Dropdown-Felder
       oosp registriert pro Meta eine GraphQL-Query meta_<name>.
       ========================================================= -->

  <xs:complexType name="Meta">
    <xs:attribute name="name"     type="xs:string" use="required"/>
    <xs:attribute name="table"    type="xs:string" use="required"/>
    <xs:attribute name="value"    type="xs:string" use="required"/>
    <xs:attribute name="label"    type="xs:string" use="required"/>
    <xs:attribute name="dsn"      type="xs:string" use="optional"/>
    <xs:attribute name="order_by" type="xs:string" use="optional"/>
  </xs:complexType>

  <!-- =========================================================
       logic + rule
       ========================================================= -->

  <xs:complexType name="Logic">
    <xs:sequence>
      <xs:element name="rule" type="Rule" minOccurs="1" maxOccurs="unbounded"/>
    </xs:sequence>
    <xs:attribute name="field" type="xs:string" use="required"/>
  </xs:complexType>

  <xs:complexType name="Rule">
    <xs:attribute name="condition" type="xs:string" use="required"/>
    <xs:attribute name="class"     type="xs:string" use="optional"/>
    <xs:attribute name="style"     type="xs:string" use="optional"/>
  </xs:complexType>

  <!-- =========================================================
       navigate
       ========================================================= -->

  <xs:complexType name="Navigate">
    <xs:attribute name="event" type="xs:string" use="required"/>
    <xs:attribute name="to"    type="xs:string" use="required"/>
    <xs:attribute name="bind"  type="xs:string" use="optional"/>
  </xs:complexType>

  <!-- =========================================================
       action — Aktion ohne Fensterwechsel (save, delete)
       ========================================================= -->

  <xs:complexType name="Action">
    <xs:attribute name="event"   type="xs:string" use="required"/>
    <xs:attribute name="type"    type="ActionType" use="required"/>
    <xs:attribute name="confirm" type="xs:string" use="optional"/>
  </xs:complexType>

  <xs:simpleType name="ActionType">
    <xs:restriction base="xs:string">
      <xs:enumeration value="save"/>
      <xs:enumeration value="delete"/>
    </xs:restriction>
  </xs:simpleType>

  <!-- =========================================================
       relation
       ========================================================= -->

  <xs:complexType name="Relation">
    <xs:attribute name="name"    type="xs:string"       use="required"/>
    <xs:attribute name="context" type="xs:string"       use="required"/>
    <xs:attribute name="source"  type="xs:string"       use="optional"/>
    <xs:attribute name="type"    type="RelationType"    use="required"/>
    <xs:attribute name="bind"    type="BindExpr"        use="required"/>
  </xs:complexType>

  <xs:simpleType name="RelationType">
    <xs:restriction base="xs:string">
      <xs:enumeration value="has_many"/>
      <xs:enumeration value="has_one"/>
      <xs:enumeration value="belongs_to"/>
    </xs:restriction>
  </xs:simpleType>

  <!-- =========================================================
       BindExpr — "local_field -> foreign_field"
       ========================================================= -->

  <xs:simpleType name="BindExpr">
    <xs:restriction base="xs:string">
      <xs:pattern value="[a-z_][a-z0-9_]* -&gt; [a-z_][a-z0-9_]*"/>
    </xs:restriction>
  </xs:simpleType>

</xs:schema>
`

// dslXSD is the authoritative grammar for *.dsl.xml screens.
// Kept in sync with /dsl.xsd at the repo root — that file remains the
// editable source, this constant is the seed payload.
const dslXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           elementFormDefault="qualified">

  <!-- =========================================================
       OOS DSL Schema (dsl.xsd)
       Validates .dsl.xml files — the UI description language
       consumed by the Fyne renderer in oos-dsl/dsl. Served from
       oos.config under namespace "schema.dsl" at runtime.

       Semantic reference: oos-dsl/dsl/builder.go (Node dispatch)
       and oos-dsl-base/base/node.go (NodeType constants).

       Design note: most containers accept a permissive choice
       over UIElement because the renderer is forgiving. A few
       containers (tabs, accordion, table, tree, radio, choices)
       restrict children to their specific inner element.
       ========================================================= -->

  <!-- Root element -->
  <xs:element name="screen" type="Screen"/>

  <!-- =========================================================
       Shared attribute groups
       ========================================================= -->

  <!-- Spacing (Tailwind-like, 1 unit = 4px). Every container and
       most widgets accept these. See oos-dsl/dsl/style.go. -->
  <xs:attributeGroup name="Spacing">
    <xs:attribute name="p"  type="xs:string"/>
    <xs:attribute name="px" type="xs:string"/>
    <xs:attribute name="py" type="xs:string"/>
    <xs:attribute name="pt" type="xs:string"/>
    <xs:attribute name="pr" type="xs:string"/>
    <xs:attribute name="pb" type="xs:string"/>
    <xs:attribute name="pl" type="xs:string"/>
    <xs:attribute name="m"  type="xs:string"/>
    <xs:attribute name="mx" type="xs:string"/>
    <xs:attribute name="my" type="xs:string"/>
    <xs:attribute name="mt" type="xs:string"/>
    <xs:attribute name="mr" type="xs:string"/>
    <xs:attribute name="mb" type="xs:string"/>
    <xs:attribute name="ml" type="xs:string"/>
  </xs:attributeGroup>

  <!-- Label color — vererbbar durch den Baum (screen → section → field). -->
  <xs:attributeGroup name="LabelColor">
    <xs:attribute name="label-color" type="LabelColorValue"/>
  </xs:attributeGroup>

  <xs:simpleType name="LabelColorValue">
    <xs:restriction base="xs:string">
      <xs:enumeration value="primary"/>
      <xs:enumeration value="muted"/>
      <xs:enumeration value="disabled"/>
      <xs:enumeration value="error"/>
      <xs:enumeration value="danger"/>
      <xs:enumeration value="success"/>
      <xs:enumeration value="warning"/>
    </xs:restriction>
  </xs:simpleType>

  <!-- Expand/focus — Hints, die der Builder an einzelnen Kindern liest. -->
  <xs:attributeGroup name="ChildHints">
    <xs:attribute name="expand" type="xs:boolean"/>
    <xs:attribute name="focus"  type="xs:boolean"/>
  </xs:attributeGroup>

  <!-- =========================================================
       UIElement — die freie Wahl innerhalb von Containern.
       Der Renderer akzeptiert jede beliebige Reihenfolge und
       Mischung dieser Kinder.
       ========================================================= -->

  <xs:group name="UIElement">
    <xs:choice>
      <!-- Containers & layout -->
      <xs:element name="box"       type="Box"/>
      <xs:element name="grid"      type="Grid"/>
      <xs:element name="gridwrap"  type="GridWrap"/>
      <xs:element name="border"    type="Border"/>
      <xs:element name="center"    type="Center"/>
      <xs:element name="stack"     type="Stack"/>
      <xs:element name="tabs"      type="Tabs"/>
      <xs:element name="section"   type="Section"/>
      <xs:element name="card"      type="Card"/>
      <xs:element name="accordion" type="Accordion"/>
      <xs:element name="form"      type="Form"/>

      <!-- Data-bound widgets -->
      <xs:element name="field"     type="Field"/>
      <xs:element name="entry"     type="Entry"/>
      <xs:element name="textarea"  type="TextArea"/>
      <xs:element name="choices"   type="Choices"/>
      <xs:element name="check"     type="Check"/>
      <xs:element name="radio"     type="Radio"/>
      <xs:element name="slider"    type="Slider"/>
      <xs:element name="progress"  type="Progress"/>

      <!-- Display widgets -->
      <xs:element name="label"     type="Label"/>
      <xs:element name="button"    type="Button"/>
      <xs:element name="hyperlink" type="Hyperlink"/>
      <xs:element name="icon"      type="Icon"/>
      <xs:element name="richtext"  type="RichText"/>
      <xs:element name="sep"       type="Sep"/>

      <!-- Collections -->
      <xs:element name="table"     type="Table"/>
      <xs:element name="list"      type="List"/>
      <xs:element name="tree"      type="Tree"/>

      <!-- Toolbar (top-level action bar — usually a single direct child of screen) -->
      <xs:element name="toolbar"   type="Toolbar"/>
    </xs:choice>
  </xs:group>

  <!-- =========================================================
       Root: <screen>
       ========================================================= -->

  <xs:complexType name="Screen">
    <xs:group ref="UIElement" minOccurs="0" maxOccurs="unbounded"/>
    <xs:attribute name="id"     type="xs:string" use="required"/>
    <xs:attribute name="title"  type="xs:string"/>
    <xs:attribute name="scroll" type="xs:boolean"/>
    <!-- Screen-weite Format-Defaults -->
    <xs:attribute name="cur"    type="xs:string"/>
    <xs:attribute name="locale" type="xs:string"/>
    <!-- Toolbar-Flags (Chrome von der App gerendert, nicht aus dem DSL-Body). -->
    <xs:attribute name="delete" type="xs:boolean"/>
    <xs:attribute name="save"   type="xs:boolean"/>
    <xs:attribute name="exit"   type="xs:boolean"/>
    <xs:attributeGroup ref="LabelColor"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <!-- =========================================================
       Containers & layout
       ========================================================= -->

  <xs:complexType name="Box">
    <xs:group ref="UIElement" minOccurs="0" maxOccurs="unbounded"/>
    <xs:attribute name="orient" type="Orientation"/>
    <xs:attribute name="align"  type="HAlign"/>
    <!-- padding="all|top|bottom|vertical|horizontal" — legacy shorthand -->
    <xs:attribute name="padding" type="PaddingShorthand"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="LabelColor"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:simpleType name="Orientation">
    <xs:restriction base="xs:string">
      <xs:enumeration value="vertical"/>
      <xs:enumeration value="horizontal"/>
    </xs:restriction>
  </xs:simpleType>

  <xs:simpleType name="HAlign">
    <xs:restriction base="xs:string">
      <xs:enumeration value="left"/>
      <xs:enumeration value="center"/>
      <xs:enumeration value="right"/>
    </xs:restriction>
  </xs:simpleType>

  <xs:simpleType name="PaddingShorthand">
    <xs:restriction base="xs:string">
      <xs:enumeration value="all"/>
      <xs:enumeration value="top"/>
      <xs:enumeration value="bottom"/>
      <xs:enumeration value="vertical"/>
      <xs:enumeration value="horizontal"/>
    </xs:restriction>
  </xs:simpleType>

  <xs:complexType name="Grid">
    <xs:group ref="UIElement" minOccurs="0" maxOccurs="unbounded"/>
    <xs:attribute name="cols" type="xs:positiveInteger"/>
    <xs:attribute name="gap"  type="xs:string"/>
    <!-- legacy alias: padding="all" -->
    <xs:attribute name="padding" type="PaddingShorthand"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="LabelColor"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="GridWrap">
    <xs:group ref="UIElement" minOccurs="0" maxOccurs="unbounded"/>
    <xs:attribute name="minw" type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="Border">
    <xs:group ref="UIElement" minOccurs="0" maxOccurs="unbounded"/>
    <xs:attribute name="minheight" type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="Center">
    <xs:group ref="UIElement" minOccurs="0" maxOccurs="unbounded"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="Stack">
    <xs:group ref="UIElement" minOccurs="0" maxOccurs="unbounded"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <!-- tabs: only <tab> children allowed. -->
  <xs:complexType name="Tabs">
    <xs:sequence>
      <xs:element name="tab" type="Tab" minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="Tab">
    <xs:group ref="UIElement" minOccurs="0" maxOccurs="unbounded"/>
    <xs:attribute name="label" type="xs:string" use="required"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <!-- section: the main grouping container. Mixes fields and
       nested sections/widgets freely. cols+gap build a grid of
       <field>-children; other children break the grid (see
       builder.buildSection). -->
  <xs:complexType name="Section">
    <xs:group ref="UIElement" minOccurs="0" maxOccurs="unbounded"/>
    <xs:attribute name="label" type="xs:string"/>
    <xs:attribute name="cols"  type="xs:positiveInteger"/>
    <xs:attribute name="gap"   type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="LabelColor"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="Card">
    <xs:group ref="UIElement" minOccurs="0" maxOccurs="unbounded"/>
    <xs:attribute name="title"    type="xs:string"/>
    <xs:attribute name="subtitle" type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <!-- accordion: only <accordion-item> children. -->
  <xs:complexType name="Accordion">
    <xs:sequence>
      <xs:element name="accordion-item" type="AccordionItem" minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="AccordionItem">
    <xs:group ref="UIElement" minOccurs="0" maxOccurs="unbounded"/>
    <xs:attribute name="label" type="xs:string" use="required"/>
    <xs:attribute name="open"  type="xs:boolean"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <!-- form: Fyne widget.Form — renders children with labels in a
       two-column layout. Sep is a legal break inside. -->
  <xs:complexType name="Form">
    <xs:group ref="UIElement" minOccurs="0" maxOccurs="unbounded"/>
    <xs:attribute name="label" type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <!-- =========================================================
       Data-bound widgets
       ========================================================= -->

  <!-- field: label above + widget below. The widget attribute
       picks the concrete input. Default is entry. -->
  <xs:complexType name="Field">
    <!-- <option> is only meaningful when widget=choices|radio. The
         schema permits it on every <field> to keep the grammar
         simple; the renderer ignores it otherwise. -->
    <xs:sequence>
      <xs:element name="option" type="Option" minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
    <xs:attribute name="label"       type="xs:string"/>
    <xs:attribute name="bind"        type="xs:string"/>
    <xs:attribute name="widget"      type="FieldWidget"/>
    <xs:attribute name="options"     type="xs:string"/>
    <xs:attribute name="placeholder" type="xs:string"/>
    <xs:attribute name="readonly"    type="xs:boolean"/>
    <xs:attribute name="format"      type="xs:string"/>
    <!-- Slider-Parameter, falls widget=slider. -->
    <xs:attribute name="min"  type="xs:string"/>
    <xs:attribute name="max"  type="xs:string"/>
    <xs:attribute name="step" type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="LabelColor"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:simpleType name="FieldWidget">
    <xs:restriction base="xs:string">
      <xs:enumeration value="entry"/>
      <xs:enumeration value="textarea"/>
      <xs:enumeration value="choices"/>
      <xs:enumeration value="radio"/>
      <xs:enumeration value="check"/>
      <xs:enumeration value="slider"/>
      <xs:enumeration value="progress"/>
    </xs:restriction>
  </xs:simpleType>

  <xs:complexType name="Entry">
    <xs:attribute name="label"       type="xs:string"/>
    <xs:attribute name="bind"        type="xs:string"/>
    <xs:attribute name="placeholder" type="xs:string"/>
    <xs:attribute name="readonly"    type="xs:boolean"/>
    <xs:attribute name="format"      type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="TextArea">
    <xs:attribute name="label"       type="xs:string"/>
    <xs:attribute name="bind"        type="xs:string"/>
    <xs:attribute name="placeholder" type="xs:string"/>
    <xs:attribute name="readonly"    type="xs:boolean"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="Choices">
    <xs:sequence>
      <xs:element name="option" type="Option" minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
    <xs:attribute name="label"   type="xs:string"/>
    <xs:attribute name="bind"    type="xs:string"/>
    <xs:attribute name="options" type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="Check">
    <xs:attribute name="label" type="xs:string"/>
    <xs:attribute name="bind"  type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="Radio">
    <xs:sequence>
      <xs:element name="option" type="Option" minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
    <xs:attribute name="label"   type="xs:string"/>
    <xs:attribute name="bind"    type="xs:string"/>
    <xs:attribute name="options" type="xs:string"/>
    <xs:attribute name="orient"  type="Orientation"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <!-- <option> — text content is the label, value attribute is
       the underlying value. -->
  <xs:complexType name="Option" mixed="true">
    <xs:attribute name="value" type="xs:string"/>
  </xs:complexType>

  <xs:complexType name="Slider">
    <xs:attribute name="min"    type="xs:string"/>
    <xs:attribute name="max"    type="xs:string"/>
    <xs:attribute name="step"   type="xs:string"/>
    <xs:attribute name="bind"   type="xs:string"/>
    <xs:attribute name="orient" type="Orientation"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="Progress">
    <xs:attribute name="bind"  type="xs:string"/>
    <xs:attribute name="value" type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <!-- =========================================================
       Display widgets
       ========================================================= -->

  <xs:complexType name="Label">
    <xs:attribute name="text"  type="xs:string"/>
    <xs:attribute name="style" type="LabelStyle"/>
    <xs:attribute name="wrap"  type="xs:boolean"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:simpleType name="LabelStyle">
    <xs:restriction base="xs:string">
      <xs:enumeration value=""/>
      <xs:enumeration value="bold"/>
      <xs:enumeration value="italic"/>
      <xs:enumeration value="mono"/>
    </xs:restriction>
  </xs:simpleType>

  <xs:complexType name="Button">
    <xs:attribute name="id"     type="xs:string"/>
    <xs:attribute name="label"  type="xs:string"/>
    <xs:attribute name="action" type="xs:string"/>
    <xs:attribute name="style"  type="ButtonStyle"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:simpleType name="ButtonStyle">
    <xs:restriction base="xs:string">
      <xs:enumeration value=""/>
      <xs:enumeration value="save"/>
      <xs:enumeration value="primary"/>
      <xs:enumeration value="delete"/>
      <xs:enumeration value="danger"/>
      <xs:enumeration value="cancel"/>
      <xs:enumeration value="secondary"/>
      <xs:enumeration value="add"/>
      <xs:enumeration value="refresh"/>
      <xs:enumeration value="home"/>
      <xs:enumeration value="settings"/>
      <xs:enumeration value="search"/>
      <xs:enumeration value="info"/>
      <xs:enumeration value="edit"/>
      <xs:enumeration value="copy"/>
    </xs:restriction>
  </xs:simpleType>

  <xs:complexType name="Hyperlink">
    <xs:attribute name="text" type="xs:string"/>
    <xs:attribute name="url"  type="xs:string" use="required"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="Icon">
    <xs:attribute name="name" type="xs:string" use="required"/>
    <xs:attribute name="size" type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <!-- richtext: either markdown=true and raw text content, or a
       sequence of <span> children. mixed=true lets us accept the
       markdown case where text lives directly in <richtext>. -->
  <xs:complexType name="RichText" mixed="true">
    <xs:sequence>
      <xs:element name="span" type="Span" minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
    <xs:attribute name="markdown" type="xs:boolean"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="Span" mixed="true">
    <xs:attribute name="style" type="SpanStyle"/>
  </xs:complexType>

  <xs:simpleType name="SpanStyle">
    <xs:restriction base="xs:string">
      <xs:enumeration value=""/>
      <xs:enumeration value="heading"/>
      <xs:enumeration value="subheading"/>
      <xs:enumeration value="bold"/>
      <xs:enumeration value="strong"/>
      <xs:enumeration value="italic"/>
      <xs:enumeration value="emphasis"/>
      <xs:enumeration value="em"/>
      <xs:enumeration value="mono"/>
      <xs:enumeration value="code"/>
      <xs:enumeration value="codeblock"/>
    </xs:restriction>
  </xs:simpleType>

  <!-- sep: standalone separator line. -->
  <xs:complexType name="Sep">
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <!-- =========================================================
       Collections
       ========================================================= -->

  <!-- table: only <column> children. -->
  <xs:complexType name="Table">
    <xs:sequence>
      <xs:element name="column" type="Column" minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
    <xs:attribute name="id"     type="xs:string"/>
    <xs:attribute name="bind"   type="xs:string"/>
    <xs:attribute name="action" type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="Column">
    <xs:attribute name="label"  type="xs:string"/>
    <xs:attribute name="field"  type="xs:string"/>
    <xs:attribute name="width"  type="xs:string"/>
    <xs:attribute name="format" type="xs:string"/>
  </xs:complexType>

  <xs:complexType name="List">
    <xs:attribute name="bind"   type="xs:string"/>
    <xs:attribute name="field"  type="xs:string"/>
    <xs:attribute name="action" type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <!-- tree: only <node> children. -->
  <xs:complexType name="Tree">
    <xs:sequence>
      <xs:element name="node" type="TreeNode" minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
    <xs:attribute name="action" type="xs:string"/>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

  <xs:complexType name="TreeNode" mixed="true">
    <xs:attribute name="id"     type="xs:string"/>
    <xs:attribute name="parent" type="xs:string"/>
    <xs:attribute name="label"  type="xs:string"/>
  </xs:complexType>

  <!-- =========================================================
       Toolbar
       ========================================================= -->

  <!-- toolbar: only <button> and <sep> children. -->
  <xs:complexType name="Toolbar">
    <xs:choice minOccurs="0" maxOccurs="unbounded">
      <xs:element name="button" type="Button"/>
      <xs:element name="sep"    type="Sep"/>
    </xs:choice>
    <xs:attributeGroup ref="ChildHints"/>
    <xs:attributeGroup ref="Spacing"/>
  </xs:complexType>

</xs:schema>
`

// dslEnrichmentXML carries the semantic layer the DSL XSD cannot
// express — German-language aliases, intent sentences, copy-pasteable
// examples and AI-facing hints — one block per DSL element.
//
// oosp merges this document with dslXSD to build element chunks in
// oos.oos_dsl_schema. The XSD supplies the hard facts (attribute
// names, enum values, child rules); the enrichment supplies the soft
// signal an LLM needs to bridge "zwei Felder nebeneinander" to
// <section cols="2">.
//
// Authoring rules:
//   - <alias> lines are natural-language phrases a user might say
//     when they want this element. German because that's the target
//     user language; multiple aliases per element are encouraged.
//   - <intent> is a single German sentence describing when to reach
//     for this element.
//   - <example> is a short, copy-pasteable snippet wrapped in CDATA.
//     It doesn't have to be a full screen — a minimal isolated use
//     is often more useful to the LLM.
//   - <hint> lines are short German rules of thumb — things the LLM
//     reliably gets wrong without them (cols semantics, nesting
//     rules, widget-attribute coupling, ...).
//
// Coverage: every element in the UIElement group of dsl.xsd plus the
// root <screen> and the specialised child types (<tab>,
// <accordion-item>, <column>, <option>, <span>, <node>).
const dslEnrichmentXML = `<?xml version="1.0" encoding="UTF-8"?>
<dsl-enrichment>

  <element name="screen">
    <alias>bildschirm</alias>
    <alias>formular</alias>
    <alias>maske</alias>
    <alias>seite</alias>
    <intent>Die Wurzel jedes DSL-Dokuments. Jedes *.dsl.xml beginnt mit genau einem screen-Element.</intent>
    <example><![CDATA[<screen id="person_detail" title="Person — Detail" save="true" delete="true" exit="true">
  <section label="Stammdaten" cols="2" gap="4">
    <field label="Vorname" bind="person.firstname"/>
    <field label="Nachname" bind="person.lastname"/>
  </section>
</screen>]]></example>
    <hint>id ist Pflicht und muss zum Context-Namen passen (z.B. id="person_detail" für den Context person_detail).</hint>
    <hint>save/delete/exit schalten die Toolbar-Buttons der Host-App ein, nicht im Body.</hint>
    <hint>cur und locale setzen Screen-weite Defaults für Währungs- und Datumsformate.</hint>
  </element>

  <element name="box">
    <alias>container</alias>
    <alias>box</alias>
    <alias>horizontal anordnen</alias>
    <alias>vertikal anordnen</alias>
    <alias>nebeneinander</alias>
    <alias>untereinander</alias>
    <intent>Primitives Layout-Element. Ordnet Kinder horizontal oder vertikal an. Das Standardmittel für freie Anordnung jenseits von section-Grids.</intent>
    <example><![CDATA[<box orient="horizontal" p="3">
  <icon name="account" size="48"/>
  <box orient="vertical" ml="3" expand="true">
    <richtext><span style="heading">Titel</span></richtext>
  </box>
</box>]]></example>
    <hint>orient="horizontal" legt Kinder nebeneinander, orient="vertical" untereinander. Ohne orient = vertical.</hint>
    <hint>expand="true" auf einem Kind macht es zum Dehnungs-Kind — es füllt den verbleibenden Platz.</hint>
    <hint>Für Grid-artige Feldgruppen ist section mit cols= meist besser als box.</hint>
  </element>

  <element name="grid">
    <alias>gitter</alias>
    <alias>spaltenraster</alias>
    <intent>Festes Spaltenraster für beliebige UI-Kinder. Anders als section mit cols folgen die Kinder der Reihenfolge ohne Label-Binding.</intent>
    <example><![CDATA[<grid cols="3" gap="4">
  <button label="A" action="on_a"/>
  <button label="B" action="on_b"/>
  <button label="C" action="on_c"/>
</grid>]]></example>
    <hint>cols ist die feste Spaltenanzahl. Kinder fließen zeilenweise hinein.</hint>
    <hint>Für Formularfelder mit Labels nimm section, nicht grid.</hint>
  </element>

  <element name="gridwrap">
    <alias>fliessgitter</alias>
    <alias>umbrechendes raster</alias>
    <alias>kachelansicht</alias>
    <intent>Fluss-Layout — Kinder brechen basierend auf einer Mindestbreite um. Gut für Kachelansichten ohne feste Spaltenanzahl.</intent>
    <example><![CDATA[<gridwrap minw="200">
  <card title="Eins">…</card>
  <card title="Zwei">…</card>
  <card title="Drei">…</card>
</gridwrap>]]></example>
    <hint>minw steuert ab welcher Breite umgebrochen wird.</hint>
  </element>

  <element name="border">
    <alias>randlayout</alias>
    <alias>fünfslotlayout</alias>
    <intent>Fünf-Slot-Layout (oben/unten/links/rechts/zentrum). Klassisches Application-Frame-Layout.</intent>
    <example><![CDATA[<border minheight="400">
  <toolbar>…</toolbar>
  <box>…</box>
</border>]]></example>
    <hint>Selten direkt nötig — die meisten Screens kommen mit box oder section aus.</hint>
  </element>

  <element name="center">
    <alias>zentrieren</alias>
    <alias>mittig</alias>
    <intent>Zentriert sein einziges Kind horizontal und vertikal.</intent>
    <example><![CDATA[<center><label text="Lade Daten..."/></center>]]></example>
  </element>

  <element name="stack">
    <alias>überlagern</alias>
    <alias>z-stack</alias>
    <intent>Legt Kinder übereinander (Z-Stack). Für Overlays, Badges und Ähnliches.</intent>
    <example><![CDATA[<stack>
  <image src="background"/>
  <label text="Overlay"/>
</stack>]]></example>
  </element>

  <element name="tabs">
    <alias>reiter</alias>
    <alias>tabs</alias>
    <alias>registerkarten</alias>
    <intent>Reiter-Container. Kinder sind ausschließlich tab-Elemente; jeder Reiter bekommt eigenen Inhalt.</intent>
    <example><![CDATA[<tabs p="2">
  <tab label="Stammdaten">
    <section cols="2" gap="4">
      <field label="Vorname" bind="person.firstname"/>
      <field label="Nachname" bind="person.lastname"/>
    </section>
  </tab>
  <tab label="Kontakt">
    <field label="E-Mail" bind="person.email"/>
  </tab>
</tabs>]]></example>
    <hint>Nur tab darf direktes Kind von tabs sein — kein field, kein section an dieser Stelle.</hint>
    <hint>Für mehr als 5-7 Reiter lieber accordion nehmen.</hint>
  </element>

  <element name="tab">
    <alias>reiter</alias>
    <alias>tab</alias>
    <intent>Ein einzelner Reiter innerhalb von tabs. Das label-Attribut ist der sichtbare Reiter-Titel.</intent>
    <example><![CDATA[<tab label="Kontakt">
  <field label="E-Mail" bind="person.email"/>
</tab>]]></example>
    <hint>label ist Pflicht.</hint>
    <hint>Inhalt ist beliebiger UIElement-Mix — typischerweise section-Gruppen.</hint>
  </element>

  <element name="section">
    <alias>gruppe</alias>
    <alias>feldgruppe</alias>
    <alias>abschnitt</alias>
    <alias>zeile mit spalten</alias>
    <alias>nebeneinander anordnen</alias>
    <intent>Der Standard-Container für Formularfelder. Gruppiert zusammengehörige Felder mit optionaler Überschrift und Spalten-Layout. Die erste Wahl für "zwei/drei Felder nebeneinander".</intent>
    <example><![CDATA[<section label="Adresse" cols="2" gap="4" p="3">
  <field label="Straße" bind="person.street"/>
  <field label="PLZ"    bind="person.zip"/>
  <field label="Stadt"  bind="person.city"/>
  <field label="Land"   bind="person.country"/>
</section>]]></example>
    <hint>cols="2" bedeutet zwei Spalten pro Zeile, nicht zwei Felder insgesamt. Vier field-Kinder mit cols="2" ergeben zwei Zeilen zu je zwei Feldern.</hint>
    <hint>Verschachtelung ist erlaubt und üblich: äußere section für die Überschrift (label=), innere für das cols-Layout.</hint>
    <hint>Ohne cols fließen die Kinder vertikal (Label oben, Eingabe drunter, nächstes Feld drunter).</hint>
    <hint>gap steuert den Abstand zwischen den Zellen — typisch 4.</hint>
  </element>

  <element name="card">
    <alias>karte</alias>
    <alias>panel</alias>
    <alias>kachel</alias>
    <intent>Gerahmtes Panel mit Titel und optionalem Untertitel. Für abgegrenzte Inhaltsblöcke.</intent>
    <example><![CDATA[<card title="Kontakt" subtitle="Geschäftlich">
  <field label="E-Mail" bind="person.email"/>
  <field label="Telefon" bind="person.phone"/>
</card>]]></example>
  </element>

  <element name="accordion">
    <alias>akkordeon</alias>
    <alias>aufklappbar</alias>
    <alias>ausklappbare abschnitte</alias>
    <intent>Einklappbare Abschnitte. Kinder sind ausschließlich accordion-item. Gut für selten genutzte Detail-Bereiche.</intent>
    <example><![CDATA[<accordion p="3">
  <accordion-item label="Notizen" open="true">
    <textarea bind="person.notes"/>
  </accordion-item>
  <accordion-item label="Technische Informationen">
    <field label="UUID" bind="person.uuid" readonly="true"/>
  </accordion-item>
</accordion>]]></example>
    <hint>Nur accordion-item darf direktes Kind von accordion sein.</hint>
  </element>

  <element name="accordion-item">
    <alias>ausklappbarer abschnitt</alias>
    <alias>accordion-eintrag</alias>
    <intent>Ein einzelner einklapp-/ausklappbarer Abschnitt innerhalb eines accordion.</intent>
    <example><![CDATA[<accordion-item label="Notizen" open="true">
  <textarea bind="person.notes"/>
</accordion-item>]]></example>
    <hint>label ist Pflicht.</hint>
    <hint>open="true" sorgt dafür, dass der Abschnitt beim Öffnen des Screens ausgeklappt ist.</hint>
  </element>

  <element name="form">
    <alias>formular</alias>
    <alias>label-eingabe-paare</alias>
    <intent>Fyne widget.Form — rendert Kinder als Label-Links / Eingabe-Rechts-Zeilen. Alternative zu section für simple Formulare.</intent>
    <example><![CDATA[<form>
  <field label="Name" bind="user.name"/>
  <field label="E-Mail" bind="user.email"/>
</form>]]></example>
    <hint>Für cols/gap-Layouts ist section flexibler als form.</hint>
  </element>

  <element name="field">
    <alias>feld</alias>
    <alias>eingabefeld</alias>
    <alias>textfeld</alias>
    <alias>eingabe</alias>
    <intent>Das zentrale Eingabe-Element. Ein Label über einem Widget; das widget-Attribut wählt das konkrete Eingabe-Element. Ohne widget = einfaches Text-Entry.</intent>
    <example><![CDATA[<field label="Vorname" bind="person.firstname"/>
<field label="Rolle" bind="person.role" widget="choices" options="roles"/>
<field label="Vermögen" bind="person.net_worth" format="@"/>
<field label="Fortschritt" bind="person.progress" widget="progress"/>
<field label="Schriftgröße" bind="person.font_size" widget="slider" min="10" max="24" step="1"/>]]></example>
    <hint>bind="entity.attribut" verknüpft das Feld mit dem zugehörigen Datenfeld aus dem Context.</hint>
    <hint>widget-Werte: entry (Default, einzeilige Eingabe), textarea (mehrzeilig), choices (Dropdown), radio (Radio-Group), check (Checkbox), slider (Schieberegler), progress (Fortschrittsbalken).</hint>
    <hint>options="name" zeigt auf eine Meta-Quelle aus dem Context (z.B. options="roles" → meta name="roles").</hint>
    <hint>readonly="true" macht das Feld nicht editierbar — typisch für id, uuid, created_at, updated_at.</hint>
    <hint>format="@" zeigt Währungswerte im Screen-Default (cur-Attribut auf screen); format="datetime:short" für Zeitstempel.</hint>
    <hint>min/max/step gelten nur für widget="slider".</hint>
  </element>

  <element name="entry">
    <alias>einzeilige eingabe</alias>
    <alias>text-eingabe</alias>
    <intent>Einzeiliges Text-Input ohne Label. Selten direkt genutzt — meist field mit Default-Widget.</intent>
    <example><![CDATA[<entry bind="search.query" placeholder="Suche..."/>]]></example>
    <hint>Für Formulare nimm field statt entry — field bringt den Label mit.</hint>
  </element>

  <element name="textarea">
    <alias>textbereich</alias>
    <alias>mehrzeilige eingabe</alias>
    <alias>notizfeld</alias>
    <alias>kommentarfeld</alias>
    <intent>Mehrzeilige Text-Eingabe mit Wortumbruch. Für Notizen, Beschreibungen, Kommentare.</intent>
    <example><![CDATA[<textarea bind="person.notes" placeholder="Interne Notizen..." p="2"/>]]></example>
    <hint>Alternativ: ein field-Element mit widget="textarea" verwenden, wenn ein Label gewünscht ist.</hint>
  </element>

  <element name="choices">
    <alias>dropdown</alias>
    <alias>auswahlliste</alias>
    <alias>einfachauswahl</alias>
    <intent>Einfach-Dropdown. Werte kommen entweder per options-Attribut aus einer Meta-Quelle oder per option-Kindern inline.</intent>
    <example><![CDATA[<choices label="Rolle" bind="person.role" options="roles"/>

<choices label="Status" bind="task.status">
  <option value="open">Offen</option>
  <option value="done">Erledigt</option>
</choices>]]></example>
    <hint>options="name" verknüpft mit einer Meta-Quelle — options="roles" liest z.B. alle Einträge aus der role-Tabelle des Context.</hint>
    <hint>Inline option-Kinder sind für feste, kleine Wertesätze gedacht.</hint>
  </element>

  <element name="check">
    <alias>checkbox</alias>
    <alias>häkchen</alias>
    <alias>ja-nein</alias>
    <intent>Einzelne Checkbox mit Label rechts daneben. Für boolesche Werte.</intent>
    <example><![CDATA[<check bind="person.active" label="Mitarbeiter aktiv"/>]]></example>
    <hint>Für Checkbox-Gruppen mehrere check untereinander in einer section legen.</hint>
  </element>

  <element name="radio">
    <alias>radio-gruppe</alias>
    <alias>optionsfelder</alias>
    <alias>einfachauswahl sichtbar</alias>
    <intent>Radio-Gruppe für Einfach-Auswahl aus sichtbaren Optionen. Unterschied zu choices: alle Optionen sind dauerhaft sichtbar, nicht in einem Dropdown.</intent>
    <example><![CDATA[<radio label="Anstellungsart" bind="person.employment" options="employment_types"/>

<radio bind="task.priority" orient="horizontal">
  <option value="low">Niedrig</option>
  <option value="high">Hoch</option>
</radio>]]></example>
    <hint>orient="horizontal" legt die Optionen nebeneinander; ohne orient = untereinander.</hint>
    <hint>Für mehr als 5-6 Optionen lieber choices (Dropdown) verwenden.</hint>
  </element>

  <element name="option">
    <alias>option</alias>
    <alias>auswahlwert</alias>
    <intent>Eine einzelne inline-Option innerhalb von choices oder radio. Der Text im Element ist das Label, value-Attribut der gespeicherte Wert.</intent>
    <example><![CDATA[<option value="open">Offen</option>]]></example>
    <hint>Text = sichtbares Label, value-Attribut = gespeicherter Wert.</hint>
  </element>

  <element name="slider">
    <alias>schieberegler</alias>
    <alias>slider</alias>
    <intent>Numerischer Schieberegler mit min/max/step. Für Einstellungen wie Lautstärke, Schriftgröße, Prozentsätze.</intent>
    <example><![CDATA[<slider bind="person.font_size" min="10" max="24" step="1"/>]]></example>
    <hint>min/max/step sind Pflicht-Parameter für sinnvolle Nutzung.</hint>
    <hint>Alternativ: ein field-Element mit widget="slider" verwenden, das bringt einen Label mit.</hint>
  </element>

  <element name="progress">
    <alias>fortschrittsbalken</alias>
    <alias>progressbar</alias>
    <intent>Nur-Lese-Fortschrittsbalken. Zeigt einen Wert zwischen 0 und 1 oder 0 und 100.</intent>
    <example><![CDATA[<progress bind="task.completion"/>]]></example>
    <hint>Nicht editierbar — für Eingabe nimm slider.</hint>
  </element>

  <element name="label">
    <alias>beschriftung</alias>
    <alias>text</alias>
    <alias>etikett</alias>
    <intent>Einfacher Text ohne Interaktion. Für Überschriften, Hinweise, statische Beschriftungen.</intent>
    <example><![CDATA[<label text="Bitte alle Pflichtfelder ausfüllen." style="italic"/>]]></example>
    <hint>style-Werte: bold, italic, mono — für minimale Typografie-Akzente.</hint>
    <hint>Für formatierten Text mit mehreren Stilen nimm richtext.</hint>
  </element>

  <element name="button">
    <alias>knopf</alias>
    <alias>schaltfläche</alias>
    <alias>button</alias>
    <intent>Klickbarer Button. action= verknüpft mit einem Context-Event, style= wählt ein Icon/Theme.</intent>
    <example><![CDATA[<button label="Speichern" action="on_save" style="save"/>
<button label="Neu" action="on_new" style="add"/>
<button label="Löschen" action="on_delete" style="danger"/>]]></example>
    <hint>style-Werte: save, primary, delete, danger, cancel, secondary, add, refresh, home, settings, search, info, edit, copy.</hint>
    <hint>save/delete-Buttons gehören typischerweise in die screen-Toolbar, nicht in den Body — dafür gibt es die save/delete-Attribute auf screen.</hint>
  </element>

  <element name="hyperlink">
    <alias>link</alias>
    <alias>verweis</alias>
    <intent>Klickbarer Link, der eine externe URL öffnet.</intent>
    <example><![CDATA[<hyperlink text="Dokumentation" url="https://docs.onisin.com"/>]]></example>
    <hint>url ist Pflicht; text ist optional (ohne text wird die URL selbst angezeigt).</hint>
  </element>

  <element name="icon">
    <alias>symbol</alias>
    <alias>icon</alias>
    <alias>piktogramm</alias>
    <intent>Ein Theme-Icon aus dem Fyne-Icon-Set. Für Avatare, Status-Anzeigen, Dekor.</intent>
    <example><![CDATA[<icon name="account" size="48"/>]]></example>
    <hint>name ist Pflicht — verfügbare Namen sind die Theme-Icon-Namen von Fyne (account, search, settings, ...).</hint>
    <hint>size in Pixeln.</hint>
  </element>

  <element name="richtext">
    <alias>formatierter text</alias>
    <alias>überschrift mit untertitel</alias>
    <alias>mehrzeiliger text mit stilen</alias>
    <intent>Formatierter Text mit zwei Modi: entweder markdown=true mit rohem Markdown als Inhalt, oder eine Folge von span-Kindern mit style-Attribut.</intent>
    <example><![CDATA[<richtext>
  <span style="heading">Person — Detail</span>
  <span style="bold">Enterprise Mitarbeiterprofil</span>
</richtext>

<richtext markdown="true">
# Willkommen
Dies ist **fett** und *kursiv*.
</richtext>]]></example>
    <hint>Für einzelne, kurze Textstücke ohne Formatierung reicht label.</hint>
    <hint>markdown="true" schließt span-Kinder aus — entweder-oder, nicht mischen.</hint>
  </element>

  <element name="span">
    <alias>textabschnitt</alias>
    <alias>textstück</alias>
    <alias>inline-stil</alias>
    <intent>Inline-Textabschnitt innerhalb von richtext mit eigenem Stil.</intent>
    <example><![CDATA[<span style="heading">Überschrift</span>
<span style="bold">Fetter Text</span>
<span style="codeblock">code-zeile</span>]]></example>
    <hint>style-Werte: heading, subheading, bold, strong, italic, emphasis, em, mono, code, codeblock.</hint>
    <hint>Nur als Kind von richtext sinnvoll.</hint>
  </element>

  <element name="sep">
    <alias>trennlinie</alias>
    <alias>separator</alias>
    <alias>horizontale linie</alias>
    <intent>Horizontale Trennlinie. Für visuelle Abgrenzung zwischen Blöcken.</intent>
    <example><![CDATA[<sep/>]]></example>
    <hint>Leaf-Element — keine Kinder, keine Pflicht-Attribute.</hint>
  </element>

  <element name="table">
    <alias>tabelle</alias>
    <alias>datentabelle</alias>
    <alias>liste mit spalten</alias>
    <intent>Daten-Tabelle. Zeilen kommen aus bind=, Spalten werden als column-Kinder deklariert. Die erste Wahl für Listen-Screens.</intent>
    <example><![CDATA[<table bind="persons" action="on_select">
  <column label="ID" field="id" width="60"/>
  <column label="Name" field="name"/>
  <column label="Vermögen" field="net_worth" format="@" width="120"/>
</table>]]></example>
    <hint>bind zeigt auf die Collection im Context (z.B. bind="persons" für die person_list-Context).</hint>
    <hint>action="on_select" ist der Event-Name, der beim Klick auf eine Zeile ausgelöst wird — per navigate lässt sich der zu einem Detail-Screen verknüpfen.</hint>
  </element>

  <element name="column">
    <alias>spalte</alias>
    <alias>tabellenspalte</alias>
    <intent>Eine Spalte innerhalb einer table. field= wählt den Datensatz-Schlüssel, width= die Pixel-Breite.</intent>
    <example><![CDATA[<column label="Vermögen" field="net_worth" format="@" width="120"/>]]></example>
    <hint>field muss zu einem Feld in der gebundenen Collection passen.</hint>
    <hint>format wie bei field: @ für Währung, datetime:short für Zeitstempel.</hint>
  </element>

  <element name="list">
    <alias>liste</alias>
    <alias>einfache liste</alias>
    <intent>Einspaltige Liste, gebunden an ein JSON-Array. Einfacher als table für simple Aufzählungen.</intent>
    <example><![CDATA[<list bind="tags" field="name" action="on_select"/>]]></example>
    <hint>Für mehrspaltige Daten nimm table.</hint>
  </element>

  <element name="tree">
    <alias>baum</alias>
    <alias>hierarchie</alias>
    <alias>baumansicht</alias>
    <intent>Hierarchischer Baum mit aufklappbaren Knoten. Kinder sind node-Elemente mit parent=-Zeigern.</intent>
    <example><![CDATA[<tree action="on_select">
  <node id="root" label="Wurzel"/>
  <node id="a" parent="root" label="A"/>
  <node id="b" parent="root" label="B"/>
  <node id="a1" parent="a" label="A.1"/>
</tree>]]></example>
    <hint>Nur node darf direktes Kind von tree sein.</hint>
  </element>

  <element name="node">
    <alias>baumknoten</alias>
    <alias>knoten</alias>
    <intent>Ein Knoten innerhalb eines tree. parent= zeigt auf die id des Eltern-Knotens (leer für Wurzeln).</intent>
    <example><![CDATA[<node id="a1" parent="a" label="A.1"/>]]></example>
    <hint>id muss pro tree eindeutig sein.</hint>
    <hint>parent="" oder weggelassen = Wurzel-Knoten.</hint>
  </element>

  <element name="toolbar">
    <alias>werkzeugleiste</alias>
    <alias>aktionsleiste</alias>
    <alias>button-leiste</alias>
    <intent>Top-Level-Aktionsleiste mit button- und sep-Kindern. Typisch das erste Kind eines Listen-Screens.</intent>
    <example><![CDATA[<toolbar>
  <button label="Neu" action="on_new" style="add"/>
  <button label="Aktualisieren" action="on_refresh" style="refresh"/>
  <sep/>
  <button label="Suchen" action="on_search" style="search"/>
</toolbar>]]></example>
    <hint>Nur button und sep sind als direkte Kinder erlaubt.</hint>
  </element>

</dsl-enrichment>
`
