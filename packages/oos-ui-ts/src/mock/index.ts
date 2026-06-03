// mock/index.ts — Public surface of the auto-mock generator.
//
// `mockEnvelope` is the only function callers normally use; the
// lower-level helpers (`mockValueForField`, `mockOptionsFor`) are
// exported in case a host wants to fabricate just one slice.
//
// `MockEnvelope` is a typed alias for `EnvelopeObject` so callers
// don't need to reach into envelope.ts for the shape.

import type { EnvelopeObject } from "../envelope";

export { mockEnvelope } from "./envelope";
export { mockValueForField } from "./values";
export { mockOptionsFor, type MockOption } from "./options";

/**
 * MockEnvelope is the shape `mockEnvelope` returns — identical to
 * `EnvelopeObject`, re-exported under a name that pairs with the
 * generator function for readability at call sites.
 */
export type MockEnvelope = EnvelopeObject;
