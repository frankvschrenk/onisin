// format.ts — Render a raw string value through a FormatDef using
// the Intl APIs. Mirrors the intent of Go's `dsl.FormatDisplay`.
//
// Locale and currency are passed in by the caller (typically pinned
// from the renderer's RenderContext). When no locale is supplied we
// fall back to the runtime default — fine for the editor preview.

import type { FormatDef } from "oos-dsls-ts/types";

export interface FormatContext {
	/** BCP-47 locale tag, e.g. "de-DE". Falls back to runtime default. */
	locale?: string;
	/** ISO-4217 currency code, e.g. "EUR". Defaults to "EUR". */
	currency?: string;
}

const defaultContext: Required<FormatContext> = {
	locale: typeof navigator !== "undefined" ? navigator.language : "en-US",
	currency: "EUR",
};

/**
 * formatValue takes a raw string value plus a FormatDef and returns
 * a display string. Any value that can't be parsed as a number for
 * numeric formats is returned as-is — the renderer still shows
 * something instead of "NaN".
 */
export function formatValue(
	raw: string,
	format: FormatDef | undefined,
	ctx: FormatContext = {},
): string {
	if (!format || raw === "") return raw;

	const locale = ctx.locale ?? defaultContext.locale;
	const currency = ctx.currency ?? defaultContext.currency;

	switch (format.kind) {
		case "currency": {
			const n = Number(raw);
			if (!Number.isFinite(n)) return raw;
			return new Intl.NumberFormat(locale, {
				style: "currency",
				currency,
			}).format(n);
		}
		case "number": {
			const n = Number(raw);
			if (!Number.isFinite(n)) return raw;
			const digits = typeof format.detail === "number" ? format.detail : undefined;
			return new Intl.NumberFormat(locale, {
				minimumFractionDigits: digits,
				maximumFractionDigits: digits,
			}).format(n);
		}
		case "percent": {
			const n = Number(raw);
			if (!Number.isFinite(n)) return raw;
			const digits = typeof format.detail === "number" ? format.detail : undefined;
			return new Intl.NumberFormat(locale, {
				style: "percent",
				minimumFractionDigits: digits,
				maximumFractionDigits: digits,
			}).format(n);
		}
		case "datetime":
		case "date":
		case "time": {
			const d = new Date(raw);
			if (Number.isNaN(d.getTime())) return raw;
			const style = mapDateStyle(format.detail);
			const opts: Intl.DateTimeFormatOptions = {};
			if (format.kind !== "time") opts.dateStyle = style;
			if (format.kind !== "date") opts.timeStyle = style;
			return new Intl.DateTimeFormat(locale, opts).format(d);
		}
	}
}

function mapDateStyle(
	detail: FormatDef["detail"],
): Intl.DateTimeFormatOptions["dateStyle"] {
	switch (detail) {
		case "short":
		case "medium":
		case "long":
		case "full":
			return detail;
		default:
			return "medium";
	}
}
