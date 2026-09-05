import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { CreateAgentDialog } from "./create-agent-dialog";

// ---------------------------------------------------------------------------
// Agent type selector — global vs project scope
// ---------------------------------------------------------------------------
//
// Regression coverage: the "Provider CLI" agent type used to be omitted
// entirely from the grid when creating a global agent (no projectId), since
// it requires a project's own static environment (see
// agentdom.ErrCLIProviderNotSupportedForGlobalAgents server-side). It's now
// always shown, but disabled with a tooltip at global scope, so the option
// is discoverable rather than silently missing.

vi.mock("@/lib/admin-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/admin-api")>("@/lib/admin-api");
	return {
		...actual,
		globalRolesQueryOptions: {
			queryKey: ["admin", "global-roles"],
			queryFn: async () => [],
		},
	};
});

vi.mock("@/lib/agent-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/agent-api")>("@/lib/agent-api");
	return {
		...actual,
		llmModelsQueryOptions: {
			queryKey: ["agents", "llm-models"],
			queryFn: async () => ({}),
		},
	};
});

function makeQueryClient() {
	return new QueryClient({
		defaultOptions: {
			mutations: { retry: false },
			queries: { retry: false, gcTime: 0 },
		},
	});
}

function renderDialog(projectId?: string) {
	function Wrapper({ children }: { children: ReactNode }) {
		return (
			<QueryClientProvider client={makeQueryClient()}>
				{children}
			</QueryClientProvider>
		);
	}

	render(
		<Wrapper>
			<CreateAgentDialog
				projectId={projectId}
				open
				onOpenChange={() => {}}
				onAcpAgentCreated={() => {}}
			/>
		</Wrapper>,
	);
}

describe("CreateAgentDialog — agent type selector", () => {
	it("shows Provider CLI enabled and selectable when creating a project agent", async () => {
		const user = userEvent.setup();
		renderDialog("proj-1");

		const providerCliCard = screen.getByText("Provider CLI").closest("button");
		expect(providerCliCard).not.toBeNull();
		expect(providerCliCard).not.toHaveAttribute("aria-disabled", "true");

		await user.click(providerCliCard as HTMLElement);
		// Selecting it reveals the CLI provider sub-select from step 2's
		// provider_cli branch's step-1-only preset-grid absence; the clearest
		// step-1 signal that selection took effect is the preset grid (LLM-only)
		// disappearing.
		expect(screen.queryByText("Start from a preset")).not.toBeInTheDocument();
	});

	it("shows Provider CLI disabled when creating a global agent, and clicking it doesn't select it", async () => {
		const user = userEvent.setup();
		renderDialog(undefined);

		const providerCliCard = screen.getByText("Provider CLI").closest("button");
		expect(providerCliCard).not.toBeNull();
		expect(providerCliCard).toHaveAttribute("aria-disabled", "true");

		// Clicking it must not select it — the LLM preset grid (only shown for
		// the still-selected "llm" type) should remain visible.
		await user.click(providerCliCard as HTMLElement);
		expect(screen.getByText("Start from a preset")).toBeInTheDocument();
	});

	it("still shows Provider CLI enabled and selectable for a project agent alongside the other two types", () => {
		renderDialog("proj-1");
		expect(screen.getByText("LLM (API key)")).toBeInTheDocument();
		expect(screen.getByText("Provider CLI")).toBeInTheDocument();
		expect(screen.getByText("ACP (local CLI)")).toBeInTheDocument();
	});
});
