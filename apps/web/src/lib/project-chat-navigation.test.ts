import { afterEach, describe, expect, it, vi } from "vitest";
import {
	mergeRequiredProjectChatSources,
	newTaskChatHref,
	newTaskChatSearch,
	projectChatContextSourcesEqual,
	shouldShowProjectChatMainOnMobile,
	taskChatInitialContextSources,
} from "./project-chat-navigation";

describe("newTaskChatHref", () => {
	afterEach(() => vi.restoreAllMocks());

	it("creates a fresh local draft nonce for every task entry", () => {
		vi.spyOn(globalThis.crypto, "randomUUID")
			.mockReturnValueOnce("00000000-0000-4000-8000-000000000001")
			.mockReturnValueOnce("00000000-0000-4000-8000-000000000002");

		const first = new URL(
			newTaskChatHref("project-1", "task-1"),
			"https://paca.test",
		);
		const second = new URL(
			newTaskChatHref("project-1", "task-1"),
			"https://paca.test",
		);

		expect(first.pathname).toBe("/projects/project-1/chats");
		expect(first.searchParams.get("contextTaskId")).toBe("task-1");
		expect(first.searchParams.has("taskId")).toBe(false);
		expect(first.searchParams.get("draft")).toBe(
			"00000000-0000-4000-8000-000000000001",
		);
		expect(second.searchParams.get("draft")).toBe(
			"00000000-0000-4000-8000-000000000002",
		);
	});

	it("carries an explicitly selected agent into the fresh draft", () => {
		vi.spyOn(globalThis.crypto, "randomUUID").mockReturnValue(
			"00000000-0000-4000-8000-000000000003",
		);
		expect(newTaskChatSearch("task-1", "agent-1")).toEqual({
			contextTaskId: "task-1",
			draft: "00000000-0000-4000-8000-000000000003",
			agentId: "agent-1",
		});
		const href = new URL(
			newTaskChatHref("project-1", "task-1", "agent-1"),
			"https://paca.test",
		);
		expect(href.searchParams.get("agentId")).toBe("agent-1");
	});

	it("preloads exactly the selected task as canonical context", () => {
		expect(taskChatInitialContextSources("task-1")).toEqual([
			{ type: "task", id: "task-1" },
		]);
		expect(taskChatInitialContextSources(undefined)).toEqual([]);
	});

	it("reasserts the required task before submission without duplicating it", () => {
		const required = [{ type: "task" as const, id: "task-1" }];
		expect(
			mergeRequiredProjectChatSources(
				[
					{ type: "session", id: "session-1" },
					{ type: "task", id: "task-1" },
				],
				required,
			),
		).toEqual([
			{ type: "task", id: "task-1" },
			{ type: "session", id: "session-1" },
		]);
	});

	it("distinguishes no-op context apply from ordered source changes", () => {
		const sources = [
			{ type: "task" as const, id: "task-1" },
			{ type: "session" as const, id: "session-1" },
		];
		expect(projectChatContextSourcesEqual([...sources], [...sources])).toBe(
			true,
		);
		expect(
			projectChatContextSourcesEqual([...sources], [...sources].reverse()),
		).toBe(false);
		expect(
			projectChatContextSourcesEqual(
				[...sources],
				[
					{ type: "task", id: "task-2" },
					{ type: "session", id: "session-1" },
				],
			),
		).toBe(false);
	});

	it("shows the correct mobile pane for list, drafts, task entries, and sessions", () => {
		expect(shouldShowProjectChatMainOnMobile(undefined, {})).toBe(false);
		expect(
			shouldShowProjectChatMainOnMobile(undefined, { draft: "draft-1" }),
		).toBe(true);
		expect(
			shouldShowProjectChatMainOnMobile(undefined, {
				contextTaskId: "task-1",
			}),
		).toBe(true);
		expect(shouldShowProjectChatMainOnMobile("session-1", {})).toBe(true);
	});
});
