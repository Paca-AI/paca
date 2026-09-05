import { describe, expect, it } from "vitest";
import {
	customFieldBadgeStyle,
	getCustomFieldOptionColor,
} from "./custom-field-colors";

describe("getCustomFieldOptionColor", () => {
	it("returns the matching option's color", () => {
		const options = [
			{ value: "Low", color: "#22c55e" },
			{ value: "High", color: "#ef4444" },
		];
		expect(getCustomFieldOptionColor(options, "High")).toBe("#ef4444");
	});

	it("returns undefined when the option has no color", () => {
		const options = [{ value: "Low" }];
		expect(getCustomFieldOptionColor(options, "Low")).toBeUndefined();
	});

	it("returns undefined when no option matches the value", () => {
		const options = [{ value: "Low", color: "#22c55e" }];
		expect(getCustomFieldOptionColor(options, "Missing")).toBeUndefined();
	});

	it("returns undefined when options is undefined", () => {
		expect(getCustomFieldOptionColor(undefined, "Low")).toBeUndefined();
	});
});

describe("customFieldBadgeStyle", () => {
	it("returns a tinted background plus solid text color for a valid hex color", () => {
		expect(customFieldBadgeStyle("#ef4444")).toEqual({
			backgroundColor: "#ef44441a",
			color: "#ef4444",
		});
	});

	it("returns undefined when color is undefined", () => {
		expect(customFieldBadgeStyle(undefined)).toBeUndefined();
	});

	// Regression test: nothing validates `color` server-side, so a value
	// written some other way (bare color name, 3-digit hex, garbage from a
	// direct API call or MCP tool call) could reach this function. Appending
	// the alpha suffix to anything other than a 6-digit hex string produces
	// invalid CSS, silently dropping the tint — reject it up front instead.
	it.each(["red", "#fff", "not-a-color", "#gggggg", "#ef444"])(
		"returns undefined for a non-hex color %s",
		(color) => {
			expect(customFieldBadgeStyle(color)).toBeUndefined();
		},
	);
});
