// navigator.clipboard.writeText requires a secure context (https, or
// localhost) -- a content script inherits the *page's* security context,
// not the extension's, so on a forwarded dev server served over plain http
// (the normal case for a port-forwarded preview) navigator.clipboard is
// either undefined or writeText rejects outright. document.execCommand
// "copy" has no such restriction and is still fully functional in every
// browser this extension targets, so it's the fallback whenever the modern
// API isn't usable.
export async function copyToClipboard(text: string): Promise<boolean> {
	if (window.isSecureContext && navigator.clipboard?.writeText) {
		try {
			await navigator.clipboard.writeText(text);
			return true;
		} catch {
			// Fall through to the execCommand fallback below.
		}
	}

	const textarea = document.createElement("textarea");
	textarea.value = text;
	// Off-screen but still focusable/selectable -- execCommand("copy") only
	// acts on the current selection, so the element must actually be
	// selected, not just present in the DOM.
	textarea.style.position = "fixed";
	textarea.style.top = "0";
	textarea.style.left = "0";
	textarea.style.opacity = "0";
	document.body.appendChild(textarea);
	textarea.focus();
	textarea.select();
	let ok: boolean;
	try {
		ok = document.execCommand("copy");
	} catch {
		ok = false;
	}
	textarea.remove();
	return ok;
}
