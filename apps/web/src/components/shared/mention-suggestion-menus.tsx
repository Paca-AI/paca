import type { BlockNoteEditor } from "@blocknote/core";
import { filterSuggestionItems } from "@blocknote/core/extensions";
import {
	type DefaultReactSuggestionItem,
	SuggestionMenuController,
} from "@blocknote/react";
import { searchMentionableTasks } from "@/lib/mention-api";
import type { customSchema } from "./blocknote-schema";

interface MentionSuggestionMenuProps {
	editor: BlockNoteEditor<
		typeof customSchema.blockSchema,
		typeof customSchema.inlineContentSchema,
		typeof customSchema.styleSchema
	>;
	teamMembers: Array<{
		id: string;
		name: string;
		username: string;
		avatar?: string | null | undefined;
	}>;
	/** Needed to search tasks live as the user types after "#" — the project's
	 *  task list has no fixed upper bound, so it's queried on demand instead
	 *  of being preloaded in full (see mention-api.ts). */
	projectId?: string | null;
	documents: Array<{ id: string; title: string }>;
}

export function MentionSuggestionMenus({
	editor,
	teamMembers,
	projectId,
	documents,
}: MentionSuggestionMenuProps) {
	const getTeamMentionItems = (): DefaultReactSuggestionItem[] => {
		return teamMembers.map((member) => ({
			title: member.name,
			subtext: `@${member.username}`,
			onItemClick: () => {
				editor.insertInlineContent([
					{
						type: "teamMention",
						props: {
							id: member.id,
							name: member.name,
							avatar: member.avatar ?? "",
						},
					},
					" ",
				]);
			},
		}));
	};

	const getTaskReferenceItems = async (
		query: string,
	): Promise<DefaultReactSuggestionItem[]> => {
		if (!projectId) return [];
		const tasks = await searchMentionableTasks(projectId, query);
		return tasks.map((task) => ({
			title: task.title,
			subtext: `#${task.task_number}`,
			onItemClick: () => {
				editor.insertInlineContent([
					{
						type: "taskReference",
						props: {
							id: task.id,
							title: task.title,
							status: "open",
						},
					},
					" ",
				]);
			},
		}));
	};

	const getDocReferenceItems = (): DefaultReactSuggestionItem[] => {
		return documents.map((doc) => ({
			title: doc.title,
			subtext: `#${doc.id.slice(0, 8)}`,
			onItemClick: () => {
				editor.insertInlineContent([
					{
						type: "docReference",
						props: {
							id: doc.id,
							title: doc.title,
						},
					},
					" ",
				]);
			},
		}));
	};

	return (
		<>
			<SuggestionMenuController
				triggerCharacter="@"
				getItems={async (query) =>
					filterSuggestionItems(getTeamMentionItems(), query)
				}
			/>
			<SuggestionMenuController
				triggerCharacter="#"
				getItems={async (query) => [
					// Tasks are already filtered server-side by `query`; re-running
					// filterSuggestionItems here would incorrectly drop matches on
					// the "#<task_number>" subtext for non-title search terms.
					...(await getTaskReferenceItems(query)),
					...filterSuggestionItems(getDocReferenceItems(), query),
				]}
			/>
		</>
	);
}
