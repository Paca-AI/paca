import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { ProjectChatNew } from "./project-chat-new";

const componentProps = vi.hoisted(() => ({
	contextPicker: vi.fn(),
	commandMenu: vi.fn(),
}));

vi.mock("@assistant-ui/react", async (importOriginal) => ({
	...(await importOriginal<typeof import("@assistant-ui/react")>()),
	AssistantRuntimeProvider: ({ children }: { children: ReactNode }) => children,
	useExternalStoreRuntime: () => ({}),
}));

vi.mock("@tanstack/react-router", () => ({
	Link: ({ children }: { children?: ReactNode }) => (
		<a href="/projects/project-1/chats">{children}</a>
	),
	useNavigate: () => vi.fn(),
}));

vi.mock("@/components/assistant-ui/thread", () => ({
	Thread: ({
		components,
		viewportHeader,
	}: {
		components: { ComposerStart: () => ReactNode };
		viewportHeader?: ReactNode;
	}) => (
		<>
			{viewportHeader}
			{components.ComposerStart()}
		</>
	),
}));

vi.mock("@/hooks/use-can-use-project-chats", () => ({
	useProjectChatPermissions: () => ({
		canUseTaskContext: true,
		canPublishConclusion: true,
	}),
}));

vi.mock("./agent-picker", async () => {
	const { createContext } = await import("react");
	return {
		AgentPickerContext: createContext(null),
		AgentPickerInline: () => <span data-testid="agent-picker" />,
		usePrivateChatAgentPicker: () => ({
			agentId: "agent-1",
			pickerState: null,
		}),
	};
});

vi.mock("./project-chat-context-picker", () => ({
	ProjectChatContextPicker: (props: Record<string, unknown>) => {
		componentProps.contextPicker(props);
		return <span data-testid="context-picker" />;
	},
}));

vi.mock("./project-chat-command-menu", () => ({
	ProjectChatCommandMenu: (props: Record<string, unknown>) => {
		componentProps.commandMenu(props);
		return <span data-testid="command-menu" />;
	},
}));

function wrapper() {
	const client = new QueryClient();
	return function Wrapper({ children }: { children: ReactNode }) {
		return (
			<QueryClientProvider client={client}>{children}</QueryClientProvider>
		);
	};
}

describe("ProjectChatNew", () => {
	it("keeps first-turn resources and commands in the composer", () => {
		componentProps.contextPicker.mockClear();
		componentProps.commandMenu.mockClear();

		render(
			<ProjectChatNew
				projectId="project-1"
				initialTaskId="task-1"
				initialAgentId="agent-1"
			/>,
			{ wrapper: wrapper() },
		);

		expect(screen.getByTestId("agent-picker")).toBeInTheDocument();
		expect(screen.getByTestId("context-picker")).toBeInTheDocument();
		expect(screen.getByTestId("command-menu")).toBeInTheDocument();
		expect(
			screen.getByRole("link", { name: "Back to chats" }),
		).toBeInTheDocument();
		expect(componentProps.contextPicker).toHaveBeenCalledWith(
			expect.objectContaining({
				iconOnly: true,
				requiredSources: [{ type: "task", id: "task-1" }],
				value: [{ type: "task", id: "task-1" }],
			}),
		);
		expect(componentProps.commandMenu).toHaveBeenCalledWith(
			expect.objectContaining({ hasTaskContext: true }),
		);
	});
});
