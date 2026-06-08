// WelcomePanel.tsx — Default landing content shown in the Welcome
// tab. Materialises automatically whenever no other tab group is
// open, so the user never sees a blank stage.
//
// Three jobs at this stage of the project:
//
//   1. Make the chat-driven model visible. New users won't know
//      what they can ask; explicit example prompts answer that.
//   2. Provide a path back into the documentation without going
//      through the burger menu.
//   3. Set the visual tone of the app for first impression.
//
// Quick access actions are wired through the tabs store so they
// behave consistently with the menu — clicking "Documentation"
// here is the same as clicking it in the burger menu.

import {
	Box,
	Button,
	Group,
	Paper,
	Stack,
	Text,
	Title,
} from "@mantine/core";
import { IconBook, IconSparkles } from "@tabler/icons-react";

import { openDocs } from "../store/tabs";

const EXAMPLE_PROMPTS = [
	"Zeig mir alle Personen.",
	"Welche Personen wohnen in Berlin?",
	"Öffne Anna Meier im Detail.",
	"Erstelle eine neue Notiz.",
];

export function WelcomePanel() {
	return (
		<Box style={{ overflow: "auto", height: "100%" }}>
			<Box p="xl" style={{ maxWidth: 720, margin: "0 auto" }}>
				<Stack gap="xl" mt="xl">
					<Stack gap="xs">
						<Group gap="sm" align="center">
							<IconSparkles size={28} stroke={1.5} />
							<Title order={2} fw={600}>
								Willkommen
							</Title>
						</Group>
						<Text c="dimmed" size="sm">
							Stell dem Assistenten eine Frage in der Chat-Spalte
							links. Er sucht das passende Schema, baut die
							Anfrage und öffnet das Ergebnis als Tab.
						</Text>
					</Stack>

					<Section title="Beispielfragen">
						<Stack gap="xs">
							{EXAMPLE_PROMPTS.map((p) => (
								<Paper key={p} withBorder p="sm" radius="sm">
									<Text size="sm">{p}</Text>
								</Paper>
							))}
						</Stack>
					</Section>

					<Section title="Schnellzugriff">
						<Group gap="sm">
							<Button
								variant="default"
								leftSection={<IconBook size={16} />}
								onClick={openDocs}
							>
								Dokumentation öffnen
							</Button>
						</Group>
					</Section>
				</Stack>
			</Box>
		</Box>
	);
}

function Section({
	title,
	children,
}: {
	title:    string;
	children: React.ReactNode;
}) {
	return (
		<Stack gap="xs">
			<Text size="xs" c="dimmed" tt="uppercase" fw={600}>
				{title}
			</Text>
			{children}
		</Stack>
	);
}
