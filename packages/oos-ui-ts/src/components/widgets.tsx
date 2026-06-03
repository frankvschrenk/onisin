// widgets.tsx — Mantine renderers for bindable widget kinds.
//
// One small component per widget kind. The dispatcher `<BoundWidget>`
// reads the bind path through `useBoundValue` and forwards [value,
// setValue] into the right Mantine input.
//
// Widget inventory:
//
//   Text inputs:  text, email, password, textarea, json
//   Numeric:      number, slider, rangeslider, rating, progress
//   Date/Time:    date, time, daterange  (@mantine/dates)
//   Choice:       select, multiselect, combobox, radio, check, switch
//   Rich:         color, file, tags, badge, avatar
//
// rangeslider stores its value as "min,max" (two numbers joined by
// a comma). tags stores its value as a comma-joined string of tokens.
// All other widgets use a single string.

import {
	Avatar,
	Badge,
	Checkbox,
	ColorInput,
	FileInput,
	JsonInput,
	MultiSelect,
	NumberInput,
	PasswordInput,
	Progress,
	RangeSlider,
	Rating,
	SegmentedControl,
	Select,
	Slider,
	Stack,
	Switch,
	TagsInput,
	Text,
	Textarea,
	TextInput,
} from "@mantine/core";
import { DateInput, DatePickerInput, TimeInput } from "@mantine/dates";
import type { ReactElement } from "react";
import type { WidgetDef } from "oos-dsls-ts/types";

import { useBoundOptions, useBoundValue, useFieldOptionsKey } from "../hooks";
import { formatValue } from "../format";

// ─── Dispatcher ───────────────────────────────────────────────────────────────

/**
 * BoundWidget picks the right specialised component based on
 * `def.widget` and supplies a uniform binding via `useBoundValue`.
 */
export function BoundWidget({ def }: { def: WidgetDef }): ReactElement {
	const path = `${def.bind.domain}.${def.bind.field}`;
	const [value, setValue] = useBoundValue(path);
	const label = def.caption;

	switch (def.widget) {

		// ─ Text inputs ─────────────────────────────────────────────────

		case "text":
		case "email":
			return (
				<TextInput
					label={label}
					placeholder={def.placeholder}
					type={def.widget === "email" ? "email" : "text"}
					value={value}
					readOnly={def.readOnly}
					onChange={(e) => setValue(e.currentTarget.value)}
					autoFocus={def.focus}
				/>
			);

		case "password":
			return (
				<PasswordInput
					label={label}
					placeholder={def.placeholder}
					value={value}
					readOnly={def.readOnly}
					onChange={(e) => setValue(e.currentTarget.value)}
					autoFocus={def.focus}
				/>
			);

		case "textarea":
			return (
				<Textarea
					label={label}
					placeholder={def.placeholder}
					value={value}
					readOnly={def.readOnly}
					onChange={(e) => setValue(e.currentTarget.value)}
					autoFocus={def.focus}
					autosize
					minRows={3}
				/>
			);

		case "json":
			return (
				<JsonInput
					label={label}
					placeholder={def.placeholder ?? "{"}
					value={value}
					readOnly={def.readOnly}
					onChange={setValue}
					autosize
					minRows={3}
					formatOnBlur
				/>
			);

		// ─ Numeric ──────────────────────────────────────────────────

		case "number": {
			const n = value === "" ? "" : Number(value);
			return (
				<NumberInput
					label={label}
					placeholder={def.placeholder}
					value={n}
					readOnly={def.readOnly}
					onChange={(v) => setValue(v === "" ? "" : String(v))}
					min={def.min}
					max={def.max}
					step={def.step}
				/>
			);
		}

		case "slider": {
			const n = value === "" ? (def.min ?? 0) : Number(value);
			return (
				<Stack gap={4}>
					{label && <Text size="sm">{label}</Text>}
					<Slider
						value={Number.isFinite(n) ? n : 0}
						onChange={(v) => setValue(String(v))}
						min={def.min}
						max={def.max}
						step={def.step}
						disabled={def.readOnly}
					/>
				</Stack>
			);
		}

		case "rangeslider": {
			// Stored as "min,max"; falls back to [0, 100] if unparseable.
			const parts = value.split(",").map(Number);
			const rangeVal: [number, number] =
				parts.length === 2 && parts.every(Number.isFinite)
					? [parts[0], parts[1]]
					: [def.min ?? 0, def.max ?? 100];
			return (
				<Stack gap={4}>
					{label && <Text size="sm">{label}</Text>}
					<RangeSlider
						value={rangeVal}
						onChange={([lo, hi]) => setValue(`${lo},${hi}`)}
						min={def.min}
						max={def.max}
						step={def.step}
						disabled={def.readOnly}
					/>
				</Stack>
			);
		}

		case "rating": {
			const n = value === "" ? 0 : Number(value);
			return (
				<Stack gap={4}>
					{label && <Text size="sm">{label}</Text>}
					<Rating
						value={Number.isFinite(n) ? n : 0}
						onChange={(v) => setValue(String(v))}
						readOnly={def.readOnly}
					/>
				</Stack>
			);
		}

		case "progress": {
			// Value is expected to be 0–100; stored as a plain number string.
			const n = value === "" ? 0 : Number(value);
			return (
				<Stack gap={4}>
					{label && <Text size="sm">{label}</Text>}
					<Progress value={Number.isFinite(n) ? n : 0} />
				</Stack>
			);
		}

		// ─ Date / Time ─────────────────────────────────────────────

		case "date": {
			// Stored as ISO date string; DateInput works with Date objects
			// internally and shows DD.MM.YYYY to the user.
			const d = value ? new Date(value) : null;
			const dValid = d && !isNaN(d.getTime()) ? d : null;
			return (
				<DateInput
					label={label}
					placeholder={def.placeholder ?? "TT.MM.JJJJ"}
					value={dValid}
					onChange={(v) => setValue(v ?? "")}
					readOnly={def.readOnly}
					valueFormat="DD.MM.YYYY"
				/>
			);
		}

		case "daterange": {
			// Stored as "YYYY-MM-DD,YYYY-MM-DD".
			const [fromStr, toStr] = value.split(",");
			const from = fromStr ? new Date(fromStr) : null;
			const to   = toStr   ? new Date(toStr)   : null;
			return (
				<DatePickerInput
					type="range"
					label={label}
					placeholder={def.placeholder ?? "Start – Ende"}
					value={[
						from && !isNaN(from.getTime()) ? from : null,
						to   && !isNaN(to.getTime())   ? to   : null,
					]}
						onChange={([f, t]) => setValue(`${f ?? ""},${t ?? ""}`)}
					readOnly={def.readOnly}
					valueFormat="DD.MM.YYYY"
				/>
			);
		}

		case "time":
			return (
				<TimeInput
					label={label}
					placeholder={def.placeholder ?? "HH:MM"}
					value={value}
					readOnly={def.readOnly}
					onChange={(e) => setValue(e.currentTarget.value)}
				/>
			);

		// ─ Choice ─────────────────────────────────────────────────

		case "select":
			return <SelectWidget def={def} value={value} setValue={setValue} />;

		case "multiselect":
			return <MultiSelectWidget def={def} value={value} setValue={setValue} />;

		case "combobox":
			return <SelectWidget def={def} value={value} setValue={setValue} searchable />;

		case "radio":
			return <RadioWidget def={def} value={value} setValue={setValue} />;

		case "check":
			return (
				<Checkbox
					label={label ?? path}
					checked={value === "true"}
					disabled={def.readOnly}
					onChange={(e) => setValue(e.currentTarget.checked ? "true" : "false")}
				/>
			);

		case "switch":
			return (
				<Switch
					label={label ?? path}
					checked={value === "true"}
					disabled={def.readOnly}
					onChange={(e) => setValue(e.currentTarget.checked ? "true" : "false")}
				/>
			);

		// ─ Rich ───────────────────────────────────────────────────

		case "color":
			return (
				<ColorInput
					label={label}
					value={value}
					onChange={setValue}
					readOnly={def.readOnly}
				/>
			);

		case "file":
			return (
				<FileInput
					label={label}
					placeholder={def.placeholder ?? "Choose file…"}
					disabled={def.readOnly}
					onChange={(f) => setValue(f ? f.name : "")}
				/>
			);

		case "tags": {
			// Stored as comma-joined token list.
			const tags = value === "" ? [] : value.split(",").map((t) => t.trim());
			return (
				<TagsInput
					label={label}
					placeholder={def.placeholder ?? "Add tag…"}
					value={tags}
					onChange={(v) => setValue(v.join(","))}
					readOnly={def.readOnly}
				/>
			);
		}

		case "badge":
			return (
				<Stack gap={4}>
					{label && <Text size="sm">{label}</Text>}
					<Badge variant="light">
						{formatValue(value, def.format) || "—"}
					</Badge>
				</Stack>
			);

		case "avatar": {
			// Treat the value as either a URL or initials (1-2 chars).
			const isUrl = value.startsWith("http") || value.startsWith("/");
			const initials = value
				.split(" ")
				.map((w) => w[0]?.toUpperCase() ?? "")
				.slice(0, 2)
				.join("");
			return (
				<Stack gap={4}>
					{label && <Text size="sm">{label}</Text>}
					<Avatar src={isUrl ? value : undefined} radius="xl">
						{!isUrl && (initials || "?")}
					</Avatar>
				</Stack>
			);
		}
	}
}

// ─── Helper components ────────────────────────────────────────────────────────────

function SelectWidget({
	def,
	value,
	setValue,
	searchable = false,
}: {
	def: WidgetDef;
	value: string;
	setValue: (v: string) => void;
	searchable?: boolean;
}): ReactElement {
	const optionsKey = useFieldOptionsKey(def.bind.field);
	const options = useBoundOptions(optionsKey ?? "");
	return (
		<Select
			label={def.caption}
			placeholder={def.placeholder}
			data={options.map((o) => ({ value: o.value, label: o.label }))}
			value={value === "" ? null : value}
			onChange={(v) => setValue(v ?? "")}
			readOnly={def.readOnly}
			searchable={searchable}
		/>
	);
}

function MultiSelectWidget({
	def,
	value,
	setValue,
}: {
	def: WidgetDef;
	value: string;
	setValue: (v: string) => void;
}): ReactElement {
	const optionsKey = useFieldOptionsKey(def.bind.field);
	const options = useBoundOptions(optionsKey ?? "");
	const selected = value === "" ? [] : value.split(",");
	return (
		<MultiSelect
			label={def.caption}
			placeholder={def.placeholder}
			data={options.map((o) => ({ value: o.value, label: o.label }))}
			value={selected}
			onChange={(v) => setValue(v.join(","))}
			readOnly={def.readOnly}
		/>
	);
}

function RadioWidget({
	def,
	value,
	setValue,
}: {
	def: WidgetDef;
	value: string;
	setValue: (v: string) => void;
}): ReactElement {
	const optionsKey = useFieldOptionsKey(def.bind.field);
	const options = useBoundOptions(optionsKey ?? "");
	return (
		<Stack gap={4}>
			{def.caption && <Text size="sm">{def.caption}</Text>}
			<SegmentedControl
				value={value}
				onChange={setValue}
				data={options.map((o) => ({ value: o.value, label: o.label }))}
				disabled={def.readOnly}
			/>
		</Stack>
	);
}
