/**
 * Shared validation constants and helpers for auth-related forms.
 *
 * Used by the login form, the change-password form, and the admin user
 * dialog so that every surface applies the same rules and messages.
 *
 * Validators take a `t` translation function so the returned messages are
 * localized at the call site (the rules themselves are language-agnostic).
 */

import type { TFunction } from "i18next";

/** Minimum number of characters a password must contain. */
export const MIN_PASSWORD_LENGTH = 8;

/** Minimum number of characters a username must contain. */
export const MIN_USERNAME_LENGTH = 3;

/**
 * Validate a username.
 *
 * Returns `undefined` when the value is valid, or an error message otherwise.
 */
export function validateUsername(
	value: string,
	t: TFunction,
): string | undefined {
	if (!value.trim()) {
		return t("validation.usernameRequired");
	}

	if (value.trim().length < MIN_USERNAME_LENGTH) {
		return t("validation.usernameMinLength", { min: MIN_USERNAME_LENGTH });
	}

	return undefined;
}

/**
 * Validate a password that is being entered "as-is" — i.e. a login password
 * or a "current password" field.
 *
 * Returns `undefined` when the value is valid, or an error message otherwise.
 */
export function validatePassword(
	value: string,
	t: TFunction,
): string | undefined {
	if (!value) {
		return t("validation.passwordRequired");
	}

	if (value.length < MIN_PASSWORD_LENGTH) {
		return t("validation.passwordMinLength", { min: MIN_PASSWORD_LENGTH });
	}

	return undefined;
}

/**
 * Validate a password that the user is choosing for the first time (or
 * replacing an old one with).
 *
 * `currentPassword` is passed so that the new password can be checked against
 * the current password to ensure they differ.
 *
 * Returns `undefined` when the value is valid, or an error message otherwise.
 */
export function validateNewPassword(
	value: string,
	t: TFunction,
	currentPassword?: string,
): string | undefined {
	if (!value) {
		return t("validation.newPasswordRequired");
	}

	if (value.length < MIN_PASSWORD_LENGTH) {
		return t("validation.newPasswordMinLength", { min: MIN_PASSWORD_LENGTH });
	}

	if (currentPassword && value === currentPassword) {
		return t("validation.newPasswordDifferent");
	}

	return undefined;
}

/**
 * Validate that the confirm-password field matches the new password.
 *
 * Returns `undefined` when the value is valid, or an error message otherwise.
 */
export function validateConfirmPassword(
	value: string,
	newPassword: string,
	t: TFunction,
): string | undefined {
	if (!value) {
		return t("validation.confirmRequired");
	}

	if (newPassword !== value) {
		return t("validation.passwordsNoMatch");
	}

	return undefined;
}
