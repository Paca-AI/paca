import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { InlineNotice } from "@/components/shared/inline-notice";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import type { User } from "@/lib/admin-api";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	getUserAllowedCidrs,
	resetUserMfa,
	setUserAllowedCidrs,
} from "@/lib/mfa-api";

function parseList(text: string): string[] {
	return text
		.split(/[\n,]/)
		.map((s) => s.trim())
		.filter(Boolean);
}

/** One account's sign-in protections: where its password login may come
 * from, and a way to clear its two-step verification when the person has
 * lost both their device and their recovery codes. */
export function UserSecurityDialog({
	user,
	onClose,
}: {
	user: User;
	onClose: () => void;
}) {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const queryKey = ["admin", "user-cidrs", user.id];
	const cidrsQuery = useQuery({
		queryKey,
		queryFn: () => getUserAllowedCidrs(user.id),
	});
	const [text, setText] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [resetDone, setResetDone] = useState(false);
	const [confirmingReset, setConfirmingReset] = useState(false);

	useEffect(() => {
		if (cidrsQuery.data) setText(cidrsQuery.data.join("\n"));
	}, [cidrsQuery.data]);

	const save = useMutation({
		mutationFn: () => setUserAllowedCidrs(user.id, parseList(text)),
		onSuccess: (cidrs) => {
			queryClient.setQueryData(queryKey, cidrs);
			setError(null);
			onClose();
		},
		onError: (err) =>
			setError(
				(
					{
						[ApiErrorCode.InvalidCidr]: t(
							"settings.security.errors.invalidCidr",
						),
						[ApiErrorCode.SelfLockout]: t(
							"settings.security.errors.selfLockout",
						),
					} as Record<string, string>
				)[getApiErrorCode(err) ?? ""] ??
					t("settings.security.errors.saveFailed"),
			),
	});
	const reset = useMutation({
		mutationFn: () => resetUserMfa(user.id),
		onSuccess: () => {
			setResetDone(true);
			setConfirmingReset(false);
		},
		onError: () => setError(t("users.security.mfa.resetFailed")),
	});

	const name = user.full_name || user.username;
	const dirty =
		parseList(text).join("\n") !== (cidrsQuery.data ?? []).join("\n");

	return (
		<Dialog open onOpenChange={(open) => !open && onClose()}>
			<DialogContent className="max-w-md">
				<DialogHeader>
					<DialogTitle>{t("users.security.title", { name })}</DialogTitle>
					<DialogDescription>
						{t("users.security.description")}
					</DialogDescription>
				</DialogHeader>

				<div className="space-y-5">
					<section className="space-y-1.5">
						<Label htmlFor="user-cidrs">{t("users.security.ip.label")}</Label>
						{cidrsQuery.isPending ? (
							<Skeleton className="h-24 w-full" />
						) : (
							<Textarea
								id="user-cidrs"
								rows={4}
								className="font-mono text-sm"
								placeholder={"203.0.113.0/24"}
								value={text}
								onChange={(e) => setText(e.target.value)}
							/>
						)}
						<p className="text-xs text-muted-foreground">
							{t("users.security.ip.hint")}
						</p>
					</section>

					<section className="space-y-2 border-t border-border/60 pt-4">
						<p className="text-sm font-medium">
							{t("users.security.mfa.title")}
						</p>
						<p className="text-sm text-muted-foreground">
							{t("users.security.mfa.description")}
						</p>
						{resetDone ? (
							<InlineNotice tone="info">
								{t("users.security.mfa.resetDone")}
							</InlineNotice>
						) : confirmingReset ? (
							<div className="flex items-center gap-2">
								<Button
									size="sm"
									variant="destructive"
									disabled={reset.isPending}
									onClick={() => reset.mutate()}
								>
									{reset.isPending ? (
										<Loader2 className="size-3.5 animate-spin" />
									) : null}
									{t("users.security.mfa.confirmReset")}
								</Button>
								<Button
									size="sm"
									variant="outline"
									onClick={() => setConfirmingReset(false)}
								>
									{t("users.security.cancel")}
								</Button>
							</div>
						) : (
							<Button
								size="sm"
								variant="outline"
								onClick={() => setConfirmingReset(true)}
							>
								{t("users.security.mfa.reset")}
							</Button>
						)}
					</section>

					{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}
				</div>

				<DialogFooter>
					<Button variant="outline" onClick={onClose}>
						{t("users.security.cancel")}
					</Button>
					<Button
						disabled={!dirty || save.isPending}
						onClick={() => save.mutate()}
					>
						{save.isPending ? (
							<Loader2 className="size-4 animate-spin" />
						) : null}
						{t("users.security.save")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
