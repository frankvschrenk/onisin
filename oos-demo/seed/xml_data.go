package seed

// xml_data.go — CTX and DSL XML definitions as Go constants.
// Used by ctx.go to populate oos.ctx and oos.dsl.

const globalConfXML = `<oos>
  <ai>
    <prompt name="identity">
      OOS (onisin OS) is an AI-first enterprise data system. All data lives in contexts.
      A context is either a collection (list) or an entity (single record).
      You help the user explore, filter, navigate and edit that data by calling
      tools. You are precise: you never invent field names, query shapes or
      context names — everything you use must be grounded in the schema chunks
      provided in this prompt or returned by oos_schema_search.
    </prompt>

    <prompt name="schema_discovery">
      Before calling oos_query, make sure you have the schema chunk for the
      target context. In compact mode it is already in this prompt. In rag
      mode call oos_schema_search first with a short query describing what
      the user wants (e.g. "persons list" or "notes by person"). Never guess
      a context name or field list from the user's words alone.
    </prompt>

    <prompt name="query_behavior">
      For collection contexts, use ONLY the fields listed under "ALLOWED
      query fields" — no more, no less. For entity contexts use every
      declared field when loading a detail record, because the detail
      screen will try to render all of them. Never add aliases, fragments
      or nested selections the schema does not mention.
    </prompt>

    <prompt name="filter_syntax">
      Filters use ONLY suffix arguments on the context query. Examples:
      age_gt, age_lt, city_like, firstname. Never use "where" blocks,
      nested objects like {gt: 50}, or GraphQL directives. If the schema
      chunk shows a filter example for the field you want, copy its shape
      verbatim.
    </prompt>

    <prompt name="dropdowns_and_meta">
      When the user opens an entity that has dropdown fields (fields whose
      schema entry shows meta=NAME), include every corresponding meta_NAME
      sub-query in the same GraphQL request. The schema chunk carries a
      "Full example combined query" for exactly this case — copy its
      structure. Without the meta results the dropdowns on the client
      will render empty.
    </prompt>

    <prompt name="mutation_behavior">
      You do not build GraphQL mutations. When the user wants to change
      data, follow this workflow:
        1. oos_query to load the current record into the board.
        2. oos_ui_change_required with the changed fields as JSON — this
           shows a preview, no write happens.
        3. Wait for explicit user confirmation ("save", "ok", "yes").
        4. oos_ui_save to persist. The client sends the data to oosp
           and oosp builds the actual mutation.
      Never call oos_ui_save without a preceding oos_ui_change_required
      in the same turn, and never without the user confirming.
    </prompt>

    <prompt name="deletion">
      Deletion is irreversible. Only call oos_delete after the user has
      explicitly said to delete and has acknowledged the specific record
      (ideally by id). A vague "remove it" is not enough — ask which one.
    </prompt>

    <prompt name="permissions">
      The user's role is shown at the top of this prompt. Before proposing
      a write or delete, check the Permissions line of the target context
      in the schema chunk. If the role does not have the required action,
      tell the user clearly what is missing ("Your role 'user' can only
      read person_list; writing requires 'manager' or 'admin'.") and do
      not attempt the tool call.
    </prompt>

    <prompt name="response_style">
      Do not narrate what you are about to do. Call the tool, then — if any
      text is needed — give a short confirmation of what happened. The
      board renders the result, so long summaries of the data are noise.
      When a tool fails, explain the error in one sentence and suggest
      the next concrete step.

      Formatting: plain Markdown only. Allowed: **bold**, *italic*,
      backticks for code, dashes for lists, standard headings. Never use
      LaTeX ($...$, \rightarrow, \times, etc.), never use HTML tags, and
      never wrap plain arrows or symbols in dollar signs. Write "->" or
      "→" directly as text, not "$\rightarrow$".
    </prompt>

    <prompt name="language">
      Reply in the user's language. The user's input language wins over
      any default. When unsure, use the language of their last message.
    </prompt>
  </ai>
  <locale name="standard"  language="de-DE" currency="EUR"/>
  <locale name="us_market" language="en-US" currency="USD"/>
</oos>`

const groupsXML = `<oos_groups ctx_dir="/data/ctx">
  <group name="oos-admin" role="admin">
    <include ctx="global.conf.xml"/>
    <include ctx="person.ctx.xml"/>
    <include ctx="note.ctx.xml"/>
  </group>
  <group name="oos-manager" role="manager">
    <include ctx="global.conf.xml"/>
    <include ctx="person.ctx.xml"/>
    <include ctx="note.ctx.xml"/>
  </group>
  <group name="oos-user" role="user">
    <include ctx="global.conf.xml"/>
    <include ctx="person.ctx.xml"/>
    <include ctx="note.ctx.xml"/>
  </group>
</oos_groups>`

const personCTXXML = `<oos>
  <context name="person_list" kind="collection" source="person" dsn="demo">
    <permission role="admin"   actions="read,write,delete"/>
    <permission role="manager" actions="read,write"/>
    <permission role="user"    actions="read"/>
    <list_fields>id, firstname, lastname, email, city, age, net_worth</list_fields>
    <field name="id"        type="int"    readonly="true"/>
    <field name="firstname" type="string" filterable="true">
      <example op="like" value="Anna">Vorname enthaelt &quot;Anna&quot;</example>
    </field>
    <field name="lastname"  type="string" filterable="true">
      <example op="like" value="Meier">Nachname enthaelt &quot;Meier&quot;</example>
    </field>
    <field name="age"       type="int"    filterable="true">
      <example op="gt" value="50">Personen aelter als 50</example>
      <example op="lt" value="30">Personen juenger als 30</example>
    </field>
    <field name="net_worth" type="float"/>
    <field name="street"    type="string"/>
    <field name="zip"       type="string"/>
    <field name="city"      type="string" filterable="true">
      <example op="eq"   value="Berlin">Genaue Stadt</example>
      <example op="like" value="Berg">Stadt enthaelt &quot;Berg&quot;</example>
    </field>
    <field name="email"     type="string" filterable="true">
      <example op="like" value="@example.com">E-Mail-Domain</example>
    </field>
    <field name="phone"     type="string"/>
    <navigate event="on_select" to="person_detail" bind="id -> id"/>
    <navigate event="on_new"    to="person_detail" bind=""/>
    <relation name="notes" context="note_list" type="has_many" bind="id -> person_id"/>
    <ai name="scope">
      Hier leben Personen als Mitarbeiter, Kunden und Kontakte. Eine Zeile pro Person.
    </ai>
    <ai name="navigation">
      Ein Klick auf eine Tabellenzeile oeffnet person_detail mit der id dieser Person.
    </ai>
    <ai name="format_hint">
      net_worth ist Betrag in EUR. age in Jahren, keine Dezimalstellen.
    </ai>
  </context>
  <context name="person_detail" kind="entity" source="person" dsn="demo">
    <permission role="admin"   actions="read,write,delete"/>
    <permission role="manager" actions="read,write"/>
    <permission role="user"    actions="read"/>
    <ai name="edit_behavior">
      id, uuid, source, created_at und updated_at sind readonly und duerfen nie geaendert werden.
    </ai>
    <ai name="dropdowns">
      role, department, employment, city, country, notify_channel, language sind Dropdowns.
      Werte muessen aus der jeweiligen Meta-Tabelle kommen, kein Freitext.
    </ai>
    <ai name="workflow">
      Aenderungen immer ueber oos_ui_change_required vorschlagen, dann oos_ui_save nach Bestaetigung.
    </ai>
    <field name="id"               type="int"      readonly="true"/>
    <field name="title"            type="string"/>
    <field name="firstname"        type="string"   filterable="true"/>
    <field name="lastname"         type="string"   filterable="true"/>
    <field name="age"              type="int"/>
    <field name="net_worth"        type="float"/>
    <field name="role"             type="string"   meta="roles"/>
    <field name="department"       type="string"   meta="departments"/>
    <field name="employment"       type="string"   meta="employment_types"/>
    <field name="active"           type="bool"/>
    <field name="profile_complete" type="float"/>
    <field name="street"           type="string"/>
    <field name="zip"              type="string"/>
    <field name="city"             type="string"   meta="cities"/>
    <field name="country"          type="string"   meta="countries"/>
    <field name="email"            type="string"/>
    <field name="phone"            type="string"/>
    <field name="mobile"           type="string"/>
    <field name="linkedin"         type="string"/>
    <field name="notify_channel"   type="string"   meta="notify_channels"/>
    <field name="notify_email"     type="bool"/>
    <field name="notify_push"      type="bool"/>
    <field name="notify_sms"       type="bool"/>
    <field name="notify_weekly"    type="bool"/>
    <field name="language"         type="string"   meta="languages"/>
    <field name="font_size"        type="int"/>
    <field name="notes"            type="text"/>
    <field name="uuid"             type="string"   readonly="true"/>
    <field name="source"           type="string"   readonly="true"/>
    <field name="created_at"       type="datetime" readonly="true"/>
    <field name="updated_at"       type="datetime" readonly="true"/>
    <meta name="roles"            table="role"            value="key"  label="label" dsn="demo" order_by="label"/>
    <meta name="departments"      table="department"      value="key"  label="label" dsn="demo" order_by="label"/>
    <meta name="employment_types" table="employment_type" value="key"  label="label" dsn="demo" order_by="label"/>
    <meta name="cities"           table="city"            value="name" label="name"  dsn="demo" order_by="name"/>
    <meta name="countries"        table="country"         value="code" label="name"  dsn="demo" order_by="name"/>
    <meta name="notify_channels"  table="notify_channel"  value="key"  label="label" dsn="demo"/>
    <meta name="languages"        table="language"        value="code" label="name"  dsn="demo" order_by="name"/>
    <navigate event="on_notes" to="note_list" bind="id -> person_id"/>
    <action event="on_delete" type="delete" confirm="Person wirklich löschen?"/>
    <action event="save"      type="save"/>
  </context>
</oos>`

const noteCTXXML = `<oos>
  <context name="note_list" kind="collection" source="note" dsn="demo">
    <permission role="admin"   actions="read,write,delete"/>
    <permission role="manager" actions="read,write"/>
    <permission role="user"    actions="read"/>
    <list_fields>id, title, created_at</list_fields>
    <field name="id"         type="int"    readonly="true"/>
    <field name="person_id"  type="int"    filterable="true">
      <example op="eq" value="42">Alle Notizen der Person mit id 42</example>
    </field>
    <field name="title"      type="string" filterable="true">
      <example op="like" value="Meeting">Titel enthaelt &quot;Meeting&quot;</example>
    </field>
    <field name="body"       type="string"/>
    <field name="created_at" type="string"/>
    <navigate event="on_select" to="note_detail" bind="id -> id"/>
    <navigate event="on_new"    to="note_detail" bind=""/>
    <ai name="scope">
      Notizen gehoeren immer zu einer Person. Fuer Notizen einer bestimmten Person
      stets nach person_id filtern.
    </ai>
    <ai name="navigation">
      Ein Klick auf eine Zeile oeffnet note_detail mit der id dieser Notiz.
    </ai>
  </context>
  <context name="note_detail" kind="entity" source="note" dsn="demo">
    <permission role="admin"   actions="read,write,delete"/>
    <permission role="manager" actions="read,write"/>
    <permission role="user"    actions="read"/>
    <ai name="edit_behavior">
      Nur title und body sind editierbar. id, person_id und created_at sind readonly.
    </ai>
    <field name="id"         type="int"    readonly="true"/>
    <field name="person_id"  type="int"    readonly="true"/>
    <field name="title"      type="string"/>
    <field name="body"       type="string"/>
    <field name="created_at" type="string" readonly="true"/>
    <action event="on_delete" type="delete" confirm="Notiz wirklich löschen?"/>
    <action event="save"      type="save"/>
  </context>
</oos>`

const personListDSL = `<?xml version="1.0" encoding="UTF-8"?>
<screen id="person_list" title="Personen" scroll="false" cur="EUR" locale="de-DE">
  <toolbar>
    <button action="on_new" style="add" label="Neu"/>
  </toolbar>
  <table bind="rows" action="on_select">
    <column field="id"        label="ID"       width="60"/>
    <column field="firstname" label="Vorname"  width="140"/>
    <column field="lastname"  label="Nachname" width="140"/>
    <column field="email"     label="E-Mail"   width="220"/>
    <column field="city"      label="Stadt"    width="120"/>
    <column field="age"       label="Alter"    width="70"  format="num:0"/>
    <column field="net_worth" label="Vermögen" width="110" format="@"/>
  </table>
</screen>`

const personDetailDSL = `<screen id="person_detail" title="Person — Detail" label-color="primary"
        delete="true" save="true" exit="true" cur="EUR" locale="de-DE">
  <box orient="horizontal" p="3">
    <icon name="account" size="48"/>
    <box orient="vertical" ml="3" expand="true">
      <richtext>
        <span style="heading">Person — Detail</span>
        <span style="bold">Enterprise Mitarbeiterprofil</span>
      </richtext>
    </box>
  </box>
  <sep/>
  <tabs p="2">
    <tab label="Stammdaten">
      <section label="Persönliche Daten" p="3">
        <field label="ID" bind="person.id" readonly="true"/>
        <section cols="2" gap="4" mt="2">
          <field focus="true" label="Vorname"  bind="person.firstname"/>
          <field              label="Nachname" bind="person.lastname"/>
          <field              label="Titel"    bind="person.title"/>
          <field              label="Alter"    bind="person.age"/>
        </section>
        <section cols="2" gap="4" mt="2">
          <field label="Vermögen (EUR)"         bind="person.net_worth"       format="@"/>
          <field label="Profil-Vollständigkeit" bind="person.profile_complete" widget="progress"/>
        </section>
      </section>
      <section label="Zugehörigkeit" p="3" pt="0">
        <section cols="2" gap="4">
          <field label="Rolle"     bind="person.role"       widget="choices" options="roles"/>
          <field label="Abteilung" bind="person.department" widget="choices" options="departments"/>
        </section>
        <section cols="2" gap="4" mt="2">
          <field label="Anstellungsart" bind="person.employment" widget="radio"  options="employment_types"/>
          <field bind="person.active" widget="check" label="Mitarbeiter aktiv"/>
        </section>
      </section>
    </tab>
    <tab label="Kontakt">
      <section label="Adresse" p="3">
        <field label="Straße" bind="person.street"/>
        <section cols="3" gap="4" mt="2">
          <field label="PLZ"   bind="person.zip"/>
          <field label="Stadt" bind="person.city"    widget="choices" options="cities"/>
          <field label="Land"  bind="person.country" widget="choices" options="countries"/>
        </section>
      </section>
      <section label="Kommunikation" p="3" pt="0">
        <section cols="2" gap="4">
          <field label="E-Mail"   bind="person.email"/>
          <field label="Telefon"  bind="person.phone"/>
          <field label="Mobil"    bind="person.mobile"/>
          <field label="LinkedIn" bind="person.linkedin"/>
        </section>
      </section>
    </tab>
    <tab label="Einstellungen">
      <section label="Benachrichtigungen" p="3">
        <field label="Bevorzugter Kanal" bind="person.notify_channel" widget="radio" options="notify_channels"/>
        <section cols="2" gap="4" mt="3">
          <check bind="person.notify_email"  label="Per E-Mail"/>
          <check bind="person.notify_push"   label="Push-Benachrichtigung"/>
          <check bind="person.notify_sms"    label="Per SMS"/>
          <check bind="person.notify_weekly" label="Wöchentliche Zusammenfassung"/>
        </section>
      </section>
      <section label="Darstellung" p="3" pt="0">
        <field label="Sprache"      bind="person.language"  widget="choices" options="languages"/>
        <field label="Schriftgröße" bind="person.font_size" widget="slider"  min="10" max="24" step="1" mt="2"/>
      </section>
    </tab>
    <tab label="System">
      <accordion p="3">
        <accordion-item label="Notizen" open="true">
          <textarea bind="person.notes" placeholder="Interne Notizen..." p="2"/>
        </accordion-item>
        <accordion-item label="Technische Informationen">
          <section cols="2" gap="4" p="2">
            <field label="Erstellt am" bind="person.created_at" readonly="true" format="datetime:short"/>
            <field label="Geändert am" bind="person.updated_at" readonly="true" format="datetime:short"/>
            <field label="UUID"        bind="person.uuid"       readonly="true"/>
            <field label="Quelle"      bind="person.source"     readonly="true"/>
          </section>
        </accordion-item>
      </accordion>
    </tab>
  </tabs>
</screen>`

const noteListDSL = `<screen id="note_list" title="Notizen" scroll="false">
  <toolbar>
    <button action="on_new" style="add" label="Neu"/>
  </toolbar>
  <table bind="rows" action="on_select">
    <column field="id"         label="ID"    width="60"/>
    <column field="title"      label="Titel" width="300"/>
    <column field="created_at" label="Datum" width="160"/>
  </table>
</screen>`

const noteDetailDSL = `<screen id="note_detail" title="Notiz — Detail" label-color="primary"
        delete="true" save="true" exit="true">
  <section label="Notiz" p="3">
    <field label="ID"        bind="note.id"         readonly="true"/>
    <field label="Person ID" bind="note.person_id"  readonly="true"/>
    <field label="Datum"     bind="note.created_at" readonly="true"/>
  </section>
  <section label="Inhalt" p="3" pt="0">
    <field    label="Titel" bind="note.title" focus="true"/>
    <textarea bind="note.body" placeholder="Notiz..." p="2" mt="2"/>
  </section>
</screen>`
