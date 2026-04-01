import { describe, expect, it } from "vitest";

import { generatePassword } from "./generate-password";

describe("generatePassword", () => {
	it("returns a 16-character string", () => {
		expect(generatePassword()).toHaveLength(16);
	});

	it("contains at least one uppercase letter", () => {
		expect(generatePassword()).toMatch(/[A-Z]/);
	});

	it("contains at least one lowercase letter", () => {
		expect(generatePassword()).toMatch(/[a-z]/);
	});

	it("contains at least one digit", () => {
		expect(generatePassword()).toMatch(/[0-9]/);
	});

	it("contains at least one symbol", () => {
		expect(generatePassword()).toMatch(/[!@#$%^&*()\-_=+[\]{}]/);
	});

	it("produces different values on successive calls", () => {
		const first = generatePassword();
		const second = generatePassword();
		// Statistically impossible to collide on two crypto-random 16-char passwords
		expect(first).not.toBe(second);
	});
});
