import type { FailedRequest } from "../shared/types";

// The background service worker's whole job: things only a privileged
// extension context can do, which the content script (running as the
// forwarded preview page itself) cannot — see shared/messages.ts's own
// doc comment for the full division of responsibility. Both content
// scripts are declared statically in manifest.json (matches: <all_urls>),
// so unlike an earlier version of this file there's no per-host
// registration to manage here: every page gets the script, and the
// paca_port cookie check inside it (see content/index.ts) is what decides
// whether it actually does anything on a given page.

// --- Failed-request capture (chrome.webRequest is background-only) -------

const MAX_BUFFERED_REQUESTS_PER_TAB = 50;
const failedRequestsByTab = new Map<number, FailedRequest[]>();

function recordFailure(tabId: number, entry: FailedRequest): void {
	if (tabId < 0) return; // not associated with a real tab (e.g. a service worker's own fetch)
	const list = failedRequestsByTab.get(tabId) ?? [];
	list.push(entry);
	if (list.length > MAX_BUFFERED_REQUESTS_PER_TAB) list.shift();
	failedRequestsByTab.set(tabId, list);
}

chrome.webRequest.onBeforeRequest.addListener(
	(details): undefined => {
		// A new top-level navigation starts a fresh page — the previous
		// page's failures are no longer relevant to a comment made on this
		// one.
		if (details.type === "main_frame") {
			failedRequestsByTab.set(details.tabId, []);
		}
		return undefined;
	},
	{ urls: ["<all_urls>"] },
);

chrome.webRequest.onCompleted.addListener(
	(details) => {
		if (details.statusCode >= 400) {
			recordFailure(details.tabId, {
				method: details.method,
				url: details.url,
				status_code: details.statusCode,
			});
		}
	},
	{ urls: ["<all_urls>"] },
);

chrome.webRequest.onErrorOccurred.addListener(
	(details) => {
		recordFailure(details.tabId, {
			method: details.method,
			url: details.url,
			status_code: 0,
			error: details.error,
		});
	},
	{ urls: ["<all_urls>"] },
);

// --- Per-tab "is this an activated Paca preview" state --------------------
// Reported by the content script itself (see content/index.ts and
// shared/messages.ts's SetActiveStateRequest) rather than derived here from
// the paca_port cookie, since that cookie is host-wide and so also present
// on the Paca app's own tabs — only the content script knows whether
// resolvePortForward actually found a match on this exact page.
const activeByTab = new Map<number, boolean>();

chrome.tabs.onRemoved.addListener((tabId) => {
	failedRequestsByTab.delete(tabId);
	activeByTab.delete(tabId);
});

// --- Message handling ------------------------------------------------------

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
	if (!message || typeof message !== "object") return undefined;

	switch (message.type) {
		case "PACA_CAPTURE_SCREENSHOT": {
			const windowId = sender.tab?.windowId;
			if (windowId === undefined) {
				sendResponse({ dataUrl: null, error: "no window for sender tab" });
				return undefined;
			}
			chrome.tabs.captureVisibleTab(windowId, { format: "png" }, (dataUrl) => {
				if (chrome.runtime.lastError) {
					sendResponse({
						dataUrl: null,
						error: chrome.runtime.lastError.message,
					});
					return;
				}
				sendResponse({ dataUrl: dataUrl ?? null });
			});
			return true; // async response
		}
		case "PACA_GET_FAILED_REQUESTS": {
			const tabId = sender.tab?.id;
			sendResponse({
				requests:
					tabId !== undefined ? (failedRequestsByTab.get(tabId) ?? []) : [],
			});
			return undefined;
		}
		case "PACA_SET_ACTIVE_STATE": {
			const tabId = sender.tab?.id;
			if (tabId !== undefined) {
				const active = message.active === true;
				activeByTab.set(tabId, active);
				// The content script calls this with false almost immediately on
				// every non-Paca page (see content/index.ts's main()), right after
				// the cheap synchronous paca_port cookie check fails — as soon as
				// we know that, there's no reason to go on holding this tab's
				// buffered request URLs/statuses (onBeforeRequest/onCompleted/
				// onErrorOccurred below have to listen on <all_urls> to have any
				// history ready *before* a comment can be made, so this is what
				// bounds how long a non-preview tab's data sticks around instead
				// of just relying on tab-close).
				if (!active) failedRequestsByTab.delete(tabId);
			}
			return undefined;
		}
		case "PACA_GET_ACTIVE_STATE": {
			const tabId = message.tabId;
			sendResponse({
				active:
					typeof tabId === "number" ? (activeByTab.get(tabId) ?? false) : false,
			});
			return undefined;
		}
		default:
			return undefined;
	}
});
