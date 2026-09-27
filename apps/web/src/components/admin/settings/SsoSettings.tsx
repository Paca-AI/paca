import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Loader2, Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { InlineNotice } from "@/components/shared/inline-notice";
import { Badge } from "@/components/ui/badge";
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
import {
	deleteSsoProvider,
	type SsoProvider,
	ssoProvidersQueryOptions,
} from "@/lib/sso-api";

import { SsoProviderDialog } from "./SsoProviderDialog";

/** Admin management of the SSO / OIDC providers users can sign in with. */
export function SsoSettings() {
	const { t } = useTranslation("admin");
	const {
		data: providers,
		isPending,
		isError,
	} = useQuery(ssoProvidersQueryOptions);
	const [editing, setEditing] = useState<SsoProvider | undefined>();
	const [formOpen, setFormOpen] = useState(false);
	const [deleting, setDeleting] = useState<SsoProvider | undefined>();

	return (
		<div className="rounded-xl border border-border/60 bg-card p-6">
			<div className="mb-4 flex flex-wrap items-start justify-between gap-3">
				<div>
					<h3 className="font-[Syne] text-base font-semibold">
						{t("settings.sso.title")}
					</h3>
					<p className="mt-0.5 text-sm text-muted-foreground">
						{t("settings.sso.description")}
					</p>
				</div>
				<Button
					size="sm"
					className="gap-1.5"
					onClick={() => {
						setEditing(undefined);
						setFormOpen(true);
					}}
				>
					<Plus className="size-3.5" />
					{t("settings.sso.add")}
				</Button>
			</div>

			{isPending ? (
				<Loader2 className="size-4 animate-spin text-muted-foreground" />
			) : isError ? (
				<InlineNotice tone="error">
					{t("settings.sso.errors.loadFailed")}
				</InlineNotice>
			) : providers.length === 0 ? (
				<p className="rounded-lg border border-dashed border-border/60 px-4 py-6 text-center text-sm text-muted-foreground">
					{t("settings.sso.empty")}
				</p>
			) : (
				<ul className="divide-y divide-border/60 rounded-lg border border-border/60">
					{providers.map((p) => (
						<li key={p.id} className="flex items-center gap-3 px-4 py-3">
							<KeyRound className="size-4 shrink-0 text-muted-foreground" />
							<div className="min-w-0 flex-1">
								<div className="flex flex-wrap items-center gap-2">
									<span className="truncate text-sm font-medium">
										{p.display_name}
									</span>
									<Badge variant={p.enabled ? "secondary" : "outline"}>
										{p.enabled
											? t("settings.sso.enabled")
											: t("settings.sso.disabled")}
									</Badge>
								</div>
								<p className="truncate text-xs text-muted-foreground">
									{p.issuer_url}
								</p>
							</div>
							<Button
								variant="ghost"
								size="icon"
								aria-label={t("settings.sso.edit", { name: p.display_name })}
								onClick={() => {
									setEditing(p);
									setFormOpen(true);
								}}
							>
								<Pencil className="size-4" />
							</Button>
							<Button
								variant="ghost"
								size="icon"
								aria-label={t("settings.sso.delete", { name: p.display_name })}
								onClick={() => setDeleting(p)}
							>
								<Trash2 className="size-4" />
							</Button>
						</li>
					))}
				</ul>
			)}

			<SsoProviderDialog
				open={formOpen}
				onOpenChange={setFormOpen}
				provider={editing}
			/>
			<DeleteProviderDialog
				provider={deleting}
				onClose={() => setDeleting(undefined)}
			/>
		</div>
	);
}

function DeleteProviderDialog({
	provider,
	onClose,
}: {
	provider?: SsoProvider;
	onClose: () => void;
}) {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const mutation = useMutation({
		mutationFn: (id: string) => deleteSsoProvider(id),
		onSuccess: async () => {
			await queryClient.invalidateQueries({
				queryKey: ssoProvidersQueryOptions.queryKey,
			});
			await queryClient.invalidateQueries({ queryKey: ["sso"] });
			onClose();
		},
	});

	return (
		<Dialog
			open={provider !== undefined}
			onOpenChange={(open) => {
				if (!open) {
					mutation.reset();
					onClose();
				}
			}}
		>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle className="text-base">
						{t("settings.sso.deleteDialog.title")}
					</DialogTitle>
					<DialogDescription>
						{t("settings.sso.deleteDialog.description", {
							name: provider?.display_name,
						})}
					</DialogDescription>
				</DialogHeader>
				{mutation.isError ? (
					<InlineNotice tone="error">
						{t("settings.sso.errors.deleteFailed")}
					</InlineNotice>
				) : null}
				<DialogFooter>
					<DialogClose render={<Button variant="outline" />}>
						{t("settings.sso.dialog.cancel")}
					</DialogClose>
					<Button
						variant="destructive"
						disabled={mutation.isPending}
						onClick={() => provider && mutation.mutate(provider.id)}
					>
						{t("settings.sso.deleteDialog.confirm")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
