import { resolve } from "node:path";
import { defineConfig } from "vite";

// Content scripts registered dynamically via chrome.scripting.registerContentScripts
// (see src/background/index.ts) always run as classic, non-module scripts — MV3 gives
// no way to load an ES module as a content script, dynamically registered or not — so
// each one is built as a fully self-contained IIFE with no runtime imports. A separate
// config per script (rather than one shared config) sidesteps Rollup's "iife/umd needs
// a single entry point" constraint entirely, which is simpler to reason about than
// fighting for multi-entry IIFE output.
export default defineConfig({
	// Vite's default publicDir copy (public/manifest.json) is handled once,
	// explicitly, by scripts/copy-manifest.mjs after all 4 builds run — skip
	// the automatic per-build copy so it doesn't also land in dist/content/.
	publicDir: false,
	build: {
		outDir: "dist/content",
		emptyOutDir: false,
		lib: {
			entry: resolve(import.meta.dirname, "src/content/index.ts"),
			formats: ["iife"],
			name: "PacaContent",
			fileName: () => "index.js",
		},
	},
});
