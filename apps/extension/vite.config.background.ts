import { resolve } from "node:path";
import { defineConfig } from "vite";

// The background service worker — declared `"type": "module"` in
// manifest.json, so (unlike the content scripts) it's allowed to be a real
// ES module and can use top-level imports normally.
export default defineConfig({
	publicDir: false,
	build: {
		outDir: "dist/background",
		emptyOutDir: false,
		lib: {
			entry: resolve(import.meta.dirname, "src/background/index.ts"),
			formats: ["es"],
			fileName: () => "index.js",
		},
		// A single-file, dependency-free service worker — no code splitting
		// to worry about registering correctly.
		rollupOptions: { output: { codeSplitting: false } },
	},
});
