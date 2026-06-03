// view.langium — Grammar for the onisin view-dsl.
//
// A view describes how a domain is shown to a human: layout,
// widgets, bindings, navigation, toolbar actions. Each view is
// bound to a single domain via "over <domain>". Fields are
// referenced as <domain>.<field>; the renderer resolves widget
// behaviour (e.g. dropdown options) from the domain.
//
// Design decisions taken in this revision (see grammar/NOTES.md
// in earlier sessions for context):
//
//   * Widgets are expressed as one generic rule with a 'kind'
//     keyword chosen from a fixed list. This keeps widget names
//     out of the global keyword set so domain field names like
//     "email" or "color" do not collide.
//
//   * Numbers use a single NUMBER terminal. There is no separate
//     INT terminal; the validator can constrain integer-ness
//     where it matters.
//
//   * Free-text positional arguments (button "Neu", icon "account",
//     toolbar item "Neu") replace the older 'label="..."' form.
//     This avoids 'label' as a top-level keyword.
//
// File extension: .view
// One view per file.

grammar View

entry ViewModel:
    view=ViewDecl;

// `default` marks one view per domain as the natural starting
// point for that domain. The agent's pre-LLM resolver picks it
// when the user mentions a domain without naming a specific view
// ("zeig mir alle Personen" → person_list because that one is
// flagged default). At most one view per domain may carry the
// flag; the validator enforces uniqueness across the workspace.
//
// The keyword goes at the very front of the declaration, on the
// same line as `view`. This reads as a natural prefix ("default
// view person_list ...") and parallels familiar constructs in
// other languages (default class, default export). The trailing-
// modifier form was tried first but felt awkward; it was changed
// before any production .view files relied on it.
// `over` lists every domain this view participates in. Each entry
// names a domain and may give it a local alias in parentheses; the
// alias is then used to qualify field references in widget bindings
// (e.g. `text "Vorname" -> p.firstname`). When the alias is omitted
// the domain name itself is the alias, so single-domain views read
// exactly like before:
//
//     view person_list "..." over person { ... }   // alias = person
//     view person_detail "..." over person(p) { ... }  // alias = p
//     view manager_view "..." over person(p), address(a) { ... }
//
// The first domain in the list is the *primary*: save and delete
// target this domain. Secondary domains contribute read-only context
// (cross-domain JOINs, lookup payloads). Mutating multiple domains
// in one save is a future extension and is not modelled here.
ViewDecl:
    (default?='default')? 'view' name=ID title=STRING
        'over' domains+=DomainBinding (',' domains+=DomainBinding)* '{'
        (autoRefresh=AutoRefresh)?
        ('toolbar' '{' toolbar+=ToolbarItem* '}')?
        body+=BodyElement*
    '}';

// `auto_refresh on <domain>.<event>` declares that the view should
// re-fetch its data whenever the named event fires on the named
// domain. The detail tab fires `<domain>.changed` after a successful
// save or delete; a list view declaring this picks it up and reloads
// without the user having to refresh manually.
//
// Today only `changed` is recognised by the runtime, but the grammar
// keeps the `domain.event` shape generic so future event names
// (`created`, `deleted`, custom workflow signals) plug in without
// another grammar change.
AutoRefresh:
    'auto_refresh' 'on' domain=IdentName '.' event=IdentName;

// One domain reference inside `over`. The optional alias lives in
// parentheses so the comma-separated list stays unambiguous to the
// parser without introducing a new keyword.
//
// `alias` accepts the same lenient set of names as field references
// (see `IdentName` below) so common one-letter aliases like `p`, `m`
// or `t` — which would otherwise collide with padding/margin/time
// keywords — are valid here.
DomainBinding:
    name=ID ('(' alias=IdentName ')')?;

// ─── Toolbar ────────────────────────────────────────────────────

ToolbarItem:
      {infer ToolbarSave}    'save'
    | {infer ToolbarDelete}  'delete' ('confirm' '=' confirm=STRING)?
    | {infer ToolbarExit}    'exit'
    | {infer ToolbarRefresh} 'refresh' (caption=STRING)?
    | {infer ToolbarNew}     'new' caption=STRING '->' target=ID
                                   navMods+=NavModifier*;

// ─── Body elements ──────────────────────────────────────────────

BodyElement:
      Section
    | Stack
    | Row
    | Grid
    | Tabs
    | Accordion
    | Card
    | Table
    | Divider
    | Separator
    | Widget
    | Button
    | LinkWidget
    | RichText
    | IconWidget;

// ─── Layout containers ──────────────────────────────────────────

Section:
    'section' (caption=STRING)? mods+=LayoutMod* '{'
        body+=BodyElement*
    '}';

Stack:
    'stack' mods+=LayoutMod* '{'
        body+=BodyElement*
    '}';

Row:
    'row' mods+=LayoutMod* '{'
        body+=BodyElement*
    '}';

Grid:
    'grid' 'cols' '=' cols=NUMBER mods+=LayoutMod* '{'
        body+=BodyElement*
    '}';

Tabs:
    'tabs' mods+=LayoutMod* '{'
        tabs+=Tab+
    '}';

Tab:
    'tab' caption=STRING '{'
        body+=BodyElement*
    '}';

Accordion:
    'accordion' mods+=LayoutMod* '{'
        items+=AccordionItem+
    '}';

AccordionItem:
    'item' caption=STRING (open?='open')? '{'
        body+=BodyElement*
    '}';

Card:
    'card' (caption=STRING)? mods+=LayoutMod* '{'
        body+=BodyElement*
    '}';

Divider:
    {infer Divider} 'divider';

Separator:
    {infer Separator} 'sep';

// ─── Tables ─────────────────────────────────────────────────────
// Tables bind to a row source (typically <domain>.rows) and
// declare columns. Row interaction is expressed as on_select with
// an explicit target view.

Table:
    'table' '->' rowSource=FieldRef '{'
        actions+=ViewAction*
        columns+=Column+
    '}';

Column:
    'column' field=FieldRef caption=STRING mods+=ColumnModifier*;

ColumnModifier:
      {infer ColumnWidth}  'width'  '=' value=NUMBER
    | {infer ColumnFormat} 'format' '=' fmt=FormatToken;

// ─── Widgets ────────────────────────────────────────────────────
// One generic rule with a 'kind' selected from a fixed enum. This
// keeps widget keywords out of the global keyword set so domain
// field names like "email" don't collide with widget identifiers.
//
// Bindable widgets take a positional caption then a -> field-ref.
// Static widgets (button, link, icon, richtext) have their own
// rules below.

Widget:
    kind=WidgetKind (caption=STRING)? '->' bind=FieldRef mods+=WidgetModifier*;

WidgetKind returns string:
      'text'
    | 'textarea'
    | 'number'
    | 'password'
    | 'email'
    | 'date'
    | 'daterange'
    | 'time'
    | 'select'
    | 'multiselect'
    | 'combobox'
    | 'radio'
    | 'check'
    | 'switch'
    | 'slider'
    | 'rangeslider'
    | 'rating'
    | 'color'
    | 'file'
    | 'progress'
    | 'badge'
    | 'avatar';

// Static widgets — no field binding.

Button:
    'button' caption=STRING (':' actionRef=ID)? mods+=LayoutMod*;

LinkWidget:
    'link' caption=STRING 'href' '=' href=STRING mods+=LayoutMod*;

IconWidget:
    'icon' name=STRING mods+=IconModifier*;

RichText:
    'richtext' '{' spans+=RichTextSpan+ '}';

RichTextSpan:
    style=RichTextStyle text=STRING;

RichTextStyle returns string:
    'plain' | 'bold' | 'italic' | 'heading' | 'subheading' | 'mono';

// ─── View-level actions ─────────────────────────────────────────

ViewAction:
    event=ViewActionEvent '->' target=ID navMods+=NavModifier*;

ViewActionEvent returns string:
    'on_select' | 'on_new' | 'on_edit';

NavModifier:
      {infer Bind}    'bind' '=' source=FieldRef
    | {infer As}      'as'   target=PresentationTarget
    | {infer Confirm} 'confirm' '=' message=STRING;

PresentationTarget returns string:
    'tab' | 'modal' | 'window' | 'replace';

// ─── Widget modifiers ───────────────────────────────────────────

WidgetModifier:
      {infer WidgetReadOnly}    'readonly'
    | {infer WidgetFocus}       'focus'
    | {infer WidgetExpand}      'expand'
    | {infer WidgetPlaceholder} 'placeholder' '=' value=STRING
    | {infer WidgetFormat}      'format'      '=' fmt=FormatToken
    | {infer WidgetMin}         'min'         '=' value=NUMBER
    | {infer WidgetMax}         'max'         '=' value=NUMBER
    | {infer WidgetStep}        'step'        '=' value=NUMBER
    | LayoutMod;

// ─── Layout / spacing modifiers ─────────────────────────────────
// Spacing values use abstract tokens (xs/sm/md/lg/xl) or a small
// integer scale, both resolved by the renderer.

LayoutMod:
      {infer Padding}    prop=PaddingProp '=' value=SpacingValue
    | {infer Margin}     prop=MarginProp  '=' value=SpacingValue
    | {infer Gap}        'gap'    '=' value=SpacingValue
    | {infer Cols}       'cols'   '=' value=NUMBER
    | {infer Expand}     'expand'
    | {infer ScrollFlag} 'scroll' '=' scroll=BoolLit;

PaddingProp returns string:
    'p' | 'pt' | 'pb' | 'pl' | 'pr' | 'px' | 'py';

MarginProp returns string:
    'm' | 'mt' | 'mb' | 'ml' | 'mr' | 'mx' | 'my';

IconModifier:
      {infer IconSize}  'size'  '=' value=NUMBER
    | LayoutMod;

// ─── Spacing values ─────────────────────────────────────────────

SpacingValue:
      {infer SpacingToken} token=SpacingTokenName
    | {infer SpacingScale} value=NUMBER;

SpacingTokenName returns string:
    'xs' | 'sm' | 'md' | 'lg' | 'xl';

// ─── Format tokens ──────────────────────────────────────────────
// Format tokens are ".kind:detail" in the legacy world; in the
// view-dsl they read like little function calls: 'currency',
// 'number:0', 'datetime:short'. Implemented as one rule with a
// 'kind' enum to keep the keyword set tight.

FormatToken:
    kind=FormatKind (':' detail=FormatDetail)?;

FormatKind returns string:
      'currency'
    | 'number'
    | 'percent'
    | 'datetime'
    | 'date'
    | 'time';

FormatDetail returns string:
    // Either a numeric digit-count for "number:2" or a date style
    // for "datetime:short". The validator picks the right meaning.
      NUMBER | 'short' | 'medium' | 'long' | 'full';

// ─── Field references ───────────────────────────────────────────
// A FieldRef is "<domain-or-alias>.<field>". Both halves can collide
// with this grammar's keywords — the domain side because authors
// like short aliases (`p`, `t`, `m`) which match padding/time/margin
// keywords, the field side because user-defined columns (`email`,
// `color`, `date`) often look like widget kinds. We accept the same
// lenient name set on both halves.

FieldRef:
    domain=IdentName '.' field=IdentName;

// IdentName allows a plain ID plus every keyword in the grammar
// that could plausibly appear as a user-chosen identifier. Two
// rules previously split this responsibility (alias on one side,
// field on the other); they have been merged because the same
// collisions apply on both ends of a `.`-qualified reference.
IdentName returns string:
    ID
    // padding / margin shorthand (one- and two-letter)
    | 'p'  | 'pt' | 'pb' | 'pl' | 'pr' | 'px' | 'py'
    | 'm'  | 'mt' | 'mb' | 'ml' | 'mr' | 'mx' | 'my'
    // widget kinds
    | 'text' | 'textarea' | 'number' | 'password' | 'email'
    | 'date' | 'daterange' | 'time'
    | 'select' | 'multiselect' | 'combobox'
    | 'radio' | 'check' | 'switch'
    | 'slider' | 'rangeslider' | 'rating'
    | 'color' | 'file' | 'progress' | 'badge' | 'avatar'
    | 'tags' | 'json'
    // format kinds
    | 'currency' | 'percent' | 'datetime'
    // toolbar / layout words that could plausibly be field names
    | 'save' | 'delete' | 'exit' | 'new' | 'open' | 'expand'
    | 'link' | 'icon' | 'button' | 'label' | 'href'
    | 'short' | 'medium' | 'long' | 'full'
    // event names (for completeness, low risk)
    | 'on_select' | 'on_new' | 'on_edit';

// ─── Terminals ──────────────────────────────────────────────────

BoolLit returns string: 'true' | 'false';

terminal ID: /[_a-zA-Z][\w_]*/;
terminal NUMBER returns number: /-?[0-9]+(\.[0-9]+)?/;
terminal STRING: /"(\\.|[^"\\])*"/;

hidden terminal WS: /\s+/;
hidden terminal ML_COMMENT: /\/\*[\s\S]*?\*\//;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
