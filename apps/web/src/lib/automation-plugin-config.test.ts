import { describe, expect, it } from "vitest";

import {
	gitNodeShortLabel,
	isGitNodeType,
	pluginConfigRows,
	pluginMatchLabel,
} from "./automation-plugin-config";

const schema = {
	properties: {
		state: {
			title: "Pull Request State",
			enum: ["merged", "ready_for_review"],
		},
		branch: { title: "Target Branch", type: "string" as const },
		method: { title: "Merge Method", enum: ["squash"], default: "squash" },
	},
};

const field = (r: { title: string; value: string }) => `${r.title}: ${r.value}`;

describe("isGitNodeType / gitNodeShortLabel", () => {
	it("recognises the git. namespace only", () => {
		expect(isGitNodeType({ type: "git.pr_merged" })).toBe(true);
		expect(isGitNodeType({ type: "com.acme.git.x" })).toBe(false);
	});

	it("drops the Git: prefix", () => {
		expect(gitNodeShortLabel("Git: Merge Pull Request")).toBe(
			"Merge Pull Request",
		);
		expect(gitNodeShortLabel("Merge")).toBe("Merge");
	});
});

describe("pluginConfigRows", () => {
	it("is undefined without schema fields", () => {
		expect(pluginConfigRows(undefined, {})).toBeUndefined();
		expect(pluginConfigRows({ properties: {} }, {})).toBeUndefined();
	});

	it("is empty when nothing is configured", () => {
		expect(pluginConfigRows(schema, {}, { useDefaults: false })).toEqual([]);
	});

	it("prettifies enums and applies defaults only when asked", () => {
		expect(pluginConfigRows(schema, { state: "ready_for_review" })).toEqual([
			{ title: "Pull Request State", value: "Ready for review", isEnum: true },
			{ title: "Merge Method", value: "Squash", isEnum: true },
		]);
		expect(
			pluginConfigRows(schema, { state: "merged" }, { useDefaults: false }),
		).toHaveLength(1);
	});

	it("formats arrays and objects without [object Object]", () => {
		const rows = pluginConfigRows(
			{ properties: { a: { title: "A" }, b: { title: "B" } } },
			{ a: ["x", "y"], b: { k: 1 } },
		);
		expect(rows?.map((r) => r.value)).toEqual(["x, y", '{"k":1}']);
	});

	it("collapses whitespace and skips empty values", () => {
		const rows = pluginConfigRows(
			{ properties: { a: { title: "A" }, b: { title: "B" } } },
			{ a: "one\n two", b: "" },
		);
		expect(rows).toEqual([{ title: "A", value: "one two", isEnum: false }]);
	});
});

describe("pluginMatchLabel", () => {
	it("falls back to undefined when unconfigured", () => {
		expect(pluginMatchLabel(undefined, field)).toBeUndefined();
		expect(pluginMatchLabel([], field)).toBeUndefined();
	});

	it("shows the bare value for a single enum field", () => {
		const rows = pluginConfigRows(
			schema,
			{ state: "merged" },
			{ useDefaults: false },
		);
		expect(pluginMatchLabel(rows, field)).toBe("Merged");
	});

	it("shows Title: value for text fields and joins several", () => {
		const one = pluginConfigRows(
			schema,
			{ branch: "main" },
			{ useDefaults: false },
		);
		expect(pluginMatchLabel(one, field)).toBe("Target Branch: main");
		const two = pluginConfigRows(
			schema,
			{ state: "merged", branch: "main" },
			{ useDefaults: false },
		);
		expect(pluginMatchLabel(two, field)).toBe(
			"Pull Request State: Merged, Target Branch: main",
		);
	});
});
