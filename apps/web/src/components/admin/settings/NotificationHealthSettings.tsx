import { useQuery } from "@tanstack/react-query";
import { CircleCheck, CircleX, RefreshCw, TriangleAlert } from "lucide-react";
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
import {
	type DeliveryChannelHealth,
	type HealthStatus,
	notificationHealthQueryOptions,
} from "@/lib/notification-health-api";
import { cn } from "@/lib/utils";

const STATUS_STYLE: Record<HealthStatus, string> = {
	ok: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
	degraded: "bg-amber-500/10 text-amber-700 dark:text-amber-400",
	failing: "bg-destructive/10 text-destructive",
};

const STATUS_ICON = {
	ok: CircleCheck,
	degraded: TriangleAlert,
	failing: CircleX,
} as const;

function seconds(value: number | null, none: string): string {
	if (value === null) return none;
	return value < 10 ? `${value.toFixed(1)} s` : `${Math.round(value)} s`;
}

function ChannelRow({ c }: { c: DeliveryChannelHealth }) {
	const { t } = useTranslation("admin");
	const none = t("settings.notifications.noData");
	return (
		<tr className="border-t align-top">
			<th scope="row" className="py-2 pr-3 text-left font-medium">
				{t(`settings.notifications.channelNames.${c.channel}`)}
			</th>
			<td className="px-3 py-2 tabular-nums">{c.pending}</td>
			<td
				className={cn(
					"px-3 py-2 tabular-nums",
					c.overdue > 0 && "font-medium text-destructive",
				)}
			>
				{c.overdue}
			</td>
			<td className="px-3 py-2 tabular-nums">{c.sent_last_hour}</td>
			<td
				className={cn(
					"px-3 py-2 tabular-nums",
					c.failed_last_hour > 0 && "font-medium text-destructive",
				)}
			>
				{c.failed_last_hour}
			</td>
			<td className="px-3 py-2 tabular-nums">
				{seconds(c.latency_p50_seconds, none)}
			</td>
			<td className="py-2 pl-3 tabular-nums">
				{seconds(c.latency_p95_seconds, none)}
			</td>
		</tr>
	);
}

/** Whether notifications are getting through: the delivery outbox, how long
 *  delivery takes, and what the stream consumers have given up on. */
export function NotificationHealthSettings() {
	const { t, i18n } = useTranslation("admin");
	const { data, isPending, isError, isFetching, refetch } = useQuery(
		notificationHealthQueryOptions,
	);

	return (
		<Card>
			<CardHeader>
				<div className="flex flex-wrap items-start justify-between gap-3">
					<div>
						<CardTitle>{t("settings.notifications.title")}</CardTitle>
						<CardDescription className="mt-1">
							{t("settings.notifications.description")}
						</CardDescription>
					</div>
					<Button
						size="sm"
						variant="outline"
						disabled={isFetching}
						onClick={() => void refetch()}
					>
						<RefreshCw
							data-icon="inline-start"
							className={cn(isFetching && "animate-spin")}
						/>
						{t("settings.notifications.refresh")}
					</Button>
				</div>
			</CardHeader>
			<CardContent className="flex flex-col gap-5">
				{isPending ? (
					<Skeleton className="h-24 w-full" />
				) : isError ? (
					<InlineNotice tone="error">
						{t("settings.notifications.loadError")}
					</InlineNotice>
				) : (
					<>
						<div
							className={cn(
								"flex flex-col gap-2 rounded-lg px-4 py-3",
								STATUS_STYLE[data.status],
							)}
						>
							{(() => {
								const Icon = STATUS_ICON[data.status];
								return (
									<p className="flex items-center gap-2 text-sm font-semibold">
										<Icon className="size-4" aria-hidden="true" />
										{t(`settings.notifications.status.${data.status}`)}
									</p>
								);
							})()}
							{data.reasons.length > 0 ? (
								<ul className="list-disc space-y-0.5 pl-5 text-sm">
									{data.reasons.map((r) => (
										<li key={r}>{r}</li>
									))}
								</ul>
							) : null}
						</div>

						<section className="flex flex-col gap-2">
							<h3 className="text-sm font-semibold">
								{t("settings.notifications.channels")}
							</h3>
							{data.channels.length === 0 ? (
								<p className="text-sm text-muted-foreground">
									{t("settings.notifications.noDeliveries")}
								</p>
							) : (
								<div className="overflow-x-auto">
									<table className="w-full text-sm">
										<thead className="text-xs text-muted-foreground">
											<tr>
												<th scope="col" className="pr-3 text-left font-medium">
													{t("settings.notifications.columns.channel")}
												</th>
												<th scope="col" className="px-3 text-left font-medium">
													{t("settings.notifications.columns.pending")}
												</th>
												<th scope="col" className="px-3 text-left font-medium">
													{t("settings.notifications.columns.overdue")}
												</th>
												<th scope="col" className="px-3 text-left font-medium">
													{t("settings.notifications.columns.sent")}
												</th>
												<th scope="col" className="px-3 text-left font-medium">
													{t("settings.notifications.columns.failed")}
												</th>
												<th scope="col" className="px-3 text-left font-medium">
													{t("settings.notifications.columns.p50")}
												</th>
												<th scope="col" className="pl-3 text-left font-medium">
													{t("settings.notifications.columns.p95")}
												</th>
											</tr>
										</thead>
										<tbody>
											{data.channels.map((c) => (
												<ChannelRow key={c.channel} c={c} />
											))}
										</tbody>
									</table>
								</div>
							)}
							{data.channels.flatMap((c) =>
								c.top_errors.map((e) => (
									<p
										key={`${c.channel}-${e.error}`}
										className="text-xs text-muted-foreground"
									>
										{t("settings.notifications.errorLine", {
											channel: t(
												`settings.notifications.channelNames.${c.channel}`,
											),
											count: e.count,
											error: e.error || t("settings.notifications.noReason"),
										})}
									</p>
								)),
							)}
						</section>

						{data.streams.length > 0 ? (
							<section className="flex flex-col gap-2">
								<h3 className="text-sm font-semibold">
									{t("settings.notifications.streams")}
								</h3>
								<div className="overflow-x-auto">
									<table className="w-full text-sm">
										<thead className="text-xs text-muted-foreground">
											<tr>
												<th scope="col" className="pr-3 text-left font-medium">
													{t("settings.notifications.columns.stream")}
												</th>
												<th scope="col" className="px-3 text-left font-medium">
													{t("settings.notifications.columns.unacked")}
												</th>
												<th scope="col" className="px-3 text-left font-medium">
													{t("settings.notifications.columns.oldestWait")}
												</th>
												<th scope="col" className="pl-3 text-left font-medium">
													{t("settings.notifications.columns.deadLetters")}
												</th>
											</tr>
										</thead>
										<tbody>
											{data.streams.map((s) => (
												<tr key={s.stream} className="border-t align-top">
													<th
														scope="row"
														className="py-2 pr-3 text-left font-mono text-xs font-medium"
													>
														{s.stream}
													</th>
													<td className="px-3 py-2 tabular-nums">
														{s.pending}
													</td>
													<td className="px-3 py-2 tabular-nums">
														{s.pending > 0
															? `${s.oldest_pending_idle_seconds} s`
															: t("settings.notifications.noData")}
													</td>
													<td
														className={cn(
															"py-2 pl-3 tabular-nums",
															s.dead_last_hour > 0 &&
																"font-medium text-destructive",
														)}
													>
														{t("settings.notifications.deadCount", {
															recent: s.dead_last_hour,
															total: s.dead_total,
														})}
													</td>
												</tr>
											))}
										</tbody>
									</table>
								</div>
							</section>
						) : null}

						<p className="text-xs text-muted-foreground">
							{t("settings.notifications.checkedAt", {
								time: new Date(data.checked_at).toLocaleTimeString(
									i18n.language,
								),
							})}
						</p>
					</>
				)}
			</CardContent>
		</Card>
	);
}
