import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ChatProvider } from "./chat-context";
import { ToolFallback } from "./tool-fallback";
import type { ChatToolCallPart } from "./types";

function baseProps(overrides: Partial<ChatToolCallPart> = {}) {
	return {
		part: {
			type: "tool-call",
			toolCallId: "tc-1",
			toolName: "list_repositories",
			argsText: "{}",
			result: "done",
			...overrides,
		} as ChatToolCallPart,
		status: { type: "complete" } as const,
		isLastMessage: true,
	};
}

function renderCard(props: ReturnType<typeof baseProps>) {
	return render(
		<ChatProvider
			value={{ isRunning: false, lastMessageId: "m", send: () => {} }}
		>
			<ToolFallback {...props} />
		</ChatProvider>,
	);
}

function getTrigger(name: string | RegExp) {
	return screen
		.getByText(name)
		.closest('[data-slot="trace-collapsible-trigger"]') as HTMLElement;
}

describe("ToolFallback expand state", () => {
	it("keeps a tool call expanded across a remount of the same toolCallId", () => {
		const props = baseProps();
		const { unmount } = renderCard(props);

		const trigger = getTrigger(/list_repositories/i);
		expect(trigger.getAttribute("aria-expanded")).toBe("false");
		fireEvent.click(trigger);
		expect(trigger.getAttribute("aria-expanded")).toBe("true");

		// Simulates the surrounding message tree remounting this leaf (e.g. a
		// group-boundary reshape as new events stream in) — the component
		// instance is fully destroyed and a fresh one created for the same
		// tool call.
		unmount();
		renderCard(props);

		const triggerAfter = getTrigger(/list_repositories/i);
		expect(triggerAfter.getAttribute("aria-expanded")).toBe("true");
	});

	it("does not leak open state onto a different tool call", () => {
		renderCard(baseProps({ toolCallId: "tc-2", toolName: "other_tool" }));

		const trigger = getTrigger(/other_tool/i);
		expect(trigger.getAttribute("aria-expanded")).toBe("false");
	});

	it("closing collapses and forgets state for a fresh remount", () => {
		const props = baseProps({ toolCallId: "tc-3", toolName: "push_branch" });
		const { unmount } = renderCard(props);

		const trigger = getTrigger(/push_branch/i);
		fireEvent.click(trigger);
		expect(trigger.getAttribute("aria-expanded")).toBe("true");
		fireEvent.click(trigger);
		expect(trigger.getAttribute("aria-expanded")).toBe("false");

		unmount();
		renderCard(props);
		expect(getTrigger(/push_branch/i).getAttribute("aria-expanded")).toBe(
			"false",
		);
	});
});
