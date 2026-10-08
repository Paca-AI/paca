import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

/**
 * The searchable role selector keeps its options in a popover. Opens it when
 * it is closed (the trigger is the combobox whose name is `label`, or the
 * only one when omitted) and returns the listbox.
 */
export async function openRoleSelect(label?: string | RegExp) {
	const open = screen.queryByRole("listbox");
	if (open) return open;
	const triggers = screen.getAllByRole(
		"combobox",
		label ? { name: label } : {},
	);
	await userEvent.click(triggers[0]);
	return screen.findByRole("listbox");
}

/** The option for a role (by its name), opening the selector if needed. */
export async function roleOption(
	name: string | RegExp,
	label?: string | RegExp,
) {
	const list = await openRoleSelect(label);
	return within(list).getByRole("option", { name });
}

/** Clicks a role's option to pick or unpick it. */
export async function toggleRole(
	name: string | RegExp,
	label?: string | RegExp,
) {
	await userEvent.click(await roleOption(name, label));
}
