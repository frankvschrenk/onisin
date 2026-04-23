package dsl

import (
	"fmt"
	"strings"
)

type ValidationError struct {
	File   string
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("dsl validation [%s]:\n  - %s", e.File, strings.Join(e.Errors, "\n  - "))
}

func Validate(f *DSLFile) error {
	if f.CTX == nil {
		return nil
	}
	v := &validator{file: f.Filename}
	v.validateFile(f.CTX)
	if len(v.errors) == 0 {
		return nil
	}
	return &ValidationError{File: f.Filename, Errors: v.errors}
}

type validator struct {
	file   string
	errors []string
}

func (v *validator) err(format string, args ...any) {
	v.errors = append(v.errors, fmt.Sprintf(format, args...))
}

func (v *validator) validateFile(f *CTXFile) {
	if f.Backbone != nil {
		for _, dsn := range f.Backbone.DSNs {
			if dsn.Name == "" {
				v.err("backbone/dsn: name darf nicht leer sein")
			}
			if dsn.Type == "" {
				v.err("backbone/dsn %q: type fehlt", dsn.Name)
			}
			if !oneOf(dsn.Type, "postgres", "mysql", "sqlite", "plugin") {
				v.err("backbone/dsn %q: unbekannter type %q", dsn.Name, dsn.Type)
			}
			if dsn.Type == "plugin" && dsn.URL == "" && dsn.Path == "" {
				v.err("backbone/dsn %q: url fehlt", dsn.Name)
			} else if dsn.Type != "plugin" && dsn.Path == "" {
				v.err("backbone/dsn %q: path fehlt", dsn.Name)
			}
		}
	}

	if f.AI != nil {
		for _, p := range f.AI.Prompts {
			if p.Name == "" {
				v.err("ai/prompt: name darf nicht leer sein")
			}
			if strings.TrimSpace(p.Text) == "" {
				v.err("ai/prompt %q: text darf nicht leer sein", p.Name)
			}
		}
	}

	for _, l := range f.Locales {
		if l.Name == "" {
			v.err("locale: name darf nicht leer sein")
		}
		if l.Language == "" {
			v.err("locale %q: language fehlt", l.Name)
		}
		if l.Currency == "" {
			v.err("locale %q: currency fehlt", l.Name)
		}
	}

	contextNames := map[string]bool{}
	for _, c := range f.Contexts {
		if contextNames[c.Name] {
			v.err("context %q: doppelter Name in dieser Datei", c.Name)
		}
		contextNames[c.Name] = true
		v.validateContext(c)
	}
}

func (v *validator) validateContext(c XMLContext) {
	prefix := fmt.Sprintf("context %q", c.Name)

	if c.Name == "" {
		v.err("context: name darf nicht leer sein")
		return
	}
	if !oneOf(c.Kind, "collection", "entity") {
		v.err("%s: kind %q ungültig (erlaubt: collection, entity)", prefix, c.Kind)
	}
	if c.Source == "" {
		v.err("%s: source fehlt", prefix)
	}
	if c.Kind == "collection" && strings.TrimSpace(c.ListFields) == "" {
		v.err("%s: collection ohne list_fields", prefix)
	}

	fieldNames := map[string]bool{}
	for _, f := range c.Fields {
		if f.Name == "" {
			v.err("%s/field: name darf nicht leer sein", prefix)
			continue
		}
		if fieldNames[f.Name] {
			v.err("%s/field %q: doppelter Feldname", prefix, f.Name)
		}
		fieldNames[f.Name] = true
		if !oneOf(f.Type, "int", "float", "string", "bool", "text", "date", "datetime") {
			v.err("%s/field %q: unbekannter type %q", prefix, f.Name, f.Type)
		}
	}

	// Meta-Validierung: name, table, value, label müssen gesetzt sein
	// MetaRef in Fields muss auf existierende Meta verweisen
	metaNames := map[string]bool{}
	for _, m := range c.Metas {
		if m.Name == "" {
			v.err("%s/meta: name darf nicht leer sein", prefix)
			continue
		}
		if metaNames[m.Name] {
			v.err("%s/meta %q: doppelter Name", prefix, m.Name)
		}
		metaNames[m.Name] = true
		if m.Table == "" {
			v.err("%s/meta %q: table fehlt", prefix, m.Name)
		}
		if m.Value == "" {
			v.err("%s/meta %q: value fehlt", prefix, m.Name)
		}
		if m.Label == "" {
			v.err("%s/meta %q: label fehlt", prefix, m.Name)
		}
	}

	for _, f := range c.Fields {
		if f.MetaRef != "" && !metaNames[f.MetaRef] {
			v.err("%s/field %q: meta=%q verweist auf nicht definierten <meta> Eintrag", prefix, f.Name, f.MetaRef)
		}
	}

	if c.Kind == "collection" {
		for _, lf := range splitListValidation(c.ListFields) {
			if !fieldNames[lf] {
				v.err("%s/list_fields: Feld %q nicht in fields definiert", prefix, lf)
			}
		}
	}

	for _, l := range c.Logics {
		if !fieldNames[l.Field] {
			v.err("%s/logic: Feld %q nicht in fields definiert", prefix, l.Field)
		}
		for _, r := range l.Rules {
			if r.Condition == "" {
				v.err("%s/logic[%s]/rule: condition darf nicht leer sein", prefix, l.Field)
			}
			if r.Class == "" && r.Style == "" {
				v.err("%s/logic[%s]/rule: class oder style muss gesetzt sein", prefix, l.Field)
			}
		}
	}

	for _, nav := range c.Navigates {
		if nav.Event == "" {
			v.err("%s/navigate: event darf nicht leer sein", prefix)
		}
		if nav.To == "" {
			v.err("%s/navigate[%s]: to darf nicht leer sein", prefix, nav.Event)
		}
		if nav.Bind != "" && !validBind(nav.Bind) {
			v.err("%s/navigate[%s]: bind %q ungültig", prefix, nav.Event, nav.Bind)
		}
	}

	for _, r := range c.Relations {
		if r.Name == "" {
			v.err("%s/relation: name darf nicht leer sein", prefix)
		}
		if r.Context == "" {
			v.err("%s/relation %q: context fehlt", prefix, r.Name)
		}
		if !oneOf(r.Type, "has_many", "has_one", "belongs_to") {
			v.err("%s/relation %q: type %q ungültig", prefix, r.Name, r.Type)
		}
		if !validBind(r.Bind) {
			v.err("%s/relation %q: bind %q ungültig", prefix, r.Name, r.Bind)
		}
	}

	for _, p := range c.AIPrompts {
		if p.Name == "" {
			v.err("%s/ai: name darf nicht leer sein", prefix)
		}
		if strings.TrimSpace(p.Text) == "" {
			v.err("%s/ai %q: text darf nicht leer sein", prefix, p.Name)
		}
	}
}

func ValidateAll(files []*DSLFile) error {
	var allErrors []string
	for _, f := range files {
		if err := Validate(f); err != nil {
			allErrors = append(allErrors, err.Error())
		}
	}
	if len(allErrors) == 0 {
		return nil
	}
	return fmt.Errorf("dsl validation fehlgeschlagen:\n%s", strings.Join(allErrors, "\n"))
}

func oneOf(val string, options ...string) bool {
	for _, o := range options {
		if val == o {
			return true
		}
	}
	return false
}

func validBind(bind string) bool {
	parts := strings.SplitN(bind, "->", 2)
	if len(parts) != 2 {
		return false
	}
	return strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != ""
}

func splitListValidation(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
