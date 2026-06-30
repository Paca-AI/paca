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

		if (!hasPermission(permissions, "users.write")) {
			throw redirect({ to: "/home" });
		}
	},
	component: PluginSettingsPage,
});

function PluginSettingsPage() {
	const { t } = useTranslation();
	const [tab, setTab] = useState<"marketplace" | "layout">("marketplace");

	return (
		<div className="flex flex-col gap-6 p-6 max-w-3xl w-full mx-auto">
			{/* Page header */}
			<div>
				<div className="flex items-center gap-2">
					<Puzzle className="size-5 text-primary" />
					<h1 className="text-xl font-semibold">{t("admin.plugins.title")}</h1>
				</div>
				<p className="mt-1 text-sm text-muted-foreground">
					{t("admin.plugins.subtitle")}
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
					{t("admin.plugins.tabMarketplace")}
				</Button>
				<Button
					type="button"
					size="sm"
					variant={tab === "layout" ? "secondary" : "ghost"}
					onClick={() => setTab("layout")}
				>
					{t("admin.plugins.tabLayout")}
				</Button>
			</div>

			{tab === "marketplace" ? (
				<Card>
					<CardHeader>
						<CardTitle className="text-base">
							{t("admin.plugins.marketplaceTitle")}
						</CardTitle>
						<CardDescription>
							{t("admin.plugins.marketplaceDesc")}
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
							{t("admin.plugins.layoutTitle")}
						</CardTitle>
						<CardDescription>{t("admin.plugins.layoutDesc")}</CardDescription>
					</CardHeader>
					<CardContent>
						<PluginPreferencesPanel />
					</CardContent>
				</Card>
			) : null}
		</div>
	);
}
