import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Shield } from "lucide-react";
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
import {
	createGlobalRole,
	type GlobalRole,
	globalRolesQueryOptions,
	updateGlobalRole,
} from "@/lib/admin-api";
import {
	expandWildcardPermissions,
	normalizePermissionsToWildcards,
} from "@/lib/permissions";

import { KNOWN_PERMISSIONS, PERMISSION_GROUPS } from "./permissions";

interface RoleFormDialogProps {
	role?: GlobalRole;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function RoleFormDialog({
	role,
	open,
	onOpenChange,
}: RoleFormDialogProps) {
	const queryClient = useQueryClient();
	const isEdit = !!role;

	const [name, setName] = useState(role?.name ?? "");
	const [permissions, setPermissions] = useState<Record<string, boolean>>(
		expandWildcardPermissions(role?.permissions, KNOWN_PERMISSIONS),
	);
	const [error, setError] = useState<string | null>(null);

	const reset = () => {
		setName(role?.name ?? "");
		setPermissions(
			expandWildcardPermissions(role?.permissions, KNOWN_PERMISSIONS),
		);
		setError(null);
	};

	const handleOpenChange = (next: boolean) => {
		if (!next) reset();
		onOpenChange(next);
	};

	const mutation = useMutation({
		mutationFn: async () => {
			if (!name.trim()) throw new Error("Role name is required.");
			const payload = {
				name: name.trim(),
				permissions: normalizePermissionsToWildcards(
					permissions,
					KNOWN_PERMISSIONS,
				),
			};
			if (isEdit && role) {
				return updateGlobalRole(role.id, payload);
			}
			return createGlobalRole(payload);
		},
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: globalRolesQueryOptions.queryKey,
			});
			onOpenChange(false);
			reset();
		},
		onError: (err: Error) => {
			setError(err.message ?? "Something went wrong.");
		},
	});

	const togglePermission = (key: string, checked: boolean) => {
		setPermissions((prev) => ({ ...prev, [key]: checked }));
	};

	const enabledCount = Object.values(permissions).filter(Boolean).length;

	return (
		<Dialog open={open} onOpenChange={handleOpenChange}>
			<DialogContent className="sm:max-w-lg">
				<DialogHeader>
					<div className="flex items-center gap-2.5">
						<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
							<Shield className="size-4" />
						</div>
						<DialogTitle className="text-base">
							{isEdit ? "Edit Role" : "Create Role"}
						</DialogTitle>
					</div>
					<DialogDescription className="mt-2">
						{isEdit
							? "Update the role name and its permission grants."
							: "Define a new system-wide role and configure its permissions."}
					</DialogDescription>
				</DialogHeader>

				<div className="flex flex-col gap-5 py-1">
					<div className="flex flex-col gap-1.5">
						<Label
							htmlFor="role-name"
							className="text-xs font-semibold uppercase tracking-wide text-muted-foreground"
						>
							Role Name
						</Label>
						<Input
							id="role-name"
							placeholder="e.g. SECURITY_ADMIN"
							value={name}
							onChange={(e) => setName(e.target.value)}
							autoComplete="off"
							className="font-mono"
						/>
					</div>

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
							{PERMISSION_GROUPS.map((group, groupIndex) => {
								const groupPerms = KNOWN_PERMISSIONS.filter(
									(permission) => permission.domain === group.domain,
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
											{groupPerms.map((permission, permissionIndex) => (
												<div key={permission.key}>
													{permissionIndex > 0 && (
														<Separator className="my-2" />
													)}
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
					<DialogClose render={<Button variant="outline" />}>
						Cancel
					</DialogClose>
					<Button
						onClick={() => mutation.mutate()}
						disabled={mutation.isPending}
					>
						{mutation.isPending
							? "Saving…"
							: isEdit
								? "Save changes"
								: "Create role"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
