// scripts/test-validator.ts — ad-hoc smoke test for the
// domain DSL validators. Not wired into a CI flow; this is a
// local sanity check that exits non-zero if the expected
// diagnostic shape stops appearing.
//
// Run:  bun run scripts/test-validator.ts
//
// Add a case to the table below when adding a new validator:
// shouldFail=true means the parser+validator pipeline must
// produce at least one severity=1 (Error) diagnostic.

import { createOnisinServices } from "oos-dsls-ts";

const svc = createOnisinServices();

const cases: { name: string; text: string; shouldFail: boolean }[] = [
	{
		name: "ok: regular name",
		text: "domain person from person@demo {\n  permission admin read, write\n  field id : int readonly\n}",
		shouldFail: false,
	},
	{
		name: "fail: starts with global.",
		text: "domain global.illegal from x@demo {\n  permission admin read\n  field id : int readonly\n}",
		shouldFail: true,
	},
	{
		name: "ok: name contains 'global' but not as prefix",
		text: "domain my_global from x@demo {\n  permission admin read\n  field id : int readonly\n}",
		shouldFail: false,
	},
];

let allOk = true;
for (const c of cases) {
	const r = await svc.parse({
		language: "domain",
		uri: `inmemory:///test/${c.name.replace(/[^\w]+/g, "_")}`,
		text: c.text,
	});
	const errors = r.diagnostics.filter((d) => d.severity === 1);
	const failed = errors.length > 0;
	const ok = failed === c.shouldFail;
	allOk = allOk && ok;
	const tag = ok ? "OK  " : "FAIL";
	console.log(`${tag}  ${c.name}`);
	for (const e of errors) {
		console.log(`       ${e.message}`);
	}
}

process.exit(allOk ? 0 : 1);
