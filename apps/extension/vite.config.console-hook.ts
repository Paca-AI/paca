import { resolve } from "node:path";
import { defineConfig } from "vite";

// The MAIN-world console/error hook — see src/content/console-hook.ts's own
// doc comment for why it needs to run in the page's actual JS context
// rather than the isolated content-script world. Same "one config per
// content script, built as a self-contained IIFE" reasoning as
// vite.config.content.ts.
export default defineConfig({
	publicDir: false,
	build: {
		outDir: "dist/content",
		emptyOutDir: false,
		lib: {
			entry: resolve(import.meta.dirname, "src/content/console-hook.ts"),
			formats: ["iife"],
			name: "PacaConsoleHook",
			fileName: () => "console-hook.js",
		},
	},
});
