import { createFileRoute } from "@tanstack/react-router";
import {
	INBOX_TABS,
	InboxPage,
	type InboxTab,
} from "@/components/team-chat/inbox-page";

export const Route = createFileRoute("/_authenticated/chat/")({
	validateSearch: (
		search: Record<string, unknown>,
	): { project?: string; tab?: InboxTab } => ({
		project: typeof search.project === "string" ? search.project : undefined,
		tab: INBOX_TABS.includes(search.tab as InboxTab)
			? (search.tab as InboxTab)
			: undefined,
	}),
	component: Inbox,
});

function Inbox() {
	const { project, tab } = Route.useSearch();
	return <InboxPage projectId={project} tab={tab ?? "all"} />;
}
