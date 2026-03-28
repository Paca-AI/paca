import { describe, expect, it } from "vitest";
import { cn } from "./utils";

describe("cn", () => {
	it("joins class names and ignores falsy values", () => {
		const value = cn("px-4", undefined, null, false, "py-2");
		expect(value).toBe("px-4 py-2");
	});

	it("merges conflicting Tailwind classes", () => {
		const value = cn("px-2", "px-4", "text-sm", "text-lg");
		expect(value).toBe("px-4 text-lg");
	});
});
