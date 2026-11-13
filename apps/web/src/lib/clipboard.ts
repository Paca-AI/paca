import { toast } from "@/components/ui/toast";
import i18n from "@/i18n";

// navigator.clipboard is only defined in a secure context (https or
// localhost). Paca is commonly served over plain http (LAN IP, internal
// hostname), where navigator.clipboard is undefined and writeText can't be
// used. document.execCommand("copy") has no such restriction, so it's the
// fallback whenever the modern API is unavailable or rejects.
async function writeToClipboard(text: string): Promise<boolean> {
	if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
		try {
			await navigator.clipboard.writeText(text);
			return true;
		} catch {
			// Fall through to the execCommand fallback below.
		}
	}

	if (typeof document === "undefined") return false;

	const previouslyFocused = document.activeElement as HTMLElement | null;
	// Mount the textarea inside the active dialog (if any) rather than on
	// <body>: modal focus traps pull focus back into their container the moment
	// it moves outside, which would drop our selection before the copy runs.
	const container =
		previouslyFocused?.closest<HTMLElement>(
			'[role="dialog"], [role="alertdialog"]',
		) ?? document.body;

	const textarea = document.createElement("textarea");
	textarea.value = text;
	textarea.setAttribute("readonly", "");
	// Off-screen but still selectable -- execCommand("copy") acts on the
	// current selection, so the element must really be selected.
	textarea.style.position = "fixed";
	textarea.style.top = "0";
	textarea.style.left = "0";
	textarea.style.opacity = "0";
	// 16px avoids iOS Safari zooming the page when the field is focused.
	textarea.style.fontSize = "16px";

	let ok = false;
	try {
		container.appendChild(textarea);
		textarea.focus({ preventScroll: true });
		textarea.select();
		// select() alone is ignored by iOS Safari for readonly fields.
		textarea.setSelectionRange(0, text.length);
		ok = document.execCommand("copy");
	} catch {
		ok = false;
	} finally {
		textarea.remove();
		previouslyFocused?.focus?.({ preventScroll: true });
	}
	return ok;
}

/**
 * Copies `text`, resolving to whether it worked. On failure it shows an error
 * toast so callers only have to handle the success state (the "Copied" flip).
 * Pass `{ notify: false }` where the caller has a more specific message.
 */
export async function copyToClipboard(
	text: string,
	{ notify = true }: { notify?: boolean } = {},
): Promise<boolean> {
	const ok = await writeToClipboard(text);
	if (!ok && notify) {
		toast.add({ type: "error", title: i18n.t("common.copyFailed") });
	}
	return ok;
}
