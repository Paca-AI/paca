import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
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
	deleteGitIdentity,
	type GitIdentity,
	gitErrorKey,
	gitQueriesKey,
} from "@/lib/git-api";

interface DeleteGitIdentityDialogProps {
	identity: GitIdentity;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	/** Removes the identity; defaults to the workspace-level removal. */
	remove?: () => Promise<void>;
	/**
	 * Repositories that use the connection: this project's by name, and how many
	 * more (in other projects) there are. They stay linked but stop working.
	 */
	usage?: { names: string[]; others: number };
	/** Query cache entry to refresh afterwards. */
	invalidateKey?: readonly unknown[];
}

export function DeleteGitIdentityDialog({
	identity,
	open,
	onOpenChange,
	remove,
	usage,
	invalidateKey = gitQueriesKey,
}: DeleteGitIdentityDialogProps) {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () => (remove ? remove() : deleteGitIdentity(identity.id)),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: invalidateKey });
			onOpenChange(false);
		},
		onError: (err: unknown) => {
			setError(t(`git.errors.${gitErrorKey(err)}` as never));
		},
	});

	return (
		<Dialog
			open={open}
			onOpenChange={(next) => {
				if (!next) setError(null);
				onOpenChange(next);
			}}
		>
			<DialogContent className="sm:max-w-sm">
				<DialogHeader>
					<div className="mb-1 flex size-9 items-center justify-center rounded-lg bg-destructive/10">
						<Trash2 className="size-4 text-destructive" />
					</div>
					<DialogTitle>{t("git.delete.title")}</DialogTitle>
					<DialogDescription className="mt-1">
						{t("git.delete.description", {
							name: identity.display_name || identity.handle,
							host: identity.host,
						})}
					</DialogDescription>
				</DialogHeader>

				{usage && usage.names.length + usage.others > 0 ? (
					<div
						role="note"
						className="flex flex-col gap-1.5 rounded-lg border border-amber-500/40 bg-amber-500/5 px-3 py-2.5 text-sm"
					>
						<p className="font-medium">
							{t("git.delete.usage.title", {
								count: usage.names.length + usage.others,
							})}
						</p>
						{usage.names.length > 0 ? (
							<ul className="list-inside list-disc text-muted-foreground">
								{usage.names.map((n) => (
									<li key={n} className="break-all">
										{n}
									</li>
								))}
							</ul>
						) : null}
						{usage.others > 0 ? (
							<p className="text-muted-foreground">
								{t("git.delete.usage.others", { count: usage.others })}
							</p>
						) : null}
						<p className="text-muted-foreground">
							{t("git.delete.usage.consequence")}
						</p>
					</div>
				) : null}

				{error ? (
					<div
						role="alert"
						className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"
					>
						<span className="shrink-0">⚠</span>
						<span>{error}</span>
					</div>
				) : null}

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
						{t("git.cancel")}
					</DialogClose>
					<Button
						type="button"
						variant="destructive"
						disabled={mutation.isPending}
						onClick={() => {
							setError(null);
							mutation.mutate();
						}}
					>
						{mutation.isPending ? (
							<Loader2 className="size-4 animate-spin" />
						) : null}
						{t("git.delete.confirm")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
