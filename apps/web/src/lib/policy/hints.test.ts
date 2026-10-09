import { describe, expect, it } from "vitest";

import { applyHint, policyHints } from "./hints";

const actions = ["tasks:read", "tasks:write", "docs:read"];
const keys = ["task.sprint_id", "doc.folder_id"];

describe("policyHints", () => {
	it("suggests actions for a partly typed action string", () => {
		const text = '{"actions": ["tas';
		const ctx = policyHints(text, text.length, actions, keys);
		expect(ctx?.hints.map((h) => h.value)).toEqual([
			"tasks:read",
			"tasks:write",
			"task.sprint_id",
		]);
		expect(ctx?.token).toBe("tas");
	});

	it("suggests attribute keys for a condition key", () => {
		const text = '"conditions": {"StringEquals": {"task.sp';
		const ctx = policyHints(text, text.length, actions, keys);
		expect(ctx?.hints).toEqual([
			{ value: "task.sprint_id", kind: "attribute" },
		]);
	});

	it("offers nothing outside a string, on an empty token or without a match", () => {
		expect(policyHints('{"a": "x"', 9, actions, keys)).toBeNull();
		expect(policyHints('["', 2, actions, keys)).toBeNull();
		expect(policyHints('["zzz', 5, actions, keys)).toBeNull();
	});

	it("replaces the token with the chosen value", () => {
		const text = '["tas", "x"]';
		const ctx = policyHints(text, 5, actions, keys);
		if (!ctx) throw new Error("expected hints");
		expect(applyHint(text, 5, ctx, "tasks:read")).toEqual({
			text: '["tasks:read", "x"]',
			caret: 12,
		});
	});
});
