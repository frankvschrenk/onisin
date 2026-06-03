// scripts/test-end-to-end.ts — Run the full pipeline without React:
//
//   1. parse a `.view` source to a ViewDef
//   2. create a ViewState
//   3. load an oosp-shaped envelope into the state
//   4. dump everything the renderer would receive
//
// This exercises both packages (`oos-dsls-ts` parser + `oos-ui-ts`
// state and envelope) without pulling in @mantine/core, so it runs
// happily under Bun's default test runner.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { parseView } from "oos-dsls-ts";
import { ViewState, loadEnvelope, buildEvent } from "oos-ui-ts";

// 1. Parse.
const viewPath = resolve(
	import.meta.dir,
	"..",
	"..",
	"oos-dsls-ts",
	"grammar",
	"examples",
	"person_list.view",
);
const source = readFileSync(viewPath, "utf-8");
const { def, diagnostics } = await parseView(source, `file://${viewPath}`);
if (!def) {
	console.error("parse failed", diagnostics);
	process.exit(1);
}

console.log(`parsed: view=${def.name} title="${def.title}" body=${def.body.length}`);

// 2 + 3. State + envelope.
const state = new ViewState();
const envelope = {
	content: {
		person: {
			rows: [
				{ id: 1, firstname: "Frank", lastname: "Müller", email: "frank@onisin.ai", city: "1", age: 38, net_worth: 125000 },
				{ id: 2, firstname: "Anna", lastname: "Schmidt", email: "anna@onisin.ai", city: "2", age: 31, net_worth: 87000 },
			],
		},
	},
	meta: {
		cities: [
			{ value: "1", label: "München" },
			{ value: "2", label: "Berlin" },
		],
	},
};
loadEnvelope(state, envelope);

console.log("state snapshot:", state.snapshot());
console.log("city options:", state.getOptions("cities"));

// 4. Outbound payload — what `Save` would POST.
state.set("person.firstname", "Frank-Edited");
const payload = buildEvent(def.name, "save", state);
console.log("outbound mutation payload:", payload);
