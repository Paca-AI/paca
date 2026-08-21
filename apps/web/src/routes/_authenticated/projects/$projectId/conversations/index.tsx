import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/conversations/",
)({
	beforeLoad: ({ params: { projectId } }) => {
		throw redirect({
			to: "/projects/$projectId/chats",
			params: { projectId },
			search: {
				contextTaskId: undefined,
				draft: undefined,
				agentId: undefined,
			},
		});
	},
});
