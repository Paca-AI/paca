// Regression tests for the date off-by-one in list/board views: date-only
// values (custom Date fields, task start/due dates) are stored as
// "YYYY-MM-DDT00:00:00Z" (UTC midnight). Formatting them in the viewer's local
// timezone (negative UTC offset, e.g. America/Sao_Paulo, UTC-3) rendered the
// previous day. formatDate must render date-only values in UTC, while keeping
// real timestamps (created_at/updated_at) in local time.
//
// TZ must be set before any Date is constructed; done via globalThis so it
// type-checks without @types/node under the app's build tsconfig (tsc -b).
(
	globalThis as unknown as { process: { env: Record<string, string> } }
).process.env.TZ = "America/Sao_Paulo";

import { describe, expect, it } from "vitest";
import { formatDate } from "./format-date";

describe("formatDate", () => {
	it("runs under a negative UTC offset (guards the TZ setup)", () => {
		// If process.env.TZ was silently ignored (can happen on ICU-backed Node),
		// these tests would pass vacuously in UTC. America/Sao_Paulo is UTC-3, so
		// getTimezoneOffset() must be positive; fail loudly otherwise.
		expect(new Date(0).getTimezoneOffset()).toBeGreaterThan(0);
	});

	it("dateOnly renders the stored calendar day regardless of timezone", () => {
		expect(
			formatDate(
				"2026-08-14T00:00:00Z",
				{ month: "short", day: "numeric" },
				{ dateOnly: true },
			),
		).toContain("14");
	});

	it("keeps real timestamps in local time (no dateOnly)", () => {
		// 2026-08-14T02:00:00Z is 2026-08-13 23:00 local (UTC-3) → day 13.
		expect(
			formatDate("2026-08-14T02:00:00Z", { month: "short", day: "numeric" }),
		).toContain("13");
	});

	it("returns empty string for invalid input", () => {
		expect(formatDate("not-a-date", { day: "numeric" })).toBe("");
	});
});
