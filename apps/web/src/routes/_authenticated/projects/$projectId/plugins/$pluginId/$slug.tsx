import { createFileRoute, notFound } from "@tanstack/react-router";
import { AlertCircle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { pluginsQueryOptions } from "@/lib/plugin-api";
import { RemoteComponent } from "@/lib/plugins/loader";
import { usePluginBaseProps } from "@/lib/plugins/plugin-props";
import { usePluginRegistry } from "@/lib/plugins/registry";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/plugins/$pluginId/$slug",
)({
	loader: async ({ context: { queryClient } }) => {
		await queryClient.ensureQueryData(pluginsQueryOptions);
	},
	component: ProjectPluginPage,
});

/**
 * Full-page route that renders a plugin's `project.page` extension-point
 * component for the given plugin/nav-item slug, resolved from the sidebar
 * nav items contributed by enabled plugins. This is the routed counterpart
 * to `<ExtensionPoint point="project.page">` — instead of embedding a
 * fragment inside a host page, the plugin owns the entire route.
 *
 * The nav item itself is always shown in the sidebar regardless of the
 * caller's permissions (see PluginProjectPages in app-sidebar.tsx) — a
 * caller who lacks the item's `requiredPermission` still reaches this
 * route, and gets a no-permission state here instead of the plugin's
 * actual page content, matching how core project pages behave (e.g.
 * TaskTypesSettings) rather than being redirected away or hidden.
 */
function ProjectPluginPage() {
	const { t } = useTranslation("errors");
	const { projectId, pluginId, slug } = Route.useParams();
	const { getNavItems, isLoading } = usePluginRegistry();
	const navItem = getNavItems("project").find(
		(item) => item.pluginId === pluginId && item.slug === slug,
	);
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const baseProps = usePluginBaseProps(navItem?.registration, projectId);

	if (isLoading) return null;
	if (!navItem) {
		throw notFound();
	}

	if (
		navItem.requiredPermission &&
		!hasProjectPermission(navItem.requiredPermission)
	) {
		return (
			<div className="flex flex-1 items-center justify-center p-6">
				<NoPermissionState
					title={t("pluginNoPermissionTitle")}
					description={t("pluginNoPermissionDescription", {
						pluginName: navItem.pluginName,
					})}
				/>
			</div>
		);
	}

	return (
		<div className="flex flex-col h-full">
			<RemoteComponent
				registration={navItem.registration}
				componentProps={{ ...baseProps, projectId }}
				fallback={
					<div className="flex items-center gap-2 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive m-6">
						<AlertCircle className="size-3.5 shrink-0" />
						<span>
							{t("pluginLoadFailedPrefix")}{" "}
							<strong>{navItem.pluginName}</strong>{" "}
							{t("pluginLoadFailedSuffix")}
						</span>
					</div>
				}
			/>
		</div>
	);
}
