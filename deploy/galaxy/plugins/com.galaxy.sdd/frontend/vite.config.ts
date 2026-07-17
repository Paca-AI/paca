import { federation } from "@module-federation/vite";
import { defineConfig } from "vite";

// Module Federation remote for the Paca host (apps/web). The host loads this
// bundle with a plain dynamic import of remoteEntry.js and drives the
// { init, get } container contract itself (apps/web/src/lib/plugins/loader.tsx),
// so the emitted format must stay an ES module — do not switch filename or
// build.target without re-testing against that loader.
export default defineConfig({
	plugins: [
		federation({
			name: "com_galaxy_sdd",
			// Emitted inside assets/ so plugin.json's
			// /plugins/com.galaxy.sdd/assets/remoteEntry.js matches the store
			// convention documented in deploy/caddy/Caddyfile.
			filename: "assets/remoteEntry.js",
			manifest: false,
			dts: false,
			exposes: {
				"./SddFleetView": "./src/SddFleetView.tsx",
				"./SddSidebarCard": "./src/SddSidebarCard.tsx",
			},
			shared: {
				// The host share scope registers exactly react@19.0.0 and
				// react-dom@19.0.0 (loader.tsx initializeShareScope), so ^19.0.0
				// resolves the host singletons; the bundled copies below are only
				// the federation fallback.
				react: { singleton: true, requiredVersion: "^19.0.0" },
				"react-dom": { singleton: true, requiredVersion: "^19.0.0" },
			},
		}),
	],
	// Classic JSX runtime — see tsconfig.json for the full rationale (the host
	// share scope has no "react/jsx-runtime" entry).
	esbuild: {
		jsx: "transform",
		jsxFactory: "React.createElement",
		jsxFragment: "React.Fragment",
	},
	build: {
		target: "esnext",
		assetsDir: "assets",
		// Everything must live under dist/assets/ next to remoteEntry.js:
		// the Caddy gateway serves the copied dist at
		// /plugins/com.galaxy.sdd/ and plugin.json points at
		// /plugins/com.galaxy.sdd/assets/remoteEntry.js.
		emptyOutDir: true,
	},
});
