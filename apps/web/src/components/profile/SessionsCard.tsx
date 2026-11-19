import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Laptop, Smartphone } from "lucide-react";
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
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import {
	revokeOtherSessions,
	revokeSession,
	type Session,
	sessionsQueryOptions,
} from "@/lib/sessions-api";

type Target = { kind: "one"; session: Session } | { kind: "others" };

/** Within this window a session reads as "active now": its activity time only
 * advances when the access token rotates, so finer than this would mislead. */
const ACTIVE_NOW_MS = 20 * 60 * 1000;

function useDeviceName() {
	const { t } = useTranslation("profile");
	return (s: Pick<Session, "browser" | "os">) =>
		s.browser && s.os
			? t("security.sessions.deviceFull", { browser: s.browser, os: s.os })
			: s.browser || s.os || t("security.sessions.unknownDevice");
}

/** The devices this account is signed in on: when each was last active and
 * from where, with a way to sign any of them (or all but this one) out. */
export function SessionsCard() {
	const { t, i18n } = useTranslation("profile");
	const deviceName = useDeviceName();
	const { data: sessions, isPending, isError } = useQuery(sessionsQueryOptions);
	const [target, setTarget] = useState<Target | null>(null);

	const others = sessions?.filter((s) => !s.current) ?? [];

	const lastActive = (iso: string) => {
		const ago = Date.now() - new Date(iso).getTime();
		if (ago < ACTIVE_NOW_MS) return t("security.sessions.activeNow");
		const rtf = new Intl.RelativeTimeFormat(i18n.language, { numeric: "auto" });
		const minutes = Math.round(ago / 60000);
		if (minutes < 60) return rtf.format(-minutes, "minute");
		const hours = Math.round(minutes / 60);
		if (hours < 48) return rtf.format(-hours, "hour");
		return rtf.format(-Math.round(hours / 24), "day");
	};

	return (
		<Card>
			<CardHeader>
				<div className="flex flex-wrap items-start justify-between gap-3">
					<div>
						<CardTitle>{t("security.sessions.title")}</CardTitle>
						<CardDescription className="mt-1">
							{t("security.sessions.description")}
						</CardDescription>
					</div>
					{others.length > 0 ? (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setTarget({ kind: "others" })}
						>
							{t("security.sessions.revokeOthers")}
						</Button>
					) : null}
				</div>
			</CardHeader>
			<CardContent>
				{isPending ? (
					<Skeleton className="h-12 w-full" />
				) : isError ? (
					<InlineNotice tone="error">
						{t("security.sessions.errors.load")}
					</InlineNotice>
				) : sessions.length === 0 ? (
					<p className="text-sm text-muted-foreground">
						{t("security.sessions.empty")}
					</p>
				) : (
					<ul className="divide-y rounded-md border">
						{sessions.map((s) => {
							const Icon =
								s.os === "iOS" || s.os === "Android" ? Smartphone : Laptop;
							const name = deviceName(s);
							return (
								<li
									key={s.id}
									className="flex items-center justify-between gap-3 px-3 py-2.5"
								>
									<div className="flex min-w-0 items-center gap-3">
										<Icon className="size-4 shrink-0 text-muted-foreground" />
										<div className="min-w-0">
											<p className="flex items-center gap-2 truncate text-sm font-medium">
												{name}
												{s.current ? (
													<Badge variant="secondary">
														{t("security.sessions.current")}
													</Badge>
												) : null}
											</p>
											<p className="text-xs text-muted-foreground">
												{[
													s.ip,
													lastActive(s.last_active_at),
													t("security.sessions.signedIn", {
														date: new Date(s.created_at).toLocaleDateString(
															i18n.language,
														),
													}),
												]
													.filter(Boolean)
													.join(" · ")}
											</p>
										</div>
									</div>
									{s.current ? null : (
										<Button
											size="sm"
											variant="ghost"
											aria-label={t("security.sessions.revokeLabel", { name })}
											onClick={() => setTarget({ kind: "one", session: s })}
										>
											{t("security.sessions.revoke")}
										</Button>
									)}
								</li>
							);
						})}
					</ul>
				)}
			</CardContent>
			<RevokeDialog
				target={target}
				onClose={() => setTarget(null)}
				deviceName={deviceName}
			/>
		</Card>
	);
}

function RevokeDialog({
	target,
	onClose,
	deviceName,
}: {
	target: Target | null;
	onClose: () => void;
	deviceName: (s: Pick<Session, "browser" | "os">) => string;
}) {
	const { t } = useTranslation("profile");
	const queryClient = useQueryClient();
	const [failed, setFailed] = useState(false);

	const mutation = useMutation({
		mutationFn: async () => {
			if (!target) return;
			if (target.kind === "one") await revokeSession(target.session.id);
			else await revokeOtherSessions();
		},
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: ["auth", "sessions"] });
			setFailed(false);
			onClose();
		},
		onError: () => setFailed(true),
	});

	const close = () => {
		setFailed(false);
		mutation.reset();
		onClose();
	};

	return (
		<Dialog
			open={target !== null}
			onOpenChange={(open) => {
				if (!open) close();
			}}
		>
			<DialogContent className="max-w-sm">
				<DialogHeader>
					<DialogTitle>
						{target?.kind === "others"
							? t("security.sessions.revokeOthersTitle")
							: t("security.sessions.revokeTitle")}
					</DialogTitle>
					<DialogDescription>
						{target?.kind === "others"
							? t("security.sessions.revokeOthersDescription")
							: t("security.sessions.revokeDescription", {
									name: target ? deviceName(target.session) : "",
								})}
					</DialogDescription>
				</DialogHeader>
				{failed ? (
					<InlineNotice tone="error">
						{t("security.sessions.errors.generic")}
					</InlineNotice>
				) : null}
				<DialogFooter>
					<Button type="button" variant="outline" onClick={close}>
						{t("actions.cancel")}
					</Button>
					<Button
						type="button"
						variant="destructive"
						disabled={mutation.isPending}
						onClick={() => mutation.mutate()}
					>
						{t("security.sessions.confirm")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
