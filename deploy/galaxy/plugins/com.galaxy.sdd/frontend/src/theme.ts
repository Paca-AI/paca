// One injected <style> tag — theme-aware (light + dark), namespaced `gxsd-`.
//
// The Paca host stamps `light`/`dark` on <html> (apps/web use-theme-mode.ts),
// so dark values key off `html.dark` with a `prefers-color-scheme` fallback for
// the instant before the SPA stamps a class. Plain text/ink prefers the HOST's
// own tokens (var(--foreground) etc.) so typography blends into Paca; the
// accent hues are explicit per mode. Same approach as com.galaxy.analytics.
export const THEME_STYLE_ID = "gxsd-theme";

export const THEME_CSS = `
.gxsd-root {
	--gxsd-surface: #ffffff;
	--gxsd-surface-2: #f5f6f8;
	--gxsd-ink: var(--foreground, #0b0e14);
	--gxsd-ink-2: var(--muted-foreground, #59616e);
	--gxsd-muted: #8b93a1;
	--gxsd-border: var(--border, rgba(11, 14, 20, 0.12));
	--gxsd-grid: rgba(11, 14, 20, 0.08);
	--gxsd-emerald: #0f9d70;
	--gxsd-sky: #2a78d6;
	--gxsd-amber: #b4770a;
	--gxsd-rose: #d64545;
	--gxsd-violet: #7c4dd6;
	--gxsd-indigo: #4f5bd6;
	--gxsd-l1: #64748b;
	--gxsd-l2: #2a78d6;
	--gxsd-l3: #b4770a;
	--gxsd-l4: #d64545;

	font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
	color: var(--gxsd-ink);
	display: flex;
	flex-direction: column;
	flex: 1 1 0%;
	min-height: 0;
	height: 100%;
	width: 100%;
}
html.dark .gxsd-root {
	--gxsd-surface: #14181f;
	--gxsd-surface-2: #1b212b;
	--gxsd-ink: var(--foreground, #e8eaed);
	--gxsd-ink-2: var(--muted-foreground, #a7b0bd);
	--gxsd-muted: #79828f;
	--gxsd-border: var(--border, rgba(255, 255, 255, 0.12));
	--gxsd-grid: rgba(255, 255, 255, 0.08);
	--gxsd-emerald: #34d39e;
	--gxsd-sky: #57a0f2;
	--gxsd-amber: #e6b04a;
	--gxsd-rose: #f27272;
	--gxsd-violet: #a98bf0;
	--gxsd-indigo: #8b95f2;
	--gxsd-l1: #94a3b8;
	--gxsd-l2: #57a0f2;
	--gxsd-l3: #e6b04a;
	--gxsd-l4: #f27272;
}
@media (prefers-color-scheme: dark) {
	html:not(.light) .gxsd-root {
		--gxsd-surface: #14181f;
		--gxsd-surface-2: #1b212b;
		--gxsd-ink: var(--foreground, #e8eaed);
		--gxsd-ink-2: var(--muted-foreground, #a7b0bd);
		--gxsd-muted: #79828f;
		--gxsd-border: var(--border, rgba(255, 255, 255, 0.12));
		--gxsd-grid: rgba(255, 255, 255, 0.08);
		--gxsd-emerald: #34d39e;
		--gxsd-sky: #57a0f2;
		--gxsd-amber: #e6b04a;
		--gxsd-rose: #f27272;
		--gxsd-violet: #a98bf0;
		--gxsd-indigo: #8b95f2;
		--gxsd-l1: #94a3b8;
		--gxsd-l2: #57a0f2;
		--gxsd-l3: #e6b04a;
		--gxsd-l4: #f27272;
	}
}

/* ── Shell: left sub-rail + main ─────────────────────────────────────────── */
.gxsd-shell { display: flex; flex: 1 1 0%; min-height: 0; }
.gxsd-rail {
	flex: 0 0 auto;
	width: 172px;
	border-right: 1px solid var(--gxsd-border);
	padding: 10px 8px;
	display: flex;
	flex-direction: column;
	gap: 2px;
	overflow-y: auto;
}
.gxsd-rail-item {
	display: flex;
	align-items: center;
	gap: 9px;
	padding: 7px 10px;
	border-radius: 8px;
	font-size: 13px;
	color: var(--gxsd-ink-2);
	background: transparent;
	border: 0;
	cursor: pointer;
	text-align: left;
	width: 100%;
	font: inherit;
	font-size: 13px;
	line-height: 1.2;
}
.gxsd-rail-item:hover { background: var(--gxsd-surface-2); color: var(--gxsd-ink); }
.gxsd-rail-item.active {
	background: var(--gxsd-surface-2);
	color: var(--gxsd-ink);
	font-weight: 600;
	box-shadow: inset 2px 0 0 var(--gxsd-emerald);
}
.gxsd-rail-ico { color: var(--gxsd-emerald); display: inline-flex; }

.gxsd-main { flex: 1 1 0%; min-width: 0; display: flex; flex-direction: column; }
.gxsd-head {
	display: flex;
	align-items: flex-start;
	gap: 12px;
	flex-wrap: wrap;
	padding: 14px 18px 8px;
	border-bottom: 1px solid var(--gxsd-border);
}
.gxsd-title { font-size: 16px; font-weight: 600; margin: 0; display: flex; align-items: center; gap: 8px; }
.gxsd-title .gxsd-rail-ico { color: var(--gxsd-emerald); }
.gxsd-sub { font-size: 12px; color: var(--gxsd-ink-2); margin: 2px 0 0; }
.gxsd-spacer { flex: 1; }
.gxsd-langs { display: inline-flex; border: 1px solid var(--gxsd-border); border-radius: 7px; overflow: hidden; }
.gxsd-lang {
	font: inherit; font-size: 11px; padding: 4px 9px; background: transparent;
	color: var(--gxsd-ink-2); border: 0; cursor: pointer; border-left: 1px solid var(--gxsd-border);
}
.gxsd-lang:first-child { border-left: 0; }
.gxsd-lang.active { background: var(--gxsd-surface-2); color: var(--gxsd-ink); font-weight: 600; }
.gxsd-btn {
	font: inherit; font-size: 12px; display: inline-flex; align-items: center; gap: 5px;
	color: var(--gxsd-ink); background: transparent; border: 1px solid var(--gxsd-border);
	border-radius: 7px; padding: 5px 11px; cursor: pointer;
}
.gxsd-btn:hover { background: var(--gxsd-surface-2); }

.gxsd-body { flex: 1 1 0%; min-height: 0; overflow-y: auto; padding: 16px 18px 24px; }
.gxsd-section { margin-bottom: 18px; }
.gxsd-h2 { font-size: 13px; font-weight: 600; margin: 0 0 10px; display: flex; align-items: center; gap: 7px; }

/* ── Stat tiles ──────────────────────────────────────────────────────────── */
.gxsd-stats {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
	gap: 10px;
	margin-bottom: 18px;
}
.gxsd-stat {
	background: var(--gxsd-surface); border: 1px solid var(--gxsd-border);
	border-radius: 10px; padding: 12px 14px;
}
.gxsd-stat-label { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--gxsd-ink-2); }
.gxsd-stat-value { margin-top: 4px; font-size: 26px; font-weight: 600; letter-spacing: -0.02em; font-variant-numeric: tabular-nums; }
.t-emerald { color: var(--gxsd-emerald); }
.t-amber { color: var(--gxsd-amber); }
.t-rose { color: var(--gxsd-rose); }
.t-sky { color: var(--gxsd-sky); }

/* ── Cards + grids ───────────────────────────────────────────────────────── */
.gxsd-card {
	background: var(--gxsd-surface); border: 1px solid var(--gxsd-border);
	border-radius: 10px; padding: 14px 16px;
}
.gxsd-card-title { font-size: 13px; font-weight: 600; margin: 0 0 4px; display: flex; align-items: center; gap: 7px; }
.gxsd-card-sub { font-size: 11px; color: var(--gxsd-ink-2); margin: 0 0 10px; }
.gxsd-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 12px; }
.gxsd-grid-2 { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 12px; }
.gxsd-cols { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 10px; }

/* ── Badges / pills ──────────────────────────────────────────────────────── */
.gxsd-badge {
	display: inline-flex; align-items: center; gap: 4px; font-size: 10px; line-height: 1.4;
	border: 1px solid var(--gxsd-border); border-radius: 5px; padding: 0 5px;
	color: var(--gxsd-ink-2); font-variant-numeric: tabular-nums; white-space: nowrap;
}
.gxsd-badge.lvl1 { color: var(--gxsd-l1); border-color: color-mix(in srgb, var(--gxsd-l1) 40%, transparent); }
.gxsd-badge.lvl2 { color: var(--gxsd-l2); border-color: color-mix(in srgb, var(--gxsd-l2) 40%, transparent); }
.gxsd-badge.lvl3 { color: var(--gxsd-l3); border-color: color-mix(in srgb, var(--gxsd-l3) 45%, transparent); }
.gxsd-badge.lvl4 { color: var(--gxsd-l4); border-color: color-mix(in srgb, var(--gxsd-l4) 45%, transparent); }
.gxsd-pill {
	display: inline-flex; align-items: center; font-size: 10px; border-radius: 999px;
	padding: 1px 8px; background: var(--gxsd-surface-2); color: var(--gxsd-ink-2); white-space: nowrap;
}
.gxsd-life { color: var(--gxsd-sky); background: color-mix(in srgb, var(--gxsd-sky) 14%, transparent); }
.gxsd-count { font-size: 11px; border-radius: 999px; padding: 1px 8px; background: var(--gxsd-surface-2); color: var(--gxsd-ink-2); font-variant-numeric: tabular-nums; }

/* ── Horizontal bars (analytics) ─────────────────────────────────────────── */
.gxsd-bars { display: flex; flex-direction: column; gap: 7px; }
.gxsd-bar-row { display: flex; align-items: center; gap: 8px; font-size: 12px; }
.gxsd-bar-label { width: 120px; flex: 0 0 auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--gxsd-ink-2); }
.gxsd-bar-track { flex: 1 1 0%; height: 10px; border-radius: 5px; background: var(--gxsd-surface-2); overflow: hidden; }
.gxsd-bar-fill { height: 100%; border-radius: 5px; background: var(--gxsd-indigo); }
.gxsd-bar-fill.c-emerald { background: var(--gxsd-emerald); }
.gxsd-bar-fill.c-sky { background: var(--gxsd-sky); }
.gxsd-bar-fill.c-violet { background: var(--gxsd-violet); }
.gxsd-bar-fill.c-l1 { background: var(--gxsd-l1); }
.gxsd-bar-fill.c-l2 { background: var(--gxsd-l2); }
.gxsd-bar-fill.c-l3 { background: var(--gxsd-l3); }
.gxsd-bar-fill.c-l4 { background: var(--gxsd-l4); }
.gxsd-bar-val { width: 34px; flex: 0 0 auto; text-align: right; color: var(--gxsd-ink-2); font-variant-numeric: tabular-nums; }

/* ── 14-day spark columns ────────────────────────────────────────────────── */
.gxsd-spark { display: flex; align-items: flex-end; gap: 3px; height: 84px; }
.gxsd-spark-col { flex: 1 1 0%; display: flex; flex-direction: column; align-items: center; justify-content: flex-end; gap: 3px; min-width: 0; }
.gxsd-spark-bar { width: 100%; border-radius: 3px 3px 0 0; background: var(--gxsd-indigo); min-height: 2px; }
.gxsd-spark-lbl { font-size: 9px; color: var(--gxsd-muted); white-space: nowrap; }

/* ── Activity / timeline rows ────────────────────────────────────────────── */
.gxsd-list { display: flex; flex-direction: column; gap: 4px; }
.gxsd-act {
	display: flex; align-items: center; gap: 8px; font-size: 12px;
	border: 1px solid var(--gxsd-border); background: var(--gxsd-surface-2);
	border-radius: 7px; padding: 6px 9px;
}
.gxsd-act-main { flex: 1 1 0%; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--gxsd-ink); }
.gxsd-act-meta { flex: 0 0 auto; font-size: 10px; color: var(--gxsd-muted); font-variant-numeric: tabular-nums; }

/* ── Board / kanban columns ──────────────────────────────────────────────── */
.gxsd-col { background: var(--gxsd-surface); border: 1px solid var(--gxsd-border); border-radius: 10px; padding: 10px; display: flex; flex-direction: column; }
.gxsd-col-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.gxsd-col-title { font-size: 12px; font-weight: 600; }
.gxsd-col-owner { font-size: 10px; color: var(--gxsd-muted); margin-bottom: 6px; }
.gxsd-cards { display: flex; flex-direction: column; gap: 6px; }
.gxsd-agent { border: 1px solid var(--gxsd-border); background: var(--gxsd-surface-2); border-radius: 7px; padding: 6px 8px; }
.gxsd-agent-top { display: flex; align-items: center; justify-content: space-between; gap: 4px; }
.gxsd-agent-name { font-size: 12px; color: var(--gxsd-ink); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.gxsd-agent-meta { margin-top: 3px; font-size: 10px; color: var(--gxsd-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* ── Table (sessions) ────────────────────────────────────────────────────── */
.gxsd-table-wrap { overflow-x: auto; }
.gxsd-table { width: 100%; border-collapse: collapse; font-size: 12px; }
.gxsd-table th { text-align: left; font-weight: 500; color: var(--gxsd-ink-2); border-bottom: 1px solid var(--gxsd-grid); padding: 6px 12px 6px 0; white-space: nowrap; }
.gxsd-table td { border-bottom: 1px solid var(--gxsd-grid); padding: 6px 12px 6px 0; white-space: nowrap; color: var(--gxsd-ink); }
.gxsd-table tr:last-child td { border-bottom: 0; }
.gxsd-table td.mut { color: var(--gxsd-ink-2); }

/* ── Status dot ──────────────────────────────────────────────────────────── */
.gxsd-dot { width: 8px; height: 8px; border-radius: 999px; display: inline-block; background: var(--gxsd-muted); flex: 0 0 auto; }
.gxsd-dot.on { background: var(--gxsd-emerald); }

/* ── States ──────────────────────────────────────────────────────────────── */
.gxsd-empty { font-size: 12px; color: var(--gxsd-ink-2); font-style: italic; padding: 10px 0; }
.gxsd-ok { font-size: 13px; color: var(--gxsd-emerald); display: flex; align-items: center; gap: 7px; padding: 4px 0; }
.gxsd-loading { font-size: 12px; color: var(--gxsd-ink-2); padding: 28px 18px; }
.gxsd-error {
	font-size: 12px; border: 1px solid color-mix(in srgb, var(--gxsd-rose) 35%, var(--gxsd-border));
	background: color-mix(in srgb, var(--gxsd-rose) 6%, transparent);
	border-radius: 8px; padding: 12px 14px; margin: 16px 18px; color: var(--gxsd-ink);
}
.gxsd-error-title { font-weight: 600; margin-bottom: 4px; display: flex; align-items: center; gap: 7px; }
.gxsd-conflict {
	border: 1px solid color-mix(in srgb, var(--gxsd-rose) 35%, var(--gxsd-border));
	background: color-mix(in srgb, var(--gxsd-rose) 6%, transparent);
	border-radius: 7px; padding: 7px 10px; font-size: 12px;
}
.gxsd-foot { font-size: 11px; color: var(--gxsd-muted); padding-top: 8px; }
.gxsd-flex { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
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
