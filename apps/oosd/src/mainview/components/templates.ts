// templates.ts — Stub source bodies for newly created domains and
// views. The dialog uses these when the user picks "Empty stub";
// "Copy current" reuses the active source instead.
//
// These are deliberately minimal scaffolds: enough for the file
// to parse cleanly so the editor diagnostics don't light up red on
// first save, but obviously incomplete so the user knows where to
// edit.

import type { Kind } from "../types";

/**
 * stubSource returns a minimal valid source for the given kind,
 * with the new id already substituted into the header. The user
 * can save it as-is and immediately start filling in fields or
 * widgets without first chasing a parser error.
 */
export function stubSource(kind: Kind, id: string): string {
	if (kind === "domain") return domainStub(id);
	return viewStub(id);
}

function domainStub(id: string): string {
	return `// ${id}.domain — auto-generated stub.

domain ${id} from ${id}@demo {

  permission admin read, write, delete

  field id : int readonly
}
`;
}

function viewStub(id: string): string {
	// New view stubs intentionally bind to a placeholder domain
	// `<domain>` — the user has to point this at an existing
	// domain before the view will render. Until then the parser
	// accepts the source and the preview shows an empty state.
	return `// ${id}.view — auto-generated stub.

view ${id} "${humanise(id)}" over <domain> {

  toolbar {
    save
    exit
  }

  section "Details" p=md {
    // text "Field" -> <domain>.<field>
  }
}
`;
}

// humanise converts an id like "customer_detail" into a window
// title like "Customer detail". Capitalises the first letter and
// replaces underscores with spaces — nothing fancier; the user
// fixes anything they don't like.
function humanise(id: string): string {
	const spaced = id.replace(/_/g, " ");
	return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}
