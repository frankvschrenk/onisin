// Preview.tsx — In-tab live preview pane.
//
// Thin wrapper around PreviewRenderer that wires the mainview's
// rpc client as the domain-source loader. The actual render
// pipeline lives in PreviewRenderer so the detached preview
// window can mount the same component.

import { useCallback } from "react";

import { PreviewRenderer } from "./PreviewRenderer";
import { rpc } from "../rpc";

export function Preview({
	source,
	viewId,
}: {
	source: string;
	viewId: string | null;
}) {
	const loadDomainSource = useCallback(
		(id: string) => rpc.loadDomain({ id }),
		[],
	);
	return (
		<PreviewRenderer
			source={source}
			viewId={viewId}
			loadDomainSource={loadDomainSource}
		/>
	);
}
