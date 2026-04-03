import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, Shield } from "lucide-react";
import { useState } from "react";

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
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	expandWildcardPermissions,
	normalizePermissionsToWildcards,
} from "@/lib/permissions";
import {
	createProjectRole,
	type ProjectRole,
	projectRolesQueryOptions,
	updateProjectRole,
} from "@/lib/project-api";

import {
	PROJECT_KNOWN_PERMISSIONS,
	PROJECT_PERMISSION_GROUPS,
} from "./permissions";

interface ProjectRoleFormDialogProps {
	projectId: string;
	role?: ProjectRole;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function ProjectRoleFormDialog({
	projectId,
	role,
	open,
	onOpenChange,
}: ProjectRoleFormDialogProps) {
	const queryClient = useQueryClient();
	const isEdit = !!role;

	const [name, setName] = useState(role?.role_name ?? "");
	const [permissions, setPermissions] = useState<Record<string, boolean>>(
		expandWildcardPermissions(
			role?.permissions as Record<string, boolean> | undefined,
			PROJECT_KNOWN_PERMISSIONS,
		),
	);
	const [error, setError] = useState<string | null>(null);
	const [nameError, setNameError] = useState<string | null>(null);

	const reset = () => {
		setName(role?.role_name ?? "");
		setPermissions(
			expandWildcardPermissions(
				role?.permissions as Record<string, boolean> | undefined,
				PROJECT_KNOWN_PERMISSIONS,
			),
		);
		setError(null);
		setNameError(null);
	};

	const mutation = useMutation({
		mutationFn: async () => {
			const normalized = normalizePermissionsToWildcards(
				permissions,
				PROJECT_KNOWN_PERMISSIONS,
			);
			if (isEdit && role) {
				return updateProjectRole(projectId, role.id, {
					role_name: name.trim(),
					permissions: normalized,
				});
			}
			return createProjectRole(projectId, {
				role_name: name.trim(),
				permissions: normalized,
			});
		},
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: projectRolesQueryOptions(projectId).queryKey,
			});
			onOpenChange(false);
			reset();
		},
		onError: (err: unknown) => {
			setNameError(null);
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.ProjectRoleNameTaken) {
				setNameError("A role with this name already exists.");
				return;
			}
			if (code === ApiErrorCode.ProjectRoleNameInvalid) {
				setNameError(
					"Role name must use uppercase letters, numbers, and underscores.",
				);
				return;
			}
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.ProjectRoleNotFound]:
					"This role no longer exists. It may have already been deleted.",
				[ApiErrorCode.Forbidden]:
					"You don't have permission to perform this action.",
				[ApiErrorCode.InternalError]:
					"Something went wrong on the server. Please try again.",
			};
			const fallback =
				err instanceof Error ? err.message : "Something went wrong.";
			setError((code && messages[code]) ?? fallback);
		},
	});

	const togglePermission = (key: string, checked: boolean) => {
		setPermissions((prev) => ({ ...prev, [key]: checked }));
	};

	const enabledCount = Object.values(permissions).filter(Boolean).length;

	const handleOpenChange = (next: boolean) => {
		if (!next) reset();
		onOpenChange(next);
	};

	return (
		<Dialog open={open} onOpenChange={handleOpenChange}>
			<DialogContent className="flex flex-col sm:max-w-lg max-h-[90svh]">
				<DialogHeader>
					<div className="flex items-center gap-2.5">
						<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
							<Shield className="size-4" />
						</div>
						<DialogTitle className="text-base">
							{isEdit ? "Edit Role" : "New Role"}
						</DialogTitle>
					</div>
					<DialogDescription className="mt-2">
						{isEdit
							? "Update the role name and configure its permission grants."
							: "Define a new project role and configure which permissions it grants to members."}
					</DialogDescription>
				</DialogHeader>

				<div className="flex flex-col gap-5 py-1 overflow-y-auto min-h-0">
					{/* Role name */}
					<div className="flex flex-col gap-1.5">
						<Label
							htmlFor="role-name"
							className="text-xs font-semibold uppercase tracking-wide text-muted-foreground"
						>
							Role Name
						</Label>
						<Input
							id="role-name"
							placeholder="e.g. PROJECT_REVIEWER"
							value={name}
							onChange={(e) => {
								setName(e.target.value);
								if (nameError) setNameError(null);
							}}
							autoComplete="off"
							className={`font-mono${nameError ? " border-destructive focus-visible:ring-destructive" : ""}`}
							aria-describedby={nameError ? "role-name-error" : undefined}
						/>
						{nameError ? (
							<p id="role-name-error" className="text-xs text-destructive">
								{nameError}
							</p>
						) : null}
					</div>

					{/* Permissions */}
					<div className="flex flex-col gap-2.5">
						<div className="flex items-center justify-between">
							<span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
								Permissions
							</span>
							{enabledCount > 0 && (
								<span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
									{enabledCount} enabled
								</span>
							)}
						</div>

						<div className="flex flex-col gap-4 rounded-lg border bg-muted/20 p-4">
							{PROJECT_PERMISSION_GROUPS.map((group, groupIndex) => {
								const groupPerms = PROJECT_KNOWN_PERMISSIONS.filter(
									(p) => p.domain === group.domain,
								);
								const { Icon } = group;
								return (
									<div key={group.domain}>
										{groupIndex > 0 && <Separator className="mb-4" />}
										<div className="mb-3 flex items-center gap-1.5">
											<Icon className="size-3.5 text-muted-foreground" />
											<span className="text-xs font-semibold text-muted-foreground">
												{group.label}
											</span>
										</div>
										<div className="flex flex-col">
											{groupPerms.map((permission, permIndex) => (
												<div key={permission.key}>
													{permIndex > 0 && <Separator className="my-2" />}
													<div className="flex items-center justify-between py-1">
														<div className="flex flex-col gap-0.5">
															<span className="text-sm font-medium">
																{permission.label}
															</span>
															<span className="text-xs text-muted-foreground">
																{permission.description}
															</span>
														</div>
														<Switch
															checked={!!permissions[permission.key]}
															onCheckedChange={(checked) =>
																togglePermission(permission.key, checked)
															}
														/>
													</div>
												</div>
											))}
										</div>
									</div>
								);
							})}
						</div>
					</div>

					{error ? (
						<div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
							<span className="shrink-0">⚠</span>
							<span>{error}</span>
						</div>
					) : null}
				</div>

				<DialogFooter>
					<DialogClose
						render={<Button variant="outline" disabled={mutation.isPending} />}
					>
						Cancel
					</DialogClose>
					<Button
						onClick={() => mutation.mutate()}
						disabled={mutation.isPending || !name.trim()}
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : null}
						{isEdit ? "Save changes" : "Create role"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
