import type { TFunction } from "i18next";
import { describeDocActivity } from "@/components/projects/docs/doc-activity-pane";
import {
	type ActivityNameMaps,
	activityDescription,
} from "@/components/projects/interactions/task-detail/activity-item";
import type { ProjectActivity } from "@/lib/activity-api";
import type { DocActivityContent, DocActivityType } from "@/lib/doc-api";

// Topics whose copy lives at activityLog.types.<topic with dots as "_">,
// interpolated with the entry's content fields. Task and doc entries reuse
// their timelines' copy instead (activityDescription/describeDocActivity).
const KNOWN_TOPICS = new Set([
	"sprint.created",
	"sprint.updated",
	"sprint.deleted",
	"sprint.completed",
	"view.created",
	"view.updated",
	"view.deleted",
	"automation.created",
	"automation.updated",
	"automation.deleted",
	"automation.activated",
	"automation.deactivated",
	"automation.node.added",
	"automation.node.removed",
	"automation.edge.added",
	"automation.edge.removed",
	"environment.created",
	"environment.updated",
	"environment.deleted",
	"environment.started",
	"environment.stopped",
	"environment.restarted",
	"environment.ssh_key.added",
	"environment.ssh_key.removed",
	"environment.access.granted",
	"environment.access.revoked",
	"environment.port_forward.added",
	"environment.port_forward.removed",
	"annotation.created",
	"annotation.resolved",
	"annotation.reopened",
	"annotation.commented",
	"member.added",
	"member.role_changed",
	"member.updated",
	"member.removed",
	"role.created",
	"role.updated",
	"role.deleted",
	"project.updated",
	"project.task_type.created",
	"project.task_type.updated",
	"project.task_type.deleted",
	"project.task_type.default_set",
	"project.task_status.created",
	"project.task_status.updated",
	"project.task_status.deleted",
	"project.task_status.default_set",
	"project.task_status.reordered",
	"project.custom_field.created",
	"project.custom_field.updated",
	"project.custom_field.deleted",
	"project_agent.created",
	"project_agent.updated",
	"project_agent.deleted",
]);

export interface ActivityDescription {
	/** What happened, e.g. `completed this sprint`. */
	text: string;
	/** Secondary detail shown muted after the entity, e.g. a role change. */
	detail?: string;
}

function contentRecord(item: ProjectActivity): Record<string, unknown> {
	const c = item.content;
	return c && !Array.isArray(c) && typeof c === "object" ? c : {};
}

function str(v: unknown): string {
	return typeof v === "string" || typeof v === "number" ? String(v) : "";
}

/** The entity's display name: its live title, or the name the entry recorded. */
export function activityEntityTitle(item: ProjectActivity): string {
	return item.entity_title || str(contentRecord(item).name);
}

/** One activity log entry as a sentence fragment for the feed. */
export function describeProjectActivity(
	item: ProjectActivity,
	names: ActivityNameMaps,
	t: TFunction<"projects">,
): ActivityDescription {
	if (item.activity_type === "comment") {
		return { text: t("activityLog.types.comment") };
	}
	if (item.entity_type === "task") {
		return {
			text: activityDescription(
				{ activity_type: item.activity_type, content: item.content },
				names,
				t,
			),
		};
	}
	if (item.entity_type === "doc") {
		return {
			text: describeDocActivity(
				{
					activity_type: item.activity_type as DocActivityType,
					content: item.content as unknown as DocActivityContent,
				},
				t,
			),
		};
	}

	const c = contentRecord(item);
	if (!KNOWN_TOPICS.has(item.activity_type)) {
		return {
			text: t("activityLog.types.fallback", { type: item.activity_type }),
		};
	}
	const key = `activityLog.types.${item.activity_type.replace(/\./g, "_")}`;
	const text = t(key as "activityLog.types.fallback", {
		name: str(c.name),
		label: str(c.label),
		port: str(c.container_port),
	});

	switch (item.activity_type) {
		case "member.added":
			return c.role_name
				? {
						text,
						detail: t("activityLog.detail.asRole", { role: str(c.role_name) }),
					}
				: { text };
		case "member.role_changed":
			return {
				text,
				detail: t("activityLog.detail.roleChange", {
					from: str(c.previous_role_name),
					to: str(c.role_name),
				}),
			};
		case "role.updated":
			return c.previous_name
				? {
						text,
						detail: t("activityLog.detail.renamedFrom", {
							name: str(c.previous_name),
						}),
					}
				: { text };
		case "project.updated": {
			const changes = Array.isArray(c.changes) ? c.changes.map(str) : [];
			return changes.length > 0
				? {
						text,
						detail: changes
							.map((f) =>
								t(
									`activityLog.projectFields.${f}` as "activityLog.projectFields.name",
									{ defaultValue: f },
								),
							)
							.join(", "),
					}
				: { text };
		}
		case "environment.access.granted":
		case "environment.access.revoked": {
			const member = names.members[str(c.member_id)];
			return member
				? { text, detail: t("activityLog.detail.member", { name: member }) }
				: { text };
		}
		case "annotation.created":
		case "annotation.resolved":
		case "annotation.reopened":
		case "annotation.commented":
			return c.body ? { text, detail: `“${str(c.body)}”` } : { text };
		default:
			return { text };
	}
}
