package seed

// schemas.go — publishes the authoritative XSD grammars into oos.config.
//
// The CTX XSD (ctx.xsd in the repo root) is the grammar the synthesist
// validates against when producing *.ctx.xml files. At runtime it is
// served from oos.config under namespace "schema.ctx" so any client —
// the eventual AI engine in particular — can fetch the current grammar
// from a single authoritative source without parsing the filesystem.
//
// A later commit will add "schema.dsl" alongside, populated from a
// dedicated dsl.xsd. The seed function below is written so adding the
// DSL row is a one-line change.
//
// Storage layout mirrors seedThemes: one row per namespace, XSD text
// in the xml column, data and json columns left empty.

import (
	"database/sql"
	"fmt"
)

// seedSchemas upserts the built-in XSD grammars into oos.config.
//
// Idempotent — re-running refreshes the xml payload and bumps
// updated_at via the config_updated_at trigger.
func seedSchemas(db *sql.DB) error {
	entries := []struct {
		namespace string
		xsd       string
	}{
		{"schema.ctx", ctxXSD},
	}

	for _, e := range entries {
		if _, err := db.Exec(`
			INSERT INTO oos.config (namespace, xml)
			VALUES ($1, $2)
			ON CONFLICT (namespace) DO UPDATE SET xml = $2, updated_at = now()
		`, e.namespace, e.xsd); err != nil {
			return fmt.Errorf("upsert %s: %w", e.namespace, err)
		}
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
