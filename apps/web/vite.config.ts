import path from "node:path";
import { fileURLToPath } from "node:url";
import { federation } from "@module-federation/vite";
import tailwindcss from "@tailwindcss/vite";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const isDocker = process.env.DOCKER === "true";

// https://vite.dev/config/
export default defineConfig({
	plugins: [
		TanStackRouterVite({
			routesDirectory: "./src/routes",
			generatedRouteTree: "./src/routeTree.gen.ts",
			autoCodeSplitting: true,
		}),
		react(),
		tailwindcss(),
		federation({
			name: "host",
			dts: false,
			remotes: {
				// Plugin remotes are registered dynamically at runtime via the
				// PluginRegistryContext — no static remotes are declared here.
			},
			shared: {
				react: { requiredVersion: "^19.0.0" },
				"react-dom": { requiredVersion: "^19.0.0" },
			},
		}),
	],
	resolve: {
		alias: {
			"@": path.resolve(__dirname, "./src"),
		},
	},
	server: {
		watch: isDocker ? { usePolling: true } : undefined,
		hmr: isDocker ? { clientPort: 3000 } : undefined,
		allowedHosts: process.env.VITE_ALLOWED_HOST
			? [process.env.VITE_ALLOWED_HOST]
			: [],
	},
	test: {
		environment: "jsdom",
		globals: true,
		setupFiles: "./src/test/setup.ts",
		// Heavy userEvent flows (dialogs, pickers) can exceed the 5s default when
		// CI runs the whole suite in parallel.
		testTimeout: 15_000,
	},
});
