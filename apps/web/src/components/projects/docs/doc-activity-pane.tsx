import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { ActivityPane } from "@/components/shared/activity-pane";
import { textToBlocks } from "@/components/shared/comment-blocknote";
import { currentUserQueryOptions } from "@/lib/auth-api";
import {
	addDocComment,
	type DocActivity,
	deleteDocComment,
	docQueryKeys,
	listActivities,
	updateDocComment,
	updateDocument,
} from "@/lib/doc-api";
import { useMemberDirectory } from "@/lib/member-directory";

type DocActivityChange = {
	field: string;
	old?: unknown;
	new?: unknown;
};

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null;
}

function getDocActivityChanges(content: unknown): DocActivityChange[] {
	if (!isRecord(content)) {
		return [];
	}

	const { changes } = content;
	if (!Array.isArray(changes)) {
		return [];
	}

	return changes.filter(
		(change): change is DocActivityChange =>
			isRecord(change) && typeof change.field === "string",
	);
}

/** A write's note, client label and task, as stored next to its changes. */
export type DocActivityNote = { note?: string; client?: string; task?: string };

/** Returns the note, client label and task a doc.created / doc.updated entry
 *  recorded, or null when it recorded none. */
export function getDocActivityNote(
	entry: Pick<DocActivity, "activity_type" | "content">,
): DocActivityNote | null {
	if (
		entry.activity_type !== "doc.created" &&
		entry.activity_type !== "doc.updated"
	)
		return null;
	if (!isRecord(entry.content) || Array.isArray(entry.content)) return null;
	const out: DocActivityNote = {};
	for (const key of ["note", "client", "task"] as const) {
		const value = entry.content[key];
		if (typeof value === "string" && value.trim()) out[key] = value;
	}
	return out.note || out.client || out.task ? out : null;
}

export function DocActivityNoteLine({ note }: { note: DocActivityNote }) {
	const meta = [note.task, note.client].filter(Boolean).join(" · ");
	return (
		<div className="pl-0.5 pb-0.5 text-xs">
			{note.note && (
				<p className="text-foreground/70 whitespace-pre-wrap break-words">
					{note.note}
				</p>
			)}
			{meta && <p className="text-muted-foreground/50 break-all">{meta}</p>}
		</div>
	);
}

// Exported (and narrowed to just the fields it needs) so the agent activity
// feed tab can reuse the same doc-activity copy without duplicating it — see
// apps/web/src/components/projects/agents/agent-activity-tab.tsx.
export function describeDocActivity(
	entry: Pick<DocActivity, "activity_type" | "content">,
	t: TFunction<"projects">,
): string {
	switch (entry.activity_type) {
		case "doc.created":
			return t("docs.activity.created");
		case "doc.updated": {
			const changes = getDocActivityChanges(entry.content);
			if (changes.length > 0) {
				const fields = changes.map((c) => c.field).join(", ");
				return t("docs.activity.updatedFields", { fields });
			}
			return t("docs.activity.updated");
		}
		case "doc.deleted":
			return t("docs.activity.deleted");
		case "doc.moved":
			return t("docs.activity.moved");
		case "comment":
			return "";
		default:
			return entry.activity_type;
	}
}

interface DocActivityPaneProps {
	projectId: string;
	docId: string;
	canEdit?: boolean;
}

export function DocActivityPane({
	projectId,
	docId,
	canEdit = true,
}: DocActivityPaneProps) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { data: currentUser } = useQuery(currentUserQueryOptions);
	const getMember = useMemberDirectory(projectId);

	const myMemberId = currentUser ? getMember(currentUser.id)?.id : undefined;

	const queryKey = docQueryKeys.activities(projectId, docId);

	const handleRevert = useCallback(
		async (entry: DocActivity) => {
			const c = entry.content as Record<string, unknown> | null;
			const changes = c?.changes as DocActivityChange[] | undefined;
			if (!changes?.length) return;

			const payload: Parameters<typeof updateDocument>[2] = {};
			for (const ch of changes) {
				switch (ch.field) {
					case "title":
						if (typeof ch.old === "string") payload.title = ch.old;
						break;
					case "content":
						if (ch.old !== undefined) {
							payload.content = Array.isArray(ch.old)
								? (ch.old as unknown[])
								: null;
						}
						break;
				}
			}

			if (Object.keys(payload).length === 0) return;
			await updateDocument(projectId, docId, payload);
			qc.invalidateQueries({
				queryKey: docQueryKeys.detail(projectId, docId),
			});
		},
		[projectId, docId, qc],
	);

	const getDiffContent = useCallback(
		(entry: DocActivity) => {
			if (entry.activity_type !== "doc.updated") return null;
			const c = entry.content as Record<string, unknown> | null;
			const changes = c?.changes as DocActivityChange[] | undefined;
			if (!changes) return null;
			const contentChange = changes.find((ch) => ch.field === "content");
			if (!contentChange || contentChange.old === undefined) return null;
			return {
				old: contentChange.old,
				new: contentChange.new,
				title: t("docs.activity.contentChangeDiff"),
			};
		},
		[t],
	);

	const isRevertable = useCallback((entry: DocActivity) => {
		if (entry.activity_type !== "doc.updated") return false;
		const c = entry.content as Record<string, unknown> | null;
		const changes = c?.changes as DocActivityChange[] | undefined;
		if (!changes?.length) return false;
		return changes.some(
			(ch) => (ch.field === "title" || ch.field === "content") && "old" in ch,
		);
	}, []);

	return (
		<ActivityPane<DocActivity>
			projectId={projectId}
			entityId={docId}
			queryKey={queryKey}
			fetchPage={(cursor) => listActivities(projectId, docId, cursor)}
			addComment={
				canEdit
					? (blocks) => addDocComment(projectId, docId, blocks)
					: undefined
			}
			updateComment={
				canEdit
					? (commentId, blocks) =>
							updateDocComment(projectId, docId, commentId, blocks)
					: undefined
			}
			deleteComment={
				canEdit
					? (commentId) => deleteDocComment(projectId, docId, commentId)
					: undefined
			}
			onRevert={canEdit ? handleRevert : undefined}
			getDiffContent={getDiffContent}
			isRevertable={canEdit ? isRevertable : undefined}
			describeActivity={(entry) => describeDocActivity(entry, t)}
			describeActivityDetail={(entry) => {
				const note = getDocActivityNote(entry);
				return note ? <DocActivityNoteLine note={note} /> : null;
			}}
			getCommentBlocks={(content) => {
				if (Array.isArray(content)) return content;
				if (content && typeof content === "object" && !("length" in content)) {
					if ("content" in content) {
						const blockContent = (content as { content?: unknown }).content;
						if (Array.isArray(blockContent)) return blockContent;
					}
					if ("text" in content) {
						const text = (content as { text?: string }).text ?? "";
						return textToBlocks(text);
					}
				}
				return [];
			}}
			currentUserId={myMemberId}
		/>
	);
}
