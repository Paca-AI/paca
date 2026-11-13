import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { AvatarUpload } from "@/components/shared/avatar-upload";
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
import { Textarea } from "@/components/ui/textarea";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { serviceAccountAvatar } from "@/lib/provider-logos";
import {
	createServiceAccount,
	type ServiceAccount,
	serviceAccountsKey,
	updateServiceAccount,
} from "@/lib/service-account-api";

interface ServiceAccountFormDialogProps {
	/** The account being edited; omit to create one. */
	account?: ServiceAccount;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

/**
 * Creates a service account, or edits its name, description and avatar. Its
 * credentials (API keys) and roles are separate dialogs: they are separate
 * privileges and decisions.
 */
export function ServiceAccountFormDialog({
	account,
	open,
	onOpenChange,
}: ServiceAccountFormDialogProps) {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const isEdit = !!account;
	const [name, setName] = useState(account?.name ?? "");
	const [description, setDescription] = useState(account?.description ?? "");
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		meta: { success: "saved", subject: "serviceAccount" },
		mutationFn: () =>
			isEdit
				? updateServiceAccount(account.id, {
						name: name.trim(),
						description: description.trim(),
					})
				: createServiceAccount({
						name: name.trim(),
						description: description.trim(),
					}),
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: serviceAccountsKey,
			});
			onOpenChange(false);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.ServiceAccountNameInvalid) {
				setError(t("serviceAccounts.formDialog.errors.nameInvalid"));
			} else if (code === ApiErrorCode.ServiceAccountDescriptionTooLong) {
				setError(t("serviceAccounts.formDialog.errors.descriptionTooLong"));
			} else if (code === ApiErrorCode.ServiceAccountNotFound) {
				setError(t("serviceAccounts.formDialog.errors.notFound"));
			} else {
				setError(t("serviceAccounts.formDialog.errors.generic"));
			}
		},
	});

	const reset = () => {
		setName(account?.name ?? "");
		setDescription(account?.description ?? "");
		setError(null);
	};

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
						{isEdit
							? t("serviceAccounts.formDialog.editTitle")
							: t("serviceAccounts.formDialog.createTitle")}
					</DialogTitle>
					<DialogDescription>
						{isEdit
							? t("serviceAccounts.formDialog.editDescription")
							: t("serviceAccounts.formDialog.createDescription")}
					</DialogDescription>
				</DialogHeader>

				<form
					className="flex flex-col gap-4 py-2"
					onSubmit={(e) => {
						e.preventDefault();
						if (name.trim()) mutation.mutate();
					}}
				>
					{isEdit ? (
						<AvatarUpload
							basePath={`/admin/service-accounts/${account.id}`}
							avatar={{
								...serviceAccountAvatar(account),
								avatarUrl: account.avatar_url,
								size: "xl",
							}}
							className="size-14 rounded-xl"
							labels={{
								change: t("serviceAccounts.formDialog.avatar.change"),
								remove: t("serviceAccounts.formDialog.avatar.remove"),
								uploading: t("serviceAccounts.formDialog.avatar.uploading"),
								invalidType: t(
									"serviceAccounts.formDialog.avatar.errors.invalidType",
								),
								tooLarge: t(
									"serviceAccounts.formDialog.avatar.errors.tooLarge",
								),
								uploadFailed: t(
									"serviceAccounts.formDialog.avatar.errors.uploadFailed",
								),
								removeFailed: t(
									"serviceAccounts.formDialog.avatar.errors.removeFailed",
								),
							}}
							onChange={() => {
								void queryClient.invalidateQueries({
									queryKey: serviceAccountsKey,
								});
							}}
						/>
					) : null}

					<div className="flex flex-col gap-1.5">
						<Label htmlFor="sa-name">
							{t("serviceAccounts.formDialog.nameLabel")}
						</Label>
						<Input
							id="sa-name"
							value={name}
							maxLength={100}
							placeholder={t("serviceAccounts.formDialog.namePlaceholder")}
							onChange={(e) => {
								setName(e.target.value);
								setError(null);
							}}
							autoFocus
						/>
					</div>

					<div className="flex flex-col gap-1.5">
						<Label htmlFor="sa-description">
							{t("serviceAccounts.formDialog.descriptionLabel")}
						</Label>
						<Textarea
							id="sa-description"
							value={description}
							maxLength={1000}
							rows={3}
							placeholder={t(
								"serviceAccounts.formDialog.descriptionPlaceholder",
							)}
							onChange={(e) => {
								setDescription(e.target.value);
								setError(null);
							}}
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
							{t("serviceAccounts.formDialog.cancel")}
						</DialogClose>
						<Button type="submit" disabled={mutation.isPending || !name.trim()}>
							{isEdit
								? mutation.isPending
									? t("serviceAccounts.formDialog.saving")
									: t("serviceAccounts.formDialog.save")
								: mutation.isPending
									? t("serviceAccounts.formDialog.creating")
									: t("serviceAccounts.formDialog.create")}
						</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	);
}
