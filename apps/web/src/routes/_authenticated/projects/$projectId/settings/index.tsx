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
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { usePermissions } from "@/hooks/use-permissions";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { RemoteComponent } from "@/lib/plugins/loader";
import { usePluginRegistry } from "@/lib/plugins/registry";
import { projectQueryOptions } from "@/lib/project-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/settings/",
)({
	// Only the project itself is prefetched here (for the header's name —
	// and already warm from the parent $projectId route's own loader
	// anyway). Roles/members/task-statuses/task-types/custom-fields each
	// belong to exactly one lazily-rendered tab below and are fetched by
	// that tab's own component via useQuery, not prefetched here: each of
	// those needs its own project.*.read permission (project.roles.read,
	// project.members.read, project.settings.task_statuses.read, etc.), and
	// a role that's missing just one of them — a hand-edited custom role
	// especially — would previously fail this Promise.all and crash the
	// entire settings page, including the General/Danger Zone tabs that
	// role could otherwise use. A component-level useQuery fails softly
	// (that one tab shows an empty/loading state) instead of blocking the
	// whole route.
	loader: async ({ context: { queryClient }, params: { projectId } }) => {
		await queryClient.ensureQueryData(projectQueryOptions(projectId));
	},
	component: SettingsPage,
});

// ── Settings Page ─────────────────────────────────────────────────────────────

const NAV_ITEMS = [
	{
		id: "general",
		labelKey: "project.settingsPage.nav.general",
		icon: Settings,
	},
	{ id: "roles", labelKey: "project.settingsPage.nav.roles", icon: Shield },
	{
		id: "task-statuses",
		labelKey: "project.settingsPage.nav.taskStatuses",
		icon: LayoutList,
	},
	{
		id: "task-types",
		labelKey: "project.settingsPage.nav.taskTypes",
		icon: Tag,
	},
	{
		id: "custom-fields",
		labelKey: "project.settingsPage.nav.customFields",
		icon: Plus,
	},
	{
		id: "danger",
		labelKey: "project.settingsPage.nav.dangerZone",
		icon: AlertTriangle,
	},
] as const;

function SettingsPage() {
	const { t } = useTranslation("projects");
	const { projectId } = Route.useParams();
	const { data: project } = useQuery(projectQueryOptions(projectId));
	const { hasPermission } = usePermissions();
	const { hasProjectPermission } = useProjectPermissions(projectId);

	const canDelete =
		hasPermission("projects.delete") || hasProjectPermission("projects.delete");
	const canEditProject =
		hasPermission("projects.write") || hasProjectPermission("projects.write");
	const canManageRoles =
		hasPermission("project.roles.write") ||
		hasProjectPermission("project.roles.write");
	const canManageTasks =
		hasPermission("tasks.write") || hasProjectPermission("tasks.write");

	const { getRegistrations } = usePluginRegistry();
	// A tab's own requiredPermission no longer hides it from this list —
	// matching how the built-in tabs above (task-types, custom-fields, etc.)
	// are always shown and instead render NoPermissionState internally when
	// the viewer lacks the relevant permission. See the plugin-tab render
	// branch below for the equivalent check.
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
							{t("project.settingsPage.title")}
						</h1>
					</div>
					<p className="text-sm text-muted-foreground">
						{project?.name} · {t("project.settingsPage.subtitle")}
					</p>
				</div>
			</div>

			{/* Body */}
			<div className="flex-1 overflow-y-auto">
				<div className="max-w-6xl mx-auto w-full px-6 py-8 flex gap-10 items-start">
					{/* Sidebar nav — hidden on small screens */}
					<aside className="hidden lg:flex flex-col gap-1 w-48 shrink-0 sticky top-8">
						<p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground/60 px-3 mb-1">
							{t("project.settingsPage.title")}
						</p>
						{visibleNavItems.map(({ id, labelKey, icon: Icon }) => (
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
								{t(labelKey)}
							</button>
						))}
						{pluginTabs.length > 0 && (
							<p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground/60 px-3 mt-4 mb-1">
								{t("project.settingsPage.nav.plugins")}
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
							{visibleNavItems.map(({ id, labelKey, icon: Icon }) => (
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
									{t(labelKey)}
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
						{pluginTabs.map((reg) => {
							if (activeSection !== `plugin:${reg.pluginId}:${reg.component}`) {
								return null;
							}
							const authorized =
								!reg.requiredPermission ||
								hasProjectPermission(reg.requiredPermission);
							if (!authorized) {
								return (
									<NoPermissionState
										key={`${reg.pluginId}:${reg.component}`}
										title={t(
											"project.settingsPage.pluginTab.noPermission.title",
										)}
										description={t(
											"project.settingsPage.pluginTab.noPermission.description",
											{ pluginName: reg.pluginName },
										)}
									/>
								);
							}
							return (
								<RemoteComponent
									key={`${reg.pluginId}:${reg.component}`}
									registration={reg}
									componentProps={{
										projectId,
										// A tab with its own requiredPermission is single-tier —
										// having just passed the `authorized` check above means
										// canEdit is simply true. A tab with no requiredPermission
										// (fully open to any project member, the pre-existing
										// default) falls back to the original projects.write
										// check so its behavior is unchanged.
										canEdit: reg.requiredPermission ? true : canEditProject,
									}}
								/>
							);
						})}
					</div>
				</div>
			</div>
		</div>
	);
}
