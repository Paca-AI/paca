import {
	captureScreenshot,
	getFailedRequests,
	setActiveState,
} from "../shared/messages";
import type { ConsoleEntry, PageAnnotation } from "../shared/types";
import * as api from "./api";
import { copyToClipboard } from "./clipboard";
import {
	accessibleNameOf,
	generateSelectors,
	outerHtmlExcerpt,
	roleOf,
} from "./selector";
import { PacaOverlay } from "./ui";

// Entry point for the isolated-world content script — declared statically
// in manifest.json (matches: <all_urls>), so it runs on every http(s)
// page. Stays completely dormant (no DOM changes, no network calls)
// unless BOTH: (1) the paca_port cookie is present — cheap, synchronous,
// checked before anything else — and (2) the port-forward resolve call
// below actually confirms this exact host:port is a real forwarded
// environment port the current user can see. Both checks matter: (1)
// alone would fire on every page on the entire internet; (2) alone would
// mean an unauthenticated request on every single page load, everywhere.

// Set by services/api's login/refresh handler alongside access_token/
// refresh_token (see auth_handler.go's portCookieName), but deliberately
// NOT HttpOnly — a plain cookie recording which port the Paca app is
// actually reachable on. Cookies are scoped by hostname only, never by
// port, so the SAME cookie set while browsing the app itself is visible
// here too, on a completely different forwarded port — which is exactly
// what lets this content script find the real API even when Paca isn't
// running on 443/80 (e.g. a local dev server on :3000), with no separate
// setup step: this cookie alone is both the "is this a Paca host" signal
// and the address to call, read fresh on every page load rather than
// trusted from some earlier point in time.
const PORT_COOKIE = "paca_port";

function readPacaPort(): number | null {
	const raw = document.cookie
		.split("; ")
		.find((c) => c.startsWith(`${PORT_COOKIE}=`))
		?.slice(PORT_COOKIE.length + 1);
	if (!raw) return null;
	const port = Number(raw);
	return Number.isInteger(port) && port > 0 && port <= 65535 ? port : null;
}

const MAX_CONSOLE_ENTRIES = 20;
const consoleBuffer: ConsoleEntry[] = [];

// Every early-return below is otherwise silent by design (most pages this
// content script loads on are NOT a Paca preview, so most of the time
// there's nothing worth saying) — but that same silence makes a genuine
// misconfiguration indistinguishable from "not a preview" to anyone
// debugging it. Logging each decision point at least gives DevTools'
// console something to grep for.
function log(...args: unknown[]): void {
	console.log("[Paca]", ...args);
}

// Relayed from the MAIN-world console hook (see console-hook.ts) via
// window.postMessage — the isolated world can't see the page's own
// console/window globals directly.
window.addEventListener("message", (event) => {
	if (event.source !== window) return;
	const data = event.data as {
		source?: string;
		level?: string;
		message?: string;
		timestamp?: string;
	};
	if (data?.source !== "paca-console-hook") return;
	if (consoleBuffer.length >= MAX_CONSOLE_ENTRIES) return;
	consoleBuffer.push({
		level: data.level ?? "error",
		message: data.message ?? "",
		timestamp: data.timestamp ?? new Date().toISOString(),
	});
});

function currentHostPort(): number {
	if (location.port) return Number(location.port);
	return location.protocol === "https:" ? 443 : 80;
}

function currentPagePath(): string {
	return location.pathname + location.search;
}

function loadImage(src: string): Promise<HTMLImageElement> {
	return new Promise((resolve, reject) => {
		const img = new Image();
		img.onload = () => resolve(img);
		img.onerror = () => reject(new Error("failed to load captured screenshot"));
		img.src = src;
	});
}

/** Crops the full-viewport screenshot captureScreenshot() returns down to
 * just the commented-on element's own bounding rect, in CSS-pixel
 * coordinates scaled up to the capture's actual (device-pixel-ratio-aware)
 * resolution. */
async function cropToElement(
	dataUrl: string,
	rect: DOMRect,
): Promise<Blob | null> {
	const img = await loadImage(dataUrl);
	const scale = img.width / window.innerWidth;
	const width = Math.max(1, Math.round(rect.width * scale));
	const height = Math.max(1, Math.round(rect.height * scale));
	const canvas = document.createElement("canvas");
	canvas.width = width;
	canvas.height = height;
	const ctx = canvas.getContext("2d");
	if (!ctx) return null;
	ctx.drawImage(
		img,
		rect.left * scale,
		rect.top * scale,
		width,
		height,
		0,
		0,
		width,
		height,
	);
	return new Promise((resolve) =>
		canvas.toBlob((b) => resolve(b), "image/png"),
	);
}

function approxRectFromBoundingBox(annotation: PageAnnotation): DOMRect {
	const bbox = annotation.bounding_box;
	return new DOMRect(
		(bbox.x_pct / 100) * window.innerWidth,
		(bbox.y_pct / 100) * window.innerHeight,
		(bbox.width_pct / 100) * window.innerWidth,
		(bbox.height_pct / 100) * window.innerHeight,
	);
}

async function main(): Promise<void> {
	// Cleared up front, before either check below — the popup must never
	// see a stale "active" left over from whatever page this tab was on
	// before this navigation (e.g. leaving a forwarded preview for the Paca
	// app's own tab, which has the same host-wide cookie but isn't one).
	setActiveState(false);

	const pacaPort = readPacaPort();
	if (pacaPort === null) {
		log(
			`no ${PORT_COOKIE} cookie on this page — log into Paca at least once in this browser (it's set on every login/session refresh) so this preview page can find it.`,
		);
		return;
	}
	// This page's own hostname/protocol are the Paca app's too — the whole
	// design rests on a forwarded preview and the Paca app sharing a
	// hostname, differing only by port (see the cookie's own doc comment
	// above) — so no separately-configured instance URL is needed at all;
	// only the port varies, and that comes fresh from the cookie every
	// time, never trusted from an earlier point in time.
	const baseUrl = `${location.protocol}//${location.hostname}:${pacaPort}`;

	let match: Awaited<ReturnType<typeof api.resolvePortForward>>;
	try {
		match = await api.resolvePortForward(baseUrl, currentHostPort());
	} catch (err) {
		// Not a recognized preview, or the caller can't see it -- stay
		// dormant. Common real causes worth checking if this is unexpected:
		// the environment isn't running yet, its port forward hasn't been
		// assigned a host_port, the logged-in user isn't a member of the
		// project it belongs to, or PORT_FORWARD_HOST for this deployment
		// doesn't actually match the hostname configured (baseUrl) above.
		log(
			`GET ${baseUrl}/api/v1/port-forwards/resolve?host_port=${currentHostPort()} failed — staying dormant.`,
			err,
		);
		return;
	}

	log(
		"activated for environment",
		match.environment_id,
		"in project",
		match.project_id,
	);
	setActiveState(true);
	const overlay = new PacaOverlay();

	async function refreshAnnotations(): Promise<void> {
		try {
			const annotations = await api.listAnnotationsForPage(
				baseUrl,
				match.project_id,
				match.environment_id,
				match.port_forward_id,
				currentPagePath(),
			);
			overlay.setAnnotations(annotations);
		} catch {
			// Transient failure -- leave whatever was last rendered in place.
		}
	}

	await refreshAnnotations();

	async function captureAndUploadScreenshot(
		rect: DOMRect,
	): Promise<string | null> {
		// chrome.tabs.captureVisibleTab shoots whatever is actually rendered
		// in the tab, and Paca's own overlay (toolbar, pins, highlight) is
		// very much part of that -- hide it for the capture so the
		// screenshot shows only the page being commented on, then restore it
		// regardless of how the capture goes. The double rAF wait is needed
		// because hiding it here only schedules the repaint; without waiting
		// for that repaint to actually land, the capture can still grab the
		// previous (overlay-visible) frame.
		overlay.hide();
		let dataUrl: string | null;
		try {
			await new Promise<void>((resolve) =>
				requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
			);
			dataUrl = await captureScreenshot();
		} finally {
			overlay.show();
		}
		if (!dataUrl) return null;
		const blob = await cropToElement(dataUrl, rect);
		if (!blob) return null;
		return api.uploadScreenshot(
			baseUrl,
			match.project_id,
			match.environment_id,
			match.port_forward_id,
			blob,
		);
	}

	async function submitComment(
		el: Element,
		rect: DOMRect,
		body: string,
	): Promise<void> {
		const { selector, fallbacks } = generateSelectors(el);
		const [screenshotFileId, failedRequests] = await Promise.all([
			captureAndUploadScreenshot(rect).catch(() => null),
			getFailedRequests().catch(() => []),
		]);
		await api.createAnnotation(
			baseUrl,
			match.project_id,
			match.environment_id,
			match.port_forward_id,
			{
				page_path: currentPagePath(),
				element_selector: selector,
				element_selector_fallbacks: fallbacks,
				bounding_box: {
					x_pct: (rect.left / window.innerWidth) * 100,
					y_pct: (rect.top / window.innerHeight) * 100,
					width_pct: (rect.width / window.innerWidth) * 100,
					height_pct: (rect.height / window.innerHeight) * 100,
					viewport_width: window.innerWidth,
					viewport_height: window.innerHeight,
				},
				element_snapshot: {
					tag_name: el.tagName.toLowerCase(),
					text_excerpt: (el.textContent ?? "").trim().slice(0, 80),
					outer_html_excerpt: outerHtmlExcerpt(el),
					accessible_name: accessibleNameOf(el),
					role: roleOf(el),
				},
				console_errors: consoleBuffer.slice(),
				failed_requests: failedRequests,
				screenshot_file_id: screenshotFileId,
				body,
			},
		);
		await refreshAnnotations();
	}

	overlay.onElementPicked((el) => {
		const rect = el.getBoundingClientRect();
		overlay.showComposer(rect, ({ body }) => {
			void submitComment(el, rect, body);
		});
	});

	// Shared by onCopyLink/onOpen below -- both point at the exact same
	// comment detail page, just via clipboard vs. a new tab.
	function commentUrl(annotationId: string): string {
		return `${baseUrl}/projects/${match.project_id}/environments/${match.environment_id}/port-forwards/${match.port_forward_id}/comments/${annotationId}`;
	}

	overlay.onPinClicked((placement) => {
		const rect = placement.el?.isConnected
			? placement.el.getBoundingClientRect()
			: approxRectFromBoundingBox(placement.annotations[0]);
		overlay.showPinPopover(placement.annotations, rect, {
			onResolve: (a) => {
				void api
					.resolveAnnotation(
						baseUrl,
						match.project_id,
						match.environment_id,
						match.port_forward_id,
						a.id,
					)
					.then(() => refreshAnnotations());
				overlay.closePanel();
			},
			onReopen: (a) => {
				void api
					.reopenAnnotation(
						baseUrl,
						match.project_id,
						match.environment_id,
						match.port_forward_id,
						a.id,
					)
					.then(() => refreshAnnotations());
				overlay.closePanel();
			},
			onReply: (a, body) => {
				void api
					.addComment(
						baseUrl,
						match.project_id,
						match.environment_id,
						match.port_forward_id,
						a.id,
						body,
					)
					.then(() => refreshAnnotations());
				overlay.closePanel();
			},
			onCopyLink: (a) => {
				// Unlike the other actions, copying isn't terminal -- the user
				// likely wants the popover to stay open (e.g. to also reply)
				// after grabbing the link, so this doesn't close the panel.
				// copyToClipboard (not navigator.clipboard directly) is required
				// here: this page is the port-forwarded preview itself, almost
				// always served over plain http, where the modern Clipboard API
				// is unavailable (it needs a secure context).
				return copyToClipboard(commentUrl(a.id));
			},
			onOpen: (a) => {
				// Opens in a new tab rather than navigating this one away from
				// the page being commented on -- doesn't close the panel,
				// same reasoning as onCopyLink above.
				window.open(commentUrl(a.id), "_blank", "noopener,noreferrer");
			},
			onCreateTask: (a) => {
				void api
					.createTaskFromAnnotation(
						baseUrl,
						match.project_id,
						match.environment_id,
						match.port_forward_id,
						a.id,
					)
					.then((updated) => {
						if (updated.task_id) {
							window.open(
								`${baseUrl}/projects/${match.project_id}/tasks/${updated.task_id}`,
								"_blank",
								"noopener,noreferrer",
							);
						}
						return refreshAnnotations();
					});
				// Doesn't close the panel -- opens the new task in a new tab
				// rather than concluding the interaction with this thread (see
				// onCopyLink/onOpen above); refreshAnnotations above picks up
				// the annotation's new task_id so a later reopen of this same
				// popover shows "Task created" instead of offering to create a
				// second one.
			},
			onCreateConversation: (a) => {
				// No API call needed here (unlike onCreateTask) -- the new tab's
				// own new-conversation route reads `annotationId` back out of
				// the URL and does the actual staging itself (see
				// apps/web's projects/$projectId/conversations/index.tsx).
				window.open(
					`${baseUrl}/projects/${match.project_id}/conversations?annotationId=${a.id}`,
					"_blank",
					"noopener,noreferrer",
				);
			},
		});
	});
}

void main();
