import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
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
	deleteServiceAccount,
	type ServiceAccount,
	serviceAccountsKey,
} from "@/lib/service-account-api";

interface DeleteServiceAccountDialogProps {
	account: ServiceAccount;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	/** Called once the account is gone (the detail page leaves for the list). */
	onDeleted?: () => void;
}

export function DeleteServiceAccountDialog({
	account,
	open,
	onOpenChange,
	onDeleted,
}: DeleteServiceAccountDialogProps) {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		meta: { success: "deleted", subject: "serviceAccount" },
		mutationFn: () => deleteServiceAccount(account.id),
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: serviceAccountsKey,
			});
			onOpenChange(false);
			onDeleted?.();
		},
		onError: () => setError(t("serviceAccounts.deleteDialog.error")),
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
					<DialogTitle>{t("serviceAccounts.deleteDialog.title")}</DialogTitle>
					<DialogDescription>
						{t("serviceAccounts.deleteDialog.description", {
							name: account.name,
						})}
					</DialogDescription>
				</DialogHeader>
				{error ? <p className="text-sm text-destructive">{error}</p> : null}
				<DialogFooter>
					<DialogClose
						render={<Button variant="outline" disabled={mutation.isPending} />}
					>
						{t("serviceAccounts.deleteDialog.cancel")}
					</DialogClose>
					<Button
						variant="destructive"
						disabled={mutation.isPending}
						onClick={() => mutation.mutate()}
					>
						<Trash2 className="size-4" />
						{mutation.isPending
							? t("serviceAccounts.deleteDialog.deleting")
							: t("serviceAccounts.deleteDialog.confirm")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
