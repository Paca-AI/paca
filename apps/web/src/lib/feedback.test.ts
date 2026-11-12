import { describe, expect, it } from "vitest";
import {
	buildIssueUrl,
	draftFromError,
	formatErrorDetails,
	MAX_ISSUE_URL_LENGTH,
} from "./feedback";

describe("buildIssueUrl", () => {
	it("targets the template and pre-fills title and fields", () => {
		const { url, truncated } = buildIssueUrl({
			kind: "bug",
			title: "Crash",
			fields: { summary: "It broke", steps: "", area: "Frontend" },
		});
		const parsed = new URL(url);
		expect(parsed.pathname).toBe("/Paca-AI/paca/issues/new");
		expect(parsed.searchParams.get("template")).toBe("bug_report.yml");
		expect(parsed.searchParams.get("title")).toBe("[Bug] Crash");
		expect(parsed.searchParams.get("summary")).toBe("It broke");
		expect(parsed.searchParams.get("area")).toBe("Frontend");
		expect(parsed.searchParams.has("steps")).toBe(false);
		expect(truncated).toBe(false);
	});

	it("uses the feature template", () => {
		const { url } = buildIssueUrl({ kind: "feature", title: "Dark mode" });
		const parsed = new URL(url);
		expect(parsed.searchParams.get("template")).toBe("feature_request.yml");
		expect(parsed.searchParams.get("title")).toBe("[Feature] Dark mode");
	});

	it("truncates the longest field to keep the URL openable", () => {
		const { url, truncated } = buildIssueUrl({
			kind: "bug",
			title: "x",
			fields: { summary: "short", logs: "é".repeat(20000) },
		});
		expect(truncated).toBe(true);
		expect(url.length).toBeLessThanOrEqual(MAX_ISSUE_URL_LENGTH);
		const parsed = new URL(url);
		expect(parsed.searchParams.get("summary")).toBe("short");
		expect(parsed.searchParams.get("logs")).toContain("truncated");
	});
});

describe("formatErrorDetails / draftFromError", () => {
	it("includes message, stack, and API response details", () => {
		const error = Object.assign(new Error("boom"), {
			config: { method: "get", url: "/api/v1/things" },
			response: { status: 500, data: { error_code: "INTERNAL" } },
		});
		const text = formatErrorDetails(error);
		expect(text).toContain("Error: boom");
		expect(text).toContain("Request: GET /api/v1/things");
		expect(text).toContain("HTTP status: 500");
		expect(text).toContain('"error_code":"INTERNAL"');
		expect(text).toContain("at ");
	});

	it("builds a bug draft from an error", () => {
		const draft = draftFromError(new Error("kaput"));
		expect(draft.kind).toBe("bug");
		expect(draft.title).toBe("kaput");
		expect(draft.fields?.logs).toContain("kaput");
		expect(draft.fields?.environment).toContain("Browser:");
	});
});
