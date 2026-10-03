import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import {
	EXPORT_POLL_INTERVAL_MS,
	isExportActive,
	type ProjectExport,
	projectExportsQueryOptions,
} from "./export-api";

const base: ProjectExport = {
	id: "e",
	project_id: "p",
	kind: "project_archive",
	status: "completed",
	created_at: "2026-01-01T00:00:00Z",
	expired: false,
};

describe("isExportActive", () => {
	it("is true only while queued or running", () => {
		expect(isExportActive({ status: "pending" })).toBe(true);
		expect(isExportActive({ status: "processing" })).toBe(true);
		expect(isExportActive({ status: "completed" })).toBe(false);
		expect(isExportActive({ status: "failed" })).toBe(false);
	});
});

describe("projectExportsQueryOptions polling", () => {
	const interval = (data: ProjectExport[] | undefined) => {
		const client = new QueryClient();
		const opts = projectExportsQueryOptions("p");
		if (data) client.setQueryData(opts.queryKey, data);
		const query = client.getQueryCache().find({ queryKey: opts.queryKey });
		const fn = opts.refetchInterval as (q: unknown) => unknown;
		return fn(query ?? { state: { data: undefined } });
	};

	it("polls while an export is active", () => {
		expect(interval([{ ...base, status: "processing" }])).toBe(
			EXPORT_POLL_INTERVAL_MS,
		);
	});

	it("stops polling once everything has settled", () => {
		expect(interval([base, { ...base, status: "failed" }])).toBe(false);
		expect(interval([])).toBe(false);
		expect(interval(undefined)).toBe(false);
	});
});
