import { describe, expect, it } from "vitest";
import { taskCreatedBlocks, titleFromText } from "./chat-tasks";
import type { Task } from "./interaction-api";

describe("chat tasks", () => {
	it("takes the first non-empty line as the title", () => {
		expect(titleFromText("\n  Fix   the login\nmore detail")).toBe(
			"Fix the login",
		);
		expect(titleFromText("   ")).toBe("");
	});

	it("cuts a long first line", () => {
		const t = titleFromText("x".repeat(300));
		expect(t).toHaveLength(120);
		expect(t.endsWith("…")).toBe(true);
	});

	it("reports a created task with a chip that holds ids, not the title", () => {
		const blocks = taskCreatedBlocks("Created:", {
			id: "t1",
			project_id: "p1",
			task_number: 7,
			title: "Secret title",
		} as Task);
		expect(blocks[0]?.content?.[1]).toEqual({
			type: "taskReference",
			props: { id: "t1", projectId: "p1", number: 7 },
		});
		expect(JSON.stringify(blocks)).not.toContain("Secret title");
	});
});
