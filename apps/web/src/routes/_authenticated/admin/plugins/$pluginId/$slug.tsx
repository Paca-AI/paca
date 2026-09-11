import { useQuery } from "@tanstack/react-query";
import { createFileRoute, notFound } from "@tanstack/react-router";
import { AlertCircle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { myPermissionsQueryOptions } from "@/lib/admin-api";
import { hasPermission } from "@/lib/permissions";
import { pluginsQueryOptions } from "@/lib/plugin-api";
import { RemoteComponent } from "@/lib/plugins/loader";
import { usePluginBaseProps } from "@/lib/plugins/plugin-props";
import { usePluginRegistry } from "@/lib/plugins/registry";

export const Route = createFileRoute(
	"/_authenticated/admin/plugins/$pluginId/$slug",
)({
	loader: async ({ context: { queryClient } }) => {
		await Promise.all([
			queryClient.ensureQueryData(pluginsQueryOptions),
			queryClient
				.fetchQuery(myPermissionsQueryOptions)
				.catch(() => [] as string[]),
		]);
	},
	component: AdminPluginPage,
});

/**
 * Full-page route that renders a plugin's `admin.page` extension-point
 * component for the given plugin/nav-item slug — the admin/global-scope
 * counterpart to `ProjectPluginPage`. Used for cross-project plugin
 * dashboards (e.g. a "total logged time across all projects" summary).
 *
 * The nav item itself is always shown once the Administration section is
 * reachable at all (see AppSidebar's `showAdminSection`/`adminPluginNavItems`
 * — a plugin's own `requiredPermission` no longer hides the link). A caller
 * who lacks the permission still reaches this route and gets a
 * no-permission state instead of the plugin's actual page content.
 */
function AdminPluginPage() {
	const { t } = useTranslation("errors");
	const { pluginId, slug } = Route.useParams();
	const { getNavItems, isLoading } = usePluginRegistry();
	const navItem = getNavItems("admin").find(
		(item) => item.pluginId === pluginId && item.slug === slug,
	);
	const { data: permissions = [] } = useQuery(myPermissionsQueryOptions);
	const baseProps = usePluginBaseProps(navItem?.registration);

	if (isLoading) return null;
	if (!navItem) {
		throw notFound();
	}

	// Nav items without a declared `requiredPermission` fall back to
	// `plugins.write`, matching the blanket gate the built-in "Plugins"
	// admin nav item (and this route, previously via redirect) already use.
	const requiredPermission = navItem.requiredPermission ?? "plugins.write";

	if (!hasPermission(permissions, requiredPermission)) {
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
				componentProps={baseProps}
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
