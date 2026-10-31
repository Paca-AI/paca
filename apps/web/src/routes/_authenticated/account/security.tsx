import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { ChangePasswordCard } from "@/components/profile/ChangePasswordCard";
import { MfaCard } from "@/components/profile/mfa/MfaCard";
import { currentUserQueryOptions } from "@/lib/auth-api";

export const Route = createFileRoute("/_authenticated/account/security")({
	component: SecurityPage,
});

function SecurityPage() {
	const { t } = useTranslation("profile");
	const { data: user } = useQuery(currentUserQueryOptions);

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h2 className="text-lg font-semibold">{t("security.title")}</h2>
				<p className="mt-0.5 text-sm text-muted-foreground">
					{t("security.subtitle")}
				</p>
			</div>
			<MfaCard />
			<ChangePasswordCard mustChange={user?.must_change_password ?? false} />
		</div>
	);
}
