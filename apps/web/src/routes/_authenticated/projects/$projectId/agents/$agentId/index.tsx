import { createFileRoute } from "@tanstack/react-router";
import { AgentDetailView } from "@/components/projects/agents/agent-detail";
import {
	agentEnvVarsQueryOptions,
	agentMCPServersQueryOptions,
	agentQueryOptions,
	agentSkillsQueryOptions,
	llmModelsQueryOptions,
} from "@/lib/agent-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/agents/$agentId/",
)({
	loader: async ({
		context: { queryClient },
		params: { projectId, agentId },
	}) => {
		await Promise.all([
			queryClient.ensureQueryData(agentQueryOptions(projectId, agentId)),
			queryClient.ensureQueryData(
				agentMCPServersQueryOptions(projectId, agentId),
			),
			queryClient.ensureQueryData(agentSkillsQueryOptions(projectId, agentId)),
			queryClient.ensureQueryData(agentEnvVarsQueryOptions(projectId, agentId)),
			queryClient.ensureQueryData(llmModelsQueryOptions),
		]);
	},
	component: ProjectAgentDetailPage,
});

function ProjectAgentDetailPage() {
	const { projectId, agentId } = Route.useParams();
	return <AgentDetailView projectId={projectId} agentId={agentId} />;
}
