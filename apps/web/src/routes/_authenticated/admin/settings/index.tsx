import { createFileRoute, redirect } from "@tanstack/react-router";
import { Palette } from "lucide-react";
import { useTranslation } from "react-i18next";
import { BrandingSettings } from "@/components/admin/settings/BrandingSettings";
import { myPermissionsQueryOptions } from "@/lib/admin-api";
import { hasPermission } from "@/lib/permissions";
import { brandingQueryOptions } from "@/lib/settings-api";

export const Route = createFileRoute("/_authenticated/admin/settings/")({
	beforeLoad: async ({ context: { queryClient } }) => {
		const permissions = await queryClient
			.fetchQuery(myPermissionsQueryOptions)
			.catch(() => [] as string[]);

		if (!hasPermission(permissions, "settings.write")) {
			throw redirect({ to: "/home" });
		}
	},
	loader: async ({ context: { queryClient } }) => {
		await queryClient.ensureQueryData(brandingQueryOptions);
	},
	component: SettingsPage,
});

function SettingsPage() {
	const { t } = useTranslation("admin");

	return (
		<div className="mx-auto flex w-full max-w-3xl flex-col gap-6 p-6">
			<header className="flex items-start gap-3">
				<div className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 ring-1 ring-primary/20">
					<Palette className="size-4 text-primary" />
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

			<BrandingSettings />
		</div>
	);
}
