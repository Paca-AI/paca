import { Edit2, KeyRound, Lock, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import type { User } from "@/lib/admin-api";

function formatDate(iso: string): string {
	return new Date(iso).toLocaleDateString(undefined, {
		year: "numeric",
		month: "short",
		day: "numeric",
	});
}

interface UsersTableProps {
	users: User[];
	canWrite: boolean;
	currentUserId?: string;
	onEdit: (user: User) => void;
	onDelete: (user: User) => void;
	onResetPassword: (user: User) => void;
}

export function UsersTable({
	users,
	canWrite,
	currentUserId,
	onEdit,
	onDelete,
	onResetPassword,
}: UsersTableProps) {
	return (
		<div className="overflow-x-auto rounded-xl border">
			<Table>
				<TableHeader>
					<TableRow className="bg-muted/40 hover:bg-muted/40">
						<TableHead className="w-44 px-5 text-xs font-semibold uppercase tracking-wide">
							Username
						</TableHead>
						<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
							Full Name
						</TableHead>
						<TableHead className="w-36 px-5 text-xs font-semibold uppercase tracking-wide">
							Role
						</TableHead>
						<TableHead className="w-32 px-5 text-xs font-semibold uppercase tracking-wide">
							Created
						</TableHead>
						{canWrite ? (
							<TableHead className="w-28 px-5 text-xs font-semibold uppercase tracking-wide" />
						) : null}
					</TableRow>
				</TableHeader>
				<TableBody>
					{users.map((user) => {
						const isSelf = user.id === currentUserId;
						return (
							<TableRow key={user.id} className="group">
								<TableCell className="px-5">
									<div className="flex items-center gap-2">
										<Lock className="size-3.5 shrink-0 text-muted-foreground/40" />
										<span className="font-mono text-sm font-medium">
											{user.username}
										</span>
										{user.must_change_password ? (
											<span className="inline-flex items-center rounded-full bg-amber-100 px-1.5 py-0.5 text-[0.65rem] font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
												pwd reset
											</span>
										) : null}
										{isSelf ? (
											<span className="inline-flex items-center rounded-full bg-primary/10 px-1.5 py-0.5 text-[0.65rem] font-medium text-primary">
												you
											</span>
										) : null}
									</div>
								</TableCell>
								<TableCell className="px-5 text-sm">
									{user.full_name || (
										<span className="italic text-muted-foreground/60">—</span>
									)}
								</TableCell>
								<TableCell className="px-5">
									<span className="inline-flex items-center rounded-full border px-2 py-0.5 font-mono text-[0.68rem] font-medium leading-none text-foreground/80">
										{user.role}
									</span>
								</TableCell>
								<TableCell className="px-5 text-sm text-muted-foreground">
									{formatDate(user.created_at)}
								</TableCell>
								{canWrite ? (
									<TableCell className="px-5">
										<div className="flex items-center justify-end gap-0.5 opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100">
											<Button
												variant="ghost"
												size="icon-sm"
												onClick={() => onResetPassword(user)}
												title="Reset password"
											>
												<KeyRound className="size-3.5" />
											</Button>
											<Button
												variant="ghost"
												size="icon-sm"
												onClick={() => onEdit(user)}
												title="Edit user"
											>
												<Edit2 className="size-3.5" />
											</Button>
											{!isSelf ? (
												<Button
													variant="ghost"
													size="icon-sm"
													className="text-destructive hover:text-destructive hover:bg-destructive/10"
													onClick={() => onDelete(user)}
													title="Delete user"
												>
													<Trash2 className="size-3.5" />
												</Button>
											) : null}
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
