import { describe, expect, it } from "vitest";
import { relativeTime } from "./relative-time";

const NOW = Date.parse("2026-06-15T12:00:00Z");
const ago = (s: number) => new Date(NOW - s * 1000).toISOString();

describe("relativeTime", () => {
	it("reads as now under a minute", () => {
		expect(relativeTime(ago(20), "en", NOW)).toMatch(/now/i);
	});

	it("picks the largest whole unit", () => {
		expect(relativeTime(ago(5 * 60), "en", NOW)).toMatch(/5/);
		expect(relativeTime(ago(5 * 3600), "en", NOW)).toMatch(/5/);
		expect(relativeTime(ago(3 * 86400), "en", NOW)).toMatch(/3/);
	});

	it("follows the locale", () => {
		expect(relativeTime(ago(3 * 3600), "ja", NOW)).not.toBe(
			relativeTime(ago(3 * 3600), "en", NOW),
		);
	});
});
