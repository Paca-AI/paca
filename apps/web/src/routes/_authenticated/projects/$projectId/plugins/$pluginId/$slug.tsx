import { createFileRoute, notFound, redirect } from "@tanstack/react-router";
import { AlertCircle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { hasPermission } from "@/lib/permissions";
import { buildNavItems, pluginsQueryOptions } from "@/lib/plugin-api";
import { RemoteComponent } from "@/lib/plugins/loader";
import { usePluginBaseProps } from "@/lib/plugins/plugin-props";
import { usePluginRegistry } from "@/lib/plugins/registry";
import { myProjectPermissionsQueryOptions } from "@/lib/project-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/plugins/$pluginId/$slug",
)({
	beforeLoad: async ({
		context: { queryClient },
		params: { projectId, pluginId, slug },
	}) => {
		const plugins = await queryClient
			.ensureQueryData(pluginsQueryOptions)
			.catch(() => []);
		const navItem = buildNavItems(plugins, "project").find(
			(item) => item.pluginId === pluginId && item.slug === slug,
		);
		// Nav items without a declared `requiredPermission` are reachable by
		// any project member, matching the pre-existing behavior of embedded
		// `<ExtensionPoint point="project.page">` fragments.
		if (!navItem?.requiredPermission) return;

		const permissionsMap = await queryClient
			.fetchQuery(myProjectPermissionsQueryOptions(projectId))
			.catch(() => ({}) as Record<string, boolean>);
		const granted = Object.entries(permissionsMap)
			.filter(([, v]) => v === true)
			.map(([k]) => k);

		if (!hasPermission(granted, navItem.requiredPermission)) {
			throw redirect({ to: "/projects/$projectId", params: { projectId } });
		}
	},
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
 */
function ProjectPluginPage() {
	const { t } = useTranslation("errors");
	const { projectId, pluginId, slug } = Route.useParams();
	const { getNavItems, isLoading } = usePluginRegistry();
	const navItem = getNavItems("project").find(
		(item) => item.pluginId === pluginId && item.slug === slug,
	);
	const baseProps = usePluginBaseProps(navItem?.registration, projectId);

	if (isLoading) return null;
	if (!navItem) {
		throw notFound();
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
