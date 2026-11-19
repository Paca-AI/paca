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
import { EntityAvatar } from "@/components/shared/entity-avatar";
import { useDebouncedAsyncCallback } from "@/hooks/use-debounced-callback";
import { useStableCallback } from "@/hooks/use-stable-callback";
import {
	searchMentionableDocuments,
	searchMentionableMembers,
	searchMentionableTasks,
} from "@/lib/mention-api";
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
	/** Needed to search team members (after "@") and tasks (after "#") live as
	 *  the user types — neither list has a fixed upper bound, so they're
	 *  queried on demand instead of being preloaded in full (see
	 *  mention-api.ts). */
	projectId?: string | null;
}

/**
 * Keeps the items of each group together, in the order the groups first
 * appear. The menu renders one label per run of items in a group and keys it
 * by the group name, so an item added at the end with a group that already
 * appeared earlier (the Mermaid item, in "Advanced") produces two labels with
 * the same key. React then reuses and drops DOM nodes unpredictably, and
 * clicking an item with the mouse stops working.
 */
export function groupContiguously<T extends { group?: string }>(
	items: readonly T[],
): T[] {
	const order: (string | undefined)[] = [];
	for (const item of items) {
		if (!order.includes(item.group)) order.push(item.group);
	}
	return order.flatMap((group) => items.filter((item) => item.group === group));
}

export function MentionSuggestionMenus({
	editor,
	projectId,
}: MentionSuggestionMenuProps) {
	const debouncedSearchMentionableTasks = useDebouncedAsyncCallback(
		searchMentionableTasks,
		TASK_SEARCH_DEBOUNCE_MS,
	);

	const debouncedSearchMentionableMembers = useDebouncedAsyncCallback(
		searchMentionableMembers,
		TASK_SEARCH_DEBOUNCE_MS,
	);

	const debouncedSearchMentionableDocuments = useDebouncedAsyncCallback(
		searchMentionableDocuments,
		TASK_SEARCH_DEBOUNCE_MS,
	);

	const getTeamMentionItems = async (
		query: string,
	): Promise<DefaultReactSuggestionItem[]> => {
		if (!projectId) return [];
		const teamMembers = await debouncedSearchMentionableMembers(
			projectId,
			query,
		);
		return teamMembers.map((member) => ({
			title: member.name,
			subtext: `@${member.username}`,
			// Resolved fresh from the live teamMembers list every time the
			// dropdown opens — safe, unlike the mention chip's own props (see
			// the avatar: "" comment below), since this is never persisted.
			icon: <EntityAvatar {...member.avatar} size="xs" />,
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

	const getDocReferenceItems = async (
		query: string,
	): Promise<DefaultReactSuggestionItem[]> => {
		if (!projectId) return [];
		const effectiveQuery =
			query.trim().length >= MIN_TASK_SEARCH_LENGTH ? query : "";
		const documents = await debouncedSearchMentionableDocuments(
			projectId,
			effectiveQuery,
		);
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

	const getSlashMenuItems = (): DefaultReactSuggestionItem[] =>
		groupContiguously([
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
		]);

	// BlockNote reloads a menu's items whenever the identity of its getItems
	// changes, and closes the menu when a reload comes back with nothing. These
	// must therefore not be recreated on every render: in a collaborative editor
	// the parent re-renders as peers' cursors move, which made the menu opened
	// by the "+" button close itself a moment after it appeared.
	const loadSlashItems = useStableCallback(async (query: string) =>
		filterSuggestionItems(getSlashMenuItems(), query),
	);
	const loadMentionItems = useStableCallback(async (query: string) =>
		// Already filtered server-side by `query`.
		getTeamMentionItems(query),
	);
	const loadReferenceItems = useStableCallback(async (query: string) => [
		// Tasks and documents are already filtered server-side by
		// `query`; re-running filterSuggestionItems here would
		// incorrectly drop matches on the "#<task_number>" subtext
		// for non-title search terms.
		...(await getTaskReferenceItems(query)),
		...(await getDocReferenceItems(query)),
	]);

	return (
		<>
			<SuggestionMenuController
				triggerCharacter="/"
				getItems={loadSlashItems}
			/>
			<SuggestionMenuController
				triggerCharacter="@"
				getItems={loadMentionItems}
			/>
			<SuggestionMenuController
				triggerCharacter="#"
				getItems={loadReferenceItems}
			/>
		</>
	);
}
