// Unsubmitted chat input is parked here so that a reload the app cannot
// prevent — an expired session bouncing through the login/SSO flow and back
// to the same URL — restores the message instead of discarding it. Reload is
// the one case the composer cannot survive on its own: everything typed lives
// in the assistant-ui runtime, which a page load throws away.
//
// sessionStorage, not localStorage: a draft only has to outlive a reload in
// this tab, and a prompt should not be left on disk after the tab is gone.
// Keyed by pathname so every page restores only what was typed on it.
const PREFIX = "paca.composer-draft:";

export function composerDraftKey(pathname: string): string {
	return `${PREFIX}${pathname}`;
}

export function loadComposerDraft(key: string): string | null {
	try {
		return window.sessionStorage.getItem(key);
	} catch {
		// Storage disabled (private mode, policy): the composer still works,
		// the draft just does not survive a reload.
		return null;
	}
}

export function saveComposerDraft(key: string, text: string): void {
	try {
		if (text) window.sessionStorage.setItem(key, text);
		else window.sessionStorage.removeItem(key);
	} catch {
		// See loadComposerDraft.
	}
}
