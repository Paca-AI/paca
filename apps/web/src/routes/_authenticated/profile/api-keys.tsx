import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Copy, Key, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { Trans, useTranslation } from "react-i18next";
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
import { Skeleton } from "@/components/ui/skeleton";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import {
	apiKeysQueryOptions,
	type CreateAPIKeyResponse,
	createAPIKey,
	revokeAPIKey,
} from "@/lib/apikey-api";

export const Route = createFileRoute("/_authenticated/profile/api-keys")({
	component: APIKeysPage,
});

function formatDate(iso: string | null, locale?: string): string {
	if (!iso) return "—";
	return new Date(iso).toLocaleDateString(locale, {
		year: "numeric",
		month: "short",
		day: "numeric",
	});
}

function APIKeysPage() {
	const { t, i18n } = useTranslation();
	const queryClient = useQueryClient();
	const { data: keys = [], isLoading } = useQuery(apiKeysQueryOptions);

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
				setCreateError(t("profile.apiKeys.errNameEmpty"));
			} else if (code === "API_KEY_NAME_TOO_LONG") {
				setCreateError(t("profile.apiKeys.errNameTooLong"));
			} else {
				setCreateError(t("profile.apiKeys.errCreateFailed"));
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
		if (!navigator.clipboard?.writeText) {
			window.alert(t("profile.apiKeys.errCopyFailed"));
			return;
		}
		navigator.clipboard
			.writeText(revealedKey.key)
			.then(() => {
				setCopied(true);
				setTimeout(() => setCopied(false), 2000);
			})
			.catch(() => {
				setCopied(false);
				window.alert(t("profile.apiKeys.errCopyFailed"));
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
		<div className="max-w-3xl mx-auto flex flex-col gap-6 p-4 md:p-6">
			<div>
				<h1 className="text-2xl font-semibold tracking-tight">
					{t("profile.apiKeys.title")}
				</h1>
				<p className="text-sm text-muted-foreground mt-1">
					{t("profile.apiKeys.subtitle")}
				</p>
			</div>

			<Card>
				<CardHeader className="flex flex-row items-center justify-between pb-2">
					<div>
						<CardTitle className="text-base">
							{t("profile.apiKeys.yourKeys")}
						</CardTitle>
						<CardDescription>
							{t("profile.apiKeys.yourKeysDesc")}
						</CardDescription>
					</div>
					<Button size="sm" onClick={() => setCreateOpen(true)}>
						<Plus className="size-4 mr-1.5" />
						{t("profile.apiKeys.newKey")}
					</Button>
				</CardHeader>

				<CardContent>
					{isLoading ? (
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>{t("profile.apiKeys.colName")}</TableHead>
									<TableHead>{t("profile.apiKeys.colPrefix")}</TableHead>
									<TableHead>{t("profile.apiKeys.colCreated")}</TableHead>
									<TableHead>{t("profile.apiKeys.colExpires")}</TableHead>
									<TableHead>{t("profile.apiKeys.colLastUsed")}</TableHead>
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
								{t("profile.apiKeys.noKeys")}
							</p>
						</div>
					) : (
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>{t("profile.apiKeys.colName")}</TableHead>
									<TableHead>{t("profile.apiKeys.colPrefix")}</TableHead>
									<TableHead>{t("profile.apiKeys.colCreated")}</TableHead>
									<TableHead>{t("profile.apiKeys.colExpires")}</TableHead>
									<TableHead>{t("profile.apiKeys.colLastUsed")}</TableHead>
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
										<TableCell>
											{formatDate(key.created_at, i18n.language)}
										</TableCell>
										<TableCell>
											{formatDate(key.expires_at, i18n.language)}
										</TableCell>
										<TableCell>
											{formatDate(key.last_used_at, i18n.language)}
										</TableCell>
										<TableCell>
											<Button
												variant="ghost"
												size="icon"
												className="size-8 text-muted-foreground hover:text-destructive"
												aria-label={t("profile.apiKeys.revokeAria")}
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
					)}
				</CardContent>
			</Card>

			{/* Create key dialog */}
			<Dialog open={createOpen} onOpenChange={handleCreateClose}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle>{t("profile.apiKeys.createTitle")}</DialogTitle>
						<DialogDescription>
							{t("profile.apiKeys.createDesc")}
						</DialogDescription>
					</DialogHeader>

					<div className="flex flex-col gap-4 py-2">
						<div className="flex flex-col gap-1.5">
							<Label htmlFor="key-name">{t("profile.apiKeys.nameLabel")}</Label>
							<Input
								id="key-name"
								placeholder={t("profile.apiKeys.namePlaceholder")}
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
								{t("profile.apiKeys.expiration")}{" "}
								<span className="text-muted-foreground font-normal">
									{t("profile.apiKeys.optional")}
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
							{t("profile.apiKeys.cancel")}
						</Button>
						<Button
							onClick={() => createMutation.mutate()}
							disabled={createMutation.isPending || !newKeyName.trim()}
						>
							{createMutation.isPending
								? t("profile.apiKeys.creating")
								: t("profile.apiKeys.createKey")}
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
						<DialogTitle>{t("profile.apiKeys.createdTitle")}</DialogTitle>
						<DialogDescription>
							{t("profile.apiKeys.createdDesc")}
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
								aria-label={t("profile.apiKeys.copyAria")}
							>
								<Copy className="size-4" />
							</Button>
						</div>
						{copied ? (
							<p className="text-xs text-green-600">
								{t("profile.apiKeys.copied")}
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
							{t("profile.apiKeys.done")}
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
						<DialogTitle>{t("profile.apiKeys.revokeTitle")}</DialogTitle>
						<DialogDescription>
							<Trans
								i18nKey="profile.apiKeys.revokeConfirm"
								values={{ name: revokeTarget?.name }}
								components={{ name: <strong /> }}
							/>
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button
							variant="outline"
							onClick={() => setRevokeTarget(null)}
							disabled={revokeMutation.isPending}
						>
							{t("profile.apiKeys.cancel")}
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
								? t("profile.apiKeys.revoking")
								: t("profile.apiKeys.revokeKey")}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
