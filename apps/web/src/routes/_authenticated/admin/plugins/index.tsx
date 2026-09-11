import { createFileRoute, redirect } from "@tanstack/react-router";
import { Puzzle } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { PluginMarketplacePanel } from "@/components/plugins/PluginMarketplacePanel";
import { PluginPreferencesPanel } from "@/components/plugins/PluginPreferencesPanel";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { myPermissionsQueryOptions } from "@/lib/admin-api";
import { hasPermission } from "@/lib/permissions";

export const Route = createFileRoute("/_authenticated/admin/plugins/")({
	beforeLoad: async ({ context: { queryClient } }) => {
		const permissions = await queryClient
			.fetchQuery(myPermissionsQueryOptions)
			.catch(() => [] as string[]);

		// plugins.write replaced users.write as a rough "is this someone
		// important" proxy once it got its own dedicated permission — see
		// authz.PermissionPluginsRead's doc comment on the Go side. This
		// gate was never updated when that happened.
		if (!hasPermission(permissions, "plugins.write")) {
			throw redirect({ to: "/home" });
		}
	},
	component: PluginSettingsPage,
});

function PluginSettingsPage() {
	const { t } = useTranslation("admin");
	const [tab, setTab] = useState<"marketplace" | "layout">("marketplace");

	return (
		<div className="flex flex-col gap-6 p-6 max-w-3xl w-full mx-auto">
			{/* Page header */}
			<div>
				<div className="flex items-center gap-2">
					<Puzzle className="size-5 text-primary" />
					<h1 className="text-xl font-semibold">{t("plugins.title")}</h1>
				</div>
				<p className="mt-1 text-sm text-muted-foreground">
					{t("plugins.description")}
				</p>
			</div>

			<Separator />

			<div className="flex items-center gap-2 rounded-lg border border-border/60 bg-muted/30 p-1 w-fit">
				<Button
					type="button"
					size="sm"
					variant={tab === "marketplace" ? "secondary" : "ghost"}
					onClick={() => setTab("marketplace")}
				>
					{t("plugins.tabs.marketplace")}
				</Button>
				<Button
					type="button"
					size="sm"
					variant={tab === "layout" ? "secondary" : "ghost"}
					onClick={() => setTab("layout")}
				>
					{t("plugins.tabs.layout")}
				</Button>
			</div>

			{tab === "marketplace" ? (
				<Card>
					<CardHeader>
						<CardTitle className="text-base">
							{t("plugins.marketplace.title")}
						</CardTitle>
						<CardDescription>
							{t("plugins.marketplace.description")}
						</CardDescription>
					</CardHeader>
					<CardContent>
						<PluginMarketplacePanel />
					</CardContent>
				</Card>
			) : null}

			{tab === "layout" ? (
				<Card>
					<CardHeader>
						<CardTitle className="text-base">
							{t("plugins.layout.title")}
						</CardTitle>
						<CardDescription>{t("plugins.layout.description")}</CardDescription>
					</CardHeader>
					<CardContent>
						<PluginPreferencesPanel />
					</CardContent>
				</Card>
			) : null}
		</div>
	);
}
