// Loader-contract smoke test for the built remote (run: bun run smoke).
//
// Replicates what the Paca host does at runtime (apps/web/.../loader.tsx):
//   1. seed globalThis.__federation_shared__.default with react/react-dom under
//      the exact { [version]: { get, version } } shape the host uses,
//   2. dynamic-import dist/assets/remoteEntry.js,
//   3. container.init(shareScope),
//   4. factory = await container.get("./SddFleetView"); mod = await factory(),
//   5. render the component to static markup — twice:
//      a. BARE: the sub-rail with all EIGHT view keys + the first view's
//         loading state (the pre-fetch frame the host paints first),
//      b. SEEDED: __view="overview" + a __testData fixture so the overview
//         actually renders its stat cards, task row and activity feed without
//         a network or the docker stack.
// It also asserts the built markup contains NO <iframe (the whole point of the
// ADR-038 rewrite). If this passes, the bundle satisfies the host loader
// contract and the native rewrite is intact.
import assert from "node:assert/strict";

const react = await import("react");
const reactDom = await import("react-dom");
const { renderToStaticMarkup } = await import("react-dom/server");

const wrap = (mod) => ({
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

const factory = await container.get("./SddFleetView");
assert.equal(typeof factory, "function", "get('./SddFleetView') returns a factory");
const mod = await factory();
const SddFleetView = mod?.default ?? mod;
assert.ok(SddFleetView, "./SddFleetView has a component export");

// ── The eight fleet view keys, as they render in the sub-rail (VN default) ───
const NAV_LABELS = [
	"Tổng quan", // overview
	"Bảng task", // tasks
	"Phiên", // sessions
	"Luồng hoạt động", // activity
	"Phân tích", // analytics
	"Điều phối", // coordination
	"Giai đoạn SDD", // sdd
	"Fleet máy", // fleet
];

// ── a. BARE render: rail + first view loading ────────────────────────────────
const bare = renderToStaticMarkup(react.createElement(SddFleetView, { projectId: "p1", __lang: "vi" }));
for (const label of NAV_LABELS) {
	assert.ok(bare.includes(label), `bare render sub-rail contains "${label}"`);
}
assert.ok(bare.includes("Đang tải"), "bare render shows the loading state");
assert.ok(!bare.includes("<iframe"), "bare render contains NO <iframe");
console.log(`ok  bare render — all 8 view keys in the sub-rail (${bare.length} bytes)`);

// ── b. SEEDED render: overview with a fixture ────────────────────────────────
const overview = {
	machines_online: 3,
	machines_total: 5,
	active_devs: 2,
	total_users: 4,
	total_sessions: 40,
	active_sessions: 7,
	total_events: 1234,
	open_conflicts: 1,
	pending_gates: 2,
	tasksByStatus: { todo: 3, assigned: 1, in_progress: 2, review: 0, done: 5 },
	recent: [
		{ phase: "bizspec", level: 3, tool_name: "EditFile", created_at: "2026-07-18T07:00:00Z", hostname: "cao-mbp", user_name: "Phan Cao" },
		{ phase: "impl", level: 2, tool_name: "RunBash", created_at: "2026-07-18T07:01:00Z", hostname: "host-2", user_name: "Dev Two" },
	],
};

const seeded = renderToStaticMarkup(
	react.createElement(SddFleetView, { projectId: "p1", __lang: "vi", __view: "overview", __testData: overview }),
);
for (const needle of [
	"Máy online", // stat label
	"Tổng event",
	"1234", // total_events, rendered raw
	"Task theo trạng thái",
	"Hoạt động đội gần đây",
	"EditFile", // recent tool
	"Phan Cao", // recent actor
]) {
	assert.ok(seeded.includes(needle), `seeded overview contains "${needle}"`);
}
assert.ok(!seeded.includes("<iframe"), "seeded render contains NO <iframe");
// Real inline SVG icons (no chart libs, but native marks) render as <svg…>.
assert.ok(seeded.includes("<svg"), "seeded render contains inline SVG icons");
console.log(`ok  seeded overview render (${seeded.length} bytes)`);

console.log("ok  remote entry satisfies the host loader contract; native, no iframe");
