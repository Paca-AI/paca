import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BellOff } from "lucide-react";
import { useTranslation } from "react-i18next";
import { InlineNotice } from "@/components/shared/inline-notice";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { dndEndTimes, isDoNotDisturbActive } from "@/lib/do-not-disturb";
import { pushSettingsQueryOptions, setDoNotDisturb } from "@/lib/push-api";

/** Do Not Disturb: silences push notifications (and the chime here) for a
 * while, or until it is turned off. The inbox and the bell's badge keep
 * counting. */
export function DoNotDisturbCard() {
	const { t, i18n } = useTranslation("profile");
	const queryClient = useQueryClient();
	const settings = useQuery(pushSettingsQueryOptions);
	const dnd = settings.data?.do_not_disturb;
	const active = isDoNotDisturbActive(dnd);

	const change = useMutation({
		mutationFn: setDoNotDisturb,
		onSuccess: () =>
			queryClient.invalidateQueries({
				queryKey: pushSettingsQueryOptions.queryKey,
			}),
	});

	const ends = dnd?.until
		? new Date(dnd.until).toLocaleString(i18n.language, {
				dateStyle: "medium",
				timeStyle: "short",
			})
		: null;
	const times = dndEndTimes();

	return (
		<Card>
			<CardHeader>
				<CardTitle className="flex items-center gap-2">
					<BellOff className="size-4" aria-hidden="true" />
					{t("preferences.dnd.title")}
				</CardTitle>
				<CardDescription>{t("preferences.dnd.description")}</CardDescription>
			</CardHeader>
			<CardContent className="flex flex-col gap-3">
				{settings.isPending ? (
					<Skeleton className="h-8 w-full" />
				) : settings.isError ? (
					<InlineNotice tone="error">
						{t("preferences.dnd.errors.load")}
					</InlineNotice>
				) : active ? (
					<div className="flex flex-wrap items-center justify-between gap-3">
						<p className="text-sm font-medium">
							{ends
								? t("preferences.dnd.onUntil", { time: ends })
								: t("preferences.dnd.onIndefinitely")}
						</p>
						<Button
							size="sm"
							variant="outline"
							disabled={change.isPending}
							onClick={() => change.mutate(null)}
						>
							{t("preferences.dnd.turnOff")}
						</Button>
					</div>
				) : (
					<div className="flex flex-wrap gap-2">
						<Button
							size="sm"
							variant="outline"
							disabled={change.isPending}
							onClick={() => change.mutate(times.oneHour)}
						>
							{t("preferences.dnd.forOneHour")}
						</Button>
						<Button
							size="sm"
							variant="outline"
							disabled={change.isPending}
							onClick={() => change.mutate(times.eightHours)}
						>
							{t("preferences.dnd.forEightHours")}
						</Button>
						<Button
							size="sm"
							variant="outline"
							disabled={change.isPending}
							onClick={() => change.mutate(times.nextMorning)}
						>
							{t("preferences.dnd.untilMorning")}
						</Button>
						<Button
							size="sm"
							variant="outline"
							disabled={change.isPending}
							onClick={() => change.mutate("indefinite")}
						>
							{t("preferences.dnd.untilOff")}
						</Button>
					</div>
				)}
				{change.isError ? (
					<InlineNotice tone="error">
						{t("preferences.dnd.errors.save")}
					</InlineNotice>
				) : null}
			</CardContent>
		</Card>
	);
}
