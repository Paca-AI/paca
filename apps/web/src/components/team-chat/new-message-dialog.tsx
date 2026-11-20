import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import {
	type AccountKind,
	chatKeys,
	DM_SCOPE,
	directoryQueryOptions,
	startDM,
} from "@/lib/chat-api";
import { chatErrorKey } from "@/lib/chat-errors";
import { cn } from "@/lib/utils";
import { PeerAvatar } from "./peer-avatar";

const KINDS: AccountKind[] = ["user", "agent", "service_account"];

/**
 * Pick someone to message: a person, an agent or a service account, among those
 * the caller shares a project with. Choosing one opens the conversation, which
 * is the existing one when the pair already has it.
 */
export function NewMessageDialog({
	open,
	onOpenChange,
	projectId,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	/** The project the person is in, if any: the conversation then opens under
	 * it, so the sidebar keeps that project's context. */
	projectId?: string;
}) {
	const { t } = useTranslation("chat");
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const [kind, setKind] = useState<AccountKind>("user");
	const [text, setText] = useState("");
	const [q, setQ] = useState("");
	const [error, setError] = useState<string | null>(null);
	// The directory is searched a moment after typing stops. Keyed on the text, so
	// a pending search is cancelled when the text changes or the dialog resets;
	// a late one could otherwise leave a stale query behind.
	useEffect(() => {
		const id = setTimeout(() => setQ(text), 250);
		return () => clearTimeout(id);
	}, [text]);

	const results = useQuery({
		...directoryQueryOptions(q, kind),
		enabled: open,
	});
	const start = useMutation({
		mutationFn: (accountId: string) => startDM(accountId),
		onSuccess: (c) => {
			void queryClient.invalidateQueries({
				queryKey: chatKeys.channels(DM_SCOPE),
			});
			onOpenChange(false);
			if (projectId) {
				void navigate({
					to: "/projects/$projectId/dm/$channelId",
					params: { projectId, channelId: c.id },
				});
			} else {
				void navigate({
					to: "/chat/dm/$channelId",
					params: { channelId: c.id },
				});
			}
		},
		onError: (e) => setError(t(`errors.${chatErrorKey(e)}`)),
	});

	// However it closed (dismissed, or after a conversation was opened), the next
	// opening starts clean.
	useEffect(() => {
		if (open) return;
		setKind("user");
		setText("");
		setQ("");
		setError(null);
	}, [open]);

	const rows = results.data ?? [];

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle>{t("dm.dialog.title")}</DialogTitle>
					<DialogDescription>{t("dm.dialog.description")}</DialogDescription>
				</DialogHeader>
				<Input
					value={text}
					placeholder={t("dm.dialog.search")}
					aria-label={t("dm.dialog.search")}
					onChange={(e) => {
						setText(e.target.value);
					}}
					autoFocus
				/>
				<div
					role="tablist"
					aria-label={t("dm.dialog.title")}
					className="flex gap-1.5"
				>
					{KINDS.map((k) => (
						<button
							key={k}
							type="button"
							role="tab"
							aria-selected={kind === k}
							onClick={() => setKind(k)}
							className={cn(
								"rounded-full border px-3 py-1 text-xs",
								kind === k
									? "border-primary bg-primary text-primary-foreground"
									: "text-muted-foreground hover:text-foreground",
							)}
						>
							{t(`dm.kinds.${k}`)}
						</button>
					))}
				</div>
				<div className="max-h-72 min-h-24 overflow-y-auto rounded-lg border">
					{results.isPending ? (
						<div className="flex justify-center p-6">
							<Spinner />
						</div>
					) : rows.length === 0 ? (
						<p className="p-6 text-center text-sm text-muted-foreground">
							{q ? t("dm.dialog.noMatch") : t("dm.dialog.empty")}
						</p>
					) : (
						<ul>
							{rows.map((p) => (
								<li key={p.account_id} className="border-b last:border-b-0">
									<Button
										variant="ghost"
										className="h-auto w-full justify-start gap-3 rounded-none px-3 py-2 text-left"
										disabled={start.isPending}
										onClick={() => {
											setError(null);
											start.mutate(p.account_id);
										}}
									>
										<PeerAvatar name={p.name} kind={p.kind} size="default" />
										<span className="min-w-0 flex-1">
											<span className="block truncate text-sm font-medium">
												{p.name}
											</span>
											{p.description ? (
												<span className="block truncate text-xs font-normal text-muted-foreground">
													{p.description}
												</span>
											) : null}
										</span>
									</Button>
								</li>
							))}
						</ul>
					)}
				</div>
				{kind !== "user" ? (
					<p className="text-xs text-muted-foreground">
						{t(`dm.hints.${kind}`)}
					</p>
				) : null}
				{error ? (
					<p role="alert" className="text-sm text-destructive">
						{error}
					</p>
				) : null}
			</DialogContent>
		</Dialog>
	);
}
