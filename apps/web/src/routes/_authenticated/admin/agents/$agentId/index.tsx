import { createFileRoute, redirect } from "@tanstack/react-router";
import { AgentDetailView } from "@/components/projects/agents/agent-detail";
import { myPermissionsQueryOptions } from "@/lib/admin-api";
import {
	globalAgentEnvVarsQueryOptions,
	globalAgentMCPServersQueryOptions,
	globalAgentQueryOptions,
	globalAgentSkillsQueryOptions,
	llmModelsQueryOptions,
} from "@/lib/agent-api";
import { hasPermission } from "@/lib/permissions";

// Global sibling of routes/.../projects/$projectId/agents/$agentId — same
// detail page (see components/projects/agents/agent-detail.tsx), just with
// no projectId.
export const Route = createFileRoute("/_authenticated/admin/agents/$agentId/")({
	beforeLoad: async ({ context: { queryClient } }) => {
		const permissions = await queryClient
			.fetchQuery(myPermissionsQueryOptions)
			.catch(() => [] as string[]);

		const canAccess =
			hasPermission(permissions, "agents.read") ||
			hasPermission(permissions, "agents.write");

		if (!canAccess) {
			throw redirect({ to: "/home" });
		}
	},
	loader: async ({ context: { queryClient }, params: { agentId } }) => {
		await Promise.all([
			queryClient.ensureQueryData(globalAgentQueryOptions(agentId)),
			queryClient.ensureQueryData(globalAgentMCPServersQueryOptions(agentId)),
			queryClient.ensureQueryData(globalAgentSkillsQueryOptions(agentId)),
			queryClient.ensureQueryData(globalAgentEnvVarsQueryOptions(agentId)),
			queryClient.ensureQueryData(llmModelsQueryOptions),
		]);
	},
	component: GlobalAgentDetailPage,
});

function GlobalAgentDetailPage() {
	const { agentId } = Route.useParams();
	return <AgentDetailView agentId={agentId} />;
}
