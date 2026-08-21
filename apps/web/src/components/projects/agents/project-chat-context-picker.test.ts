import { describe, expect, it } from "vitest";
import {
	MAX_USER_CONTEXT_SOURCES,
	toggleProjectChatContextSource,
} from "./project-chat-context-picker";

describe("toggleProjectChatContextSource", () => {
	it("deduplicates by type and id while preserving user order", () => {
		const first = { type: "task" as const, id: "shared-id" };
		const second = { type: "session" as const, id: "shared-id" };

		expect(toggleProjectChatContextSource([first], second)).toEqual([
			first,
			second,
		]);
		expect(toggleProjectChatContextSource([first, second], first)).toEqual([
			second,
		]);
	});

	it("reserves one server-side slot by stopping at 63 supplemental sources", () => {
		const full = Array.from(
			{ length: MAX_USER_CONTEXT_SOURCES },
			(_, index) => ({
				type: "task" as const,
				id: `task-${index}`,
			}),
		);

		expect(
			toggleProjectChatContextSource(full, {
				type: "run",
				id: "run-over-limit",
			}),
		).toBe(full);
	});

	it("cannot toggle off a required task-entry source", () => {
		const required = { type: "task" as const, id: "task-1" };
		const current = [required, { type: "run" as const, id: "run-1" }];

		expect(toggleProjectChatContextSource(current, required, [required])).toBe(
			current,
		);
	});
});
