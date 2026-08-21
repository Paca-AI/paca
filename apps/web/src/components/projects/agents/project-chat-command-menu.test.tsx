import { fireEvent, render, screen } from "@testing-library/react";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ProjectChatCommandMenu } from "./project-chat-command-menu";

const assistant = vi.hoisted(() => ({
	text: "/",
	setText: vi.fn(),
}));

vi.mock("@assistant-ui/react", () => ({
	useAui: () => ({
		composer: () => ({ setText: assistant.setText }),
	}),
	useAuiState: (selector: (state: { composer: { text: string } }) => unknown) =>
		selector({ composer: { text: assistant.text } }),
}));

vi.mock("@/components/assistant-ui/tooltip-icon-button", () => ({
	TooltipIconButton: ({
		children,
		tooltip: _tooltip,
		...props
	}: ButtonHTMLAttributes<HTMLButtonElement> & {
		children?: ReactNode;
		tooltip?: ReactNode;
	}) => <button {...props}>{children}</button>,
}));

function renderMenu(props?: { disabled?: boolean; hasTaskContext?: boolean }) {
	return render(
		<>
			<input aria-label="Composer" className="aui-composer-input" />
			<ProjectChatCommandMenu
				disabled={props?.disabled}
				hasTaskContext={props?.hasTaskContext ?? true}
			/>
		</>,
	);
}

describe("ProjectChatCommandMenu", () => {
	beforeEach(() => {
		assistant.text = "/";
		assistant.setText.mockReset();
	});

	it("opens from slash input and supports arrow plus Enter selection", () => {
		renderMenu();
		const input = screen.getByRole("textbox", { name: "Composer" });
		input.focus();

		expect(screen.getByRole("listbox", { name: "Commands" })).toBeVisible();
		expect(input).toHaveAttribute("aria-expanded", "true");
		fireEvent.keyDown(input, { key: "ArrowDown" });
		expect(
			screen.getByRole("option", { name: "Record conclusion" }),
		).toHaveAttribute("aria-selected", "true");
		fireEvent.keyDown(input, { key: "Enter" });

		expect(assistant.setText).toHaveBeenCalledWith("/record-conclusion ");
	});

	it("filters commands and Escape dismisses the typed menu", () => {
		assistant.text = "/record";
		renderMenu();
		const input = screen.getByRole("textbox", { name: "Composer" });
		input.focus();

		expect(
			screen.queryByRole("option", { name: "Update description" }),
		).not.toBeInTheDocument();
		expect(
			screen.getByRole("option", { name: "Record conclusion" }),
		).toBeVisible();
		fireEvent.keyDown(input, { key: "Escape" });
		expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
	});

	it("does not select during IME composition or modified Enter", () => {
		renderMenu();
		const input = screen.getByRole("textbox", { name: "Composer" });
		input.focus();

		fireEvent.keyDown(input, { key: "Enter", isComposing: true });
		fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
		expect(assistant.setText).not.toHaveBeenCalled();
	});

	it("keeps commands unavailable without task context or while disabled", () => {
		const { rerender } = renderMenu({ hasTaskContext: false });
		expect(
			screen.getByRole("option", { name: "Update description" }),
		).toBeDisabled();

		rerender(
			<>
				<input aria-label="Composer" className="aui-composer-input" />
				<ProjectChatCommandMenu disabled hasTaskContext />
			</>,
		);
		expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Commands" })).toBeDisabled();
	});
});
