// store/node-id.ts — in-memory singleton for the local oos node ID.
//
// Populated once by LoginScreen from the getInitialState RPC response.
// Read by Footer and SettingsConnectionPanel for display.

let _nodeId = "";

/** setLocalNodeId stores the node ID received from Bun on startup. */
export function setLocalNodeId(id: string): void {
	_nodeId = id;
}

/** getLocalNodeId returns the 52-char Base32 node ID for this oos instance. */
export function getLocalNodeId(): string {
	return _nodeId;
}
