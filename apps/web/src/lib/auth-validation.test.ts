import { describe, expect, it } from "vitest";

import i18n from "@/i18n";
import {
	MIN_PASSWORD_LENGTH,
	MIN_USERNAME_LENGTH,
	validateConfirmPassword,
	validateNewPassword,
	validatePassword,
	validateUsername,
} from "./auth-validation";

// Validators are language-agnostic; the messages they return are translated
// via the passed `t`. Tests run under the default (en) language, so the
// asserted English strings come straight from the en translation bundle.
const t = i18n.t.bind(i18n);

describe("validateUsername", () => {
	it("requires a non-empty value", () => {
		expect(validateUsername("", t)).toBe("Username is required.");
	});

	it("requires a non-whitespace value", () => {
		expect(validateUsername("   ", t)).toBe("Username is required.");
	});

	it("enforces the minimum length", () => {
		expect(validateUsername("ab", t)).toBe(
			`Username must be at least ${MIN_USERNAME_LENGTH} characters.`,
		);
	});

	it("accepts a sufficiently long value", () => {
		expect(validateUsername("alice", t)).toBeUndefined();
	});
});

describe("validatePassword", () => {
	it("requires a non-empty value", () => {
		expect(validatePassword("", t)).toBe("Password is required.");
	});

	it("enforces the minimum length", () => {
		expect(validatePassword("short", t)).toBe(
			`Password must be at least ${MIN_PASSWORD_LENGTH} characters.`,
		);
	});

	it("accepts a sufficiently long value", () => {
		expect(validatePassword("validpass1", t)).toBeUndefined();
	});
});

describe("validateNewPassword", () => {
	it("requires a non-empty value", () => {
		expect(validateNewPassword("", t)).toBe("New password is required.");
	});

	it("enforces the minimum length", () => {
		expect(validateNewPassword("short", t)).toBe(
			`New password must be at least ${MIN_PASSWORD_LENGTH} characters.`,
		);
	});

	it("rejects the same value as the current password", () => {
		expect(validateNewPassword("SamePass1", t, "SamePass1")).toBe(
			"New password must be different from current password.",
		);
	});

	it("accepts a sufficiently long, different value", () => {
		expect(validateNewPassword("NewPass123", t, "OldPass123")).toBeUndefined();
	});
});

describe("validateConfirmPassword", () => {
	it("requires a non-empty value", () => {
		expect(validateConfirmPassword("", "NewPass123", t)).toBe(
			"Please confirm your new password.",
		);
	});

	it("rejects a mismatch", () => {
		expect(validateConfirmPassword("OtherPass1", "NewPass123", t)).toBe(
			"Passwords do not match.",
		);
	});

	it("accepts a matching value", () => {
		expect(
			validateConfirmPassword("NewPass123", "NewPass123", t),
		).toBeUndefined();
	});
});
