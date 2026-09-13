// Shared by both content scripts (isolated-world content/index.ts and
// MAIN-world content/console-hook.ts) — each is bundled as its own
// self-contained IIFE (see the vite.config.*.ts files), so this module gets
// inlined into both rather than shared at runtime; it exists to avoid the
// two copies of this cookie-parsing logic drifting apart, not to share
// state.
//
// Set by services/api's login/refresh handler alongside the auth cookies
// (see auth_handler.go's portCookieName/schemeCookieName), but deliberately
// NOT HttpOnly — plain cookies recording where the Paca app is actually
// reachable. Cookies are scoped by hostname only, never by port, so the
// SAME cookies set while browsing the app itself are visible here too, on a
// completely different forwarded port — which is exactly what lets a
// content script find the real API even when Paca isn't running on 443/80
// (e.g. a local dev server on :3000), with no separate setup step: these
// cookies alone are both the "is this a Paca host" signal and the address
// to call, read fresh on every page load rather than trusted from some
// earlier point in time.
export const PORT_COOKIE = "paca_port";

// paca_scheme's own doc comment (below) explains why this exists alongside
// PORT_COOKIE rather than the content script just assuming
// location.protocol — the forwarded preview page and the Paca app share a
// hostname, not necessarily a scheme.
export const SCHEME_COOKIE = "paca_scheme";

function readCookie(name: string): string | null {
	return (
		document.cookie
			.split("; ")
			.find((c) => c.startsWith(`${name}=`))
			?.slice(name.length + 1) ?? null
	);
}

/** Reads and validates the paca_port cookie on the current document, or
 * null if it's absent or malformed. Cheap and synchronous — meant to gate
 * any more expensive work (a network call, hooking page globals) on "is
 * this even plausibly a Paca host" before doing it. */
export function readPacaPort(): number | null {
	const raw = readCookie(PORT_COOKIE);
	if (!raw) return null;
	const port = Number(raw);
	return Number.isInteger(port) && port > 0 && port <= 65535 ? port : null;
}

/** Reads the paca_scheme cookie — "http" or "https", whichever the Paca app
 * actually answers on at the port readPacaPort() returns. Needed because
 * the forwarded preview page's own scheme (the dev server's — whatever the
 * project happens to run) has no necessary relationship to the Paca app's:
 * a self-hosted instance might sit behind a plain-HTTP proxy while a
 * project's dev server serves its own local HTTPS, or vice versa. Returns
 * null if the cookie is absent (an older services/api that predates it) or
 * holds anything other than exactly "http"/"https" — callers should fall
 * back to location.protocol in that case, same as before this cookie
 * existed. */
export function readPacaScheme(): "http" | "https" | null {
	const raw = readCookie(SCHEME_COOKIE);
	return raw === "http" || raw === "https" ? raw : null;
}
