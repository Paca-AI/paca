import { Tabs } from "@base-ui/react/tabs";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { Palette, Settings, ShieldCheck } from "lucide-react";
import { useTranslation } from "react-i18next";
import { BrandingSettings } from "@/components/admin/settings/BrandingSettings";
import { SSOSettings } from "@/components/admin/settings/SSOSettings";
import { myPermissionsQueryOptions } from "@/lib/admin-api";
import { hasPermission } from "@/lib/permissions";
import { brandingQueryOptions } from "@/lib/settings-api";
import { ssoSettingsQueryOptions } from "@/lib/sso-settings-api";

export const Route = createFileRoute("/_authenticated/admin/settings/")({
	beforeLoad: async ({ context: { queryClient } }) => {
		const permissions = await queryClient
			.fetchQuery(myPermissionsQueryOptions)
			.catch(() => [] as string[]);

		if (
			!hasPermission(permissions, "settings.write") &&
			!hasPermission(permissions, "authentication.write")
		) {
			throw redirect({ to: "/home" });
		}
	},
	loader: async ({ context: { queryClient } }) => {
		const permissions = await queryClient.fetchQuery(myPermissionsQueryOptions);
		await Promise.all([
			hasPermission(permissions, "settings.write")
				? queryClient.ensureQueryData(brandingQueryOptions)
				: Promise.resolve(),
			hasPermission(permissions, "authentication.write")
				? queryClient.ensureQueryData(ssoSettingsQueryOptions)
				: Promise.resolve(),
		]);
	},
	component: SettingsPage,
});

function SettingsPage() {
	const { t } = useTranslation("admin");
	const { data: permissions = [] } = useQuery(myPermissionsQueryOptions);
	const canManageBranding = hasPermission(permissions, "settings.write");
	const canManageSSO = hasPermission(permissions, "authentication.write");
	const showTabs = canManageBranding && canManageSSO;

	return (
		<div className="mx-auto flex w-full max-w-3xl flex-col gap-6 p-6">
			<header className="flex items-start gap-3">
				<div className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 ring-1 ring-primary/20">
					<Settings className="size-4 text-primary" />
				</div>
				<div>
					<h1 className="text-xl font-bold text-foreground">
						{t("settings.title")}
					</h1>
					<p className="mt-0.5 text-sm text-muted-foreground">
						{t("settings.description")}
					</p>
				</div>
			</header>

			{showTabs ? (
				<Tabs.Root defaultValue="branding">
					<Tabs.List className="mb-5 flex w-fit gap-1 rounded-lg bg-muted p-1">
						<Tabs.Tab
							value="branding"
							className="flex h-7 items-center gap-1.5 rounded-md px-3 text-xs font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring data-active:bg-background data-active:text-foreground data-active:shadow-sm"
						>
							<Palette className="size-3.5" />
							{t("settings.tabs.branding")}
						</Tabs.Tab>
						<Tabs.Tab
							value="sso"
							className="flex h-7 items-center gap-1.5 rounded-md px-3 text-xs font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring data-active:bg-background data-active:text-foreground data-active:shadow-sm"
						>
							<ShieldCheck className="size-3.5" />
							{t("settings.tabs.sso")}
						</Tabs.Tab>
					</Tabs.List>
					<Tabs.Panel value="branding">
						<BrandingSettings />
					</Tabs.Panel>
					<Tabs.Panel value="sso">
						<SSOSettings />
					</Tabs.Panel>
				</Tabs.Root>
			) : canManageBranding ? (
				<BrandingSettings />
			) : canManageSSO ? (
				<SSOSettings />
			) : null}
		</div>
	);
}
