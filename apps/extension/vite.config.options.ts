import { resolve } from "node:path";
import { defineConfig } from "vite";

// The options/popup page — a normal HTML entry, built as Vite's standard
// multi-page app output (unlike the content scripts and background worker,
// this one benefits from nothing special: it's just a page).
export default defineConfig({
	root: resolve(import.meta.dirname, "src/options"),
	base: "",
	build: {
		outDir: resolve(import.meta.dirname, "dist/options"),
		emptyOutDir: true,
		rollupOptions: {
			input: resolve(import.meta.dirname, "src/options/index.html"),
		},
	},
});
