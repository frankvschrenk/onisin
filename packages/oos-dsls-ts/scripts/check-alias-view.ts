// scripts/check-alias-view.ts — Verify the new alias / multi-domain
// syntax round-trips through the parser and the runtime mapper.
//
// The fixtures here live inline so they can exercise alias edge
// cases (single domain with alias, multi-domain, duplicate alias
// rejected by the validator) without having to seed real .view
// files into the demo set.

import { parseView } from "oos-dsls-ts";

interface Case {
	name:       string;
	source:     string;
	expectErr?: string;
}

const cases: Case[] = [
	{
		name: "single domain, no alias",
		source:
`view person_list "Personen" over person {
  toolbar {
    new "Neu" -> person_detail
  }
  table -> person.rows {
    column person.firstname "Vorname"
  }
}
`,
	},
	{
		name: "single domain, with alias",
		source:
`view person_detail "Person" over person(p) {
  toolbar {
    save
    exit
  }
  text "Vorname"  -> p.firstname focus
  text "Nachname" -> p.lastname
}
`,
	},
	{
		name: "two domains with aliases",
		source:
`view manager_view "Manager" over person(p), address(a) {
  toolbar {
    save
    exit
  }
  text "Vorname" -> p.firstname
  text "Stadt"   -> a.city
}
`,
	},
	{
		name: "duplicate alias rejected",
		source:
`view bad_view "Bad" over person(x), address(x) {
  text "X" -> x.foo
}
`,
		expectErr: "already used",
	},
];

let failures = 0;
for (const tc of cases) {
	let result;
	try {
		result = await parseView(tc.source, `inmemory://check/${tc.name}`);
	} catch (e) {
		console.log(`[ERR] ${tc.name}: parser threw: ${(e as Error).message}`);
		failures++;
		continue;
	}
	const errors = result.diagnostics.filter((d) => d.severity === 1);
	const allDiags = result.diagnostics;
	if (allDiags.length > 0) {
		console.log(`      diagnostics for "${tc.name}":`);
		for (const d of allDiags) {
			console.log(`        [${d.severity}] ${d.message}`);
		}
	}

	if (tc.expectErr) {
		const matched = errors.some((e) => e.message.includes(tc.expectErr));
		if (matched) {
			console.log(`[OK ] ${tc.name}: rejected with "${tc.expectErr}"`);
		} else {
			console.log(`[ERR] ${tc.name}: expected error "${tc.expectErr}", got:`);
			for (const e of errors) console.log(`        ${e.message}`);
			if (errors.length === 0) console.log(`        (no errors)`);
			failures++;
		}
		continue;
	}

	if (errors.length > 0 || !result.def) {
		console.log(`[ERR] ${tc.name}: unexpected errors`);
		for (const e of errors) console.log(`        ${e.message}`);
		failures++;
		continue;
	}
	const desc = result.def.domains
		.map((d) => `${d.name}${d.alias === d.name ? "" : `(${d.alias})`}${d.primary ? "*" : ""}`)
		.join(", ");
	console.log(`[OK ] ${tc.name}: domains = ${desc}`);
}

if (failures > 0) {
	console.log(`\n${failures} case(s) failed.`);
	process.exit(1);
}
console.log(`\nAll ${cases.length} cases passed.`);
