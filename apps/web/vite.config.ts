import tailwindcss from "@tailwindcss/vite";
import { devtools } from "@tanstack/devtools-vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import viteReact from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import tsconfigPaths from "vite-tsconfig-paths";

// macOS/Windows → Docker: native fs events don't cross the VM boundary;
// enable polling only when explicitly running inside a container.
const usePolling = process.env.DOCKER === "true";

const config = defineConfig({
	plugins: [
		devtools(),
		tsconfigPaths({ projects: ["./tsconfig.json"] }),
		tailwindcss(),
		tanstackStart(),
		viteReact({
			babel: {
				plugins: ["babel-plugin-react-compiler"],
			},
		}),
	],
	server: {
		watch: {
			usePolling,
			...(usePolling && { interval: 300 }),
		},
	},
});

export default config;
