// Runs in the page's own MAIN world (registered with world: "MAIN" — see
// background/index.ts), not the isolated content-script world, because
// console.error/console.warn and window.onerror must be wrapped on the
// page's *actual* console/window objects to observe what the page itself
// logs — an isolated-world content script has its own separate copy of
// those globals and would never see the page's own calls. MAIN-world
// scripts have no access to chrome.* APIs at all, so the only way out is
// window.postMessage — relayed to the isolated content script (see
// content/index.ts), which keeps its own rolling buffer.

const MAX_ENTRIES = 20;
let entryCount = 0;

function post(level: string, args: unknown[]): void {
	if (entryCount >= MAX_ENTRIES) return;
	entryCount++;
	const message = args
		.map((a) => {
			if (typeof a === "string") return a;
			try {
				return JSON.stringify(a);
			} catch {
				return String(a);
			}
		})
		.join(" ")
		.slice(0, 2000);
	window.postMessage(
		{
			source: "paca-console-hook",
			level,
			message,
			timestamp: new Date().toISOString(),
		},
		"*",
	);
}

const originalError = console.error.bind(console);
console.error = (...args: unknown[]) => {
	post("error", args);
	originalError(...args);
};

const originalWarn = console.warn.bind(console);
console.warn = (...args: unknown[]) => {
	post("warn", args);
	originalWarn(...args);
};

window.addEventListener("error", (event) => {
	post("onerror", [event.message ?? String(event.error)]);
});

window.addEventListener("unhandledrejection", (event) => {
	const reason = event.reason;
	post("unhandledrejection", [
		reason instanceof Error ? reason.message : String(reason),
	]);
});
