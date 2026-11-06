import { describe, expect, it } from "vitest";
import enProjects from "@/i18n/locales/en/projects.json";
import { describeTaskChange } from "./activity-item";

function mockT(key: string, options?: Record<string, unknown>): string {
	const parts = key.split(".");
	let curr: unknown = enProjects;
	for (const part of parts) {
		curr = (curr as Record<string, unknown>)?.[part];
	}
	if (typeof curr !== "string") return key;
	let res = curr;
	if (options) {
		for (const [k, v] of Object.entries(options)) {
			res = res.replaceAll(`{{${k}}}`, String(v));
		}
	}
	return res;
}

const emptyNames = { members: {}, sprints: {} };

describe("describeTaskChange with custom fields", () => {
	it("describes numeric custom field update with before and after numbers", () => {
		const result = describeTaskChange(
			{ field: "Actual Dev Efforts", old: 2, new: 5 },
			emptyNames,
			mockT as never,
		);
		expect(result).toBe("changed Actual Dev Efforts from 2 to 5");
	});

	it("describes setting a numeric custom field", () => {
		const result = describeTaskChange(
			{ field: "Actual Dev Efforts", old: null, new: 5 },
			emptyNames,
			mockT as never,
		);
		expect(result).toBe("set Actual Dev Efforts to 5");
	});

	it("describes clearing a custom field", () => {
		const result = describeTaskChange(
			{ field: "Actual Dev Efforts", old: 5, new: null },
			emptyNames,
			mockT as never,
		);
		expect(result).toBe("cleared Actual Dev Efforts");
	});

	it("describes date custom field update with formatted dates", () => {
		const result = describeTaskChange(
			{
				field: "Development Completion Date",
				old: "2026-11-01T00:00:00Z",
				new: "2026-11-05T00:00:00Z",
			},
			emptyNames,
			mockT as never,
		);
		expect(result).toBe(
			"changed Development Completion Date from Nov 1, 2026 to Nov 5, 2026",
		);
	});

	it("describes text custom field update (e.g. QA by)", () => {
		const result = describeTaskChange(
			{ field: "QA by", old: "Alice", new: "Bob" },
			emptyNames,
			mockT as never,
		);
		expect(result).toBe("changed QA by from Alice to Bob");
	});

	it("describes setting QA Start Date and QA End Date", () => {
		const startResult = describeTaskChange(
			{ field: "QA Start Date", old: null, new: "2026-11-06T00:00:00Z" },
			emptyNames,
			mockT as never,
		);
		expect(startResult).toBe("set QA Start Date to Nov 6, 2026");

		const endResult = describeTaskChange(
			{ field: "QA End Date", old: null, new: "2026-11-08T00:00:00Z" },
			emptyNames,
			mockT as never,
		);
		expect(endResult).toBe("set QA End Date to Nov 8, 2026");
	});

	it("replaces underscores in field key if display name is snake_cased", () => {
		const result = describeTaskChange(
			{ field: "actual_dev_efforts", old: 1, new: 3 },
			emptyNames,
			mockT as never,
		);
		expect(result).toBe("changed actual dev efforts from 1 to 3");
	});

	it("preserves legacy custom_fields entry without crashing", () => {
		const result = describeTaskChange(
			{ field: "custom_fields" },
			emptyNames,
			mockT as never,
		);
		expect(result).toBe("updated custom fields");
	});
});

