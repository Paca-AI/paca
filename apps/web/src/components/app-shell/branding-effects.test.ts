import { describe, expect, it } from "vitest";
import { darken, foregroundFor, hexToRgb, rgbToHex } from "./branding-effects";

describe("hexToRgb", () => {
	it("parses a lowercase hex color", () => {
		expect(hexToRgb("#5a9e1c")).toEqual([0x5a, 0x9e, 0x1c]);
	});

	it("parses an uppercase hex color case-insensitively", () => {
		expect(hexToRgb("#5A9E1C")).toEqual([0x5a, 0x9e, 0x1c]);
	});

	it("returns null for a value missing the leading #", () => {
		expect(hexToRgb("5a9e1c")).toBeNull();
	});

	it("returns null for a shorthand 3-digit hex", () => {
		expect(hexToRgb("#5a9")).toBeNull();
	});

	it("returns null for non-hex characters", () => {
		expect(hexToRgb("#zzzzzz")).toBeNull();
	});
});

describe("rgbToHex", () => {
	it("round-trips with hexToRgb", () => {
		const rgb = hexToRgb("#2563eb");
		expect(rgb).not.toBeNull();
		expect(rgbToHex(rgb as [number, number, number])).toBe("#2563eb");
	});

	it("clamps out-of-range and rounds fractional components", () => {
		expect(rgbToHex([-10, 300, 127.6])).toBe("#00ff80");
	});
});

describe("foregroundFor", () => {
	it("picks dark text for a light preset color", () => {
		// COLOR_PRESETS "green" dark-mode value from BrandingSettings.tsx —
		// bright enough that dark text should stay readable on it.
		expect(foregroundFor("#9ed957")).toBe("#0a0a0a");
	});

	it("picks white text for a dark/saturated preset color", () => {
		// COLOR_PRESETS "indigo" light-mode value — dark enough to need
		// white text for contrast.
		expect(foregroundFor("#4f46e5")).toBe("#ffffff");
	});

	it("falls back to white text for an invalid hex", () => {
		expect(foregroundFor("not-a-color")).toBe("#ffffff");
	});
});

describe("darken", () => {
	it("darkens each channel toward black by the given amount", () => {
		expect(darken("#9ed957", 0.25)).toBe(
			rgbToHex([0x9e * 0.75, 0xd9 * 0.75, 0x57 * 0.75]),
		);
	});

	it("leaves the color unchanged when amount is 0", () => {
		expect(darken("#5a9e1c", 0)).toBe("#5a9e1c");
	});

	it("returns the input unchanged for an invalid hex", () => {
		expect(darken("not-a-color", 0.25)).toBe("not-a-color");
	});
});
