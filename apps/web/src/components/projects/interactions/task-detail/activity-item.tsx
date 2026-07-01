import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import {
	CommentDisplay,
	isBlocksContent,
	textToBlocks,
} from "@/components/shared/comment-blocknote";
import { cn } from "@/lib/utils";
import { timeAgo } from "./helpers";
import type { ActivityEntry } from "./types";

export type ActivityNameMaps = {
	members: Record<string, string>; // user_id → display name
	sprints: Record<string, string>; // sprint_id → sprint name
};

type FieldChange = {
	field: string;
	old?: unknown;
	new?: unknown;
};

function label(value: unknown, t: TFunction): string {
	if (value === null || value === undefined || value === "")
		return t("project.interactions.taskDetail.activity.noneValue");
	if (Array.isArray(value))
		return value.length > 0
			? value.join(", ")
			: t("project.interactions.taskDetail.activity.noneValue");
	return String(value);
}

export function describeTaskChange(
	change: FieldChange,
	names: ActivityNameMaps,
	t: TFunction,
): string {
	const k = (key: string) => `project.interactions.taskDetail.activity.${key}`;
	const oldVal = label(change.old, t);
	const newVal = label(change.new, t);
	const hasOld =
		change.old !== null && change.old !== undefined && change.old !== "";
	const hasNew =
		change.new !== null && change.new !== undefined && change.new !== "";

	const noneVal = t(k("noneValue"));
	const resolveMember = (id: unknown) =>
		(id && names.members[String(id)]) || (id ? String(id).slice(0, 8) : noneVal);
	const resolveSprint = (id: unknown) =>
		(id && names.sprints[String(id)]) || (id ? String(id).slice(0, 8) : noneVal);

	switch (change.field) {
		case "status":
			if (hasOld && hasNew)
				return t(k("statusChanged"), { old: oldVal, new: newVal });
			if (hasNew) return t(k("statusSet"), { new: newVal });
			return t(k("statusCleared"));
		case "task_type":
			if (hasOld && hasNew)
				return t(k("typeChanged"), { old: oldVal, new: newVal });
			if (hasNew) return t(k("typeSet"), { new: newVal });
			return t(k("typeCleared"));
		case "title":
			if (hasOld) return t(k("titleRenamed"), { old: oldVal, new: newVal });
			return t(k("titleSet"), { new: newVal });
		case "importance":
			if (hasOld) return t(k("priorityChanged"), { old: oldVal, new: newVal });
			return t(k("prioritySet"), { new: newVal });
		case "assignee": {
			const oldName = resolveMember(change.old);
			const newName = resolveMember(change.new);
			if (hasOld && hasNew)
				return t(k("assigneeChanged"), { old: oldName, new: newName });
			if (hasNew) return t(k("assigneeSet"), { new: newName });
			return t(k("assigneeRemoved"), { old: oldName });
		}
		case "reporter": {
			const oldName = resolveMember(change.old);
			const newName = resolveMember(change.new);
			if (hasOld && hasNew)
				return t(k("reporterChanged"), { old: oldName, new: newName });
			if (hasNew) return t(k("reporterSet"), { new: newName });
			return t(k("reporterRemoved"), { old: oldName });
		}
		case "sprint": {
			const oldSprint = resolveSprint(change.old);
			const newSprint = resolveSprint(change.new);
			if (hasOld && hasNew)
				return t(k("sprintMoved"), { old: oldSprint, new: newSprint });
			if (hasNew) return t(k("sprintAdded"), { new: newSprint });
			return t(k("sprintRemoved"), { old: oldSprint });
		}
		case "parent_task":
			if (hasNew) return t(k("parentSet"));
			return t(k("parentRemoved"));
		case "due_date":
			if (hasOld && hasNew)
				return t(k("dueDateChanged"), { old: oldVal, new: newVal });
			if (hasNew) return t(k("dueDateSet"), { new: newVal });
			return t(k("dueDateRemoved"));
		case "start_date":
			if (hasOld && hasNew)
				return t(k("startDateChanged"), { old: oldVal, new: newVal });
			if (hasNew) return t(k("startDateSet"), { new: newVal });
			return t(k("startDateRemoved"));
		case "description":
			return t(k("descriptionUpdated"));
		case "tags":
			return t(k("tagsUpdated"));
		case "custom_fields":
			return t(k("customFieldsUpdated"));
		default:
			return t(k("fieldUpdated"), { field: change.field.replace(/_/g, " ") });
	}
}

function activityDescription(
	entry: ActivityEntry,
	names: ActivityNameMaps,
	t: TFunction,
): string {
	const k = (key: string) => `project.interactions.taskDetail.activity.${key}`;
	const c = entry.content ?? {};
	const content = c as Record<string, unknown>;
	switch (entry.activity_type) {
		case "task.created":
			return t(k("created"));
		case "task.deleted":
			return t(k("deleted"));
		case "task.updated": {
			const changes = content.changes as FieldChange[] | undefined;
			if (changes && changes.length === 1) {
				return describeTaskChange(changes[0], names, t);
			}
			if (changes && changes.length > 1) {
				return changes
					.map((ch) => describeTaskChange(ch, names, t))
					.join("; ");
			}
			return t(k("updated"));
		}
		case "task.attachment.added":
			return content.file_name
				? t(k("attachmentAddedNamed"), { name: content.file_name })
				: t(k("attachmentAdded"));
		case "task.attachment.removed":
			return content.file_name
				? t(k("attachmentRemovedNamed"), { name: content.file_name })
				: t(k("attachmentRemoved"));
		case "task.link.added": {
			const linkType =
				content.link_type === "blocks"
					? t(k("linkTypeBlocks"))
					: content.link_type === "relates_to"
						? t(k("linkTypeRelatesTo"))
						: t(k("linkTypeDuplicates"));
			return t(k("linkAdded"), { linkType });
		}
		case "task.link.removed":
			return t(k("linkRemoved"));
		default:
			return (
				(content._description as string | undefined) ?? t(k("madeChange"))
			);
	}
}

export function ActivityItem({
	entry,
	names = { members: {}, sprints: {} },
}: {
	entry: ActivityEntry;
	names?: ActivityNameMaps;
}) {
	const { t } = useTranslation();
	const isComment = entry.activity_type === "comment";
	const displayName =
		entry.actor_name ||
		entry.actor_username ||
		t("project.interactions.taskDetail.activity.system");
	const initial = displayName.slice(0, 1).toUpperCase();

	const commentBlocks = isComment
		? isBlocksContent(entry.content)
			? entry.content
			: textToBlocks((entry.content as { text?: string })?.text ?? "")
		: null;

	return (
		<div className="flex gap-3">
			<div
				className={cn(
					"flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-bold mt-0.5 ring-1",
					isComment
						? "bg-linear-to-br from-primary/20 to-primary/10 text-primary ring-primary/15"
						: "bg-muted/40 text-muted-foreground/80 ring-border/20",
				)}
			>
				{initial}
			</div>
			<div className="flex-1 min-w-0">
				{isComment ? (
					<div className="rounded-xl rounded-tl-lg border border-border/25 bg-card/70 px-3.5 py-2.5">
						<div className="mb-1 flex items-center gap-2">
							<span className="text-sm font-semibold text-foreground">
								{displayName}
							</span>
							<span className="text-xs text-muted-foreground/50">
								{timeAgo(entry.created_at, t)}
							</span>
						</div>
						{commentBlocks && commentBlocks.length > 0 ? (
							<div className="[&_.bn-editor]:text-sm [&_.bn-editor]:leading-relaxed [&_.bn-editor]:p-0">
								<CommentDisplay blocks={commentBlocks} />
							</div>
						) : (
							<p className="text-sm text-foreground leading-relaxed">
								{(entry.content as { text?: string })?.text ?? ""}
							</p>
						)}
					</div>
				) : (
					<div className="flex flex-wrap items-baseline gap-1.5 py-0.5">
						<span className="text-sm font-medium text-foreground/80">
							{displayName}
						</span>
						<span className="text-sm text-muted-foreground/70">
							{activityDescription(entry, names, t)}
						</span>
						<span className="text-xs text-muted-foreground/45">
							{timeAgo(entry.created_at, t)}
						</span>
					</div>
				)}
			</div>
		</div>
	);
}
