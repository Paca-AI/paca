import { createFileRoute, notFound, redirect } from "@tanstack/react-router";
import { AlertCircle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { myPermissionsQueryOptions } from "@/lib/admin-api";
import { hasPermission } from "@/lib/permissions";
import { buildNavItems, pluginsQueryOptions } from "@/lib/plugin-api";
import { RemoteComponent } from "@/lib/plugins/loader";
import { usePluginBaseProps } from "@/lib/plugins/plugin-props";
import { usePluginRegistry } from "@/lib/plugins/registry";

export const Route = createFileRoute(
	"/_authenticated/admin/plugins/$pluginId/$slug",
)({
	beforeLoad: async ({
		context: { queryClient },
		params: { pluginId, slug },
	}) => {
		const [permissions, plugins] = await Promise.all([
			queryClient
				.fetchQuery(myPermissionsQueryOptions)
				.catch(() => [] as string[]),
			queryClient.ensureQueryData(pluginsQueryOptions).catch(() => []),
		]);

		const navItem = buildNavItems(plugins, "admin").find(
			(item) => item.pluginId === pluginId && item.slug === slug,
		);
		// Nav items without a declared `requiredPermission` fall back to
		// `users.write`, matching the blanket gate the built-in "Plugins"
		// admin nav item (and this route, previously) already use.
		const requiredPermission = navItem?.requiredPermission ?? "users.write";

		if (!hasPermission(permissions, requiredPermission)) {
			throw redirect({ to: "/home" });
		}
	},
	loader: async ({ context: { queryClient } }) => {
		await queryClient.ensureQueryData(pluginsQueryOptions);
	},
	component: AdminPluginPage,
});

/**
 * Full-page route that renders a plugin's `admin.page` extension-point
 * component for the given plugin/nav-item slug — the admin/global-scope
 * counterpart to `ProjectPluginPage`. Used for cross-project plugin
 * dashboards (e.g. a "total logged time across all projects" summary).
 */
function AdminPluginPage() {
	const { t } = useTranslation("errors");
	const { pluginId, slug } = Route.useParams();
	const { getNavItems, isLoading } = usePluginRegistry();
	const navItem = getNavItems("admin").find(
		(item) => item.pluginId === pluginId && item.slug === slug,
	);
	const baseProps = usePluginBaseProps(navItem?.registration);

	if (isLoading) return null;
	if (!navItem) {
		throw notFound();
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
