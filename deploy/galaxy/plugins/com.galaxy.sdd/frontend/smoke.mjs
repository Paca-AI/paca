// Loader-contract smoke test for the built remote (run: bun run smoke).
//
// Replicates, step by step, what the Paca host does at runtime in
// apps/web/src/lib/plugins/loader.tsx:
//   1. seed globalThis.__federation_shared__.default with react/react-dom
//      under the exact { [version]: { get, version } } shape the host uses,
//   2. dynamic-import dist/assets/remoteEntry.js,
//   3. container.init(shareScope),
//   4. factory = await container.get("./<Component>"); mod = await factory(),
//   5. render the component to static markup.
// If this passes, the bundle satisfies the host's Module Federation contract
// without needing the full docker stack.
import assert from "node:assert/strict";

const react = await import("react");
const reactDom = await import("react-dom");
const { renderToStaticMarkup } = await import("react-dom/server");

const wrap = (mod) => ({
	// Host share scope entries resolve to a factory returning the module.
	get: () => Promise.resolve(() => Promise.resolve(mod)),
	version: "19.0.0",
});

globalThis.__federation_shared__ = {
	default: {
		react: { "19.0.0": wrap(react) },
		"react-dom": { "19.0.0": wrap(reactDom) },
	},
};
const shareScope = globalThis.__federation_shared__.default;

const entryUrl = new URL("./dist/assets/remoteEntry.js", import.meta.url);
const container = await import(entryUrl.href).then((m) => m.default ?? m);

assert.equal(typeof container.init, "function", "remoteEntry exports init()");
assert.equal(typeof container.get, "function", "remoteEntry exports get()");

await container.init(shareScope);

for (const [name, checks] of [
	["SddFleetView", ["<iframe", "ai.skyplatform.net/sdd-server", "sandbox=", "allow-same-origin", "SDD Fleet"]],
	["SddSidebarCard", ["/projects/proj-123/plugins/com.galaxy.sdd/sdd-fleet", "target=\"_blank\"", "SDD Sensor"]],
]) {
	const factory = await container.get(`./${name}`);
	assert.equal(typeof factory, "function", `${name}: get() returns factory`);
	const mod = await factory();
	const Component = mod?.default ?? mod;
	assert.ok(Component, `${name}: module has a component export`);

	const html = renderToStaticMarkup(
		react.createElement(Component, { projectId: "proj-123" }),
	);
	for (const needle of checks) {
		assert.ok(html.includes(needle), `${name}: markup contains ${needle}`);
	}
	console.log(`ok  ./${name} (${html.length} bytes of markup)`);
}

console.log("ok  remote entry satisfies the host loader contract");
