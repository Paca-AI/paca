/**
 * Galaxy chat dock (ADR-038 P3.2) — mounts the platform-wide
 * `<galaxy-chat-dock>` web component after login, the same way the
 * Galaxy AI Wiki does (see Galaxy-AI-Wiki server/static/dock-bootstrap.js,
 * the integration this file ports to the SPA).
 *
 * The dock bundle (`dock.js`, built by Galaxy-Nexus
 * packages/galaxy-chat-dock) authenticates with the Galaxy session JWT it
 * finds in localStorage.auth_token. That token lives in the PORTAL
 * origin's localStorage (ai.skyplatform.net) — not here. The gap is
 * closed with a one-time top-level round-trip through the portal's
 * /dock-sso relay page, which copies the session token back to us in the
 * URL fragment (fragments never reach servers or logs).
 *
 * Flow, once per browser session (sessionStorage guard):
 *   app page → portal /dock-sso?return=<here> → back here with
 *   #dock_sso=<payload> → store token → dock appears. If the portal has
 *   no session either, the relay sends back "none" and the dock simply
 *   stays unmounted.
 *
 * The fragment payload is only accepted when this app initiated a relay
 * in the same browser session — a crafted external link carrying a token
 * fragment is ignored.
 *
 * The dock's API calls (`/api/agentops`, `/api/identity`) are RELATIVE
 * URLs; the Caddy gateway bridges those paths to the platform gateway
 * over galaxy_network (see deploy/caddy/Caddyfile, galaxy_dock_bridge),
 * so no CORS is involved and SSE streams stay intact — the wiki pattern.
 */

import { useQuery } from "@tanstack/react-query";
import { useEffect } from "react";

import { authConfigQueryOptions } from "@/lib/auth-api";

const TOKEN_KEY = "auth_token";
const USER_KEY = "auth_user";
const GUARD_KEY = "galaxy-dock-sso-attempted";
const HASH_PREFIX = "#dock_sso=";
const DEFAULT_PORTAL = "https://ai.skyplatform.net";

declare module "react" {
	namespace JSX {
		interface IntrinsicElements {
			/** Custom element defined by the Galaxy dock bundle (dock.js). */
			"galaxy-chat-dock": React.DetailedHTMLProps<
				React.HTMLAttributes<HTMLElement>,
				HTMLElement
			> & {
				"app-id"?: string;
				endpoint?: string;
			};
		}
	}
}

function jwtExpiresSoon(token: string): boolean {
	try {
		const parts = token.split(".");
		if (parts.length !== 3 || !parts[1]) {
			return true;
		}
		const payload = JSON.parse(
			atob(parts[1].replace(/-/g, "+").replace(/_/g, "/")),
		) as { exp?: number };
		// No exp claim → treat as non-expiring.
		if (!payload.exp) {
			return false;
		}
		return payload.exp * 1000 < Date.now() + 60 * 1000;
	} catch {
		return true;
	}
}

function hasUsableGalaxyToken(): boolean {
	try {
		const token = window.localStorage.getItem(TOKEN_KEY);
		return !!token && !jwtExpiresSoon(token);
	} catch {
		return false;
	}
}

/**
 * Consume a `#dock_sso=<payload>` return fragment from the portal relay.
 * Must run at app boot, before anything else navigates. Returns true when
 * a relay fragment was present (whatever its validity).
 */
export function consumeDockSsoRelayHash(): boolean {
	if (!window.location.hash.startsWith(HASH_PREFIX)) {
		return false;
	}
	const raw = window.location.hash.slice(HASH_PREFIX.length);
	// Always scrub the fragment so the token doesn't linger in the URL bar
	// or get copied into shared links.
	window.history.replaceState(
		null,
		"",
		window.location.pathname + window.location.search,
	);
	let initiated = false;
	try {
		initiated = window.sessionStorage.getItem(GUARD_KEY) === "1";
	} catch {
		initiated = false;
	}
	if (!initiated || raw === "none") {
		return true;
	}
	try {
		const payload = JSON.parse(decodeURIComponent(raw)) as {
			t?: unknown;
			u?: unknown;
		};
		if (
			payload &&
			typeof payload.t === "string" &&
			payload.t.split(".").length === 3 &&
			!jwtExpiresSoon(payload.t)
		) {
			window.localStorage.setItem(TOKEN_KEY, payload.t);
			if (typeof payload.u === "string" && payload.u) {
				window.localStorage.setItem(USER_KEY, payload.u);
			}
			// Success — lift the once-per-session guard so a token that
			// expires mid-session can trigger a fresh relay on the next load.
			window.sessionStorage.removeItem(GUARD_KEY);
			window.dispatchEvent(new Event("galaxy:auth-changed"));
		}
	} catch {
		// Malformed payload — leave the dock unmounted rather than break the app.
	}
	return true;
}

function relayThroughPortal(portalOrigin: string): void {
	let attempted = false;
	try {
		attempted = window.sessionStorage.getItem(GUARD_KEY) === "1";
	} catch {
		// No sessionStorage → a failed relay would loop forever. Don't try.
		return;
	}
	if (attempted) {
		return;
	}
	window.sessionStorage.setItem(GUARD_KEY, "1");
	if (portalOrigin.startsWith(window.location.origin)) {
		return;
	}
	window.location.replace(
		`${portalOrigin}/dock-sso?return=${encodeURIComponent(window.location.href)}`,
	);
}

/** Portal origin hosting the /dock-sso relay, derived from the dock src. */
function portalOriginFor(dockSrc: string): string {
	try {
		return new URL(dockSrc).origin;
	} catch {
		// Relative dock src (same-origin gateway bridge) — use the default
		// portal origin, mirroring the wiki bootstrap's fallback.
		return DEFAULT_PORTAL;
	}
}

/** Idempotently inject the dock bundle script. */
function loadDockScript(src: string): void {
	if (document.querySelector("script[data-galaxy-dock]")) {
		return;
	}
	const script = document.createElement("script");
	script.src = src;
	script.defer = true;
	script.dataset.galaxyDock = "true";
	document.head.appendChild(script);
}

/**
 * Renders the Galaxy chat dock when the API advertises it on /auth/config
 * (env GALAXY_DOCK_SRC). Mounted from the authenticated layout only, so the
 * dock never appears on login/public pages.
 */
export function GalaxyChatDock() {
	const { data: config } = useQuery(authConfigQueryOptions);
	const dockSrc =
		config?.dock_enabled && config.dock_src ? config.dock_src : "";

	useEffect(() => {
		if (!dockSrc) {
			return;
		}
		if (!hasUsableGalaxyToken()) {
			// One bounce per browser session through the portal SSO relay;
			// comes back to this exact URL with a #dock_sso fragment that
			// consumeDockSsoRelayHash() (main.tsx) stores at boot.
			relayThroughPortal(portalOriginFor(dockSrc));
			return;
		}
		loadDockScript(dockSrc);
	}, [dockSrc]);

	if (!dockSrc) {
		return null;
	}
	// Same element + attributes as the wiki integration; `endpoint` is left
	// at its default (/api/agentops) which the gateway bridges same-origin.
	return <galaxy-chat-dock app-id="paca" />;
}
