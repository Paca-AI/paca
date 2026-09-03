// Shared by both content scripts (isolated-world content/index.ts and
// MAIN-world content/console-hook.ts) — each is bundled as its own
// self-contained IIFE (see the vite.config.*.ts files), so this module gets
// inlined into both rather than shared at runtime; it exists to avoid the
// two copies of this cookie-parsing logic drifting apart, not to share
// state.
//
// Set by services/api's login/refresh handler alongside access_token/
// refresh_token (see auth_handler.go's portCookieName), but deliberately
// NOT HttpOnly — a plain cookie recording which port the Paca app is
// actually reachable on. Cookies are scoped by hostname only, never by
// port, so the SAME cookie set while browsing the app itself is visible
// here too, on a completely different forwarded port — which is exactly
// what lets a content script find the real API even when Paca isn't
// running on 443/80 (e.g. a local dev server on :3000), with no separate
// setup step: this cookie alone is both the "is this a Paca host" signal
// and the address to call, read fresh on every page load rather than
// trusted from some earlier point in time.
export const PORT_COOKIE = "paca_port";

/** Reads and validates the paca_port cookie on the current document, or
 * null if it's absent or malformed. Cheap and synchronous — meant to gate
 * any more expensive work (a network call, hooking page globals) on "is
 * this even plausibly a Paca host" before doing it. */
export function readPacaPort(): number | null {
	const raw = document.cookie
		.split("; ")
		.find((c) => c.startsWith(`${PORT_COOKIE}=`))
		?.slice(PORT_COOKIE.length + 1);
	if (!raw) return null;
	const port = Number(raw);
	return Number.isInteger(port) && port > 0 && port <= 65535 ? port : null;
}
