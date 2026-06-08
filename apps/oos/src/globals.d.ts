// Ambient declarations for CSS side-effect imports.
// Bundlers (Vite, esbuild, Bun) inline CSS at build time; TypeScript
// has no knowledge of that step and would otherwise reject the import.
declare module "*.css";

// `three` is pulled in transitively by electrobun's bun entry point but
// has no types of its own — we never call into it, so a permissive
// declaration is enough to make tsgo green.
declare module "three";
