import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Bot } from "lucide-react";
import { useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ActivityPane } from "@/components/shared/activity-pane";
import { textToBlocks } from "@/components/shared/comment-blocknote";
import { currentUserQueryOptions } from "@/lib/auth-api";
import {
	type Activity,
	addComment,
	deleteComment,
	listTaskActivities,
	sprintsQueryOptions,
	updateComment,
	updateTask,
} from "@/lib/interaction-api";
import {
	projectMembersQueryOptions,
	taskStatusesQueryOptions,
	taskTypesQueryOptions,
} from "@/lib/project-api";
import { describeTaskChange } from "./activity-item";

type FieldChange = {
	field: string;
	old?: unknown;
	new?: unknown;
};

interface TaskActivityPaneProps {
	projectId: string;
	taskId: string;
	canEdit?: boolean;
}

export function TaskActivityPane({
	projectId,
	taskId,
	canEdit = true,
}: TaskActivityPaneProps) {
	const { t } = useTranslation();
	const qc = useQueryClient();
	const { data: membersData } = useQuery(projectMembersQueryOptions(projectId));
	const { data: sprintsData } = useQuery(sprintsQueryOptions(projectId));
	const { data: currentUser } = useQuery(currentUserQueryOptions);
	const { data: statusesData } = useQuery(taskStatusesQueryOptions(projectId));
	const { data: taskTypesData } = useQuery(taskTypesQueryOptions(projectId));

	const myMemberId = useMemo(() => {
		if (!currentUser || !membersData) return undefined;
		return membersData.find((m) => m.user_id === currentUser.id)?.id;
	}, [currentUser, membersData]);

	const nameMaps = useMemo(() => {
		const members: Record<string, string> = {};
		for (const m of membersData ?? []) {
			members[m.id] = m.full_name || m.username;
		}
		const sprints: Record<string, string> = {};
		for (const s of sprintsData ?? []) {
			sprints[s.id] = s.name;
		}
		return { members, sprints };
	}, [membersData, sprintsData]);

	const describeActivity = useCallback(
		(entry: Activity) => {
			const k = (key: string) =>
				`project.interactions.taskDetail.activity.${key}`;
			const c = entry.content ?? {};
			switch (entry.activity_type) {
				case "task.created":
					return t(k("created"));
				case "task.deleted":
					return t(k("deleted"));
				case "task.updated": {
					const changes = (c as Record<string, unknown>).changes as
						| FieldChange[]
						| undefined;
					if (changes && changes.length === 1) {
						return describeTaskChange(changes[0], nameMaps, t);
					}
					if (changes && changes.length > 1) {
						return changes
							.map((ch) => describeTaskChange(ch, nameMaps, t))
							.join("; ");
					}
					return t(k("updated"));
				}
				case "task.attachment.added": {
					const fileName = (c as Record<string, unknown>).file_name;
					return fileName
						? t(k("attachmentAddedNamed"), { name: fileName })
						: t(k("attachmentAdded"));
				}
				case "task.attachment.removed": {
					const fileName = (c as Record<string, unknown>).file_name;
					return fileName
						? t(k("attachmentRemovedNamed"), { name: fileName })
						: t(k("attachmentRemoved"));
				}
				case "task.link.added": {
					const linkType =
						(c as Record<string, unknown>).link_type === "blocks"
							? t(k("linkTypeBlocks"))
							: (c as Record<string, unknown>).link_type === "relates_to"
								? t(k("linkTypeRelatesTo"))
								: t(k("linkTypeDuplicates"));
					return t(k("linkAdded"), { linkType });
				}
				case "task.link.removed":
					return t(k("linkRemoved"));
				case "agent.session.started": {
					const convId = (c as Record<string, unknown>).conversation_id as
						| string
						| undefined;
					return (
						<span className="flex items-center gap-1.5 flex-wrap">
							<span>{t(k("aiSessionStarted"))}</span>
							{convId && (
								<Link
									to="/projects/$projectId/conversations/$conversationId"
									params={{ projectId, conversationId: convId }}
									target="_blank"
									rel="noreferrer"
									className="inline-flex items-center gap-1 text-xs font-medium text-primary/80 hover:text-primary underline-offset-2 hover:underline transition-colors"
								>
									<Bot className="size-3" />
									{t(k("watchSession"))}
								</Link>
							)}
						</span>
					);
				}
				default:
					return (
						((c as Record<string, unknown>)._description as
							| string
							| undefined) ?? t(k("madeChange"))
					);
			}
		},
		[nameMaps, projectId, t],
	);

	const queryKey = [
		"projects",
		projectId,
		"tasks",
		taskId,
		"activities",
	] as const;

	const handleRevert = useCallback(
		async (entry: Activity) => {
			const c = entry.content as Record<string, unknown> | null;
			const changes = c?.changes as
				| Array<{ field: string; old?: unknown }>
				| undefined;
			if (!changes?.length) return;

			const payload: Parameters<typeof updateTask>[2] = {};
			for (const ch of changes) {
				const oldVal = ch.old;
				switch (ch.field) {
					case "status":
						if (oldVal === null || oldVal === undefined) {
							payload.status_id = null;
						} else if (typeof oldVal === "string" && oldVal) {
							const s = statusesData?.find((s) => s.name === oldVal);
							if (s) payload.status_id = s.id;
						} else {
							payload.status_id = null;
						}
						break;
					case "task_type":
						if (oldVal === null || oldVal === undefined) {
							payload.task_type_id = null;
						} else if (typeof oldVal === "string" && oldVal) {
							const t = taskTypesData?.find((t) => t.name === oldVal);
							if (t) payload.task_type_id = t.id;
						} else {
							payload.task_type_id = null;
						}
						break;
					case "title":
						if (typeof oldVal === "string") payload.title = oldVal;
						break;
					case "importance":
						if (typeof oldVal === "number") payload.importance = oldVal;
						break;
					case "assignee":
						payload.assignee_id =
							typeof oldVal === "string" && oldVal ? oldVal : null;
						break;
					case "reporter":
						payload.reporter_id =
							typeof oldVal === "string" && oldVal ? oldVal : null;
						break;
					case "sprint":
						payload.sprint_id =
							typeof oldVal === "string" && oldVal ? oldVal : null;
						break;
					case "parent_task":
						payload.parent_task_id =
							typeof oldVal === "string" && oldVal ? oldVal : null;
						break;
					case "start_date":
						payload.start_date =
							typeof oldVal === "string" && oldVal ? oldVal : null;
						break;
					case "due_date":
						payload.due_date =
							typeof oldVal === "string" && oldVal ? oldVal : null;
						break;
					case "description":
						if (oldVal !== undefined) {
							payload.description = Array.isArray(oldVal)
								? (oldVal as unknown[])
								: null;
						}
						break;
					case "tags":
						if (Array.isArray(oldVal)) {
							payload.tags = oldVal.filter(
								(v): v is string => typeof v === "string",
							);
						}
						break;
				}
			}

			if (Object.keys(payload).length === 0) return;
			await updateTask(projectId, taskId, payload);
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "tasks", taskId],
			});
		},
		[projectId, taskId, statusesData, taskTypesData, qc],
	);

	const getDiffContent = useCallback((entry: Activity) => {
		if (entry.activity_type !== "task.updated") return null;
		const c = entry.content as Record<string, unknown> | null;
		const changes = c?.changes as
			| Array<{ field: string; old?: unknown; new?: unknown }>
			| undefined;
		if (!changes) return null;
		const descChange = changes.find((ch) => ch.field === "description");
		if (!descChange || descChange.old === undefined) return null;
		return {
			old: descChange.old,
			new: descChange.new,
			title: t("project.interactions.taskDetail.activity.descriptionDiff"),
		};
	}, [t]);

	const isRevertable = useCallback((entry: Activity) => {
		if (entry.activity_type !== "task.updated") return false;
		const c = entry.content as Record<string, unknown> | null;
		const changes = c?.changes as Array<Record<string, unknown>> | undefined;
		if (!changes?.length) return false;
		return changes.some((ch) => typeof ch.field === "string" && "old" in ch);
	}, []);

	return (
		<ActivityPane<Activity>
			projectId={projectId}
			entityId={taskId}
			queryKey={queryKey}
			queryFn={() => listTaskActivities(projectId, taskId)}
			addComment={
				canEdit ? (blocks) => addComment(projectId, taskId, blocks) : undefined
			}
			updateComment={
				canEdit
					? (commentId, blocks) =>
							updateComment(projectId, taskId, commentId, blocks)
					: undefined
			}
			deleteComment={
				canEdit
					? (commentId) => deleteComment(projectId, taskId, commentId)
					: undefined
			}
			onRevert={canEdit ? handleRevert : undefined}
			getDiffContent={getDiffContent}
			isRevertable={canEdit ? isRevertable : undefined}
			describeActivity={describeActivity}
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
