import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { type Agent, agentsQueryOptions } from "@/lib/agent-api";
import { TaskChatAgentDialog } from "./task-chat-agent-dialog";

const navigate = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", async (importOriginal) => ({
	...(await importOriginal<typeof import("@tanstack/react-router")>()),
	useNavigate: () => navigate,
}));

const agents = [
	{
		id: "agent-llm",
		project_id: "project-1",
		name: "DeepSeek",
		handle: "deepseek",
		agent_type: "llm",
	},
	{
		id: "agent-acp",
		project_id: "project-1",
		name: "Codex ACP",
		handle: "codex",
		agent_type: "acp",
	},
] as Agent[];

function makeWrapper() {
	const client = new QueryClient({
		defaultOptions: {
			queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
		},
	});
	client.setQueryData(agentsQueryOptions("project-1").queryKey, agents);
	return function Wrapper({ children }: { children: ReactNode }) {
		return (
			<QueryClientProvider client={client}>{children}</QueryClientProvider>
		);
	};
}

describe("TaskChatAgentDialog", () => {
	afterEach(() => {
		vi.restoreAllMocks();
		navigate.mockReset();
	});

	it("requires an available LLM selection and carries it into a fresh task draft", async () => {
		vi.spyOn(globalThis.crypto, "randomUUID").mockReturnValue(
			"00000000-0000-4000-8000-000000000004",
		);
		const onOpenChange = vi.fn();
		const onParentClick = vi.fn();
		render(
			<button type="button" aria-label="parent task" onClick={onParentClick}>
				<TaskChatAgentDialog
					projectId="project-1"
					taskId="task-1"
					taskTitle="Fix login"
					open
					onOpenChange={onOpenChange}
				/>
			</button>,
			{ wrapper: makeWrapper() },
		);

		const continueButton = screen.getByRole("button", {
			name: /continue to chat/i,
		});
		expect(screen.getByRole("dialog")).toHaveClass("max-h-[calc(100dvh-2rem)]");
		expect(continueButton).toBeDisabled();
		expect(screen.getByRole("button", { name: /codex acp/i })).toBeDisabled();

		const deepSeekOption = screen.getByRole("button", { name: "DeepSeek" });
		expect(deepSeekOption).toHaveAttribute("aria-pressed", "false");
		fireEvent.click(deepSeekOption);
		expect(onParentClick).not.toHaveBeenCalled();
		expect(deepSeekOption).toHaveAttribute("aria-pressed", "true");
		expect(continueButton).toBeEnabled();
		fireEvent.click(continueButton);
		expect(onParentClick).not.toHaveBeenCalled();

		expect(onOpenChange).toHaveBeenCalledWith(false);
		expect(navigate).toHaveBeenCalledWith({
			to: "/projects/$projectId/chats",
			params: { projectId: "project-1" },
			search: {
				contextTaskId: "task-1",
				draft: "00000000-0000-4000-8000-000000000004",
				agentId: "agent-llm",
			},
		});
	});
});
