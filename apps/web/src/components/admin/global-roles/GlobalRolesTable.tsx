import { Edit2, Lock, Trash2 } from "lucide-react";
import {
	activePermissions,
	formatDate,
	permissionBadgeClass,
} from "@/components/admin/global-roles/utils";
import { Button } from "@/components/ui/button";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import type { GlobalRole } from "@/lib/admin-api";

interface GlobalRolesTableProps {
	roles: GlobalRole[];
	canWrite: boolean;
	onEdit: (role: GlobalRole) => void;
	onDelete: (role: GlobalRole) => void;
}

export function GlobalRolesTable({
	roles,
	canWrite,
	onEdit,
	onDelete,
}: GlobalRolesTableProps) {
	return (
		<div className="rounded-xl border overflow-hidden">
			<Table>
				<TableHeader>
					<TableRow className="bg-muted/40 hover:bg-muted/40">
						<TableHead className="w-44 px-5 text-xs font-semibold uppercase tracking-wide">
							Name
						</TableHead>
						<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
							Permissions
						</TableHead>
						<TableHead className="w-32 px-5 text-xs font-semibold uppercase tracking-wide">
							Created
						</TableHead>
						{canWrite ? (
							<TableHead className="w-20 px-5 text-xs font-semibold uppercase tracking-wide" />
						) : null}
					</TableRow>
				</TableHeader>
				<TableBody>
					{roles.map((role) => {
						const active = activePermissions(role.permissions);
						return (
							<TableRow key={role.id} className="group">
								<TableCell className="px-5">
									<div className="flex items-center gap-2">
										<Lock className="size-3.5 shrink-0 text-muted-foreground/40" />
										<span className="font-mono text-sm font-medium">
											{role.name}
										</span>
									</div>
								</TableCell>
								<TableCell className="px-5">
									{active.length === 0 ? (
										<span className="text-xs italic text-muted-foreground/60">
											No permissions assigned
										</span>
									) : (
										<div className="flex flex-wrap gap-1">
											{active.map((permission) => (
												<span
													key={permission}
													className={`inline-flex items-center rounded-full border px-2 py-0.5 font-mono text-[0.68rem] font-medium leading-none ${permissionBadgeClass(permission)}`}
												>
													{permission}
												</span>
											))}
										</div>
									)}
								</TableCell>
								<TableCell className="px-5 text-sm text-muted-foreground">
									{formatDate(role.created_at)}
								</TableCell>
								{canWrite ? (
									<TableCell className="px-5">
										<div className="flex items-center justify-end gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
											<Button
												variant="ghost"
												size="icon-sm"
												onClick={() => onEdit(role)}
												title="Edit role"
											>
												<Edit2 className="size-3.5" />
											</Button>
											<Button
												variant="ghost"
												size="icon-sm"
												className="text-destructive hover:text-destructive hover:bg-destructive/10"
												onClick={() => onDelete(role)}
												title="Delete role"
											>
												<Trash2 className="size-3.5" />
											</Button>
										</div>
									</TableCell>
								) : null}
							</TableRow>
						);
					})}
				</TableBody>
			</Table>
		</div>
	);
}
