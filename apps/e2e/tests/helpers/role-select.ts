import { expect, type Locator, type Page } from "@playwright/test";

/**
 * The role selector is a searchable multi-select: a combobox field (named
 * `label`) that opens a listbox of roles in a popover, outside the dialog or
 * panel the field sits in. These helpers open it, pick or unpick a role by
 * name, and close it again so it never covers the buttons beneath.
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
	if (!(await list.isVisible().catch(() => false))) {
		await roleSelectField(scope, label).click();
	}
	await expect(list).toBeVisible();
	return list;
}

/** Closes an open selector with Escape. */
export async function closeRoleSelect(page: Page, label: string | RegExp) {
	const list = page.getByRole("listbox", { name: label });
	if (await list.isVisible().catch(() => false)) {
		await page.keyboard.press("Escape");
	}
	await expect(list).toHaveCount(0);
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
