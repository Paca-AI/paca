import { createFileRoute } from "@tanstack/react-router";
import { ProjectChatNew } from "@/components/projects/agents/project-chat-new";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/chats/",
)({
	validateSearch: (search: Record<string, unknown>) => ({
		contextTaskId:
			typeof search.contextTaskId === "string"
				? search.contextTaskId
				: undefined,
		draft: typeof search.draft === "string" ? search.draft : undefined,
		agentId: typeof search.agentId === "string" ? search.agentId : undefined,
	}),
	component: ProjectChatNewPage,
});

function ProjectChatNewPage() {
	const { projectId } = Route.useParams();
	const { contextTaskId, draft, agentId } = Route.useSearch();
	return (
		<ProjectChatNew
			key={draft ?? "new-project-chat"}
			projectId={projectId}
			initialTaskId={contextTaskId}
			initialAgentId={agentId}
		/>
	);
}
