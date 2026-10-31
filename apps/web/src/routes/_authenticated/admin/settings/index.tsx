import { createFileRoute, Link, redirect } from "@tanstack/react-router";
import { KeyRound, Lock, Palette } from "lucide-react";
import { useTranslation } from "react-i18next";

import { BrandingSettings } from "@/components/admin/settings/BrandingSettings";
import { LoginSecuritySettings } from "@/components/admin/settings/LoginSecuritySettings";
import { SsoSettings } from "@/components/admin/settings/SsoSettings";
import { myPermissionsQueryOptions } from "@/lib/admin-api";
import { hasPermission } from "@/lib/permissions";
import { brandingQueryOptions } from "@/lib/settings-api";
import { cn } from "@/lib/utils";

type Section = "branding" | "sso" | "security";

const SECTIONS: Section[] = ["branding", "sso", "security"];

export const Route = createFileRoute("/_authenticated/admin/settings/")({
	validateSearch: (search: Record<string, unknown>): { section?: Section } => ({
		section: SECTIONS.find((s) => s === search.section),
	}),
	beforeLoad: async ({ context: { queryClient } }) => {
		const permissions = await queryClient
			.fetchQuery(myPermissionsQueryOptions)
			.catch(() => [] as string[]);

		const can = {
			branding: hasPermission(permissions, "settings:write"),
			sso: hasPermission(permissions, "settings.sso:write"),
			security: hasPermission(permissions, "settings.security:write"),
		};
		if (!can.branding && !can.sso && !can.security) {
			throw redirect({ to: "/home" });
		}
		return { can };
	},
	loader: async ({ context: { queryClient } }) => {
		await queryClient.ensureQueryData(brandingQueryOptions);
	},
	component: SettingsPage,
});

const ICONS = { branding: Palette, sso: KeyRound, security: Lock } as const;

function SettingsPage() {
	const { t } = useTranslation("admin");
	const { can } = Route.useRouteContext();
	const { section } = Route.useSearch();

	// Only the sections this admin may change, grouped by what they are for.
	const visible = SECTIONS.filter((s) => can[s]);
	const active: Section = section && can[section] ? section : visible[0];

	const groups: { label: string; items: Section[] }[] = [
		{
			label: t("settings.nav.workspace"),
			items: visible.filter((s) => s === "branding"),
		},
		{
			label: t("settings.nav.signIn"),
			items: visible.filter((s) => s === "sso" || s === "security"),
		},
	].filter((g) => g.items.length > 0);

	return (
		<div className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4 md:p-6">
			<header>
				<h1 className="text-xl font-bold text-foreground">
					{t("settings.title")}
				</h1>
				<p className="mt-0.5 text-sm text-muted-foreground">
					{t("settings.description")}
				</p>
			</header>

			<div className="flex flex-col gap-6 md:flex-row md:items-start">
				<nav
					aria-label={t("settings.title")}
					className="flex gap-4 overflow-x-auto md:sticky md:top-6 md:w-52 md:shrink-0 md:flex-col md:gap-5 md:overflow-visible"
				>
					{groups.map((group) => (
						<div key={group.label} className="flex shrink-0 flex-col gap-1">
							<p className="px-3 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
								{group.label}
							</p>
							<div className="flex gap-1 md:flex-col">
								{group.items.map((s) => {
									const Icon = ICONS[s];
									return (
										<Link
											key={s}
											to="/admin/settings"
											search={{ section: s }}
											aria-current={s === active ? "page" : undefined}
											className={cn(
												"flex shrink-0 items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-muted hover:text-foreground",
												s === active
													? "bg-muted text-foreground"
													: "text-muted-foreground",
											)}
										>
											<Icon className="size-4" />
											{t(`settings.nav.${s}`)}
										</Link>
									);
								})}
							</div>
						</div>
					))}
				</nav>

				<main className="min-w-0 flex-1">
					{active === "branding" ? <BrandingSettings /> : null}
					{active === "sso" ? <SsoSettings /> : null}
					{active === "security" ? <LoginSecuritySettings /> : null}
				</main>
			</div>
		</div>
	);
}
