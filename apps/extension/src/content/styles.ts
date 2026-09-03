// Injected into the shadow root so none of it leaks onto (or is affected
// by) the host page's own styles — the whole point of rendering inside a
// shadow root at all. Tokens AND component metrics mirror apps/web's own
// shadcn/ui primitives as closely as hand-written CSS reasonably can:
// button height/radius/padding from components/ui/button.tsx's "default"
// size, the panel's radius/ring/shadow from components/ui/popover.tsx
// (an anchored, dismissible panel is a popover here, not a centered
// dialog), and the textarea from components/ui/textarea.tsx. Colors
// switch by prefers-color-scheme — a shadow root doesn't block that media
// feature, it's a browser/OS-level signal — matching the extension's
// options page. This can't follow a signed-in user's explicit in-app
// theme override (that preference lives in the Paca app's own
// localStorage, on a different origin the preview page has no access
// to); prefers-color-scheme is the closest available signal.
export const STYLES = /* css */ `
:host, * { box-sizing: border-box; }
:host {
	all: initial;
	position: fixed;
	inset: 0;
	pointer-events: none;
	z-index: 2147483647;
	font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
	font-size: 14px;

	--background: #ffffff;
	--foreground: #111111;
	--card: #ffffff;
	--card-foreground: #111111;
	--primary: #5a9e1c;
	--primary-foreground: #ffffff;
	--secondary: #f0f0f0;
	--secondary-foreground: #111111;
	--muted: #f5f5f5;
	--muted-foreground: #737373;
	--border: #d4d4d4;
	--destructive: #c0341f;
	--radius: 0.5rem;
}
@media (prefers-color-scheme: dark) {
	:host {
		--background: #0a0a0a;
		--foreground: #f0f0f0;
		--card: #111111;
		--card-foreground: #f0f0f0;
		--primary: #9ed957;
		--primary-foreground: #0a0a0a;
		--secondary: #1e1e1e;
		--secondary-foreground: #f0f0f0;
		--muted: #1a1a1a;
		--muted-foreground: #888888;
		--border: #2a2a2a;
		--destructive: #e35d4a;
	}
}

/* --- Shared button (toolbar + panel actions alike) — metrics match
   apps/web's Button "default" size exactly: h-8, rounded-lg, px-2.5,
   text-sm/font-medium, gap-1.5. --- */
.btn {
	all: unset;
	box-sizing: border-box;
	cursor: pointer;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: 4px;
	height: 28px;
	padding: 0 10px;
	border-radius: calc(var(--radius) - 2px);
	font-size: 12px;
	font-weight: 500;
	white-space: nowrap;
	transition: background-color 120ms ease, color 120ms ease;
}
.btn-primary { background: var(--primary); color: var(--primary-foreground); }
.btn-primary:hover { background: color-mix(in srgb, var(--primary) 85%, var(--card)); }
.btn-secondary { background: var(--secondary); color: var(--secondary-foreground); }
.btn-secondary:hover { background: color-mix(in srgb, var(--secondary) 80%, var(--card)); }
.btn-ghost { background: transparent; color: inherit; }
.btn-ghost:hover { background: var(--muted); }
.btn-ghost.active { background: var(--primary); color: var(--primary-foreground); }
.btn-ghost.toggled { background: var(--muted); color: var(--foreground); }

/* Icon-only variant of .btn — square instead of padded-for-text, used by
   the thread header's action buttons (see ui.ts's renderThread): with five
   actions to fit in one row, a text label on each would either wrap or
   force the popover wider than Figma's own comment panel ever is. */
.btn-icon { width: 28px; height: 28px; padding: 0; }
.btn-icon svg { flex-shrink: 0; }

/* --- Toolbar --- */
.toolbar-wrap {
	position: absolute;
	bottom: 0;
	left: 50%;
	transform: translateX(-50%) translateY(calc(100% - 8px));
	transition: transform 160ms ease;
	pointer-events: auto;
}
.toolbar-wrap:hover, .toolbar-wrap.pinned {
	transform: translateX(-50%) translateY(0);
}
.toolbar-handle {
	height: 8px;
	width: 64px;
	margin: 0 auto;
	background: var(--card);
	border-radius: 6px 6px 0 0;
	box-shadow: 0 0 0 1px color-mix(in srgb, var(--foreground) 10%, transparent);
	opacity: 0.9;
}
.toolbar {
	display: flex;
	align-items: center;
	gap: 4px;
	background: var(--card);
	color: var(--card-foreground);
	padding: 6px;
	border-radius: 10px 10px 0 0;
	box-shadow:
		0 0 0 1px color-mix(in srgb, var(--foreground) 10%, transparent),
		0 -4px 20px rgba(0, 0, 0, 0.15);
}
.toolbar .brand {
	display: flex; align-items: center; gap: 6px;
	font-weight: 600; font-size: 14px; padding: 0 8px 0 6px;
}
.toolbar .brand svg { flex-shrink: 0; }
.toolbar .brand .logo-dark { display: none; }
@media (prefers-color-scheme: dark) {
	.toolbar .brand .logo-light { display: none; }
	.toolbar .brand .logo-dark { display: inline-flex; }
}
.toolbar .sep { width: 1px; align-self: stretch; margin: 4px 2px; background: var(--border); }
.toolbar .count {
	min-width: 16px; height: 16px; padding: 0 4px;
	border-radius: 999px; background: var(--primary); color: var(--primary-foreground);
	font-size: 10px; font-weight: 700; display: inline-flex;
	align-items: center; justify-content: center;
}

/* --- Highlight overlay while picking an element --- */
.highlight {
	position: absolute;
	border: 2px solid var(--primary);
	background: color-mix(in srgb, var(--primary) 14%, transparent);
	border-radius: 3px;
	pointer-events: none;
	display: none;
}

/* --- Pins — Figma's own comment-pin shape: a teardrop badge (rounded on
   every corner but the bottom-left, which comes to a point) filled with
   the commenter's avatar, tinted with a color derived from their identity
   rather than one flat brand color, exactly like Figma tints each
   collaborator's cursor/pin/avatar ring by their assigned color. A small
   count badge in the corner is a Paca addition (Figma's own pins carry no
   count) for threads with more than one message. --- */
.pins-layer { position: absolute; inset: 0; pointer-events: none; }

/* .pin-cluster is the positioned anchor point (placed via reposition()'s
   left/top, same as a lone pin always was) — normally just a 0-sized point
   with one pin centered on it via transform. When it groups more than one
   thread (setAnnotations' proximity clustering) it instead becomes a
   generously sized, always-hoverable hit zone: hovering anywhere in it
   (the collapsed pin OR the gap around it) fans the individual pins out
   from the same center point so each becomes its own click target,
   spiderfy-style, without the reveal flickering off between them. */
.pin-cluster { position: absolute; width: 0; height: 0; }
.pin-cluster.has-satellites {
	width: 84px; height: 84px;
	margin-left: -42px; margin-top: -42px;
	pointer-events: auto;
}
.pin-cluster .pin { position: absolute; left: 50%; top: 50%; }
.pin-center { transition: opacity 120ms ease; }
.pin-satellites .pin {
	opacity: 0;
	pointer-events: none;
	transition: opacity 120ms ease;
}
.pin-cluster.has-satellites:hover .pin-center {
	opacity: 0;
	pointer-events: none;
}
.pin-cluster.has-satellites:hover .pin-satellites .pin {
	opacity: 1;
	pointer-events: auto;
}

/* --- The pin itself — Figma's own comment-pin shape: a teardrop badge
   (rounded on every corner but the bottom-left, which comes to a point)
   filled with the commenter's avatar, tinted with a color derived from
   their identity rather than one flat brand color, exactly like Figma
   tints each collaborator's cursor/pin/avatar ring by their assigned
   color. A small count badge in the corner is a Paca addition (Figma's
   own pins carry no count) for threads with more than one message. --- */
.pin {
	width: 28px; height: 28px;
	border-radius: 50% 50% 50% 4px;
	background: var(--pin-color, var(--primary));
	color: #fff;
	display: flex; align-items: center; justify-content: center;
	font-size: 12px; font-weight: 700;
	cursor: pointer;
	pointer-events: auto;
	box-shadow: 0 0 0 2px var(--card), 0 2px 6px rgba(0, 0, 0, 0.35);
}
.pin img { width: 100%; height: 100%; border-radius: inherit; object-fit: cover; display: block; }
.pin.resolved { background: var(--muted-foreground); }
.pin.approximate { border: 2px dashed var(--card); }
.pin-badge {
	position: absolute;
	top: -6px; right: -6px;
	min-width: 16px; height: 16px; padding: 0 3px;
	border-radius: 999px;
	background: var(--card);
	color: var(--card-foreground);
	border: 1px solid var(--border);
	font-size: 10px; font-weight: 700;
	display: flex; align-items: center; justify-content: center;
}

/* --- Popover / composer shared panel — radius/ring/shadow match
   apps/web's components/ui/popover.tsx exactly (rounded-lg, ring-1
   ring-foreground/10, shadow-md), since an anchored dismissible panel is
   what this is. --- */
.panel {
	position: absolute;
	width: 300px;
	max-width: calc(100vw - 24px);
	background: var(--card);
	color: var(--card-foreground);
	border-radius: var(--radius);
	box-shadow:
		0 0 0 1px color-mix(in srgb, var(--foreground) 10%, transparent),
		0 4px 16px rgba(0, 0, 0, 0.15);
	pointer-events: auto;
	overflow: hidden;
}
.panel-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 8px;
	padding: 10px 6px 10px 12px;
	font-weight: 500;
	font-size: 14px;
}
.panel-close {
	all: unset;
	box-sizing: border-box;
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	width: 28px;
	height: 28px;
	border-radius: calc(var(--radius) - 2px);
	color: var(--muted-foreground);
	flex-shrink: 0;
}
.panel-close:hover { background: var(--muted); color: var(--card-foreground); }
.panel-body { padding: 0 10px 10px; }
.panel textarea {
	width: 100%;
	min-height: 64px;
	resize: vertical;
	border: 1px solid var(--border);
	border-radius: var(--radius);
	padding: 8px 10px;
	font: inherit;
	font-size: 14px;
	background: transparent;
	color: var(--foreground);
	transition: border-color 120ms ease;
}
.panel textarea::placeholder { color: var(--muted-foreground); }
.panel textarea:focus {
	border-color: var(--primary);
	box-shadow: 0 0 0 3px color-mix(in srgb, var(--primary) 50%, transparent);
}
.comment-item { padding: 8px 0; border-bottom: 1px solid var(--border); font-size: 13px; }
.comment-item:last-child { border-bottom: none; }
.comment-header { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
.avatar {
	width: 22px; height: 22px; border-radius: 999px; flex-shrink: 0;
	background: var(--muted); color: var(--muted-foreground);
	display: flex; align-items: center; justify-content: center;
	font-size: 10px; font-weight: 600; overflow: hidden;
}
.avatar img { width: 100%; height: 100%; object-fit: cover; display: block; }
.comment-author { font-weight: 600; }
.comment-meta { font-size: 11px; color: var(--muted-foreground); }
.comment-body { padding-left: 30px; word-wrap: break-word; }
.screenshot-preview { width: 100%; border-radius: calc(var(--radius) - 2px); margin-bottom: 8px; display: block; }

/* --- Threads: a pin can carry more than one comment thread when two
   comments were started independently on the same element — each renders
   as its own block, separated by a divider, so acting on one never
   touches the other. --- */
.thread-divider { height: 1px; background: var(--border); margin: 12px 0; }
.thread-header { display: flex; justify-content: flex-end; gap: 6px; margin-bottom: 8px; }

/* The comment + reply list is the ONLY scrollable region in a thread — the
   header above and the reply box below (see renderThread) stay outside it,
   always visible, so scrolling through a long thread never pushes either
   out of view. Thin, theme-colored scrollbar instead of the browser's
   chunky default -- this extension only ever runs in Chromium, which
   honors the ::-webkit-scrollbar pseudo-elements below; scrollbar-width/
   -color are the zero-cost standard-track equivalent for any other engine
   that might ever load this content script. */
.thread-comments {
	max-height: 240px;
	overflow-y: auto;
	scrollbar-width: thin;
	scrollbar-color: var(--border) transparent;
}
.thread-comments::-webkit-scrollbar { width: 6px; }
.thread-comments::-webkit-scrollbar-track { background: transparent; }
.thread-comments::-webkit-scrollbar-thumb {
	background: var(--border);
	border-radius: 999px;
}
.thread-comments::-webkit-scrollbar-thumb:hover { background: var(--muted-foreground); }

/* --- Create dropdown (thread header) — a small anchored menu, same
   radius/ring/shadow language as .panel itself since it's the same kind of
   floating popover, just anchored to a button instead of a pin. --- */
.dropdown { position: relative; }
.dropdown-menu {
	position: absolute;
	top: calc(100% + 4px);
	right: 0;
	min-width: 168px;
	background: var(--card);
	color: var(--card-foreground);
	border-radius: calc(var(--radius) - 2px);
	box-shadow:
		0 0 0 1px color-mix(in srgb, var(--foreground) 10%, transparent),
		0 4px 16px rgba(0, 0, 0, 0.15);
	padding: 4px;
	z-index: 1;
}
.dropdown-item {
	all: unset;
	box-sizing: border-box;
	width: 100%;
	display: flex;
	align-items: center;
	gap: 8px;
	padding: 6px 8px;
	border-radius: calc(var(--radius) - 4px);
	font-size: 12px;
	font-weight: 500;
	cursor: pointer;
	white-space: nowrap;
}
.dropdown-item:hover { background: var(--muted); }
.dropdown-item svg { flex-shrink: 0; }
.dropdown-item[disabled] { cursor: default; color: var(--muted-foreground); pointer-events: none; }

/* The reply/new-comment box itself — a rounded pill with a send button,
   matching Figma's own comment-input control exactly (new comment and
   reply use the identical control there too). */
.reply-row {
	display: flex; align-items: flex-end; gap: 6px;
	margin-top: 8px;
	background: var(--muted);
	border-radius: 999px;
	padding: 4px 4px 4px 12px;
	transition: box-shadow 120ms ease;
}
.reply-row:focus-within {
	box-shadow: 0 0 0 2px color-mix(in srgb, var(--primary) 50%, transparent);
}
.reply-row textarea {
	flex: 1;
	min-height: 20px;
	max-height: 100px;
	padding: 6px 0;
	border: none;
	border-radius: 0;
	background: transparent;
	resize: none;
	font: inherit;
	font-size: 13px;
	color: var(--foreground);
}
.reply-row textarea::placeholder { color: var(--muted-foreground); }
.reply-row textarea:focus { box-shadow: none; }
.send-btn {
	all: unset;
	box-sizing: border-box;
	flex-shrink: 0;
	cursor: pointer;
	width: 26px; height: 26px;
	border-radius: 999px;
	display: flex; align-items: center; justify-content: center;
	background: var(--primary);
	color: var(--primary-foreground);
}
.send-btn:hover { background: color-mix(in srgb, var(--primary) 85%, var(--card)); }

.panel-close-wrap { display: flex; justify-content: flex-end; padding: 6px 6px 0; }
`;
