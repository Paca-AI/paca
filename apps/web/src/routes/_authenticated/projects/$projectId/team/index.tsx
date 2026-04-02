import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import {
	Bot,
	Check,
	ChevronDown,
	Loader2,
	MoreHorizontal,
	Plus,
	Search,
	Shield,
	Trash2,
	UserRound,
	Users,
} from "lucide-react";
import { useMemo, useRef, useState } from "react";

import { Avatar, AvatarFallback } from "@/components/ui/avatar";
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
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { getUsers, type User } from "@/lib/admin-api";
import {
	addProjectMember,
	type ProjectMember,
	type ProjectRole,
	projectMembersQueryOptions,
	projectQueryOptions,
	projectRolesQueryOptions,
	removeProjectMember,
	updateProjectMemberRole,
} from "@/lib/project-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/team/",
)({
	loader: async ({ context: { queryClient }, params: { projectId } }) => {
		await Promise.all([
			queryClient.ensureQueryData(projectMembersQueryOptions(projectId)),
			queryClient.ensureQueryData(projectRolesQueryOptions(projectId)),
		]);
	},
	component: TeamPage,
});

function getInitials(name: string): string {
	return name
		.split(" ")
		.filter(Boolean)
		.map((n) => n[0])
		.join("")
		.toUpperCase()
		.slice(0, 2);
}

// ── Add Member Dialog ──────────────────────────────────────────────────────────

function UserPickerItem({
	user,
	selected,
	onSelect,
}: {
	user: User;
	selected: boolean;
	onSelect: (user: User) => void;
}) {
	const display = user.full_name || user.username;
	return (
		<button
			type="button"
			className={`flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm transition-colors hover:bg-accent ${selected ? "bg-accent" : ""}`}
			onClick={() => onSelect(user)}
		>
			<Avatar className="size-7 shrink-0">
				<AvatarFallback className="text-[10px] bg-primary/10 text-primary font-semibold">
					{getInitials(display)}
				</AvatarFallback>
			</Avatar>
			<div className="min-w-0 flex-1">
				<span className="font-medium truncate block">{display}</span>
				{user.full_name && (
					<span className="text-xs text-muted-foreground truncate block">
						@{user.username}
					</span>
				)}
			</div>
			{selected && <Check className="size-4 shrink-0 text-primary" />}
		</button>
	);
}

function AddMemberDialog({
	open,
	onOpenChange,
	projectId,
	roles,
	existingMemberIds,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	projectId: string;
	roles: ProjectRole[];
	existingMemberIds: Set<string>;
}) {
	const queryClient = useQueryClient();
	const [selectedUser, setSelectedUser] = useState<User | null>(null);
	const [selectedRoleId, setSelectedRoleId] = useState<string>("");
	const [userSearch, setUserSearch] = useState("");
	const [error, setError] = useState<string | null>(null);
	const searchRef = useRef<HTMLInputElement>(null);

	const { data: usersData, isLoading: isLoadingUsers } = useQuery({
		queryKey: ["admin", "users", "all"],
		queryFn: () => getUsers(1, 500),
		enabled: open,
	});

	const filteredUsers = useMemo<User[]>(() => {
		const items: User[] = usersData?.items ?? [];
		const q = userSearch.toLowerCase();
		return items
			.filter((u) => !existingMemberIds.has(u.id))
			.filter(
				(u) =>
					!q ||
					u.username.toLowerCase().includes(q) ||
					(u.full_name ?? "").toLowerCase().includes(q),
			);
	}, [usersData, existingMemberIds, userSearch]);

	const addMutation = useMutation({
		mutationFn: () => {
			if (!selectedUser || !selectedRoleId) {
				return Promise.reject(new Error("User and role are required"));
			}

			return addProjectMember(projectId, {
				user_id: selectedUser.id,
				project_role_id: selectedRoleId,
			});
		},
		onSuccess: async () => {
			await queryClient.invalidateQueries({
				queryKey: projectMembersQueryOptions(projectId).queryKey,
			});
			handleClose();
		},
		onError: (err: unknown) => {
			const e = err as { response?: { data?: { error?: string } } };
			setError(
				e?.response?.data?.error ?? "Failed to add member. Please try again.",
			);
		},
	});

	function handleClose() {
		setSelectedUser(null);
		setSelectedRoleId("");
		setUserSearch("");
		setError(null);
		onOpenChange(false);
	}

	const canSubmit = selectedUser && selectedRoleId && !addMutation.isPending;
	const selectedUserId = selectedUser?.id ?? null;

	return (
		<Dialog
			open={open}
			onOpenChange={(v) => {
				if (!v) handleClose();
			}}
		>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<div className="flex size-10 items-center justify-center rounded-full bg-primary/10 mb-2">
						<Users className="size-5 text-primary" />
					</div>
					<DialogTitle>Add member</DialogTitle>
					<DialogDescription>
						Select a user and assign them a role in this project.
					</DialogDescription>
				</DialogHeader>

				<div className="space-y-4 py-1">
					{/* User search */}
					<div className="space-y-1.5">
						<p className="text-sm font-medium">User</p>
						{selectedUser ? (
							<div className="flex items-center gap-3 rounded-lg border border-primary/40 bg-primary/5 px-3 py-2">
								<Avatar className="size-7 shrink-0">
									<AvatarFallback className="text-[10px] bg-primary/10 text-primary font-semibold">
										{getInitials(
											selectedUser.full_name || selectedUser.username,
										)}
									</AvatarFallback>
								</Avatar>
								<div className="min-w-0 flex-1">
									<span className="text-sm font-medium">
										{selectedUser.full_name || selectedUser.username}
									</span>
									{selectedUser.full_name && (
										<span className="ml-2 text-xs text-muted-foreground">
											@{selectedUser.username}
										</span>
									)}
								</div>
								<button
									type="button"
									className="text-xs text-muted-foreground hover:text-foreground"
									onClick={() => {
										setSelectedUser(null);
										setTimeout(() => searchRef.current?.focus(), 50);
									}}
								>
									Change
								</button>
							</div>
						) : (
							<div className="rounded-lg border border-border overflow-hidden">
								<div className="flex items-center gap-2 px-3 py-2 border-b border-border/50">
									<Search className="size-3.5 shrink-0 text-muted-foreground" />
									<input
										ref={searchRef}
										className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
										placeholder="Search by name or username…"
										value={userSearch}
										onChange={(e) => setUserSearch(e.target.value)}
										autoFocus
									/>
								</div>
								<div className="max-h-44 overflow-y-auto p-1">
									{isLoadingUsers ? (
										<div className="flex items-center justify-center py-4">
											<Loader2 className="size-4 animate-spin text-muted-foreground" />
										</div>
									) : filteredUsers.length === 0 ? (
										<p className="py-4 text-center text-xs text-muted-foreground">
											{userSearch
												? "No users match your search."
												: "No users available to add."}
										</p>
									) : (
										filteredUsers.map((user: User) => (
											<UserPickerItem
												key={user.id}
												user={user}
												selected={selectedUserId === user.id}
												onSelect={setSelectedUser}
											/>
										))
									)}
								</div>
							</div>
						)}
					</div>

					{/* Role picker */}
					<div className="space-y-1.5">
						<p className="text-sm font-medium">Role</p>
						<Select
							value={selectedRoleId}
							onValueChange={(v) => {
								if (v != null) setSelectedRoleId(v);
							}}
						>
							<SelectTrigger className="w-full">
								<SelectValue placeholder="Select a role…" />
							</SelectTrigger>
							<SelectContent>
								{roles.map((role) => (
									<SelectItem key={role.id} value={role.id}>
										{role.role_name}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>

					{error && (
						<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
							{error}
						</p>
					)}
				</div>

				<DialogFooter>
					<DialogClose
						render={
							<Button
								variant="outline"
								size="sm"
								disabled={addMutation.isPending}
							/>
						}
					>
						Cancel
					</DialogClose>
					<Button
						size="sm"
						disabled={!canSubmit}
						onClick={() => addMutation.mutate()}
					>
						{addMutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : (
							<Plus className="size-3.5" />
						)}
						Add member
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// ── Role Chip (inline popover role picker) ────────────────────────────────────

function RoleChip({
	member,
	projectId,
	roles,
}: {
	member: ProjectMember;
	projectId: string;
	roles: ProjectRole[];
}) {
	const queryClient = useQueryClient();
	const [open, setOpen] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: (roleId: string) =>
			updateProjectMemberRole(projectId, member.user_id, {
				project_role_id: roleId,
			}),
		onMutate: async (roleId) => {
			await queryClient.cancelQueries({
				queryKey: projectMembersQueryOptions(projectId).queryKey,
			});
			const previous = queryClient.getQueryData(
				projectMembersQueryOptions(projectId).queryKey,
			);
			const newRole = roles.find((r) => r.id === roleId);
			if (newRole) {
				queryClient.setQueryData(
					projectMembersQueryOptions(projectId).queryKey,
					(old: ProjectMember[] | undefined) =>
						old?.map((m) =>
							m.user_id === member.user_id
								? {
										...m,
										project_role_id: roleId,
										role_name: newRole.role_name,
									}
								: m,
						),
				);
			}
			return { previous };
		},
		onError: (_err, _vars, context) => {
			if (context?.previous) {
				queryClient.setQueryData(
					projectMembersQueryOptions(projectId).queryKey,
					context.previous,
				);
			}
			const e = _err as { response?: { data?: { error?: string } } };
			setError(e?.response?.data?.error ?? "Failed to change role.");
		},
		onSuccess: () => {
			setOpen(false);
			setError(null);
		},
		onSettled: () => {
			queryClient.invalidateQueries({
				queryKey: projectMembersQueryOptions(projectId).queryKey,
			});
		},
	});

	return (
		<Popover
			open={open}
			onOpenChange={(v) => {
				setOpen(v);
				if (!v) setError(null);
			}}
		>
			<PopoverTrigger
				type="button"
				aria-label="Change role"
				disabled={mutation.isPending}
				className="flex shrink-0 items-center gap-1.5 rounded-full border border-border/60 bg-secondary/50 px-2.5 py-1 text-xs font-medium text-secondary-foreground transition-all hover:bg-accent hover:border-border focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
			>
				{mutation.isPending ? (
					<Loader2 className="size-3 animate-spin text-muted-foreground" />
				) : (
					<Shield className="size-3 text-muted-foreground" />
				)}
				<span>{member.role_name}</span>
				{!mutation.isPending && (
					<ChevronDown className="size-3 text-muted-foreground/70" />
				)}
			</PopoverTrigger>
			<PopoverContent className="w-52 p-1.5" align="end">
				<p className="px-2 py-1 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">
					Change role
				</p>
				<div className="mt-0.5 space-y-px">
					{roles.map((role) => {
						const isCurrent = role.id === member.project_role_id;
						return (
							<button
								key={role.id}
								type="button"
								className={`flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-sm transition-colors hover:bg-accent ${
									isCurrent
										? "font-medium text-foreground"
										: "text-muted-foreground hover:text-foreground"
								}`}
								onClick={() => {
									if (!isCurrent) mutation.mutate(role.id);
									else setOpen(false);
								}}
							>
								<Check
									className={`size-3.5 shrink-0 text-primary transition-opacity ${
										isCurrent ? "opacity-100" : "opacity-0"
									}`}
								/>
								{role.role_name}
							</button>
						);
					})}
				</div>
				{error && (
					<p className="mt-1.5 rounded-md bg-destructive/10 px-2 py-1.5 text-xs text-destructive">
						{error}
					</p>
				)}
			</PopoverContent>
		</Popover>
	);
}

// ── Member Row ─────────────────────────────────────────────────────────────────

function MemberRow({
	member,
	projectId,
	roles,
	onRemove,
}: {
	member: ProjectMember;
	projectId: string;
	roles: ProjectRole[];
	onRemove: (member: ProjectMember) => void;
}) {
	const display = member.full_name || member.username;
	const isBot =
		member.username.startsWith("bot-") ||
		member.role_name.toLowerCase().includes("agent");

	return (
		<div className="flex items-center gap-3 rounded-xl border border-border/50 bg-card px-4 py-3 transition-colors hover:bg-muted/30">
			<Avatar className="size-9 shrink-0">
				<AvatarFallback className="text-xs bg-primary/10 text-primary font-semibold">
					{isBot ? <Bot className="size-4" /> : getInitials(display)}
				</AvatarFallback>
			</Avatar>
			<div className="min-w-0 flex-1">
				<p className="text-sm font-medium truncate">{display}</p>
				<p className="text-xs text-muted-foreground truncate">
					@{member.username}
				</p>
			</div>
			<RoleChip member={member} projectId={projectId} roles={roles} />
			<DropdownMenu>
				<DropdownMenuTrigger className="flex size-7 shrink-0 items-center justify-center rounded-md p-0 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground">
					<MoreHorizontal className="size-4" />
				</DropdownMenuTrigger>
				<DropdownMenuContent align="end" className="w-44">
					<DropdownMenuItem
						className="text-destructive focus:text-destructive focus:bg-destructive/10"
						onClick={() => onRemove(member)}
					>
						<Trash2 className="size-3.5 mr-2" />
						Remove member
					</DropdownMenuItem>
				</DropdownMenuContent>
			</DropdownMenu>
		</div>
	);
}

// ── Page ───────────────────────────────────────────────────────────────────────

function TeamPage() {
	const { projectId } = Route.useParams();
	const queryClient = useQueryClient();
	const [addMemberOpen, setAddMemberOpen] = useState(false);
	const [removingMember, setRemovingMember] = useState<ProjectMember | null>(
		null,
	);

	const { data: project } = useQuery(projectQueryOptions(projectId));
	const { data: members, isLoading } = useQuery(
		projectMembersQueryOptions(projectId),
	);
	const { data: roles = [] } = useQuery(projectRolesQueryOptions(projectId));

	const existingMemberIds = useMemo(
		() => new Set((members ?? []).map((m) => m.user_id)),
		[members],
	);

	const removeMutation = useMutation({
		mutationFn: () => {
			if (!removingMember) return Promise.resolve();
			return removeProjectMember(projectId, removingMember.user_id);
		},
		onSuccess: async () => {
			await queryClient.invalidateQueries({
				queryKey: projectMembersQueryOptions(projectId).queryKey,
			});
			setRemovingMember(null);
		},
	});

	return (
		<div className="flex flex-col">
			{/* Header */}
			<div className="relative overflow-hidden border-b border-border/50">
				<div
					className="pointer-events-none absolute inset-0 opacity-50"
					style={{
						backgroundImage:
							"radial-gradient(circle, color-mix(in oklch, var(--color-primary) 12%, transparent) 1px, transparent 1px)",
						backgroundSize: "20px 20px",
						maskImage:
							"radial-gradient(ellipse 70% 100% at 0% 0%, black 20%, transparent 70%)",
					}}
				/>
				<div className="relative flex items-end justify-between px-6 py-8">
					<div>
						<h1 className="font-[Syne] text-2xl font-bold tracking-tight">
							Team
						</h1>
						<p className="mt-1 text-sm text-muted-foreground">
							{project?.name} · Manage project members and roles
						</p>
					</div>
					<Button
						size="sm"
						className="gap-1.5 shadow-sm shadow-primary/20"
						onClick={() => setAddMemberOpen(true)}
					>
						<Plus className="size-3.5" />
						Add Member
					</Button>
				</div>
			</div>

			{/* Content */}
			<div className="p-6">
				{isLoading ? (
					<div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
						{[...Array(4)].map((_, i) => (
							<Skeleton
								// biome-ignore lint/suspicious/noArrayIndexKey: static skeleton
								key={i}
								className="h-16 w-full rounded-xl"
							/>
						))}
					</div>
				) : !members?.length ? (
					<div className="flex flex-col items-center gap-3 rounded-2xl border border-dashed border-border/60 bg-muted/10 py-14">
						<div className="flex size-12 items-center justify-center rounded-xl bg-primary/10">
							<Users className="size-6 text-primary" />
						</div>
						<div className="text-center">
							<p className="text-sm font-medium">No members yet</p>
							<p className="mt-0.5 text-xs text-muted-foreground">
								Add teammates or AI agents to this project to get started.
							</p>
						</div>
						<Button
							size="sm"
							className="gap-1.5 mt-1"
							onClick={() => setAddMemberOpen(true)}
						>
							<Plus className="size-3.5" />
							Add first member
						</Button>
					</div>
				) : (
					<div>
						<div className="mb-3 flex items-center justify-between">
							<p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">
								{members.length} {members.length === 1 ? "member" : "members"}
							</p>
						</div>
						<div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
							{members.map((member) => (
								<MemberRow
									key={member.id}
									member={member}
									projectId={projectId}
									roles={roles}
									onRemove={setRemovingMember}
								/>
							))}
						</div>
					</div>
				)}
			</div>

			{/* Add Member Dialog */}
			<AddMemberDialog
				open={addMemberOpen}
				onOpenChange={setAddMemberOpen}
				projectId={projectId}
				roles={roles}
				existingMemberIds={existingMemberIds}
			/>

			{/* Remove confirmation dialog */}
			<Dialog
				open={!!removingMember}
				onOpenChange={(open) => {
					if (!open) setRemovingMember(null);
				}}
			>
				<DialogContent className="sm:max-w-sm">
					<DialogHeader>
						<div className="flex size-10 items-center justify-center rounded-full bg-destructive/10 mb-2">
							<UserRound className="size-5 text-destructive" />
						</div>
						<DialogTitle>Remove member</DialogTitle>
						<DialogDescription>
							Remove{" "}
							<span className="font-medium text-foreground">
								{removingMember?.full_name || removingMember?.username}
							</span>{" "}
							from{" "}
							<span className="font-medium text-foreground">
								{project?.name}
							</span>
							? They will lose access immediately.
						</DialogDescription>
					</DialogHeader>
					{removeMutation.isError ? (
						<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
							Failed to remove member. Please try again.
						</p>
					) : null}
					<DialogFooter>
						<DialogClose
							render={
								<Button
									variant="outline"
									size="sm"
									disabled={removeMutation.isPending}
								/>
							}
						>
							Cancel
						</DialogClose>
						<Button
							variant="destructive"
							size="sm"
							disabled={removeMutation.isPending}
							onClick={() => removeMutation.mutate()}
						>
							{removeMutation.isPending ? (
								<Loader2 className="size-3.5 animate-spin" />
							) : (
								<Trash2 className="size-3.5" />
							)}
							Remove
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
