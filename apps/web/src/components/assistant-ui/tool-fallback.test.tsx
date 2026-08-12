import type { ToolCallMessagePartProps } from "@assistant-ui/react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ToolFallback } from "./tool-fallback";

function baseProps(
	overrides: Partial<ToolCallMessagePartProps> = {},
): ToolCallMessagePartProps {
	return {
		type: "tool-call",
		toolCallId: "tc-1",
		toolName: "list_repositories",
		args: {},
		argsText: "{}",
		result: "done",
		status: { type: "complete" },
		addResult: () => {},
		resume: () => {},
		respondToApproval: () => {},
		...overrides,
	} as ToolCallMessagePartProps;
}

function getTrigger(name: string | RegExp) {
	return screen
		.getByText(name)
		.closest('[data-slot="tool-fallback-trigger"]') as HTMLElement;
}

describe("ToolFallback expand state", () => {
	it("keeps a tool call expanded across a remount of the same toolCallId", () => {
		const props = baseProps();
		const { unmount } = render(<ToolFallback {...props} />);

		const trigger = getTrigger(/list_repositories/i);
		expect(trigger.getAttribute("aria-expanded")).toBe("false");
		fireEvent.click(trigger);
		expect(trigger.getAttribute("aria-expanded")).toBe("true");

		// Simulates the surrounding message tree remounting this leaf (e.g. a
		// group-boundary reshape as new events stream in) — the component
		// instance is fully destroyed and a fresh one created for the same
		// tool call.
		unmount();
		render(<ToolFallback {...props} />);

		const triggerAfter = getTrigger(/list_repositories/i);
		expect(triggerAfter.getAttribute("aria-expanded")).toBe("true");
	});

	it("does not leak open state onto a different tool call", () => {
		render(
			<ToolFallback
				{...baseProps({ toolCallId: "tc-2", toolName: "other_tool" })}
			/>,
		);

		const trigger = getTrigger(/other_tool/i);
		expect(trigger.getAttribute("aria-expanded")).toBe("false");
	});

	it("closing collapses and forgets state for a fresh remount", () => {
		const props = baseProps({ toolCallId: "tc-3", toolName: "push_branch" });
		const { unmount } = render(<ToolFallback {...props} />);

		const trigger = getTrigger(/push_branch/i);
		fireEvent.click(trigger);
		expect(trigger.getAttribute("aria-expanded")).toBe("true");
		fireEvent.click(trigger);
		expect(trigger.getAttribute("aria-expanded")).toBe("false");

		unmount();
		render(<ToolFallback {...props} />);
		expect(getTrigger(/push_branch/i).getAttribute("aria-expanded")).toBe(
			"false",
		);
	});
});
