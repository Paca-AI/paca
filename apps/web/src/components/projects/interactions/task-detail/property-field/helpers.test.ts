// Regression tests for the custom-field / sprint date off-by-one bug: a
// date-only value is stored as "YYYY-MM-DDT00:00:00Z" (UTC midnight), so
// parsing/formatting it in a negative-offset timezone (e.g. America/Sao_Paulo,
// UTC-3) used to render the *previous* calendar day. These helpers must treat
// the value as a plain calendar date, independent of the viewer's timezone.
//
// TZ must be set before any Date is constructed (V8 reads it lazily but caches
// it once used), so it is forced here at module load — before the test bodies
// run. Done via globalThis so it type-checks without @types/node in the app's
// build tsconfig (tsc -b), where the bare `process` global is not declared.
(
	globalThis as unknown as { process: { env: Record<string, string> } }
).process.env.TZ = "America/Sao_Paulo";

import { describe, expect, it } from "vitest";
import { displayDate, toDateObject, toISODate } from "./helpers";

describe("date helpers (timezone-safe, calendar-date semantics)", () => {
	it("displayDate shows the stored calendar day, not the day before", () => {
		expect(displayDate("2026-09-04T00:00:00Z")).toContain("4");
		expect(displayDate("2026-01-01T00:00:00Z")).toContain("1");
	});

	it("toDateObject resolves to the stored calendar day locally", () => {
		const d = toDateObject("2026-09-04T00:00:00Z");
		expect(d?.getFullYear()).toBe(2026);
		expect(d?.getMonth()).toBe(8); // September (0-based)
		expect(d?.getDate()).toBe(4);
	});

	it("round-trips a picked date without drifting", () => {
		// Calendar hands back a Date at LOCAL midnight for the picked day.
		const picked = new Date(2026, 8, 4); // 4 Sep 2026, local
		const iso = toISODate(picked);
		expect(iso).toBe("2026-09-04T00:00:00Z");
		expect(toDateObject(iso)?.getDate()).toBe(4);
		expect(displayDate(iso)).toContain("4");
	});

	it("handles empty / invalid input", () => {
		expect(displayDate(null)).toBeNull();
		expect(displayDate("")).toBeNull();
		expect(displayDate("not-a-date")).toBeNull();
		expect(toDateObject(null)).toBeUndefined();
		expect(toDateObject("")).toBeUndefined();
	});
});
