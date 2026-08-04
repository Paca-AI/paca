import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import {
	CommentDisplay,
	isBlocksContent,
	textToBlocks,
} from "@/components/shared/comment-blocknote";
import { timeAgo } from "@/lib/time-ago";
import { cn } from "@/lib/utils";
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

function label(value: unknown, t: TFunction<"projects">): string {
	if (value === null || value === undefined || value === "")
		return t("taskDetail.activity.none");
	if (Array.isArray(value))
		return value.length > 0 ? value.join(", ") : t("taskDetail.activity.none");
	return String(value);
}

export function describeTaskChange(
	change: FieldChange,
	names: ActivityNameMaps,
	t: TFunction<"projects">,
): string {
	const oldVal = label(change.old, t);
	const newVal = label(change.new, t);
	const hasOld =
		change.old !== null && change.old !== undefined && change.old !== "";
	const hasNew =
		change.new !== null && change.new !== undefined && change.new !== "";

	const resolveMember = (id: unknown) =>
		(id && names.members[String(id)]) ||
		(id ? String(id).slice(0, 8) : t("taskDetail.activity.none"));
	const resolveSprint = (id: unknown) =>
		(id && names.sprints[String(id)]) ||
		(id ? String(id).slice(0, 8) : t("taskDetail.activity.none"));

	switch (change.field) {
		case "status":
			if (hasOld && hasNew)
				return t("taskDetail.activity.changedStatus", { oldVal, newVal });
			if (hasNew) return t("taskDetail.activity.setStatus", { newVal });
			return t("taskDetail.activity.clearedStatus");
		case "task_type":
			if (hasOld && hasNew)
				return t("taskDetail.activity.changedType", { oldVal, newVal });
			if (hasNew) return t("taskDetail.activity.setType", { newVal });
			return t("taskDetail.activity.clearedType");
		case "title":
			if (hasOld) return t("taskDetail.activity.renamed", { oldVal, newVal });
			return t("taskDetail.activity.setTitle", { newVal });
		case "importance":
			if (hasOld)
				return t("taskDetail.activity.changedPriority", { oldVal, newVal });
			return t("taskDetail.activity.setPriority", { newVal });
		case "assignee": {
			const oldIds = Array.isArray(change.old) ? change.old : [];
			const newIds = Array.isArray(change.new) ? change.new : [];
			const resolveMembers = (ids: unknown[]) =>
				ids.map((id) => resolveMember(id)).join(", ");
			const oldName = resolveMembers(oldIds);
			const newName = resolveMembers(newIds);
			if (oldIds.length > 0 && newIds.length > 0)
				return t("taskDetail.activity.changedAssignee", {
					oldName,
					newName,
				});
			if (newIds.length > 0)
				return t("taskDetail.activity.assignedTo", { newName });
			return t("taskDetail.activity.removedAssignee", { oldName });
		}
		case "reporter": {
			const oldName = resolveMember(change.old);
			const newName = resolveMember(change.new);
			if (hasOld && hasNew)
				return t("taskDetail.activity.changedReporter", {
					oldName,
					newName,
				});
			if (hasNew) return t("taskDetail.activity.setReporter", { newName });
			return t("taskDetail.activity.removedReporter", { oldName });
		}
		case "sprint": {
			const oldSprint = resolveSprint(change.old);
			const newSprint = resolveSprint(change.new);
			if (hasOld && hasNew)
				return t("taskDetail.activity.movedSprint", {
					oldSprint,
					newSprint,
				});
			if (hasNew) return t("taskDetail.activity.addedToSprint", { newSprint });
			return t("taskDetail.activity.removedFromSprint", { oldSprint });
		}
		case "parent_task":
			if (hasNew) return t("taskDetail.activity.setParentTask");
			return t("taskDetail.activity.removedParentTask");
		case "due_date":
			if (hasOld && hasNew)
				return t("taskDetail.activity.changedDueDate", { oldVal, newVal });
			if (hasNew) return t("taskDetail.activity.setDueDate", { newVal });
			return t("taskDetail.activity.removedDueDate");
		case "start_date":
			if (hasOld && hasNew)
				return t("taskDetail.activity.changedStartDate", {
					oldVal,
					newVal,
				});
			if (hasNew) return t("taskDetail.activity.setStartDate", { newVal });
			return t("taskDetail.activity.removedStartDate");
		case "description":
			return t("taskDetail.activity.updatedDescription");
		case "tags":
			return t("taskDetail.activity.updatedTags");
		case "custom_fields":
			return t("taskDetail.activity.updatedCustomFields");
		default:
			return t("taskDetail.activity.updatedField", {
				field: change.field.replace(/_/g, " "),
			});
	}
}

// Exported (and narrowed to just the fields it needs) so the agent activity
// feed tab can reuse the same task-activity copy without duplicating it —
// see apps/web/src/components/projects/agents/agent-activity-tab.tsx.
export function activityDescription(
	entry: Pick<ActivityEntry, "activity_type" | "content">,
	names: ActivityNameMaps,
	t: TFunction<"projects">,
): string {
	const c = entry.content ?? {};
	const content = c as Record<string, unknown>;
	switch (entry.activity_type) {
		case "task.created":
			return t("taskDetail.activity.created");
		case "task.deleted":
			return t("taskDetail.activity.deleted");
		case "task.updated": {
			const changes = content.changes as FieldChange[] | undefined;
			if (changes && changes.length === 1) {
				return describeTaskChange(changes[0], names, t);
			}
			if (changes && changes.length > 1) {
				return changes.map((ch) => describeTaskChange(ch, names, t)).join("; ");
			}
			return t("taskDetail.activity.updated");
		}
		case "task.attachment.added":
			return content.file_name
				? t("taskDetail.activity.addedAttachmentNamed", {
						fileName: content.file_name,
					})
				: t("taskDetail.activity.addedAttachment");
		case "task.attachment.removed":
			return content.file_name
				? t("taskDetail.activity.removedAttachmentNamed", {
						fileName: content.file_name,
					})
				: t("taskDetail.activity.removedAttachment");
		case "task.link.added": {
			const linkType =
				content.link_type === "blocks"
					? t("taskDetail.activity.linkTypeBlocks")
					: content.link_type === "relates_to"
						? t("taskDetail.activity.linkTypeRelatedTo")
						: t("taskDetail.activity.linkTypeDuplicates");
			return t("taskDetail.activity.addedTaskLink", { linkType });
		}
		case "task.link.removed":
			return t("taskDetail.activity.removedTaskLink");
		default:
			return (
				(content._description as string | undefined) ??
				t("taskDetail.activity.madeChange")
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
	const { t } = useTranslation("projects");
	const { t: tCommon } = useTranslation("common");
	const isComment = entry.activity_type === "comment";
	const displayName =
		entry.actor_name || entry.actor_username || t("taskDetail.activity.system");
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
								{timeAgo(entry.created_at, tCommon)}
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
							{timeAgo(entry.created_at, tCommon)}
						</span>
					</div>
				)}
			</div>
		</div>
	);
}
