import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import { Bell, Key, ShieldCheck, User } from "lucide-react";
import { useTranslation } from "react-i18next";

import { usePluginAccess } from "@/lib/plugins/access";
import { usePluginRegistry } from "@/lib/plugins/registry";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/_authenticated/account")({
	component: AccountLayout,
});

/** The account area: one section per concern instead of one long page. */
function AccountLayout() {
	const { t } = useTranslation("profile");
	const { getRegistrations } = usePluginRegistry();
	const canAccess = usePluginAccess();
	// Plugins can contribute personal settings (e.g. notification
	// preferences); the section only exists while at least one does.
	const hasPreferences = getRegistrations("user.settings.tab")
		.filter(canAccess)
		.some((r) => !r.hidden);

	const items = [
		{ to: "/account", icon: User, label: t("nav.profile"), exact: true },
		{ to: "/account/security", icon: ShieldCheck, label: t("nav.security") },
		{ to: "/account/api-keys", icon: Key, label: t("nav.apiKeys") },
		...(hasPreferences
			? [
					{
						to: "/account/preferences",
						icon: Bell,
						label: t("nav.preferences"),
					},
				]
			: []),
	] as const;

	return (
		<div className="mx-auto flex w-full max-w-4xl flex-col gap-6 p-4 md:p-6">
			<header>
				<h1 className="text-xl font-semibold">{t("page.title")}</h1>
				<p className="mt-1 text-sm text-muted-foreground">
					{t("page.subtitle")}
				</p>
			</header>

			<div className="flex flex-col gap-6 md:flex-row md:items-start">
				<nav
					aria-label={t("page.title")}
					className="-mx-1 flex gap-1 overflow-x-auto px-1 md:sticky md:top-6 md:mx-0 md:w-48 md:shrink-0 md:flex-col md:overflow-visible md:px-0"
				>
					{items.map(({ to, icon: Icon, label, ...rest }) => (
						<Link
							key={to}
							to={to}
							activeOptions={{ exact: "exact" in rest }}
							className={cn(
								"flex shrink-0 items-center gap-2 rounded-md px-3 py-2 text-sm font-medium text-muted-foreground transition-colors",
								"hover:bg-muted hover:text-foreground",
							)}
							activeProps={{ className: "bg-muted text-foreground" }}
						>
							<Icon className="size-4" />
							{label}
						</Link>
					))}
				</nav>

				<main className="min-w-0 flex-1">
					<Outlet />
				</main>
			</div>
		</div>
	);
}
