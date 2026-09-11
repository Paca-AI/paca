import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Shield } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
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
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	expandWildcardPermissions,
	normalizePermissionsToWildcards,
} from "@/lib/permissions";
import {
	collectPluginCustomPermissions,
	type Plugin,
	pluginsQueryOptions,
} from "@/lib/plugin-api";
import {
	createProjectRole,
	type ProjectRole,
	projectRolesQueryOptions,
	updateProjectRole,
} from "@/lib/project-api";

import {
	type KnownPermission,
	PROJECT_KNOWN_PERMISSIONS,
	PROJECT_PERMISSION_GROUPS,
	toPluginKnownPermissions,
} from "./permissions";

// Stable reference so `allKnownPermissions` doesn't change identity on every
// render while the plugins query has no data yet (pending/error) — an
// inline `= []` default creates a new array each render, which re-triggers
// the effect below in an infinite loop.
const EMPTY_PLUGINS: Plugin[] = [];

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
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const isEdit = !!role;

	const { data: plugins = EMPTY_PLUGINS } = useQuery(pluginsQueryOptions);
	const allKnownPermissions = useMemo<KnownPermission[]>(
		() => [
			...PROJECT_KNOWN_PERMISSIONS,
			...toPluginKnownPermissions(
				collectPluginCustomPermissions(plugins, "project"),
			),
		],
		[plugins],
	);

	const [name, setName] = useState(role?.role_name ?? "");
	const [permissions, setPermissions] = useState<Record<string, boolean>>({});
	// isFullAccess tracks whether the role's *stored* permissions are the
	// bare wildcard ({"*": true}, e.g. a project's seeded "Admin" role —
	// see 000056_set_admin_role_wildcard_permission.sql) rather than an
	// enumerated set. expandWildcardPermissions below turns "*" into every
	// known permission's checkbox reading true for display, which is
	// correct for rendering but loses the "*" itself — without tracking it
	// separately, saving an untouched full-access role would silently
	// re-derive it as today's enumerated wildcards via
	// normalizePermissionsToWildcards, undoing 000056's future-proofing
	// (any permission added later would need its own backfill again). Reset
	// to false the moment the admin touches any individual toggle — at that
	// point they're making an explicit choice, and the resulting save
	// should reflect exactly what's checked, not the original "*".
	const [isFullAccess, setIsFullAccess] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [nameError, setNameError] = useState<string | null>(null);

	// Mirrors the latest allKnownPermissions for the full-reset effect below
	// to read without depending on it — see that effect's comment.
	const allKnownPermissionsRef = useRef(allKnownPermissions);
	allKnownPermissionsRef.current = allKnownPermissions;

	// Full reset of `permissions`/`isFullAccess` from the role's stored
	// permissions — but only on a fresh open or when the role's own
	// permissions actually change (switching which role is being edited, or
	// a save completing and refetching). Deliberately does NOT depend on
	// allKnownPermissions: that reference also changes whenever the
	// unrelated plugins query settles (see EMPTY_PLUGINS' comment above),
	// and re-running a full reset on every such settle would silently
	// discard whatever the admin has already toggled mid-edit — isFullAccess
	// included, since a role stored as {"*": true} would just get
	// re-flagged full-access again over the admin's own narrowing. Newly
	// discovered permission keys (e.g. plugin data finishing after mount)
	// are instead merged in by the effect below, which doesn't touch
	// anything already present.
	useEffect(() => {
		if (!open) return;
		const rolePermissions = role?.permissions as
			| Record<string, boolean>
			| undefined;
		setPermissions(
			expandWildcardPermissions(rolePermissions, allKnownPermissionsRef.current),
		);
		setIsFullAccess(rolePermissions?.["*"] === true);
		// biome-ignore lint/correctness/useExhaustiveDependencies: allKnownPermissions is read via allKnownPermissionsRef so this effect isn't re-triggered by every plugins-query settle — see the comment above
	}, [open, role?.permissions]);

	// Merges in any permission keys not yet tracked in `permissions` —
	// handles plugin-declared permissions that load after the dialog is
	// already open, without resetting permissions (or isFullAccess) the
	// admin has already touched, unlike a full re-derive would.
	useEffect(() => {
		if (!open) return;
		const rolePermissions = role?.permissions as
			| Record<string, boolean>
			| undefined;
		setPermissions((prev) => {
			const missing = allKnownPermissions.filter((p) => !(p.key in prev));
			if (missing.length === 0) return prev;
			return {
				...prev,
				...expandWildcardPermissions(rolePermissions, missing),
			};
		});
	}, [open, allKnownPermissions, role?.permissions]);

	const reset = () => {
		const rolePermissions = role?.permissions as
			| Record<string, boolean>
			| undefined;
		setName(role?.role_name ?? "");
		setPermissions(
			expandWildcardPermissions(rolePermissions, allKnownPermissions),
		);
		setIsFullAccess(rolePermissions?.["*"] === true);
		setError(null);
		setNameError(null);
	};

	const mutation = useMutation({
		// Takes the permissions to submit as an explicit argument, computed by
		// the caller at click-time (see the submit button below), rather than
		// reading `permissions`/`isFullAccess` from this closure — a stale
		// mutationFn closure otherwise risks submitting an isFullAccess value
		// from an earlier render than the one that just fired.
		mutationFn: async (normalizedPermissions: Record<string, boolean>) => {
			if (isEdit && role) {
				return updateProjectRole(projectId, role.id, {
					role_name: name.trim(),
					permissions: normalizedPermissions,
				});
			}
			return createProjectRole(projectId, {
				role_name: name.trim(),
				permissions: normalizedPermissions,
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
				setNameError(t("roles.formDialog.errors.nameTaken"));
				return;
			}
			if (code === ApiErrorCode.ProjectRoleNameInvalid) {
				setNameError(t("roles.formDialog.errors.nameInvalid"));
				return;
			}
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.ProjectRoleNotFound]: t(
					"roles.formDialog.errors.notFound",
				),
				[ApiErrorCode.Forbidden]: t("roles.formDialog.errors.forbidden"),
				[ApiErrorCode.InternalError]: t(
					"roles.formDialog.errors.internalError",
				),
			};
			const fallback =
				err instanceof Error
					? err.message
					: t("roles.formDialog.errors.generic");
			setError((code && messages[code]) ?? fallback);
		},
	});

	const permissionLabel = (permission: KnownPermission): string =>
		permission.rawLabel ?? t(permission.labelKey as never);

	const permissionDescription = (permission: KnownPermission): string =>
		permission.rawDescription ?? t(permission.descriptionKey as never);

	const togglePermission = (key: string, checked: boolean) => {
		setPermissions((prev) => ({ ...prev, [key]: checked }));
		// Any explicit toggle exits full-access mode for this editing session
		// — see isFullAccess's doc comment above.
		setIsFullAccess(false);
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
							{isEdit
								? t("roles.formDialog.editTitle")
								: t("roles.formDialog.createTitle")}
						</DialogTitle>
					</div>
					<DialogDescription className="mt-2">
						{isEdit
							? t("roles.formDialog.editDescription")
							: t("roles.formDialog.createDescription")}
					</DialogDescription>
				</DialogHeader>

				<div className="flex flex-col gap-5 py-1 overflow-y-auto min-h-0">
					{/* Role name */}
					<div className="flex flex-col gap-1.5">
						<Label
							htmlFor="role-name"
							className="text-xs font-semibold uppercase tracking-wide text-muted-foreground"
						>
							{t("roles.formDialog.roleNameLabel")}
						</Label>
						<Input
							id="role-name"
							placeholder={t("roles.formDialog.roleNamePlaceholder")}
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
								{t("roles.formDialog.permissionsLabel")}
							</span>
							{isFullAccess ? (
								<span className="rounded-full bg-primary px-2 py-0.5 text-xs font-medium text-primary-foreground">
									{t("roles.formDialog.fullAccessBadge")}
								</span>
							) : (
								enabledCount > 0 && (
									<span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
										{t("roles.formDialog.enabledCount", { count: enabledCount })}
									</span>
								)
							)}
						</div>
						{isFullAccess && (
							<p className="text-xs text-muted-foreground">
								{t("roles.formDialog.fullAccessDescription")}
							</p>
						)}

						<div className="flex flex-col gap-4 rounded-lg border bg-muted/20 p-4">
							{PROJECT_PERMISSION_GROUPS.map((group, groupIndex) => {
								const groupPerms = allKnownPermissions.filter(
									(p) => p.domain === group.domain,
								);
								if (groupPerms.length === 0) return null;
								const { Icon } = group;
								return (
									<div key={group.domain}>
										{groupIndex > 0 && <Separator className="mb-4" />}
										<div className="mb-3 flex items-center gap-1.5">
											<Icon className="size-3.5 text-muted-foreground" />
											<span className="text-xs font-semibold text-muted-foreground">
												{t(group.labelKey)}
											</span>
										</div>
										<div className="flex flex-col">
											{groupPerms.map((permission, permIndex) => (
												<div key={permission.key}>
													{permIndex > 0 && <Separator className="my-2" />}
													<div className="flex items-center justify-between py-1">
														<div className="flex flex-col gap-0.5">
															<span className="text-sm font-medium">
																{permissionLabel(permission)}
															</span>
															<span className="text-xs text-muted-foreground">
																{permissionDescription(permission)}
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
						{t("roles.formDialog.cancel")}
					</DialogClose>
					<Button
						onClick={() => {
							// Preserve the bare wildcard as-is rather than re-deriving it
							// from checkbox state — see isFullAccess's doc comment above.
							const normalized = isFullAccess
								? { "*": true }
								: normalizePermissionsToWildcards(
										permissions,
										allKnownPermissions,
									);
							mutation.mutate(normalized);
						}}
						disabled={mutation.isPending || !name.trim()}
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : null}
						{isEdit
							? t("roles.formDialog.saveChanges")
							: t("roles.formDialog.createRole")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
