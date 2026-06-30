import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import {
	AlertTriangle,
	LayoutList,
	Plus,
	Settings,
	Shield,
	Tag,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { CustomFieldsSettings } from "@/components/projects/settings/CustomFieldsSettings";
import { DangerZone } from "@/components/projects/settings/DangerZone";
import { GeneralSettings } from "@/components/projects/settings/GeneralSettings";
import { RolesSettings } from "@/components/projects/settings/RolesSettings";
import { TaskStatusesSettings } from "@/components/projects/settings/TaskStatusesSettings";
import { TaskTypesSettings } from "@/components/projects/settings/TaskTypesSettings";
import { usePermissions } from "@/hooks/use-permissions";
import { currentUserQueryOptions } from "@/lib/auth-api";
import { RemoteComponent } from "@/lib/plugins/loader";
import { usePluginRegistry } from "@/lib/plugins/registry";
import {
	customFieldsQueryOptions,
	type ProjectMember,
	type ProjectRole,
	projectMembersQueryOptions,
	projectQueryOptions,
	projectRolesQueryOptions,
	taskStatusesQueryOptions,
	taskTypesQueryOptions,
} from "@/lib/project-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/settings/",
)({
	loader: async ({ context: { queryClient }, params: { projectId } }) => {
		await Promise.all([
			queryClient.ensureQueryData(projectQueryOptions(projectId)),
			queryClient.ensureQueryData(projectRolesQueryOptions(projectId)),
			queryClient.ensureQueryData(projectMembersQueryOptions(projectId)),
			queryClient.ensureQueryData(taskStatusesQueryOptions(projectId)),
			queryClient.ensureQueryData(taskTypesQueryOptions(projectId)),
			queryClient.ensureQueryData(customFieldsQueryOptions(projectId)),
		]);
	},
	component: SettingsPage,
});

// ── Settings Page ─────────────────────────────────────────────────────────────

const NAV_ITEMS = [
	{ id: "general", tabKey: "general", icon: Settings },
	{ id: "roles", tabKey: "roles", icon: Shield },
	{ id: "task-statuses", tabKey: "taskStatuses", icon: LayoutList },
	{ id: "task-types", tabKey: "taskTypes", icon: Tag },
	{ id: "custom-fields", tabKey: "customFields", icon: Plus },
	{ id: "danger", tabKey: "danger", icon: AlertTriangle },
] as const;

function SettingsPage() {
	const { t } = useTranslation();
	const { projectId } = Route.useParams();
	const { data: project } = useQuery(projectQueryOptions(projectId));
	const { hasPermission } = usePermissions();
	const { data: currentUser } = useQuery(currentUserQueryOptions);
	const { data: members = [] } = useQuery(
		projectMembersQueryOptions(projectId),
	);
	const { data: roles = [] } = useQuery(projectRolesQueryOptions(projectId));

	const myMembership = (members as ProjectMember[]).find(
		(m) => m.user_id === currentUser?.id,
	);
	const myRole = (roles as ProjectRole[]).find(
		(r) => r.id === myMembership?.project_role_id,
	);
	const hasProjectDelete = Boolean(
		(myRole?.permissions as Record<string, boolean> | undefined)?.[
			"projects.delete"
		],
	);
	const hasProjectWrite = Boolean(
		(myRole?.permissions as Record<string, boolean> | undefined)?.[
			"projects.write"
		],
	);
	const hasProjectRolesWrite = Boolean(
		(myRole?.permissions as Record<string, boolean> | undefined)?.[
			"project.roles.write"
		],
	);
	const canDelete = hasPermission("projects.delete") || hasProjectDelete;
	const canEditProject = hasPermission("projects.write") || hasProjectWrite;
	const canManageRoles =
		hasPermission("project.roles.write") || hasProjectRolesWrite;
	const hasTasksWrite = Boolean(
		(myRole?.permissions as Record<string, boolean> | undefined)?.[
			"tasks.write"
		],
	);
	const canManageTasks = hasPermission("tasks.write") || hasTasksWrite;

	const { getRegistrations } = usePluginRegistry();
	const pluginTabs = getRegistrations("project.settings.tab").filter(
		(r) => !r.hidden,
	);

	const visibleNavItems = canDelete
		? NAV_ITEMS
		: NAV_ITEMS.filter((i) => i.id !== "danger");

	const [activeSection, setActiveSection] = useState<
		| "general"
		| "roles"
		| "task-statuses"
		| "task-types"
		| "custom-fields"
		| "danger"
		| string
	>("general");

	return (
		<div className="flex flex-col min-h-0 flex-1">
			{/* Header */}
			<div className="relative overflow-hidden border-b border-border/50 shrink-0">
				<div
					className="pointer-events-none absolute inset-0 opacity-50"
					style={{
						backgroundImage:
							"radial-gradient(circle, color-mix(in oklch, var(--color-primary) 12%, transparent) 1px, transparent 1px)",
						backgroundSize: "20px 20px",
						maskImage:
							"radial-gradient(ellipse 70% 100% at 0% 0%, black 20%, transparent 70%)",
					}}
				/>
				<div className="relative px-6 py-7 max-w-6xl mx-auto w-full">
					<div className="flex items-center gap-2.5 mb-1">
						<Settings className="size-4 text-muted-foreground" />
						<h1 className="font-[Syne] text-2xl font-bold tracking-tight">
							{t("project.settings.title")}
						</h1>
					</div>
					<p className="text-sm text-muted-foreground">
						{project?.name} · {t("project.settings.subtitle")}
					</p>
				</div>
			</div>

			{/* Body */}
			<div className="flex-1 overflow-y-auto">
				<div className="max-w-6xl mx-auto w-full px-6 py-8 flex gap-10 items-start">
					{/* Sidebar nav — hidden on small screens */}
					<aside className="hidden lg:flex flex-col gap-1 w-48 shrink-0 sticky top-8">
						<p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground/60 px-3 mb-1">
							{t("project.settings.navLabel")}
						</p>
						{visibleNavItems.map(({ id, tabKey, icon: Icon }) => (
							<button
								key={id}
								type="button"
								onClick={() => setActiveSection(id)}
								className={`flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium transition-colors text-left ${
									activeSection === id
										? "bg-accent text-foreground"
										: "text-muted-foreground hover:bg-accent/60 hover:text-foreground"
								} ${id === "danger" ? "mt-2 text-destructive/70 hover:text-destructive hover:bg-destructive/8" : ""}`}
							>
								<Icon className="size-3.5 shrink-0" />
								{t(`project.settings.tabs.${tabKey}`)}
							</button>
						))}
						{pluginTabs.length > 0 && (
							<p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground/60 px-3 mt-4 mb-1">
								{t("project.settings.plugins")}
							</p>
						)}
						{pluginTabs.map((reg) => (
							<button
								key={`${reg.pluginId}:${reg.component}`}
								type="button"
								onClick={() =>
									setActiveSection(`plugin:${reg.pluginId}:${reg.component}`)
								}
								className={`flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium transition-colors text-left ${
									activeSection === `plugin:${reg.pluginId}:${reg.component}`
										? "bg-accent text-foreground"
										: "text-muted-foreground hover:bg-accent/60 hover:text-foreground"
								}`}
							>
								{reg.pluginName}
							</button>
						))}
					</aside>

					{/* Content */}
					<div className="flex-1 min-w-0">
						{/* Mobile section picker */}
						<div className="flex gap-1 mb-6 lg:hidden flex-wrap">
							{visibleNavItems.map(({ id, tabKey, icon: Icon }) => (
								<button
									key={id}
									type="button"
									onClick={() => setActiveSection(id)}
									className={`flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
										activeSection === id
											? "bg-accent text-foreground"
											: "text-muted-foreground hover:bg-accent/60 hover:text-foreground"
									}`}
								>
									<Icon className="size-3 shrink-0" />
									{t(`project.settings.tabs.${tabKey}`)}
								</button>
							))}
							{pluginTabs.map((reg) => (
								<button
									key={`${reg.pluginId}:${reg.component}`}
									type="button"
									onClick={() =>
										setActiveSection(`plugin:${reg.pluginId}:${reg.component}`)
									}
									className={`flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
										activeSection === `plugin:${reg.pluginId}:${reg.component}`
											? "bg-accent text-foreground"
											: "text-muted-foreground hover:bg-accent/60 hover:text-foreground"
									}`}
								>
									{reg.pluginName}
								</button>
							))}
						</div>

						{activeSection === "general" && (
							<GeneralSettings projectId={projectId} canEdit={canEditProject} />
						)}
						{activeSection === "roles" && (
							<RolesSettings
								projectId={projectId}
								canManageRoles={canManageRoles}
							/>
						)}
						{activeSection === "task-statuses" && (
							<TaskStatusesSettings
								projectId={projectId}
								canWrite={canManageTasks}
							/>
						)}
						{activeSection === "task-types" && (
							<TaskTypesSettings
								projectId={projectId}
								canWrite={canManageTasks}
							/>
						)}
						{activeSection === "custom-fields" && (
							<CustomFieldsSettings
								projectId={projectId}
								canWrite={canManageTasks}
							/>
						)}
						{activeSection === "danger" && canDelete && (
							<DangerZone projectId={projectId} />
						)}
						{/* Plugin settings tabs */}
						{pluginTabs.map((reg) =>
							activeSection === `plugin:${reg.pluginId}:${reg.component}` ? (
								<RemoteComponent
									key={`${reg.pluginId}:${reg.component}`}
									registration={reg}
									componentProps={{ projectId, canEdit: canEditProject }}
								/>
							) : null,
						)}
					</div>
				</div>
			</div>
		</div>
	);
}
