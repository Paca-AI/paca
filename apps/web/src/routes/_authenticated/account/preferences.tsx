import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { DoNotDisturbCard } from "@/components/profile/DoNotDisturbCard";
import { PushNotificationsCard } from "@/components/profile/PushNotificationsCard";
import { currentUserQueryOptions } from "@/lib/auth-api";
import { ExtensionPoint } from "@/lib/plugins/extension-point";

export const Route = createFileRoute("/_authenticated/account/preferences")({
	component: PreferencesPage,
});

/** Personal notification settings: Web Push, plus whatever plugins contribute (e.g. email preferences). */
function PreferencesPage() {
	const { t } = useTranslation("profile");
	const { data: user } = useQuery(currentUserQueryOptions);

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h2 className="text-lg font-semibold">{t("preferences.title")}</h2>
				<p className="mt-0.5 text-sm text-muted-foreground">
					{t("preferences.subtitle")}
				</p>
			</div>
			<DoNotDisturbCard />
			<PushNotificationsCard />
			{user ? (
				<ExtensionPoint
					point="user.settings.tab"
					componentProps={{ userId: user.id }}
				/>
			) : null}
		</div>
	);
}
