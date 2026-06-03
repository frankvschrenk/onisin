// PreviewRenderer.tsx — Pure live-preview renderer.
//
// Renders a `.view` source as a Mantine form using the same
// auto-mock pipeline that the in-tab Preview uses. Extracted from
// Preview.tsx so the same React tree can mount in either:
//
//   * the in-tab Preview pane (App's mainview), or
//   * a detached preview window (previewview).
//
// The component receives a domain-source loader as a prop instead
// of reaching for a singleton RPC handle. That keeps it agnostic
// to *which* renderer it lives in: the mainview passes its rpc
// client, the detached window receives the domain source through
// a messaging hop and supplies a synchronous lookup.
//
// Pipeline:
//   source text  →  parseViewInWorker()  →  ViewDef
//   ↓
//   loadDomainSource(view.domain) → parseDomainInWorker → DomainDef
//   ↓
//   mockEnvelope(view, domain) → loadEnvelope(state) → <OnisinView>
//
// Parsing happens off the main thread (parse-client.ts) because
// the Langium dependency chain breaks Electrobun's main-thread
// bundler. Type imports erase at compile time so they stay.
//
// The pane re-parses on a 300 ms debounce to keep typing
// responsive. Parse errors are shown as a banner; the rendered
// output reflects the last successful parse so the user can keep
// editing without flicker.

import { useEffect, useMemo, useRef, useState } from "react";
import { Alert, ScrollArea, Stack, Text } from "@mantine/core";
import type { DomainDef, ViewDef } from "oos-dsls-ts/types";
import { OnisinView, ViewState, loadEnvelope, mockEnvelope } from "oos-ui-ts";

import { parseDomainInWorker, parseViewInWorker } from "../../lang/worker/parse-client";

const DEBOUNCE_MS = 300;

/**
 * Function the host renderer supplies for resolving a bound
 * domain's source. The mainview hands over its rpc.loadDomain;
 * the detached preview window resolves through its own RPC
 * channel after asking the bun process.
 */
export type LoadDomainSource = (
	id: string,
) => Promise<{ source: string | null; error?: string }>;

export function PreviewRenderer({
	source,
	viewId,
	loadDomainSource,
}: {
	source: string;
	viewId: string | null;
	loadDomainSource: LoadDomainSource;
}) {
	const [def, setDef] = useState<ViewDef | undefined>(undefined);
	const [domainDef, setDomainDef] = useState<DomainDef | undefined>(undefined);
	const [errors, setErrors] = useState<string[]>([]);
	const [parsing, setParsing] = useState(false);

	// Re-parse on debounced source change.
	const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
	useEffect(() => {
		if (timer.current) clearTimeout(timer.current);
		if (!source.trim()) {
			setDef(undefined);
			setDomainDef(undefined);
			setErrors([]);
			return;
		}
		setParsing(true);
		timer.current = setTimeout(async () => {
			try {
				const result = await parseViewInWorker(
					source,
					`inmemory://oosd/preview/${viewId ?? "scratch"}`,
				);
				setDef(result.def);
				setErrors(
					result.diagnostics
						.filter((d) => d.severity === 1)
						.map((d) => `${d.range.start.line + 1}:${d.range.start.character + 1}  ${d.message}`),
				);
			} catch (err) {
				setErrors([err instanceof Error ? err.message : String(err)]);
			} finally {
				setParsing(false);
			}
		}, DEBOUNCE_MS);
		return () => {
			if (timer.current) clearTimeout(timer.current);
		};
	}, [source, viewId]);

	// Whenever the parsed view changes, fetch and parse its bound
	// domain so the auto-mock generator has the field shapes it
	// needs. Failures here are silent — a missing domain just means
	// the preview renders with an empty envelope, which is still
	// useful for layout work.
	useEffect(() => {
		if (!def) {
			setDomainDef(undefined);
			return;
		}
		let cancelled = false;
		(async () => {
			try {
				// The preview hydrates from the primary domain; secondary
				// domains in multi-domain views are not mocked yet.
				const primaryDomain = def.domains[0]?.name;
				if (!primaryDomain) {
					setDomainDef(undefined);
					return;
				}
				const res = await loadDomainSource(primaryDomain);
				if (cancelled) return;
				if (!res.source) {
					setDomainDef(undefined);
					return;
				}
				const parsed = await parseDomainInWorker(
					res.source,
					`inmemory://oosd/preview-domain/${primaryDomain}`,
				);
				if (!cancelled) setDomainDef(parsed.def);
			} catch {
				if (!cancelled) setDomainDef(undefined);
			}
		})();
		return () => {
			cancelled = true;
		};
	}, [def, loadDomainSource]);

	// Build a fresh ViewState whenever the def or domainDef changes.
	// Switching to a new view should not leak the previous form's
	// values; switching domains should regenerate the mock envelope.
	const state = useMemo(() => {
		const s = new ViewState();
		if (def && domainDef && def.domains[0]?.name === domainDef.name) {
			loadEnvelope(s, mockEnvelope(def, domainDef));
		}
		return s;
	}, [def, domainDef]);

	if (!source.trim()) {
		return <EmptyState message="Select a view to preview." />;
	}

	if (!def && errors.length > 0) {
		return (
			<Stack p="md" gap="sm">
				<Alert color="red" title="Parse errors">
					<Stack gap={4}>
						{errors.map((e, i) => (
							<Text key={i} size="sm" ff="monospace">
								{e}
							</Text>
						))}
					</Stack>
				</Alert>
			</Stack>
		);
	}

	if (!def) {
		return <EmptyState message={parsing ? "Parsing…" : "No view to render."} />;
	}

	return (
		<ScrollArea style={{ height: "100%" }}>
			<Stack p="md" gap="sm">
				{errors.length > 0 && (
					<Alert color="orange" title="Parse warnings">
						<Stack gap={4}>
							{errors.map((e, i) => (
								<Text key={i} size="sm" ff="monospace">
									{e}
								</Text>
							))}
						</Stack>
					</Alert>
				)}
				<OnisinView def={def} state={state} domain={domainDef} />
			</Stack>
		</ScrollArea>
	);
}

function EmptyState({ message }: { message: string }) {
	return (
		<Stack
			align="center"
			justify="center"
			style={{ height: "100%" }}
			c="dimmed"
		>
			<Text>{message}</Text>
		</Stack>
	);
}
