// Loader-contract smoke test for the built remote (run: bun run smoke).
//
// Replicates, step by step, what the Paca host does at runtime in
// apps/web/src/lib/plugins/loader.tsx:
//   1. seed globalThis.__federation_shared__.default with react/react-dom
//      under the exact { [version]: { get, version } } shape the host uses,
//   2. dynamic-import dist/assets/remoteEntry.js,
//   3. container.init(shareScope),
//   4. factory = await container.get("./<Component>"); mod = await factory(),
//   5. render the component to static markup — twice per component:
//      a. bare (the pre-fetch "loading" frame the host paints first), and
//      b. seeded with a __testData fixture (test-only prop) so every panel,
//         every SVG chart and the honesty footnote actually render and can
//         be asserted on, without a network or the docker stack.
// If this passes, the bundle satisfies the host's Module Federation contract.
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

// ── Fixture: 2 completed + 1 active + 1 planned sprint, mixed tasks ──────────

const P = "proj-123";
const NOW = Date.parse("2026-07-13T09:00:00Z");
const iso = (s) => new Date(Date.parse(s)).toISOString();
const status = (id, name, category, position) => ({
	id, project_id: P, name, category, position,
});
const sprint = (id, name, status_, start, end) => ({
	id, project_id: P, name, status: status_,
	start_date: start ? iso(start) : null,
	end_date: end ? iso(end) : null,
	created_at: iso("2026-06-01"), updated_at: iso("2026-07-01"),
});
let taskNo = 0;
const task = (sprintId, statusId, pts) => ({
	id: `t-${++taskNo}`, project_id: P, task_number: taskNo,
	title: `Task ${taskNo}`, status_id: statusId, sprint_id: sprintId,
	importance: 0, story_points: pts,
	created_at: iso("2026-06-01"), updated_at: iso("2026-07-01"),
});

const ST = {
	backlog: status("st-b", "Backlog", "backlog", 0),
	todo: status("st-t", "To Do", "todo", 1),
	doing: status("st-i", "Doing", "inprogress", 2),
	done: status("st-d", "Done", "done", 3),
};

const testData = {
	sprints: [
		sprint("sp-1", "Sprint 1", "completed", "2026-06-01", "2026-06-14"),
		sprint("sp-2", "Sprint 2", "completed", "2026-06-15", "2026-06-28"),
		sprint("sp-3", "Sprint 3", "active", "2026-07-06", "2026-07-20"),
		sprint("sp-4", "Sprint 4", "planned", null, null),
	],
	statuses: Object.values(ST),
	tasks: [
		// Sprint 1 (completed): 10 pts done. Sprint 2 (completed): 13 pts done.
		task("sp-1", ST.done.id, 3), task("sp-1", ST.done.id, 5), task("sp-1", ST.done.id, 2),
		task("sp-2", ST.done.id, 8), task("sp-2", ST.done.id, 5),
		// Sprint 3 (active): 18 pts total, 8 done -> 44%, 10 pts left.
		task("sp-3", ST.done.id, 5), task("sp-3", ST.done.id, 3),
		task("sp-3", ST.doing.id, 5), task("sp-3", ST.todo.id, 2), task("sp-3", ST.todo.id, 3),
		// Backlog: one per category flavor + one with NO status (uncategorized).
		task(null, ST.backlog.id, null), task(null, ST.todo.id, 1), task(null, null, null),
	],
	fetchedAt: NOW,
};

// ── Contract replay for every exposed component ──────────────────────────────

const CASES = [
	{
		name: "AnalyticsView",
		bareProps: { projectId: P },
		bareChecks: ["Analytics", "Loading analytics"],
		dataProps: { projectId: P, __testData: testData, __testNowMs: NOW },
		dataChecks: [
			// panel titles
			"Sprint Progress", "Velocity", "Status Distribution", "Sprint Report",
			// real SVG marks, no iframe
			"<svg", "viewBox",
			// sprint progress: 8/18 done -> hero 44%, burndown "10 pts left"
			"44%", "10 pts left", "Sprint 3",
			// velocity: (10+13)/2 -> avg 11.5, labeled extreme/latest bar
			"avg 11.5",
			// distribution legend with visible counts + uncategorized bucket
			"In Progress", "No status",
			// planned sprint with zero tasks renders its empty marker
			"no tasks",
			// report table + honesty footnote
			"Sprint 1", "completed", "cached 60s", "moves unfinished tasks out",
		],
		forbidden: ["<iframe"],
	},
];

for (const c of CASES) {
	const factory = await container.get(`./${c.name}`);
	assert.equal(typeof factory, "function", `${c.name}: get() returns factory`);
	const mod = await factory();
	const Component = mod?.default ?? mod;
	assert.ok(Component, `${c.name}: module has a component export`);

	const bare = renderToStaticMarkup(react.createElement(Component, c.bareProps));
	for (const needle of c.bareChecks) {
		assert.ok(bare.includes(needle), `${c.name} (bare): markup contains ${needle}`);
	}
	console.log(`ok  ./${c.name} bare render (${bare.length} bytes of markup)`);

	const full = renderToStaticMarkup(react.createElement(Component, c.dataProps));
	for (const needle of c.dataChecks) {
		assert.ok(full.includes(needle), `${c.name} (data): markup contains ${needle}`);
	}
	for (const needle of c.forbidden ?? []) {
		assert.ok(!full.includes(needle), `${c.name} (data): markup must NOT contain ${needle}`);
	}
	console.log(`ok  ./${c.name} data render (${full.length} bytes of markup)`);
}

console.log("ok  remote entry satisfies the host loader contract");
