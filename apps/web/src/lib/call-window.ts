import { DM_SCOPE } from "./chat-api";

/** The path of the call tab for a channel (or direct message, scope DM_SCOPE). */
export function callTabPath(
	scope: string,
	channelId: string,
	name: string,
): string {
	return `/call/${scope === DM_SCOPE ? DM_SCOPE : scope}/${channelId}?name=${encodeURIComponent(name)}`;
}

/**
 * Opens the call in its own tab, or brings the one already open for this
 * channel to the front. The call keeps running there while the app is used in
 * this tab.
 */
export function openCallTab(
	scope: string,
	channelId: string,
	name: string,
): void {
	const w = window.open(
		callTabPath(scope, channelId, name),
		`paca-call-${channelId}`,
	);
	w?.focus();
}
