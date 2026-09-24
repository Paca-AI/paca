import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import type { Environment, EnvironmentFolder } from "@/lib/environment-api";
import {
	AgentPickerContext,
	AgentPickerInline,
	type AgentPickerState,
	AUTO_AGENT_ID,
	EnvironmentPickerContext,
	EnvironmentPickerInline,
	type EnvironmentPickerState,
	FolderPickerInline,
} from "./agent-picker";

// EnvironmentPickerInline/FolderPickerInline each render a *CreateDialog
// sibling (EnvironmentCreateDialog/FolderCreateDialog) that calls
// useQueryClient for its own invalidation on success — needed here purely
// to satisfy that hook, since neither dialog is ever opened in these tests.
function renderWithQueryClient(ui: ReactNode) {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return render(
		<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
	);
}

// Regression coverage: an ACP-type agent runs entirely through the user's
// own local ACP client (apps/acp-bridge) and never attaches to one of the
// project's static environments — see useEnvironmentPicker's own `hidden`
// doc comment and agent-detail.tsx's identical `!isAcp` gate on its own
// Environment section. The inline pickers docked in the "new conversation"
// composer (new-conversation-thread.tsx's ComposerStartRow) must hide
// entirely for that agent type, not merely disable — this exercises the
// `hidden` flag directly against a hand-built EnvironmentPickerState rather
// than through useEnvironmentPicker's own queries, since both components
// only ever read that state via context.

const folder: EnvironmentFolder = {
	id: "folder-1",
	path: "/repo",
	created_at: "2024-01-01T00:00:00.000Z",
};

const environment: Environment = {
	id: "env-1",
	project_id: "project-1",
	name: "Env One",
	slug: "env-one",
	ssh_port: null,
	status: "running",
	backend: "docker",
	image: null,
	cpu_limit: "1",
	memory_limit: "1Gi",
	disk_limit_gb: 10,
	docker_enabled: true,
	idle_timeout_minutes: 30,
	last_active_at: "2024-01-01T00:00:00.000Z",
	ports_pending_restart: false,
	access_mode: "open",
	access_granted: true,
	created_at: "2024-01-01T00:00:00.000Z",
	updated_at: "2024-01-01T00:00:00.000Z",
	folders: [folder],
};

const baseState: EnvironmentPickerState = {
	projectId: "project-1",
	environments: [environment],
	environmentsLoading: false,
	environmentId: "",
	onEnvironmentChange: () => {},
	folders: [folder],
	folderId: "",
	onFolderChange: () => {},
};

describe("EnvironmentPickerInline", () => {
	it("hides entirely when hidden (ACP-type agent)", () => {
		renderWithQueryClient(
			<EnvironmentPickerContext.Provider value={{ ...baseState, hidden: true }}>
				<EnvironmentPickerInline />
			</EnvironmentPickerContext.Provider>,
		);
		expect(screen.queryByText("Temporary environment")).not.toBeInTheDocument();
	});

	it("renders when not hidden", () => {
		renderWithQueryClient(
			<EnvironmentPickerContext.Provider
				value={{ ...baseState, hidden: false }}
			>
				<EnvironmentPickerInline />
			</EnvironmentPickerContext.Provider>,
		);
		expect(screen.getByText("Temporary environment")).toBeInTheDocument();
	});
});

describe("FolderPickerInline", () => {
	const withEnvironmentSelected: EnvironmentPickerState = {
		...baseState,
		environmentId: environment.id,
	};

	it("hides entirely when hidden (ACP-type agent), even with an environment selected", () => {
		renderWithQueryClient(
			<EnvironmentPickerContext.Provider
				value={{ ...withEnvironmentSelected, hidden: true }}
			>
				<FolderPickerInline />
			</EnvironmentPickerContext.Provider>,
		);
		expect(screen.queryByText("Select a folder…")).not.toBeInTheDocument();
	});

	it("renders when not hidden and an environment is selected", () => {
		renderWithQueryClient(
			<EnvironmentPickerContext.Provider
				value={{ ...withEnvironmentSelected, hidden: false }}
			>
				<FolderPickerInline />
			</EnvironmentPickerContext.Provider>,
		);
		expect(screen.getByText("Select a folder…")).toBeInTheDocument();
	});
});

describe("AgentPickerInline", () => {
	// Regression coverage for Auto mode (see useAgentPicker's own doc
	// comment): when the picker's agent list includes the synthetic
	// AUTO_AGENT_ID entry (prepended by useAgentPicker/useGlobalAgentPicker
	// only when Jev is configured), it must render as a real, selectable
	// option alongside the actual agents — tested here directly against a
	// hand-built AgentPickerState, same as the EnvironmentPickerInline
	// tests above, rather than through the hook's own query/useJevEnabled
	// wiring.
	const agentPickerState: AgentPickerState = {
		agents: [
			{ id: AUTO_AGENT_ID, name: "Auto" },
			{ id: "agent-1", name: "Real Agent" },
		],
		agentsLoading: false,
		agentId: AUTO_AGENT_ID,
		onAgentChange: () => {},
		emptyStateLink: null,
	};

	it("renders the Auto option alongside real agents", () => {
		renderWithQueryClient(
			<AgentPickerContext.Provider value={agentPickerState}>
				<AgentPickerInline />
			</AgentPickerContext.Provider>,
		);
		expect(screen.getByText("Auto")).toBeInTheDocument();
	});
});
