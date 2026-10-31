import { useQuery } from "@tanstack/react-query";
import { ShieldCheck, ShieldOff } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { InlineNotice } from "@/components/shared/inline-notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { mfaStatusQueryOptions } from "@/lib/mfa-api";

import { type MfaCodeAction, MfaCodeDialog } from "./MfaCodeDialog";
import { MfaSetupDialog } from "./MfaSetupDialog";

/** Two-step verification: turn it on with an authenticator app, see how many
 * recovery codes are left, replace them, or turn it off. */
export function MfaCard() {
	const { t } = useTranslation("profile");
	const { data: status, isPending, isError } = useQuery(mfaStatusQueryOptions);
	const [setupOpen, setSetupOpen] = useState(false);
	const [codeAction, setCodeAction] = useState<MfaCodeAction | null>(null);

	return (
		<Card>
			<CardHeader>
				<div className="flex flex-wrap items-start justify-between gap-3">
					<div>
						<CardTitle className="flex items-center gap-2">
							{status?.enabled ? (
								<ShieldCheck className="size-4 text-emerald-600" />
							) : (
								<ShieldOff className="size-4 text-muted-foreground" />
							)}
							{t("security.mfa.title")}
						</CardTitle>
						<CardDescription className="mt-1">
							{t("security.mfa.description")}
						</CardDescription>
					</div>
					{status ? (
						<Badge variant={status.enabled ? "secondary" : "outline"}>
							{status.enabled ? t("security.mfa.on") : t("security.mfa.off")}
						</Badge>
					) : null}
				</div>
			</CardHeader>
			<CardContent className="space-y-4">
				{isPending ? (
					<Skeleton className="h-9 w-40" />
				) : isError ? (
					<InlineNotice tone="error">
						{t("security.mfa.errors.load")}
					</InlineNotice>
				) : status.enabled ? (
					<>
						<p className="text-sm text-muted-foreground">
							{t("security.mfa.recoveryLeft", {
								count: status.recovery_codes_left,
							})}
						</p>
						{status.recovery_codes_left <= 2 ? (
							<InlineNotice tone="warning">
								{t("security.mfa.recoveryLow")}
							</InlineNotice>
						) : null}
						<div className="flex flex-wrap gap-2">
							<Button
								size="sm"
								variant="outline"
								onClick={() => setCodeAction("regenerate")}
							>
								{t("security.mfa.regenerate.button")}
							</Button>
							<Button
								size="sm"
								variant="outline"
								onClick={() => setCodeAction("disable")}
							>
								{t("security.mfa.disable.button")}
							</Button>
						</div>
					</>
				) : (
					<Button size="sm" onClick={() => setSetupOpen(true)}>
						{t("security.mfa.enable")}
					</Button>
				)}
			</CardContent>

			<MfaSetupDialog open={setupOpen} onOpenChange={setSetupOpen} />
			<MfaCodeDialog action={codeAction} onClose={() => setCodeAction(null)} />
		</Card>
	);
}
