import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
	type ActivityEntry,
	ActivityPane,
} from "@/components/shared/activity-pane";
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
import { projectMembersQueryOptions } from "@/lib/project-api";

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

function describeDocActivity(entry: ActivityEntry, t: TFunction): string {
	const activity = entry as DocActivity;
	switch (activity.activity_type) {
		case "doc.created":
			return t("project.docs.actCreated");
		case "doc.updated": {
			const changes = getDocActivityChanges(activity.content);
			if (changes.length > 0) {
				const fields = changes.map((c) => c.field).join(", ");
				return t("project.docs.actUpdatedFields", { fields });
			}
			return t("project.docs.actUpdated");
		}
		case "doc.deleted":
			return t("project.docs.actDeleted");
		case "doc.moved":
			return t("project.docs.actMoved");
		case "comment":
			return "";
		default:
			return activity.activity_type;
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
	const { t } = useTranslation();
	const qc = useQueryClient();
	const { data: currentUser } = useQuery(currentUserQueryOptions);
	const { data: membersData } = useQuery(projectMembersQueryOptions(projectId));

	const myMemberId = useMemo(() => {
		if (!currentUser || !membersData) return undefined;
		return membersData.find((m) => m.user_id === currentUser.id)?.id;
	}, [currentUser, membersData]);

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

	const getDiffContent = useCallback((entry: DocActivity) => {
		if (entry.activity_type !== "doc.updated") return null;
		const c = entry.content as Record<string, unknown> | null;
		const changes = c?.changes as DocActivityChange[] | undefined;
		if (!changes) return null;
		const contentChange = changes.find((ch) => ch.field === "content");
		if (!contentChange || contentChange.old === undefined) return null;
		return {
			old: contentChange.old,
			new: contentChange.new,
			title: t("project.docs.contentDiff"),
		};
	}, [t]);

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
			queryFn={() => listActivities(projectId, docId)}
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
			sortAscending
			currentUserId={myMemberId}
		/>
	);
}
