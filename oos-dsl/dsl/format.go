package dsl

// format.go — Display-Formatierung für DSL-Felder.
//
// FormatDisplay wandelt einen Rohwert (string aus dem State) in einen
// formatierten Anzeigestring um — abhängig vom format-Attribut und
// dem RenderContext (Locale + Currency).
//
// Format-Syntax:
//   @            → Screen-Default (cur:Currency aus RenderContext)
//   cur          → Währung mit RenderContext.Currency
//   cur:EUR      → Währung explizit EUR
//   cur:USD      → Währung explizit USD
//   num:0        → Ganzzahl mit Tausendertrenner
//   num:2        → Dezimalzahl mit 2 Stellen
//   date:short   → 31.03.2026 (locale-abhängig)
//   date:long    → 31. März 2026
//   date:iso     → 2026-03-31
//   datetime:short → 31.03.2026 14:49
//   percent      → 0.75 → "75,0 %"
//
// Editierbare Felder zeigen immer den Rohwert — Format gilt nur readonly/Tabelle.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bojanz/currency"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// FormatDisplay formatiert einen Rohwert für die Anzeige.
// format="" → Rohwert unverändert zurückgeben.
func FormatDisplay(raw, format string, rc RenderContext) string {
	if format == "" || raw == "" {
		return raw
	}

	// @ → Screen-Default: cur mit RenderContext.Currency
	if format == "@" {
		format = "cur:" + rc.Currency
	}

	typ, param := splitFormat(format)

	switch typ {
	case "cur":
		return formatCurrency(raw, param, rc)
	case "num":
		return formatNumber(raw, param, rc)
	case "date":
		return formatDate(raw, param, rc)
	case "datetime":
		return formatDateTime(raw, param, rc)
	case "percent":
		return formatPercent(raw, rc)
	}

	return raw
}

// resolveFormat löst ein format-Attribut gegen den Screen-Default auf.
// Gibt das effektive Format zurück das an FormatDisplay übergeben wird.
func resolveFormat(fieldFormat, screenCur string) string {
	if fieldFormat == "@" && screenCur != "" {
		return "cur:" + screenCur
	}
	return fieldFormat
}

// ── Interne Format-Funktionen ─────────────────────────────────────────────────

func formatCurrency(raw, currencyCode string, rc RenderContext) string {
	if currencyCode == "" {
		currencyCode = rc.Currency
	}
	if currencyCode == "" {
		currencyCode = "EUR"
	}

	// Rohwert parsen
	f, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
	if err != nil {
		return raw
	}

	// bojanz/currency: Amount aus String
	amountStr := strconv.FormatFloat(f, 'f', 2, 64)
	amount, err := currency.NewAmount(amountStr, currencyCode)
	if err != nil {
		return raw
	}

	locale := currency.NewLocale(rc.Locale)
	formatter := currency.NewFormatter(locale)
	return formatter.Format(amount)
}

func formatNumber(raw, decimals string, rc RenderContext) string {
	prec := 0
	if decimals != "" {
		fmt.Sscanf(decimals, "%d", &prec)
	}

	f, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
	if err != nil {
		return raw
	}

	// golang.org/x/text/message für locale-korrekte Tausendertrenner
	tag := parseLanguageTag(rc.Locale)
	p := message.NewPrinter(tag)
	if prec == 0 {
		return p.Sprintf("%d", int64(f))
	}
	format := fmt.Sprintf("%%.%df", prec)
	return p.Sprintf(format, f)
}

func formatDate(raw, style string, rc RenderContext) string {
	t := parseTime(raw)
	if t.IsZero() {
		return raw
	}

	switch style {
	case "iso":
		return t.Format("2006-01-02")
	case "long":
		return formatDateLong(t, rc.Locale)
	default: // "short"
		return formatDateShort(t, rc.Locale)
	}
}

func formatDateTime(raw, style string, rc RenderContext) string {
	t := parseTime(raw)
	if t.IsZero() {
		return raw
	}

	dateStr := formatDateShort(t, rc.Locale)
	timeStr := t.Format("15:04")
	return dateStr + " " + timeStr
}

func formatPercent(raw string, rc RenderContext) string {
	f, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
	if err != nil {
		return raw
	}

	tag := parseLanguageTag(rc.Locale)
	p := message.NewPrinter(tag)
	return p.Sprintf("%.1f %%", f*100)
}

// ── Datum-Hilfsfunktionen ─────────────────────────────────────────────────────

// parseTime versucht gängige Datumsformate zu parsen.
var dateFormats = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02",
}

func parseTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	for _, layout := range dateFormats {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

func formatDateShort(t time.Time, locale string) string {
	// Locale-spezifisches Kurzformat
	switch {
	case strings.HasPrefix(locale, "en"):
		return t.Format("01/02/2006")
	default: // de, fr, ...
		return t.Format("02.01.2006")
	}
}

func formatDateLong(t time.Time, locale string) string {
	months := monthNames(locale)
	return fmt.Sprintf("%d. %s %d", t.Day(), months[t.Month()-1], t.Year())
}

func monthNames(locale string) []string {
	if strings.HasPrefix(locale, "en") {
		return []string{
			"January", "February", "March", "April", "May", "June",
			"July", "August", "September", "October", "November", "December",
		}
	}
	// Deutsch als Default
	return []string{
		"Januar", "Februar", "März", "April", "Mai", "Juni",
		"Juli", "August", "September", "Oktober", "November", "Dezember",
	}
}

// ── Sprach-Hilfsfunktionen ───────────────────────────────────────────────────

func parseLanguageTag(locale string) language.Tag {
	if locale == "" {
		return language.German
	}
	tag, err := language.Parse(locale)
	if err != nil {
		return language.German
	}
	return tag
}

func splitFormat(format string) (typ, param string) {
	parts := strings.SplitN(format, ":", 2)
	typ = parts[0]
	if len(parts) == 2 {
		param = parts[1]
	}
	return
}
