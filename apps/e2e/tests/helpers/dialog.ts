import type { Locator, Page } from "@playwright/test";

/**
 * Dialog and AlertDialog panels. A bare `getByRole("dialog")` would also match
 * toasts, which base-ui renders with `role="dialog"` and `data-slot="toast"`,
 * so a locator for a dismissed modal re-resolves to the success toast.
 */
export const DIALOG_CONTENT =
	'[data-slot="dialog-content"], [data-slot="alert-dialog-content"]';

/** The open `Dialog` (role="dialog"), never a toast. */
export function dialogPanel(page: Page): Locator {
	return page
		.getByRole("dialog")
		.and(page.locator(':not([data-slot="toast"])'));
}
