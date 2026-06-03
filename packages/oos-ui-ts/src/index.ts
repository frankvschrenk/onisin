// index.ts — Public API of the oos-ui-ts package.

export { ViewState, type OptionEntry, type StateListener } from "./state";
export { loadEnvelope, buildEvent } from "./envelope";
export {
	ViewStateContext,
	ViewDomainContext,
	useViewState,
	useBoundValue,
	useBoundOptions,
	useFieldOptionsKey,
} from "./hooks";
export { formatValue, type FormatContext } from "./format";

// Top-level renderer.
export { OnisinView, type OnisinViewProps } from "./components/view";

// Lower-level renderers — exported in case a host wants to render
// only a slice (preview a single section, etc.).
export { BodyElementRenderer } from "./components/dispatch";
export { TableRenderer, type TableRendererProps } from "./components/table";
export { BoundWidget } from "./components/widgets";

// Auto-mock generator — the editor preview uses this when no real
// data is available, so a fresh view with no DB record still
// renders with realistic placeholder content.
export {
	mockEnvelope,
	mockValueForField,
	mockOptionsFor,
	type MockEnvelope,
} from "./mock";
