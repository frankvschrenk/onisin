// NewItemDialog.tsx — Modal for creating a new domain or view row.
//
// Two inputs:
//   - the new id (validated against the DSL identifier rules and
//     the reserved `global.` prefix; collisions with an existing
//     id are flagged before the round-trip),
//   - a template choice — empty stub or a copy of the currently
//     selected row. Copying is the high-leverage option: most new
//     rows the user creates are variants of an existing one, and
//     hand-typing a 200-line view from scratch is rarely the path.
//
// The component is fully controlled — App.tsx owns `opened` and
// drives `onSubmit` with the gathered fields. That keeps creation
// state out of this component and makes it trivial to reset
// between invocations.

import { useEffect, useMemo, useState } from "react";
import {
	Alert,
	Button,
	Group,
	Modal,
	Radio,
	Stack,
	TextInput,
} from "@mantine/core";

import type { Kind } from "../types";

const ID_RE = /^[a-z][a-z0-9_]*$/;

export type Template = "empty" | "copy";

export type SubmitArgs = {
	id:       string;
	template: Template;
};

export function NewItemDialog({
	opened,
	onClose,
	onSubmit,
	kind,
	existingIds,
	currentSelection,
}: {
	opened: boolean;
	onClose: () => void;
	onSubmit: (args: SubmitArgs) => Promise<{ ok: boolean; error?: string }>;
	kind: Kind;
	existingIds: string[];
	currentSelection: string | null;
}) {
	const [id, setId] = useState("");
	const [template, setTemplate] = useState<Template>("empty");
	const [submitting, setSubmitting] = useState(false);
	const [error, setError] = useState<string | null>(null);

	// Reset on open so a previous attempt doesn't bleed in.
	useEffect(() => {
		if (opened) {
			setId("");
			setTemplate(currentSelection ? "copy" : "empty");
			setError(null);
			setSubmitting(false);
		}
	}, [opened, currentSelection]);

	const validation = useMemo(() => validate(id, existingIds), [id, existingIds]);
	const canSubmit = validation === null && !submitting;

	async function handleSubmit() {
		if (!canSubmit) return;
		setSubmitting(true);
		setError(null);
		try {
			const res = await onSubmit({ id, template });
			if (res.ok) {
				onClose();
			} else {
				setError(res.error ?? "creation failed");
			}
		} finally {
			setSubmitting(false);
		}
	}

	const heading = kind === "domain" ? "New Domain" : "New View";
	const placeholder =
		kind === "domain" ? "e.g. customer" : "e.g. customer_detail";

	return (
		<Modal
			opened={opened}
			onClose={onClose}
			title={heading}
			centered
			size="md"
		>
			<Stack gap="md">
				<TextInput
					label="Id"
					placeholder={placeholder}
					value={id}
					onChange={(e) => setId(e.currentTarget.value)}
					error={validation && id.length > 0 ? validation : undefined}
					autoFocus
					data-autofocus
					onKeyDown={(e) => {
						if (e.key === "Enter") handleSubmit();
					}}
				/>
				<Radio.Group
					label="Template"
					value={template}
					onChange={(v) => setTemplate(v as Template)}
				>
					<Stack gap="xs" mt="xs">
						<Radio value="empty" label="Empty stub" />
						<Radio
							value="copy"
							label={
								currentSelection
									? `Copy of ${currentSelection}`
									: "Copy current (none selected)"
							}
							disabled={!currentSelection}
						/>
					</Stack>
				</Radio.Group>
				{error && (
					<Alert color="red" title="Could not create">
						{error}
					</Alert>
				)}
				<Group justify="flex-end" mt="sm">
					<Button variant="subtle" onClick={onClose} disabled={submitting}>
						Cancel
					</Button>
					<Button onClick={handleSubmit} loading={submitting} disabled={!canSubmit}>
						Create
					</Button>
				</Group>
			</Stack>
		</Modal>
	);
}

// validate returns an error message or null if the id is acceptable.
// Empty input is allowed silently — the form just leaves the submit
// button disabled — so the user can open the dialog and read the
// rules without seeing red right away.
function validate(id: string, existingIds: string[]): string | null {
	if (id.length === 0)             return "";
	if (id.startsWith("global."))    return "ids starting with 'global.' are reserved";
	if (!ID_RE.test(id))             return "lowercase letters, digits, underscore — must start with a letter";
	if (existingIds.includes(id))    return `'${id}' already exists`;
	return null;
}
