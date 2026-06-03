# Grammar notes — open questions and design choices

Stand: 30. April 2026, nach erstem erfolgreichem Parse-Durchlauf
aller Beispiele.

Die 5 Punkte aus der Vor-Session sind durch:

1. **AI-Hint-Body** — von `{...}`-Terminal auf `STRING` umgestellt.
   Robust, mehrzeilig per Newline im String. Triple-Quote-Strings
   sind später möglich, heute nicht nötig.
2. **Widget-Regeln** — auf eine generische `Widget: kind=WidgetKind ...`
   reduziert. Nicht nur kompakter, sondern *notwendig* damit Widget-
   Namen nicht als globale Keywords den Lexer für Field-Namen
   blockieren.
3. **Modifier-Eindeutigkeit** — bleibt offen, kommt als Validator
   nach (nicht in der Grammar). Siehe Punkt A unten.
4. **`cols` doppelt** — bereinigt, nur noch in `Grid` und `LayoutMod`.
   `Grid` hat `cols=NUMBER` als Pflicht, `LayoutMod` hat es als
   optionalen Modifier. Das ist absichtlich so, weil `card cols=3`
   immer noch denkbar ist.
5. **Ausgelassen** — bleibt ausgelassen (Imports, Conditional, Computed,
   Sub-Tabellen).

Neuer wichtiger Stand:

## Lexer ist über Sprachen geteilt

Beide Grammars (`domain.langium` und `view.langium`) erzeugen über
`langium-config.json` *einen* gemeinsamen Service-Bundle und damit
einen geteilten Lexer-Token-Set. Das war die wichtigste Erkenntnis:
ein Keyword in der View-Grammar (`email`, `date`, `color`, ...)
verhindert, dass dieses Wort als ID in der Domain-Grammar funktioniert.

Lösung: `FieldName returns string: ID | 'email' | 'date' | ...` als
data-type rule im FieldRef der View-Grammar. Das ist der idiomatische
Langium-Weg (siehe „Keywords as Identifiers" in der Doku).

Die Liste in `view.langium` ist nicht vollständig — sie deckt nur
Keywords ab, die *plausible* Field-Namen wären (also primär die
Widget-Kinds und Format-Kinds). Wenn jemand mal ein Domain-Field
`gap` oder `width` braucht, müssen diese ergänzt werden.

## Validator-Themen (für später)

A. **Modifier-Eindeutigkeit:** `bind=x bind=y` ist syntaktisch
   erlaubt, semantisch Quatsch. Gehört in den Langium-Validator,
   sobald wir LSP haben.

B. **`format=number:short` vs. `format=date:0`:** Die `FormatDetail`-
   Regel akzeptiert Zahl *oder* Style-Token, aber die Kombination
   muss zur `FormatKind` passen. `currency:2` ist sinnvoll,
   `datetime:2` nicht. Validator.

C. **Cross-References:** `OptionsRef` benutzt `[Meta:ID]` und
   referenziert eine Meta-Definition im selben Domain-Block. Das
   funktioniert solange wir nur einzelne Files parsen. Wenn der
   Workspace mehrere Files sieht, brauchen wir einen Scope-Provider.

D. **View bindet an Domain:** Die `over <domain>`-Klausel referenziert
   heute nur einen `ID`, nicht die echte Domain-Definition. Echte
   Cross-Reference (`over=[DomainDecl:ID]`) erst sinnvoll wenn der
   Workspace mehrere Files versteht.

## Was als Nächstes ansteht

Die Grammars sind jetzt verlässlich. Damit sind wir reif für:

1. **LSP-Worker** pro Sprache, der über `vscode-languageserver/browser`
   spricht. Der Generator hat schon `module.ts` mit den
   `*GeneratedModule`s gebaut, die der Worker injiziert.
2. **Monaco-Anbindung** über `monaco-languageclient`/`@typefox/monaco-editor-react`.
3. **Sprach-Switch in `SourceEditor.tsx`**: bei `kind === "view"` →
   `onisin-view`, bei `kind === "domain"` → `onisin-domain`.

Der Migrationspfad für die DB-Inhalte (heute XML, morgen die neue
DSL) wartet auf den Editor-Stand. Sobald der Editor mit echter
Sprachunterstützung läuft, können wir die Beispiele aus
`grammar/examples/` in die DB schreiben und parallel pflegen.
