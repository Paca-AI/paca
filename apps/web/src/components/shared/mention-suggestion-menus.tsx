import type { BlockNoteEditor } from "@blocknote/core";
import {
	filterSuggestionItems,
	insertOrUpdateBlockForSlashMenu,
} from "@blocknote/core/extensions";
import {
	type DefaultReactSuggestionItem,
	getDefaultReactSlashMenuItems,
	SuggestionMenuController,
} from "@blocknote/react";
import { Workflow } from "lucide-react";
import { EntityAvatarContent } from "@/components/shared/entity-avatar";
import { useDebouncedAsyncCallback } from "@/hooks/use-debounced-callback";
import { searchMentionableTasks } from "@/lib/mention-api";
import type { customSchema } from "./blocknote-schema";

/** BlockNote calls getItems on every keystroke after "#" — debounce the
 *  actual network call so a fast typist doesn't fire one request per
 *  character. BlockNote's own stale-response check (comparing the query
 *  active when a call resolves) still discards results for a query the
 *  user has since typed past. */
const TASK_SEARCH_DEBOUNCE_MS = 300;

// Below this length, `search` does a leading-wildcard ILIKE scan server-side
// (no trigram index backs it) — treat a too-short query the same as no query
// (unfiltered first page) rather than firing a broad single-character scan.
const MIN_TASK_SEARCH_LENGTH = 2;

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
		avatarThumbUrl?: string | null | undefined;
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
	const debouncedSearchMentionableTasks = useDebouncedAsyncCallback(
		searchMentionableTasks,
		TASK_SEARCH_DEBOUNCE_MS,
	);

	const getTeamMentionItems = (): DefaultReactSuggestionItem[] => {
		return teamMembers.map((member) => ({
			title: member.name,
			subtext: `@${member.username}`,
			// Resolved fresh from the live teamMembers list every time the
			// dropdown opens — safe, unlike the mention chip's own props (see
			// the avatar: "" comment below), since this is never persisted.
			icon: (
				<div className="flex size-5 items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/10 text-primary text-[10px] font-bold overflow-hidden">
					<EntityAvatarContent avatarUrl={member.avatarThumbUrl}>
						{member.name.slice(0, 1).toUpperCase()}
					</EntityAvatarContent>
				</div>
			),
			onItemClick: () => {
				editor.insertInlineContent([
					{
						type: "teamMention",
						props: {
							id: member.id,
							name: member.name,
							// Never populated with a real URL: mention props are baked
							// into permanently-stored document content, and a presigned
							// avatar URL would expire long before the mention does. The
							// live suggestion dropdown (below) shows the real avatar
							// instead, since it's resolved fresh every time it opens.
							avatar: "",
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
		const effectiveQuery =
			query.trim().length >= MIN_TASK_SEARCH_LENGTH ? query : "";
		const tasks = await debouncedSearchMentionableTasks(
			projectId,
			effectiveQuery,
		);
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

	const getSlashMenuItems = (): DefaultReactSuggestionItem[] => [
		...getDefaultReactSlashMenuItems(editor),
		{
			title: "Mermaid Diagram",
			subtext: "Insert a Mermaid diagram",
			aliases: ["mermaid", "diagram", "flowchart", "graph", "sequence"],
			group: "Advanced",
			icon: <Workflow size={18} />,
			onItemClick: () => {
				insertOrUpdateBlockForSlashMenu(editor, { type: "mermaid" });
			},
		},
	];

	return (
		<>
			<SuggestionMenuController
				triggerCharacter="/"
				getItems={async (query) =>
					filterSuggestionItems(getSlashMenuItems(), query)
				}
			/>
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
