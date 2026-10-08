import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Building2, FolderKanban, TriangleAlert } from "lucide-react";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import type { PluginKnownPermission } from "@/lib/plugin-api";
import type { Policy, RoleScope } from "@/lib/policy";
import { createRole, type Role, updateRole } from "@/lib/role-api";

import { type PermissionGroupDef, PolicyEditor } from "./PolicyEditor";
import { roleErrorKey } from "./role-errors";
import { usePolicyEditor } from "./use-policy-editor";

const EMPTY_POLICY: Policy = { statements: [] };

/** The longest description the form accepts. The API stores any length; this
 *  keeps it a short summary that fits the roles table. */
const DESCRIPTION_MAX = 500;

/** Already-translated copy that differs between the workspace and the project
 *  role dialogs. */
export interface RoleFormLabels {
	editTitle: string;
	createTitle: string;
	editDescription: string;
	createDescription: string;
	nameLabel: string;
	namePlaceholder: string;
	permissionsLabel: string;
	enabledCount: (count: number) => string;
	cancel: string;
	saveChanges: string;
	createRole: string;
}

export interface RoleFormDialogProps {
	scope: RoleScope;
	/** The project a project role belongs to. */
	projectId?: string;
	role?: Role;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	permissions: PluginKnownPermission[];
	groups: readonly PermissionGroupDef[];
	/** Translates the host's own permission and group labels. */
	tScope: (key: string) => string;
	labels: RoleFormLabels;
	/** The query to refresh after a save. */
	invalidateKey: readonly unknown[];
}

/**
 * Create or edit a role: a name, an optional description and its permissions, either as a switch per
 * permission or as the policy JSON. Shared by the workspace and project role
 * pages.
 */
export function RoleFormDialog({
	scope,
	projectId,
	role,
	open,
	onOpenChange,
	permissions,
	groups,
	tScope,
	labels,
	invalidateKey,
}: RoleFormDialogProps) {
	const { t } = useTranslation("roles");
	const queryClient = useQueryClient();
	const isEdit = !!role;

	const [name, setName] = useState(role?.name ?? "");
	const [description, setDescription] = useState(role?.description ?? "");
	const [error, setError] = useState<string | null>(null);
	const [nameError, setNameError] = useState<string | null>(null);

	const editor = usePolicyEditor(
		role?.policy ?? EMPTY_POLICY,
		scope,
		permissions.map((p) => p.key),
		projectId,
		open,
	);

	const reset = () => {
		setName(role?.name ?? "");
		setDescription(role?.description ?? "");
		editor.reset(role?.policy ?? EMPTY_POLICY);
		setError(null);
		setNameError(null);
	};

	const mutation = useMutation({
		mutationFn: async () => {
			const trimmed = name.trim();
			if (!trimmed) throw new NameRequired();
			const policy = editor.buildPolicy();
			if (!policy) throw new PolicyUnreadable();
			const input = {
				name: trimmed,
				description: description.trim(),
				policy,
			};
			return isEdit && role
				? updateRole(role.id, input, projectId)
				: createRole(input, projectId);
		},
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: [...invalidateKey] });
			onOpenChange(false);
			reset();
		},
		onError: (err: unknown) => {
			setNameError(null);
			setError(null);
			if (err instanceof NameRequired) {
				setNameError(t("errors.nameInvalid"));
				return;
			}
			if (err instanceof PolicyUnreadable) {
				setError(t("editor.invalidJson", { message: editor.parseError ?? "" }));
				return;
			}
			const key = roleErrorKey(err);
			if (key === "nameTaken" || key === "nameInvalid") {
				setNameError(t(`errors.${key}`));
				return;
			}
			setError(t(`errors.${key}` as never));
		},
	});

	const blocked =
		editor.mode === "advanced" &&
		(!!editor.parseError || editor.issues.length > 0 || editor.validating);

	const handleOpenChange = (next: boolean) => {
		if (!next) reset();
		onOpenChange(next);
	};

	const ScopeIcon = scope === "project" ? FolderKanban : Building2;

	return (
		<Dialog open={open} onOpenChange={handleOpenChange}>
			<DialogContent className="flex max-h-[92svh] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
				<DialogHeader className="gap-0 border-b px-5 py-4 pr-12 sm:px-6">
					<div className="flex items-center gap-3">
						<div className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary">
							<ScopeIcon className="size-5" aria-hidden="true" />
						</div>
						<div className="flex min-w-0 flex-col gap-1">
							<div className="flex flex-wrap items-center gap-x-2 gap-y-1">
								<DialogTitle className="text-base font-semibold">
									{isEdit ? labels.editTitle : labels.createTitle}
								</DialogTitle>
								<span className="inline-flex items-center rounded-full border bg-muted/50 px-2 py-0.5 text-xs font-medium text-muted-foreground">
									{t(
										scope === "project"
											? "editor.scopeProject"
											: "editor.scopeWorkspace",
									)}
								</span>
							</div>
							<DialogDescription className="text-xs">
								{isEdit ? labels.editDescription : labels.createDescription}
							</DialogDescription>
						</div>
					</div>
				</DialogHeader>

				<div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto px-5 py-5 sm:px-6">
					<div className="flex flex-col gap-4">
						<div className="flex flex-col gap-1.5">
							<Label htmlFor="role-name" className="text-sm font-medium">
								{labels.nameLabel}
							</Label>
							<Input
								id="role-name"
								placeholder={labels.namePlaceholder}
								value={name}
								onChange={(e) => {
									setName(e.target.value);
									if (nameError) setNameError(null);
								}}
								autoComplete="off"
								aria-invalid={nameError ? true : undefined}
								className="font-mono"
								aria-describedby={nameError ? "role-name-error" : undefined}
							/>
							{nameError ? (
								<p id="role-name-error" className="text-xs text-destructive">
									{nameError}
								</p>
							) : null}
						</div>
						<div className="flex flex-col gap-1.5">
							<div className="flex items-baseline justify-between gap-2">
								<Label
									htmlFor="role-description"
									className="text-sm font-medium"
								>
									{t("editor.descriptionLabel")}
									<span className="ml-1.5 text-xs font-normal text-muted-foreground">
										{t("editor.optional")}
									</span>
								</Label>
								<span className="text-xs tabular-nums text-muted-foreground">
									{description.length}/{DESCRIPTION_MAX}
								</span>
							</div>
							<Textarea
								id="role-description"
								rows={3}
								maxLength={DESCRIPTION_MAX}
								placeholder={t("editor.descriptionPlaceholder")}
								value={description}
								onChange={(e) => setDescription(e.target.value)}
								className="min-h-20 resize-y"
							/>
						</div>
					</div>

					<section
						aria-label={labels.permissionsLabel}
						className="flex flex-col gap-3"
					>
						<h3 className="text-sm font-semibold">{labels.permissionsLabel}</h3>
						<PolicyEditor
							editor={editor}
							projectId={projectId}
							permissions={permissions}
							groups={groups}
							tScope={tScope}
							enabledCount={labels.enabledCount}
						/>
					</section>

					{error ? (
						<div
							role="alert"
							className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"
						>
							<TriangleAlert className="size-4 shrink-0" aria-hidden="true" />
							<span>{error}</span>
						</div>
					) : null}
				</div>

				<DialogFooter className="mx-0 mb-0 px-5 py-3 sm:px-6">
					<DialogClose render={<Button variant="outline" />}>
						{labels.cancel}
					</DialogClose>
					<Button
						onClick={() => mutation.mutate()}
						disabled={mutation.isPending || blocked}
					>
						{mutation.isPending
							? t("editor.saving")
							: isEdit
								? labels.saveChanges
								: labels.createRole}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

class NameRequired extends Error {}
class PolicyUnreadable extends Error {}
