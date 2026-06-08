// PipelineExamplesDrawer.tsx — reference drawer showing example pipelines.
//
// Opened via the ? button in PipelineListPanel header. Shows all supported
// pipeline patterns side by side so the user can copy-paste into their
// own pipeline. Each example is a read-only code block.
//
// We deliberately do NOT use Monaco here: keeping 5 editor instances alive
// inside Mantine Tabs broke Monaco's shared InstantiationService on
// re-open ("InstantiationService has been disposed"). For static read-only
// snippets a Code block is simpler and avoids the lifecycle entirely.

import { useState } from "react";
import {
	ActionIcon,
	Code,
	Drawer,
	ScrollArea,
	Stack,
	Tabs,
	Text,
	Tooltip,
} from "@mantine/core";
import { IconHelp } from "@tabler/icons-react";

// ── Examples ──────────────────────────────────────────────────────────────────

const EXAMPLES: Array<{ label: string; description: string; source: string }> = [
	{
		label: "Einfacher LLM",
		description: "Alle Dokumente einer DB-Quelle direkt in ein LLM laden (mode=context).",
		source: [
			`pipeline "Einfache Analyse" {`,
			`  source db schaeden from pipeline_documents`,
			``,
			`  mode    context`,
			`  llm     "gemma4:26b"`,
			``,
			`  step llm zusammenfassung {`,
			`    from   schaeden`,
			`    mode   aggregate`,
			`    system "Du bist ein erfahrener Versicherungsgutachter."`,
			`    prompt "Erstelle eine strukturierte Zusammenfassung aller Fälle."`,
			`  }`,
			``,
			`  out editor`,
			`}`,
		].join("\n"),
	},
	{
		label: "Einzelfall",
		description: "Einen einzelnen Fall analysieren — Fall-Nr direkt im Editor eintragen.",
		source: [
			`pipeline "Einzel-Fall Analyse" {`,
			`  source db schaeden from pipeline_documents`,
			``,
			`  mode    context`,
			`  llm     "gemma4:latest"`,
			``,
			`  step where einzel {`,
			`    from schaeden`,
			`    where {`,
			`      { fall_nr: { _eq: "SCH-2024-0001" } }`,
			`    }`,
			`  }`,
			``,
			`  step llm analyse {`,
			`    from   einzel`,
			`    mode   aggregate`,
			`    system "Du bist ein erfahrener Versicherungsgutachter."`,
			`    prompt "Erstelle eine pr\u00e4zise Zusammenfassung dieses Schadensfalles."`,
			`  }`,
			``,
			`  out editor`,
			`}`,
		].join("\n"),
	},
	{
		label: "SQL + Semantic",
		description: "SQL-Filter → Cosine-Suche → LLM Analyse.",
		source: [
			`pipeline "Betrugs-Analyse" {`,
			`  source db schaeden from pipeline_documents`,
			``,
			`  mode      context`,
			`  llm       "gemma4:26b"`,
			`  embedding "bge-m3:latest"`,
			``,
			`  step where vorauswahl {`,
			`    from schaeden`,
			`    where {`,
			`      { kategorie: { _eq: "Einbruch" } }`,
			`    }`,
			`  }`,
			``,
			`  step semantic betrug {`,
			`    from   vorauswahl`,
			`    query  "Betrugsverdacht mit Serientäter"`,
			`    query  "kein unabhängiger Zeuge"`,
			`    limit  20`,
			`  }`,
			``,
			`  step llm pruefung {`,
			`    from   betrug`,
			`    mode   per-row`,
			`    system "Du bist ein erfahrener Versicherungsgutachter."`,
			`    prompt "Prüfe diesen Fall auf Betrugsindikatoren. Antworte strukturiert."`,
			`  }`,
			``,
			`  step llm bericht {`,
			`    from   pruefung`,
			`    mode   aggregate`,
			`    prompt "Erstelle einen Betrugsbericht mit Risikoranking."`,
			`  }`,
			``,
			`  out editor`,
			`}`,
		].join("\n"),
	},
	{
		label: "Map-Reduce",
		description: "Alle Fälle einzeln analysieren, dann zusammenfassen.",
		source: [
			`pipeline "Portfolio Analyse" {`,
			`  source db schaeden from pipeline_documents`,
			``,
			`  mode    context`,
			`  llm     "gemma4:26b"`,
			``,
			`  step llm analyse {`,
			`    from   schaeden`,
			`    mode   per-row`,
			`    system "Du bist ein erfahrener Versicherungsgutachter."`,
			`    prompt "Prüfe diesen Schadensfall: Betrug? Risiko? Besonderheiten?"`,
			`  }`,
			``,
			`  step llm bericht {`,
			`    from   analyse`,
			`    mode   aggregate`,
			`    prompt "Erstelle eine Zusammenfassung aller auffälligen Punkte mit Risikoranking."`,
			`  }`,
			``,
			`  out editor`,
			`}`,
		].join("\n"),
	},
	{
		label: "Vektor-Vorauswahl (mass)",
		description: "Vektor-Vorauswahl → LLM pro Fall → aggregierter Bericht (mode=mass).",
		source: [
			`pipeline "Fraud Detection (mass)" {`,
			`  source db  schaeden from pipeline_documents`,
			``,
			`  mode      mass`,
			`  llm       "gemma4:26b"`,
			`  embedding "bge-m3:latest"`,
			``,
			`  step semantic vorauswahl {`,
			`    from  schaeden`,
			`    query "Anzeichen von Versicherungsbetrug"`,
			`    query "widersprüchliche Angaben zum Schadenshergang"`,
			`    limit 20`,
			`  }`,
			``,
			`  step llm pruefung {`,
			`    from   vorauswahl`,
			`    mode   per-row`,
			`    prompt "Prüfe auf Anzeichen von Versicherungsbetrug"`,
			`  }`,
			``,
			`  step llm bericht {`,
			`    from   pruefung`,
			`    mode   aggregate`,
			`    prompt "Erstelle einen Betrugsprüfungs-Bericht mit Risikoranking"`,
			`  }`,
			``,
			`  out editor`,
			`}`,
		].join("\n"),
	},
];

// ── Component ──────────────────────────────────────────────────────────────────

export function PipelineExamplesDrawer() {
	const [open, setOpen] = useState(false);

	return (
		<>
			<Tooltip label="Pipeline examples" withArrow>
				<ActionIcon
					size="sm" variant="subtle"
					onClick={() => setOpen(true)}
				>
					<IconHelp size={16} />
				</ActionIcon>
			</Tooltip>

			<Drawer
				opened={open}
				onClose={() => setOpen(false)}
				title="Pipeline Examples"
				position="right"
				size="lg"
				offset={8}
				radius="md"
			>
				<Tabs
					defaultValue={EXAMPLES[0].label}
					orientation="vertical"
					keepMounted={false}
					style={{ height: "100%" }}
				>
					<Tabs.List w={140} style={{ flexShrink: 0 }}>
						{EXAMPLES.map(e => (
							<Tabs.Tab key={e.label} value={e.label} fz="xs">
								{e.label}
							</Tabs.Tab>
						))}
					</Tabs.List>

					{EXAMPLES.map(e => (
						<Tabs.Panel key={e.label} value={e.label} style={{ flex: 1, overflow: "hidden", display: "flex", flexDirection: "column" }}>
							<Stack gap="xs" p="xs" style={{ flex: 1, overflow: "hidden" }}>
								<Text size="xs" c="dimmed">{e.description}</Text>
								<ScrollArea style={{ flex: 1 }}>
									<Code block style={{ fontSize: 12, whiteSpace: "pre" }}>
										{e.source}
									</Code>
								</ScrollArea>
							</Stack>
						</Tabs.Panel>
					))}
				</Tabs>
			</Drawer>
		</>
	);
}
