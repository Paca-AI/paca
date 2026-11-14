import { expect, type Locator, type Page } from "@playwright/test";

/**
 * The role selector is a searchable multi-select: a combobox field (named
 * `label`) that opens a listbox of roles in a popover, outside the dialog or
 * panel the field sits in (the change-role dialog lists the roles inline
 * instead). These helpers open it, pick or unpick a role by name, and close
 * it again so it never covers the buttons beneath.
 */

/** The field that opens the selector, inside `scope`. */
export function roleSelectField(
	scope: Locator,
	label: string | RegExp,
): Locator {
	return scope.getByRole("combobox", {
		name: label,
		exact: typeof label === "string",
	});
}

/** Opens the selector (if it is not open) and returns its listbox. */
export async function openRoleSelect(
	page: Page,
	scope: Locator,
	label: string | RegExp,
): Promise<Locator> {
	const list = page.getByRole("listbox", { name: label });
	const field = roleSelectField(scope, label);
	// Wait for either the inline list (change-role dialog) or the field to
	// render; checking list visibility instantly races the dialog mounting on
	// slower engines (webkit) and ends up waiting for a field that never exists.
	await list.or(field).first().waitFor();
	if (!(await list.isVisible())) {
		await field.click();
	}
	await expect(list).toBeVisible();
	return list;
}

/**
 * Closes an open selector with Escape. A selector shown inline in a dialog
 * (the change-role dialog) is not a popover: it has nothing to close, and
 * Escape would close the dialog itself, so it is left as it is.
 */
export async function closeRoleSelect(page: Page, label: string | RegExp) {
	const popover = page
		.locator("[data-slot=popover-content]")
		.getByRole("listbox", { name: label });
	if (await popover.isVisible().catch(() => false)) {
		await page.keyboard.press("Escape");
		await expect(popover).toHaveCount(0);
	}
}

/** One role's option in an open selector. */
export function roleOptionIn(list: Locator, name: string): Locator {
	return list.getByRole("option", { name, exact: true });
}

/** Picks (`selected` true) or unpicks a role, then closes the selector. */
export async function setRole(
	page: Page,
	scope: Locator,
	label: string | RegExp,
	name: string,
	selected: boolean,
) {
	const list = await openRoleSelect(page, scope, label);
	const option = roleOptionIn(list, name);
	const isSelected = (await option.getAttribute("aria-selected")) === "true";
	if (isSelected !== selected) await option.click();
	await expect(option).toHaveAttribute("aria-selected", String(selected));
	await closeRoleSelect(page, label);
}
