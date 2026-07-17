// Chart theme — one injected <style> tag, CSS custom properties, both modes.
//
// The Paca host stamps `light` / `dark` on <html> (apps/web
// use-theme-mode.ts), so the dark values are keyed off `html.dark` with a
// `prefers-color-scheme` fallback for the instant before the SPA stamps a
// class. Series colors are real hex per mode (validated with the dataviz
// six-checks against the surfaces below), while plain text/ink prefers the
// HOST's own tokens (var(--foreground) etc.) so typography blends in.
//
// Palette provenance (dataviz reference palette, validator-passed):
//  - categorical slots 1-6 (status categories, FIXED mapping — colors follow
//    the category, never the row): adjacent-pair CVD dE >= 8 in both modes;
//    three light-mode slots sit below 3:1 contrast, so the legend always
//    shows name + count (the "relief rule") and stacked segments keep 2px
//    surface gaps as secondary encoding.
//  - single-series accent (velocity bars, meter fill, burndown "now"):
//    slot 1 blue; the meter track is a lighter step of the same ramp.
//  - a 6-step one-hue ordinal ramp was REJECTED for the category stack: on
//    the light surface the blue ramp cannot fit six steps with dL >= 0.06
//    while its light end still clears 2:1 (validator FAIL), so identity hues
//    + legend it is.
export const THEME_STYLE_ID = "gxan-theme";

export const THEME_CSS = `
.gxan-root {
	--gxan-surface: #fcfcfb;
	--gxan-ink: var(--foreground, #0b0b0b);
	--gxan-ink-2: var(--muted-foreground, #52514e);
	--gxan-muted: #898781;
	--gxan-grid: #e1e0d9;
	--gxan-baseline: #c3c2b7;
	--gxan-border: var(--border, rgba(11, 11, 11, 0.1));
	--gxan-accent: #2a78d6;
	--gxan-accent-track: #cde2fb;
	--gxan-cat-backlog: #2a78d6;
	--gxan-cat-refinement: #008300;
	--gxan-cat-ready: #e87ba4;
	--gxan-cat-todo: #eda100;
	--gxan-cat-inprogress: #1baf7a;
	--gxan-cat-done: #eb6834;
	--gxan-tip-bg: #ffffff;

	font-family: system-ui, -apple-system, "Segoe UI", sans-serif;
	color: var(--gxan-ink);
	display: flex;
	flex-direction: column;
	flex: 1 1 0%;
	min-height: 0;
	height: 100%;
	width: 100%;
	overflow-y: auto;
}
html.dark .gxan-root {
	--gxan-surface: #1a1a19;
	--gxan-ink: var(--foreground, #ffffff);
	--gxan-ink-2: var(--muted-foreground, #c3c2b7);
	--gxan-muted: #898781;
	--gxan-grid: #2c2c2a;
	--gxan-baseline: #383835;
	--gxan-border: var(--border, rgba(255, 255, 255, 0.1));
	--gxan-accent: #3987e5;
	--gxan-accent-track: #184f95;
	--gxan-cat-backlog: #3987e5;
	--gxan-cat-refinement: #008300;
	--gxan-cat-ready: #d55181;
	--gxan-cat-todo: #c98500;
	--gxan-cat-inprogress: #199e70;
	--gxan-cat-done: #d95926;
	--gxan-tip-bg: #262624;
}
@media (prefers-color-scheme: dark) {
	html:not(.light) .gxan-root {
		--gxan-surface: #1a1a19;
		--gxan-ink: var(--foreground, #ffffff);
		--gxan-ink-2: var(--muted-foreground, #c3c2b7);
		--gxan-muted: #898781;
		--gxan-grid: #2c2c2a;
		--gxan-baseline: #383835;
		--gxan-border: var(--border, rgba(255, 255, 255, 0.1));
		--gxan-accent: #3987e5;
		--gxan-accent-track: #184f95;
		--gxan-cat-backlog: #3987e5;
		--gxan-cat-refinement: #008300;
		--gxan-cat-ready: #d55181;
		--gxan-cat-todo: #c98500;
		--gxan-cat-inprogress: #199e70;
		--gxan-cat-done: #d95926;
		--gxan-tip-bg: #262624;
	}
}

.gxan-head {
	display: flex;
	align-items: center;
	gap: 12px;
	flex-wrap: wrap;
	padding: 12px 16px 4px;
}
.gxan-title { font-size: 15px; font-weight: 600; }
.gxan-sub { font-size: 12px; color: var(--gxan-ink-2); }
.gxan-spacer { flex: 1; }
.gxan-select {
	font: inherit;
	font-size: 12px;
	color: var(--gxan-ink);
	background: transparent;
	border: 1px solid var(--gxan-border);
	border-radius: 6px;
	padding: 4px 8px;
	max-width: 220px;
}
.gxan-btn {
	font: inherit;
	font-size: 12px;
	color: var(--gxan-ink);
	background: transparent;
	border: 1px solid var(--gxan-border);
	border-radius: 6px;
	padding: 4px 10px;
	cursor: pointer;
}
.gxan-btn:hover { background: var(--gxan-grid); }

.gxan-grid {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(340px, 1fr));
	gap: 12px;
	padding: 12px 16px 4px;
	align-items: start;
}
.gxan-card {
	background: var(--gxan-surface);
	border: 1px solid var(--gxan-border);
	border-radius: 10px;
	padding: 14px 16px 12px;
	position: relative;
	min-width: 0;
}
.gxan-card-wide { grid-column: 1 / -1; }
.gxan-card-title { font-size: 13px; font-weight: 600; margin: 0 0 2px; }
.gxan-card-sub { font-size: 11px; color: var(--gxan-ink-2); margin: 0 0 10px; }
.gxan-hero {
	font-size: 48px;
	font-weight: 600;
	line-height: 1.1;
	letter-spacing: -0.02em;
}
.gxan-empty {
	font-size: 12px;
	color: var(--gxan-ink-2);
	padding: 18px 0;
}
.gxan-error {
	font-size: 12px;
	border: 1px solid var(--gxan-border);
	border-radius: 8px;
	padding: 10px 12px;
	margin: 12px 16px;
	color: var(--gxan-ink);
}
.gxan-meter {
	height: 10px;
	border-radius: 5px;
	background: var(--gxan-accent-track);
	overflow: hidden;
	margin: 10px 0 6px;
}
.gxan-meter > div {
	height: 100%;
	border-radius: 5px 0 0 5px;
	background: var(--gxan-accent);
}
.gxan-kv { display: flex; gap: 16px; flex-wrap: wrap; margin-top: 6px; }
.gxan-kv div { font-size: 11px; color: var(--gxan-ink-2); }
.gxan-kv strong { color: var(--gxan-ink); font-weight: 600; font-variant-numeric: tabular-nums; }

.gxan-legend {
	display: flex;
	flex-wrap: wrap;
	gap: 6px 14px;
	margin-top: 10px;
	font-size: 11px;
	color: var(--gxan-ink-2);
}
.gxan-legend .sw {
	display: inline-block;
	width: 10px;
	height: 10px;
	border-radius: 2px;
	margin-right: 5px;
	vertical-align: -1px;
}
.gxan-legend strong { color: var(--gxan-ink); font-variant-numeric: tabular-nums; }

.gxan-table-wrap { overflow-x: auto; }
.gxan-table {
	width: 100%;
	border-collapse: collapse;
	font-size: 12px;
}
.gxan-table th {
	text-align: left;
	font-weight: 500;
	color: var(--gxan-ink-2);
	border-bottom: 1px solid var(--gxan-grid);
	padding: 6px 10px 6px 0;
	white-space: nowrap;
}
.gxan-table td {
	border-bottom: 1px solid var(--gxan-grid);
	padding: 6px 10px 6px 0;
	white-space: nowrap;
}
.gxan-table td.num, .gxan-table th.num {
	text-align: right;
	font-variant-numeric: tabular-nums;
}
.gxan-table tr:last-child td { border-bottom: 0; }
.gxan-badge {
	display: inline-block;
	font-size: 10px;
	border: 1px solid var(--gxan-border);
	border-radius: 999px;
	padding: 1px 8px;
	color: var(--gxan-ink-2);
}

.gxan-tip {
	position: absolute;
	pointer-events: none;
	background: var(--gxan-tip-bg);
	border: 1px solid var(--gxan-border);
	border-radius: 8px;
	padding: 7px 10px;
	font-size: 11px;
	line-height: 1.5;
	box-shadow: 0 4px 14px rgba(0, 0, 0, 0.18);
	z-index: 20;
	max-width: 240px;
}
.gxan-tip .t-title { color: var(--gxan-ink-2); margin-bottom: 2px; }
.gxan-tip .t-row { display: flex; align-items: center; gap: 6px; }
.gxan-tip .t-key { display: inline-block; width: 10px; height: 2px; border-radius: 1px; }
.gxan-tip .t-val { font-weight: 600; color: var(--gxan-ink); font-variant-numeric: tabular-nums; }
.gxan-tip .t-lbl { color: var(--gxan-ink-2); }

.gxan-foot {
	font-size: 11px;
	color: var(--gxan-ink-2);
	padding: 8px 16px 16px;
	line-height: 1.6;
}
.gxan-svg { display: block; width: 100%; height: auto; }
.gxan-svg text { font-family: inherit; }
`;

/** Idempotently add the <style> tag (no-op under SSR / smoke rendering). */
export function ensureThemeInjected(): void {
	if (typeof document === "undefined") return;
	if (document.getElementById(THEME_STYLE_ID)) return;
	const style = document.createElement("style");
	style.id = THEME_STYLE_ID;
	style.textContent = THEME_CSS;
	document.head.appendChild(style);
}

/** Category -> CSS var reference (fixed mapping; color follows the entity). */
export const CATEGORY_VAR: Record<string, string> = {
	backlog: "var(--gxan-cat-backlog)",
	refinement: "var(--gxan-cat-refinement)",
	ready: "var(--gxan-cat-ready)",
	todo: "var(--gxan-cat-todo)",
	inprogress: "var(--gxan-cat-inprogress)",
	done: "var(--gxan-cat-done)",
};
