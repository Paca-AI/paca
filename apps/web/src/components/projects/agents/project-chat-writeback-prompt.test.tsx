import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ProjectChatTurnHistoryItem } from "@/lib/agent-api";
import { type Task, taskQueryOptions } from "@/lib/interaction-api";
import { ProjectChatWritebackPrompt } from "./project-chat-writeback-prompt";

const apiMocks = vi.hoisted(() => ({
	listTaskConclusions: vi.fn(),
	prepareProjectConclusion: vi.fn(),
	confirmProjectConclusion: vi.fn(),
}));

vi.mock("@/lib/agent-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/agent-api")>("@/lib/agent-api");
	return { ...actual, ...apiMocks };
});

const task = {
	id: "task-1",
	project_id: "project-1",
	title: "Fix login",
	task_number: 1,
	description: [
		{
			type: "paragraph",
			content: [{ type: "text", text: "Existing scope", styles: {} }],
		},
	],
	importance: 0,
	custom_fields: {},
	created_at: "2026-08-19T00:00:00Z",
	updated_at: "2026-08-19T00:00:00Z",
} satisfies Task;

const sourceItem = {
	turn: {
		id: "turn-command",
		turn_index: 2,
		input_text: "/update-description",
		status: "succeeded",
	},
	result: {
		terminal_status: "succeeded",
		stable_output:
			"# Login reliability\n\nKeep the existing scope.\n\n## Acceptance criteria\n- Login succeeds",
	},
} as ProjectChatTurnHistoryItem;

function wrapper() {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	client.setQueryData(
		taskQueryOptions(task.project_id, task.id).queryKey,
		task,
	);
	return function Wrapper({ children }: { children: ReactNode }) {
		return (
			<QueryClientProvider client={client}>{children}</QueryClientProvider>
		);
	};
}

describe("ProjectChatWritebackPrompt", () => {
	beforeEach(() => {
		window.sessionStorage.clear();
		apiMocks.listTaskConclusions.mockReset();
		apiMocks.prepareProjectConclusion.mockReset();
		apiMocks.confirmProjectConclusion.mockReset();
		apiMocks.listTaskConclusions.mockResolvedValue({
			items: [],
			next_cursor: null,
		});
		apiMocks.prepareProjectConclusion.mockResolvedValue({
			preparation: {
				id: "preparation-1",
				summary_version: 1,
				summary_sha256: "a".repeat(64),
			},
			replayed: false,
		});
		apiMocks.confirmProjectConclusion.mockResolvedValue({
			publication: { target_task_id: task.id },
			replayed: false,
		});
	});

	it("auto-selects the only related task and confirms the visible AI output", async () => {
		render(
			<ProjectChatWritebackPrompt
				projectId={task.project_id}
				sessionId="session-1"
				sourceItem={sourceItem}
				relatedTaskIds={[task.id]}
				onContinue={vi.fn()}
			/>,
			{ wrapper: wrapper() },
		);

		const updateOption = await screen.findByRole("button", {
			name: /^update description$/i,
		});
		expect(updateOption).toHaveAttribute("aria-pressed", "true");
		const confirmButton = screen.getByRole("button", { name: /^confirm$/i });
		await waitFor(() => expect(confirmButton).toBeEnabled());
		fireEvent.click(confirmButton);

		await waitFor(() => {
			expect(apiMocks.confirmProjectConclusion).toHaveBeenCalled();
		});
		expect(apiMocks.prepareProjectConclusion).toHaveBeenCalledWith(
			task.project_id,
			sourceItem.turn.id,
			expect.objectContaining({
				target_task_id: task.id,
				summary_override: sourceItem.result?.stable_output,
				update_description: true,
				description_base: task.description,
				proposed_description: expect.arrayContaining([
					expect.objectContaining({ type: "heading" }),
					expect.objectContaining({ type: "bulletListItem" }),
				]),
			}),
			expect.any(String),
		);
	});

	it("turns typed feedback into a new visible command turn", async () => {
		const onContinue = vi.fn().mockResolvedValue(undefined);
		render(
			<ProjectChatWritebackPrompt
				projectId={task.project_id}
				sessionId="session-2"
				sourceItem={sourceItem}
				relatedTaskIds={[task.id]}
				onContinue={onContinue}
			/>,
			{ wrapper: wrapper() },
		);

		const writebackOption = (
			await screen.findAllByRole("button", {
				name: /^update description$/i,
			})
		)[0];
		const revisionInput = await screen.findByRole("textbox", {
			name: /describe what to adjust/i,
		});
		expect(screen.queryByRole("button", { name: /^revise$/i })).toBeNull();
		fireEvent.focus(revisionInput);
		expect(writebackOption).toHaveAttribute("aria-pressed", "false");
		expect(screen.getByRole("button", { name: /^confirm$/i })).toBeDisabled();
		fireEvent.change(revisionInput, {
			target: { value: "Make the acceptance criteria more specific" },
		});
		expect(writebackOption).toHaveAttribute("aria-pressed", "false");
		const confirmButton = screen.getByRole("button", { name: /^confirm$/i });
		await waitFor(() => expect(confirmButton).toBeEnabled());
		fireEvent.click(confirmButton);

		await waitFor(() => {
			expect(onContinue).toHaveBeenCalledWith(
				"/update-description Make the acceptance criteria more specific",
			);
		});
		expect(apiMocks.prepareProjectConclusion).not.toHaveBeenCalled();
	});

	it("does not offer a second writeback when the source turn is published", async () => {
		apiMocks.listTaskConclusions.mockResolvedValue({
			items: [
				{
					id: "publication-1",
					target_task_id: task.id,
					source_accessible: true,
					source_turn_id: sourceItem.turn.id,
					description_updated: true,
				},
			],
			next_cursor: null,
		});
		const view = render(
			<ProjectChatWritebackPrompt
				projectId={task.project_id}
				sessionId="session-3"
				sourceItem={sourceItem}
				relatedTaskIds={[task.id]}
				onContinue={vi.fn()}
			/>,
			{ wrapper: wrapper() },
		);

		await waitFor(() =>
			expect(apiMocks.listTaskConclusions).toHaveBeenCalled(),
		);
		await waitFor(() => expect(view.container).toBeEmptyDOMElement());
	});
});
