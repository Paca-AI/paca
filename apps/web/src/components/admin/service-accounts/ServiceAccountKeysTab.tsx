import {
	useInfiniteQuery,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { Copy, KeyRound, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { LoadMoreSentinel } from "@/components/shared/load-more-sentinel";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/toast";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import type { APIKey, CreateAPIKeyResponse } from "@/lib/apikey-api";
import { copyToClipboard } from "@/lib/clipboard";
import { formatDate as formatDateLocale } from "@/lib/format-date";
import { notifyError } from "@/lib/notify";
import {
	createServiceAccountKey,
	revokeServiceAccountKey,
	type ServiceAccount,
	serviceAccountKeysInfiniteQueryOptions,
} from "@/lib/service-account-api";

function formatDate(iso: string): string {
	return formatDateLocale(iso, {
		year: "numeric",
		month: "short",
		day: "numeric",
	});
}

// ── Create dialog ─────────────────────────────────────────────────────────────

function CreateKeyDialog({
	account,
	open,
	onOpenChange,
	onCreated,
}: {
	account: ServiceAccount;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	/** Receives the new key with its secret, which is shown only this once. */
	onCreated: (key: CreateAPIKeyResponse) => void;
}) {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const [name, setName] = useState("");
	const [expiry, setExpiry] = useState("");
	const [error, setError] = useState<string | null>(null);

	const reset = () => {
		setName("");
		setExpiry("");
		setError(null);
	};

	const mutation = useMutation({
		mutationFn: () =>
			createServiceAccountKey(account.id, {
				name: name.trim(),
				expires_at: expiry ? `${expiry}T00:00:00Z` : null,
			}),
		onSuccess: (result) => {
			void queryClient.invalidateQueries({
				queryKey: serviceAccountKeysInfiniteQueryOptions(account.id).queryKey,
			});
			reset();
			onOpenChange(false);
			onCreated(result);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.APIKeyNameInvalid) {
				setError(t("serviceAccounts.keys.errors.nameRequired"));
			} else if (code === ApiErrorCode.APIKeyNameTooLong) {
				setError(t("serviceAccounts.keys.errors.nameTooLong"));
			} else {
				setError(t("serviceAccounts.keys.errors.createFailed"));
			}
		},
	});

	return (
		<Dialog
			open={open}
			onOpenChange={(next) => {
				if (!next) reset();
				onOpenChange(next);
			}}
		>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle>
						{t("serviceAccounts.keys.createDialog.title")}
					</DialogTitle>
					<DialogDescription>
						{t("serviceAccounts.keys.createDialog.description", {
							name: account.name,
						})}
					</DialogDescription>
				</DialogHeader>
				<form
					className="flex flex-col gap-4 py-2"
					onSubmit={(e) => {
						e.preventDefault();
						if (name.trim()) mutation.mutate();
					}}
				>
					<div className="flex flex-col gap-1.5">
						<Label htmlFor="sa-key-name">
							{t("serviceAccounts.keys.nameLabel")}
						</Label>
						<Input
							id="sa-key-name"
							value={name}
							maxLength={100}
							placeholder={t("serviceAccounts.keys.namePlaceholder")}
							onChange={(e) => {
								setName(e.target.value);
								setError(null);
							}}
							autoFocus
						/>
					</div>
					<div className="flex flex-col gap-1.5">
						<Label htmlFor="sa-key-expiry">
							{t("serviceAccounts.keys.expiryLabel")}{" "}
							<span className="font-normal text-muted-foreground">
								{t("serviceAccounts.keys.expiryOptional")}
							</span>
						</Label>
						<Input
							id="sa-key-expiry"
							type="date"
							value={expiry}
							onChange={(e) => setExpiry(e.target.value)}
						/>
					</div>
					{error ? <p className="text-sm text-destructive">{error}</p> : null}
					<DialogFooter>
						<DialogClose
							render={
								<Button
									type="button"
									variant="outline"
									disabled={mutation.isPending}
								/>
							}
						>
							{t("serviceAccounts.keys.createDialog.cancel")}
						</DialogClose>
						<Button type="submit" disabled={mutation.isPending || !name.trim()}>
							{mutation.isPending
								? t("serviceAccounts.keys.creating")
								: t("serviceAccounts.keys.create")}
						</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	);
}

// ── Tab ───────────────────────────────────────────────────────────────────────

/**
 * A service account's API keys. A key is shown once, when it is created; only a
 * signed-in session can create or revoke keys (the server refuses an API key
 * doing it), so a leaked key can never create more credentials.
 */
export function ServiceAccountKeysTab({
	account,
	canWrite,
}: {
	account: ServiceAccount;
	/** Whether the viewer may create and revoke keys (service_accounts:write). */
	canWrite: boolean;
}) {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const queryKey = serviceAccountKeysInfiniteQueryOptions(account.id).queryKey;
	const keysQuery = useInfiniteQuery(
		serviceAccountKeysInfiniteQueryOptions(account.id),
	);
	const { isLoading } = keysQuery;
	const keys = keysQuery.data?.pages.flatMap((p) => p.items) ?? [];
	const keyCount = keysQuery.data?.pages[0]?.total_count ?? keys.length;

	const [createOpen, setCreateOpen] = useState(false);
	const [revealed, setRevealed] = useState<CreateAPIKeyResponse | null>(null);
	const [copied, setCopied] = useState(false);
	const [revokeTarget, setRevokeTarget] = useState<APIKey | null>(null);

	const revokeMutation = useMutation({
		meta: { success: "removed", subject: "serviceAccountKey" },
		mutationFn: (keyId: string) => revokeServiceAccountKey(account.id, keyId),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey });
			setRevokeTarget(null);
		},
		onError: (err) => {
			setRevokeTarget(null);
			notifyError(err, t("serviceAccounts.keys.errors.revokeFailed"));
		},
	});

	function handleCopy() {
		if (!revealed) return;
		void copyToClipboard(revealed.key, { notify: false }).then((ok) => {
			if (!ok) {
				toast.add({
					type: "error",
					title: t("serviceAccounts.keys.errors.copyFailed"),
				});
				return;
			}
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
		});
	}

	return (
		<div className="max-w-3xl space-y-4">
			<div className="flex items-center justify-between gap-3">
				<p className="text-sm text-muted-foreground">
					{t("serviceAccounts.keys.count", { count: keyCount })}
				</p>
				{canWrite ? (
					<Button size="sm" onClick={() => setCreateOpen(true)}>
						<Plus className="size-4 mr-1.5" />
						{t("serviceAccounts.keys.newKey")}
					</Button>
				) : null}
			</div>

			{isLoading ? (
				<div className="space-y-2">
					<Skeleton className="h-14 w-full rounded-lg" />
					<Skeleton className="h-14 w-full rounded-lg" />
				</div>
			) : keys.length === 0 ? (
				<div className="flex flex-col items-center justify-center gap-3 py-14 rounded-xl border border-dashed border-border">
					<KeyRound className="size-8 text-muted-foreground/40" />
					<p className="text-sm text-muted-foreground">
						{t("serviceAccounts.keys.empty")}
					</p>
					{canWrite ? (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setCreateOpen(true)}
						>
							<Plus className="size-3.5 mr-1" />
							{t("serviceAccounts.keys.createFirst")}
						</Button>
					) : null}
				</div>
			) : (
				<div className="space-y-2">
					{keys.map((key) => (
						<div
							key={key.id}
							className="flex items-center justify-between gap-3 rounded-lg border border-border/60 bg-card px-4 py-3"
						>
							<div className="flex min-w-0 items-center gap-3">
								<KeyRound className="size-4 shrink-0 text-muted-foreground" />
								<div className="min-w-0">
									<p className="truncate text-sm font-medium">
										{key.name}{" "}
										<span className="font-mono text-xs font-normal text-muted-foreground">
											paca_{key.key_prefix}…
										</span>
									</p>
									<p className="truncate text-xs text-muted-foreground">
										{t("serviceAccounts.keys.row.created", {
											date: formatDate(key.created_at),
										})}
										{" · "}
										{key.expires_at
											? t("serviceAccounts.keys.row.expires", {
													date: formatDate(key.expires_at),
												})
											: t("serviceAccounts.keys.row.noExpiry")}
										{" · "}
										{key.last_used_at
											? t("serviceAccounts.keys.row.lastUsed", {
													date: formatDate(key.last_used_at),
												})
											: t("serviceAccounts.keys.row.neverUsed")}
									</p>
								</div>
							</div>
							{canWrite ? (
								<Button
									variant="ghost"
									size="icon"
									className="size-7 shrink-0 text-muted-foreground hover:text-destructive"
									aria-label={t("serviceAccounts.keys.revokeAria", {
										name: key.name,
									})}
									onClick={() => setRevokeTarget(key)}
								>
									<Trash2 className="size-3.5" />
								</Button>
							) : null}
						</div>
					))}
					<LoadMoreSentinel
						hasMore={keysQuery.hasNextPage}
						isLoadingMore={keysQuery.isFetchingNextPage}
						isError={keysQuery.isFetchNextPageError}
						onLoadMore={() => void keysQuery.fetchNextPage()}
					/>
				</div>
			)}

			<CreateKeyDialog
				account={account}
				open={createOpen}
				onOpenChange={setCreateOpen}
				onCreated={setRevealed}
			/>

			<Dialog
				open={!!revealed}
				onOpenChange={(next) => {
					if (!next) {
						setRevealed(null);
						setCopied(false);
					}
				}}
			>
				<DialogContent className="sm:max-w-lg">
					<DialogHeader>
						<DialogTitle>{t("serviceAccounts.keys.reveal.title")}</DialogTitle>
						<DialogDescription>
							{t("serviceAccounts.keys.reveal.description")}
						</DialogDescription>
					</DialogHeader>
					<div className="flex flex-col gap-3 py-2">
						<p className="text-sm font-medium">{revealed?.name}</p>
						<div className="flex items-center gap-2">
							<code className="flex-1 select-all break-all rounded-md bg-muted px-3 py-2 font-mono text-xs">
								{revealed?.key}
							</code>
							<Button
								variant="outline"
								size="icon"
								className="shrink-0"
								onClick={handleCopy}
								aria-label={t("serviceAccounts.keys.reveal.copyAria")}
							>
								<Copy className="size-4" />
							</Button>
						</div>
						{copied ? (
							<p className="text-xs text-success">
								{t("serviceAccounts.keys.reveal.copied")}
							</p>
						) : null}
					</div>
					<DialogFooter>
						<Button
							onClick={() => {
								setRevealed(null);
								setCopied(false);
							}}
						>
							{t("serviceAccounts.keys.reveal.done")}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>

			<Dialog
				open={!!revokeTarget}
				onOpenChange={(next) => {
					if (!next) setRevokeTarget(null);
				}}
			>
				<DialogContent className="sm:max-w-sm">
					<DialogHeader>
						<DialogTitle>{t("serviceAccounts.keys.revoke.title")}</DialogTitle>
						<DialogDescription>
							{t("serviceAccounts.keys.revoke.description", {
								name: revokeTarget?.name,
							})}
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button
							variant="outline"
							onClick={() => setRevokeTarget(null)}
							disabled={revokeMutation.isPending}
						>
							{t("serviceAccounts.keys.revoke.cancel")}
						</Button>
						<Button
							variant="destructive"
							disabled={revokeMutation.isPending}
							onClick={() =>
								revokeTarget && revokeMutation.mutate(revokeTarget.id)
							}
						>
							{revokeMutation.isPending
								? t("serviceAccounts.keys.revoke.revoking")
								: t("serviceAccounts.keys.revoke.confirm")}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
