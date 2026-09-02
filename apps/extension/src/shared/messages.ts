import type { FailedRequest } from "./types";

// The extension's whole runtime-messaging surface, in one place — the
// background worker only ever needs to do two things a content script
// cannot do itself: capture a screenshot of the tab (chrome.tabs is not
// exposed to content scripts) and hand back the rolling buffer of failed
// requests it observed via chrome.webRequest (also background-only). Every
// other piece of extension logic — auth, reading/writing annotations,
// detection — happens directly in the content script itself (see its own
// doc comment for why: same-hostname same-site cookies mean it can call
// the Paca API with no relay needed). Both content scripts are declared
// statically in manifest.json now, so there's no "which sites are
// enabled" state to notify anyone about either.

export interface CaptureScreenshotRequest {
	type: "PACA_CAPTURE_SCREENSHOT";
}

export interface CaptureScreenshotResponse {
	dataUrl: string | null;
	error?: string;
}

export interface GetFailedRequestsRequest {
	type: "PACA_GET_FAILED_REQUESTS";
}

export interface GetFailedRequestsResponse {
	requests: FailedRequest[];
}

// Reported by the content script once it knows whether THIS page is an
// actually-activated Paca preview (i.e. resolvePortForward found a match) —
// not just whether the paca_port cookie is present, which is host-wide and
// so also true on the Paca app's own tabs. The popup reads this back
// instead of re-deriving (and getting wrong) the same answer from the
// cookie itself.
export interface SetActiveStateRequest {
	type: "PACA_SET_ACTIVE_STATE";
	active: boolean;
}

export interface GetActiveStateRequest {
	type: "PACA_GET_ACTIVE_STATE";
	tabId: number;
}

export interface GetActiveStateResponse {
	active: boolean;
}

export type ExtensionRequest =
	| CaptureScreenshotRequest
	| GetFailedRequestsRequest
	| SetActiveStateRequest
	| GetActiveStateRequest;

export async function captureScreenshot(): Promise<string | null> {
	const resp = (await chrome.runtime.sendMessage({
		type: "PACA_CAPTURE_SCREENSHOT",
	} satisfies CaptureScreenshotRequest)) as CaptureScreenshotResponse;
	return resp?.dataUrl ?? null;
}

export async function getFailedRequests(): Promise<FailedRequest[]> {
	const resp = (await chrome.runtime.sendMessage({
		type: "PACA_GET_FAILED_REQUESTS",
	} satisfies GetFailedRequestsRequest)) as GetFailedRequestsResponse;
	return resp?.requests ?? [];
}

// Fire-and-forget from the content script's side — the background worker
// attributes it to sender.tab.id, so there's nothing to await here.
export function setActiveState(active: boolean): void {
	chrome.runtime
		.sendMessage({
			type: "PACA_SET_ACTIVE_STATE",
			active,
		} satisfies SetActiveStateRequest)
		.catch(() => {});
}

export async function getActiveState(tabId: number): Promise<boolean> {
	const resp = (await chrome.runtime.sendMessage({
		type: "PACA_GET_ACTIVE_STATE",
		tabId,
	} satisfies GetActiveStateRequest)) as GetActiveStateResponse;
	return resp?.active ?? false;
}
