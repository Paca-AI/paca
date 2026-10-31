import {
	keepPreviousData,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Copy, Key, Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Pagination } from "@/components/ui/pagination";
import { Skeleton } from "@/components/ui/skeleton";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { useTablePaging } from "@/hooks/use-table-paging";
import {
	apiKeysPageQueryOptions,
	type CreateAPIKeyResponse,
	createAPIKey,
	revokeAPIKey,
} from "@/lib/apikey-api";
import { copyToClipboard } from "@/lib/clipboard";
import { formatDate as formatDateLocale } from "@/lib/format-date";

export const Route = createFileRoute("/_authenticated/account/api-keys")({
	component: APIKeysPage,
});

function formatDate(iso: string | null): string {
	if (!iso) return "—";
	return formatDateLocale(iso, {
		year: "numeric",
		month: "short",
		day: "numeric",
	});
}

function APIKeysPage() {
	const { t } = useTranslation("profile");
	const queryClient = useQueryClient();
	const { page, pageSize, setPage, setPageSize } = useTablePaging("api-keys");
	const keysQuery = useQuery({
		...apiKeysPageQueryOptions(page, pageSize),
		placeholderData: keepPreviousData,
	});
	const { isLoading } = keysQuery;
	const keys = keysQuery.data?.items ?? [];
	const keyTotal = keysQuery.data?.total_count ?? 0;
	const totalPages = Math.max(1, Math.ceil(keyTotal / pageSize));
	// Revoking the last key of the last page leaves it empty: step back.
	useEffect(() => {
		if (keysQuery.data && page > totalPages) setPage(totalPages);
	}, [keysQuery.data, page, totalPages, setPage]);

	// Create dialog state
	const [createOpen, setCreateOpen] = useState(false);
	const [newKeyName, setNewKeyName] = useState("");
	const [newKeyExpiry, setNewKeyExpiry] = useState("");
	const [createError, setCreateError] = useState<string | null>(null);

	// Reveal dialog state (shown once after creation)
	const [revealedKey, setRevealedKey] = useState<CreateAPIKeyResponse | null>(
		null,
	);
	const [copied, setCopied] = useState(false);

	// Revoke confirm dialog state
	const [revokeTarget, setRevokeTarget] = useState<{
		id: string;
		name: string;
	} | null>(null);

	const createMutation = useMutation({
		mutationFn: () =>
			createAPIKey({
				name: newKeyName.trim(),
				expires_at: newKeyExpiry ? `${newKeyExpiry}T00:00:00Z` : null,
			}),
		onSuccess: (result) => {
			queryClient.invalidateQueries({ queryKey: ["api-keys"] });
			setCreateOpen(false);
			setNewKeyName("");
			setNewKeyExpiry("");
			setCreateError(null);
			setRevealedKey(result);
		},
		onError: (err: { response?: { data?: { error_code?: string } } }) => {
			const code = err.response?.data?.error_code;
			if (code === "API_KEY_NAME_INVALID") {
				setCreateError(t("apiKeys.errors.nameRequired"));
			} else if (code === "API_KEY_NAME_TOO_LONG") {
				setCreateError(t("apiKeys.errors.nameTooLong"));
			} else {
				setCreateError(t("apiKeys.errors.createFailed"));
			}
		},
	});

	const revokeMutation = useMutation({
		mutationFn: (id: string) => revokeAPIKey(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["api-keys"] });
			setRevokeTarget(null);
		},
	});

	function handleCopy() {
		if (!revealedKey) return;
		void copyToClipboard(revealedKey.key).then((ok) => {
			if (!ok) {
				setCopied(false);
				window.alert(t("apiKeys.errors.copyFailed"));
				return;
			}
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
		});
	}

	function handleCreateClose(open: boolean) {
		if (!open) {
			setNewKeyName("");
			setNewKeyExpiry("");
			setCreateError(null);
		}
		setCreateOpen(open);
	}

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h2 className="text-lg font-semibold">{t("apiKeys.title")}</h2>
				<p className="text-sm text-muted-foreground mt-0.5">
					{t("apiKeys.subtitle")}
				</p>
			</div>

			<Card>
				<CardHeader className="flex flex-row items-center justify-between pb-2">
					<div>
						<CardTitle className="text-base">
							{t("apiKeys.yourKeys.title")}
						</CardTitle>
						<CardDescription>
							{t("apiKeys.yourKeys.description")}
						</CardDescription>
					</div>
					<Button size="sm" onClick={() => setCreateOpen(true)}>
						<Plus className="size-4 mr-1.5" />
						{t("apiKeys.actions.newKey")}
					</Button>
				</CardHeader>

				<CardContent>
					{isLoading ? (
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>{t("apiKeys.table.name")}</TableHead>
									<TableHead>{t("apiKeys.table.prefix")}</TableHead>
									<TableHead>{t("apiKeys.table.created")}</TableHead>
									<TableHead>{t("apiKeys.table.expires")}</TableHead>
									<TableHead>{t("apiKeys.table.lastUsed")}</TableHead>
									<TableHead className="w-10" />
								</TableRow>
							</TableHeader>
							<TableBody>
								{[...Array(3)].map((_, i) => (
									<TableRow
										// biome-ignore lint/suspicious/noArrayIndexKey: static skeleton
										key={i}
									>
										<TableCell>
											<Skeleton className="h-4 w-28" />
										</TableCell>
										<TableCell>
											<Skeleton className="h-4 w-24" />
										</TableCell>
										<TableCell>
											<Skeleton className="h-4 w-20" />
										</TableCell>
										<TableCell>
											<Skeleton className="h-4 w-20" />
										</TableCell>
										<TableCell>
											<Skeleton className="h-4 w-20" />
										</TableCell>
										<TableCell>
											<Skeleton className="size-8 rounded-md" />
										</TableCell>
									</TableRow>
								))}
							</TableBody>
						</Table>
					) : keys.length === 0 ? (
						<div className="flex flex-col items-center gap-2 py-10 text-center">
							<Key className="size-8 text-muted-foreground/50" />
							<p className="text-sm text-muted-foreground">
								{t("apiKeys.empty")}
							</p>
						</div>
					) : (
						<>
							<Table>
								<TableHeader>
									<TableRow>
										<TableHead>{t("apiKeys.table.name")}</TableHead>
										<TableHead>{t("apiKeys.table.prefix")}</TableHead>
										<TableHead>{t("apiKeys.table.created")}</TableHead>
										<TableHead>{t("apiKeys.table.expires")}</TableHead>
										<TableHead>{t("apiKeys.table.lastUsed")}</TableHead>
										<TableHead className="w-10" />
									</TableRow>
								</TableHeader>
								<TableBody>
									{keys.map((key) => (
										<TableRow key={key.id}>
											<TableCell className="font-medium">{key.name}</TableCell>
											<TableCell className="font-mono text-xs text-muted-foreground">
												paca_{key.key_prefix}…
											</TableCell>
											<TableCell>{formatDate(key.created_at)}</TableCell>
											<TableCell>{formatDate(key.expires_at)}</TableCell>
											<TableCell>{formatDate(key.last_used_at)}</TableCell>
											<TableCell>
												<Button
													variant="ghost"
													size="icon"
													className="size-8 text-muted-foreground hover:text-destructive"
													aria-label={t("apiKeys.actions.revokeKeyAria")}
													onClick={() =>
														setRevokeTarget({
															id: key.id,
															name: key.name,
														})
													}
												>
													<Trash2 className="size-4" />
												</Button>
											</TableCell>
										</TableRow>
									))}
								</TableBody>
							</Table>
							<Pagination
								className="border-t px-4 py-3"
								page={page}
								totalPages={totalPages}
								onPageChange={setPage}
								total={keyTotal}
								pageSize={pageSize}
								onPageSizeChange={setPageSize}
							/>
						</>
					)}
				</CardContent>
			</Card>

			{/* Create key dialog */}
			<Dialog open={createOpen} onOpenChange={handleCreateClose}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle>{t("apiKeys.createDialog.title")}</DialogTitle>
						<DialogDescription>
							{t("apiKeys.createDialog.description")}
						</DialogDescription>
					</DialogHeader>

					<div className="flex flex-col gap-4 py-2">
						<div className="flex flex-col gap-1.5">
							<Label htmlFor="key-name">
								{t("apiKeys.createDialog.nameLabel")}
							</Label>
							<Input
								id="key-name"
								placeholder={t("apiKeys.createDialog.namePlaceholder")}
								value={newKeyName}
								onChange={(e) => {
									setNewKeyName(e.target.value);
									setCreateError(null);
								}}
								autoFocus
							/>
						</div>

						<div className="flex flex-col gap-1.5">
							<Label htmlFor="key-expiry">
								{t("apiKeys.createDialog.expiryLabel")}{" "}
								<span className="text-muted-foreground font-normal">
									{t("apiKeys.createDialog.expiryOptional")}
								</span>
							</Label>
							<Input
								id="key-expiry"
								type="date"
								value={newKeyExpiry}
								onChange={(e) => setNewKeyExpiry(e.target.value)}
							/>
						</div>

						{createError ? (
							<p className="text-sm text-destructive">{createError}</p>
						) : null}
					</div>

					<DialogFooter>
						<Button
							variant="outline"
							onClick={() => handleCreateClose(false)}
							disabled={createMutation.isPending}
						>
							{t("apiKeys.actions.cancel")}
						</Button>
						<Button
							onClick={() => createMutation.mutate()}
							disabled={createMutation.isPending || !newKeyName.trim()}
						>
							{createMutation.isPending
								? t("apiKeys.actions.creating")
								: t("apiKeys.actions.createKey")}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>

			{/* One-time key reveal dialog */}
			<Dialog
				open={!!revealedKey}
				onOpenChange={(open) => {
					if (!open) {
						setRevealedKey(null);
						setCopied(false);
					}
				}}
			>
				<DialogContent className="sm:max-w-lg">
					<DialogHeader>
						<DialogTitle>{t("apiKeys.revealDialog.title")}</DialogTitle>
						<DialogDescription>
							{t("apiKeys.revealDialog.description")}
						</DialogDescription>
					</DialogHeader>

					<div className="flex flex-col gap-3 py-2">
						<p className="text-sm font-medium">{revealedKey?.name}</p>
						<div className="flex items-center gap-2">
							<code className="flex-1 text-xs bg-muted rounded-md px-3 py-2 break-all select-all font-mono">
								{revealedKey?.key}
							</code>
							<Button
								variant="outline"
								size="icon"
								className="shrink-0"
								onClick={handleCopy}
								aria-label={t("apiKeys.actions.copyKeyAria")}
							>
								<Copy className="size-4" />
							</Button>
						</div>
						{copied ? (
							<p className="text-xs text-green-600">
								{t("apiKeys.revealDialog.copied")}
							</p>
						) : null}
					</div>

					<DialogFooter>
						<Button
							onClick={() => {
								setRevealedKey(null);
								setCopied(false);
							}}
						>
							{t("apiKeys.actions.done")}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>

			{/* Revoke confirm dialog */}
			<Dialog
				open={!!revokeTarget}
				onOpenChange={(open) => {
					if (!open) setRevokeTarget(null);
				}}
			>
				<DialogContent className="sm:max-w-sm">
					<DialogHeader>
						<DialogTitle>{t("apiKeys.revokeDialog.title")}</DialogTitle>
						<DialogDescription>
							{t("apiKeys.revokeDialog.confirmPrefix")}{" "}
							<strong>{revokeTarget?.name}</strong>
							{t("apiKeys.revokeDialog.confirmSuffix")}
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button
							variant="outline"
							onClick={() => setRevokeTarget(null)}
							disabled={revokeMutation.isPending}
						>
							{t("apiKeys.actions.cancel")}
						</Button>
						<Button
							variant="destructive"
							disabled={revokeMutation.isPending}
							onClick={() => {
								if (revokeTarget) {
									revokeMutation.mutate(revokeTarget.id);
								}
							}}
						>
							{revokeMutation.isPending
								? t("apiKeys.actions.revoking")
								: t("apiKeys.actions.revokeKey")}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
