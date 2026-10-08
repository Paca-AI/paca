import {
	keepPreviousData,
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import {
	Bot,
	Check,
	ChevronDown,
	Loader2,
	MoreHorizontal,
	PenLine,
	Plus,
	Search,
	Shield,
	Trash2,
	UserRound,
	Users,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { RoleOptionList } from "@/components/admin/global-roles/role-option-list";
import { RoleSelectPanel } from "@/components/admin/global-roles/role-select";
import { projectPermissionBadgeClass } from "@/components/projects/roles/utils";
import { MembersFilters } from "@/components/projects/team/MembersFilters";
import { HighlightMatch } from "@/components/shared/highlight-match";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { enrichRoles, RoleBadgeList } from "@/components/shared/role-badge";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
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
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { useCanAssignProjectRole } from "@/hooks/use-can-assign-project-role";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import { usePermissions } from "@/hooks/use-permissions";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { type User, usersInfiniteQueryOptions } from "@/lib/admin-api";
import { type Agent, chattableAgentsQueryOptions } from "@/lib/agent-api";
import {
	ApiErrorCode,
	getApiErrorCode,
	isForbiddenError,
} from "@/lib/api-error";
import { filterProjectMembers } from "@/lib/filter-project-members";
import { getInitials } from "@/lib/initials";
import {
	addProjectMember,
	type ProjectMember,
	projectMembersQueryOptions,
	projectQueryOptions,
	removeProjectMember,
	updateProjectMemberRole,
} from "@/lib/project-api";
import {
	resolveAgentAvatarUrl,
	resolveMemberAvatarUrl,
} from "@/lib/provider-logos";
import {
	projectRolesQueryOptions,
	type Role,
	replaceMemberRoles,
} from "@/lib/role-api";
import { createLoadMoreScrollHandler } from "@/lib/scroll-pagination";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/team/",
)({
	// Neither roles nor the member list itself is prefetched here. Roles are
	// only needed for the role-switcher dropdown and the add-member dialog
	// (both gated behind project.members.write/project.roles.write already);
	// TeamPage's own useQuery already defaults roles to [] while
	// loading/erroring. The member list needs project.members.read, which
	// this page's own canManageMembers check (project.members.write) doesn't
	// imply — prefetching it unconditionally meant every visit without that
	// read permission threw from both the loader *and* the component's own
	// useQuery on mount, hitting the API twice per visit for a request that
	// was always going to 403. Gated on canRead + NoPermissionState below
	// instead, same as the agents/environments/automation list pages.
	component: TeamPage,
});

/** The roles a member holds, as one label. */
function memberRoleNames(member: ProjectMember): string {
	return member.roles.map((r) => r.name).join(", ");
}

const MIN_VISIBLE_USER_ROWS = 5;

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
				{user.avatar_thumb_url ? (
					<AvatarImage src={user.avatar_thumb_url} />
				) : null}
				<AvatarFallback className="text-xs bg-primary/10 text-primary font-semibold">
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
	existingAgentIds,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	projectId: string;
	roles: Role[];
	existingMemberIds: Set<string>;
	existingAgentIds: Set<string>;
}) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const { hasPermission: can } = usePermissions();
	const [mode, setMode] = useState<"user" | "agent">("user");
	const [selectedUser, setSelectedUser] = useState<User | null>(null);
	const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null);
	const [selectedRoleIds, setSelectedRoleIds] = useState<string[]>([]);
	const [userSearch, setUserSearch] = useState("");
	const [debouncedUserSearch, setDebouncedUserSearch] = useState("");
	const applyUserSearch = useDebouncedCallback(setDebouncedUserSearch, 300);
	const [description, setDescription] = useState("");
	const [error, setError] = useState<string | null>(null);
	const searchRef = useRef<HTMLInputElement>(null);
	const canReadUsers = can("users:read");
	const selectedAgentAvatarUrl = selectedAgent
		? resolveAgentAvatarUrl(selectedAgent)
		: undefined;

	const { data: allAgents = [], isLoading: isLoadingAgents } = useQuery({
		...chattableAgentsQueryOptions,
		enabled: open && mode === "agent",
	});
	const availableAgents = allAgents.filter((a) => !existingAgentIds.has(a.id));

	const {
		data: usersPages,
		isLoading: isLoadingUsers,
		isFetchNextPageError,
		isPlaceholderData,
		fetchNextPage,
		hasNextPage,
		isFetchingNextPage,
	} = useInfiniteQuery({
		...usersInfiniteQueryOptions(debouncedUserSearch),
		enabled: open && canReadUsers,
		placeholderData: keepPreviousData,
	});

	const usersData = useMemo(
		() => usersPages?.pages.flatMap((page) => page.items) ?? [],
		[usersPages],
	);

	const handleUsersListScroll = createLoadMoreScrollHandler({
		hasMore: !!hasNextPage,
		isLoadingMore: isFetchingNextPage,
		onLoadMore: () => void fetchNextPage(),
	});

	// Search is done server-side; existing members are only hidden here.
	const filteredUsers = useMemo<User[]>(
		() => usersData.filter((u) => !existingMemberIds.has(u.id)),
		[usersData, existingMemberIds],
	);

	// Hiding existing members can leave a page with few or no rows (and then
	// nothing to scroll), so keep loading until a few rows are visible.
	useEffect(() => {
		if (
			hasNextPage &&
			!isFetchingNextPage &&
			!isFetchNextPageError &&
			!isPlaceholderData &&
			!isLoadingUsers &&
			filteredUsers.length < MIN_VISIBLE_USER_ROWS
		) {
			void fetchNextPage();
		}
	}, [
		hasNextPage,
		isFetchingNextPage,
		isFetchNextPageError,
		isPlaceholderData,
		isLoadingUsers,
		filteredUsers.length,
		fetchNextPage,
	]);

	const addMutation = useMutation({
		mutationFn: () => {
			if (selectedRoleIds.length === 0) {
				return Promise.reject(new Error("Role is required"));
			}
			if (mode === "agent") {
				if (!selectedAgent) {
					return Promise.reject(new Error("Agent is required"));
				}
				return addProjectMember(projectId, {
					agent_id: selectedAgent.id,
					role_ids: selectedRoleIds,
				});
			}
			if (!selectedUser) {
				return Promise.reject(new Error("User is required"));
			}
			return addProjectMember(projectId, {
				user_id: selectedUser.id,
				role_ids: selectedRoleIds,
				description: description.trim(),
			});
		},
		onSuccess: async () => {
			await queryClient.invalidateQueries({
				queryKey: projectMembersQueryOptions(projectId).queryKey,
			});
			handleClose();
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			setError(
				code === ApiErrorCode.ProjectMemberAlreadyAdded
					? t("team.addMemberDialog.errors.alreadyAdded")
					: code === ApiErrorCode.Forbidden
						? t("team.addMemberDialog.errors.roleForbidden")
						: code === ApiErrorCode.RoleNotAttachable ||
								code === ApiErrorCode.RoleNotFound
							? t("team.addMemberDialog.errors.roleNotFound")
							: t("team.addMemberDialog.errors.addFailed"),
			);
		},
	});

	function handleClose() {
		setMode("user");
		setSelectedUser(null);
		setSelectedAgent(null);
		setSelectedRoleIds([]);
		setUserSearch("");
		setDebouncedUserSearch("");
		// Supersede any pending debounce so it can't restore the old term.
		applyUserSearch("");
		setDescription("");
		setError(null);
		onOpenChange(false);
	}

	const canSubmit =
		selectedRoleIds.length > 0 &&
		!addMutation.isPending &&
		(mode === "agent" ? !!selectedAgent : !!selectedUser);
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
					<DialogTitle>{t("team.addMemberDialog.title")}</DialogTitle>
					<DialogDescription>
						{t("team.addMemberDialog.description")}
					</DialogDescription>
				</DialogHeader>

				<div className="space-y-4 py-1">
					{/* Mode toggle: invite a human user, or invite a global agent */}
					<div className="grid grid-cols-2 gap-2">
						{(["user", "agent"] as const).map((m) => {
							const isSelected = mode === m;
							return (
								<button
									key={m}
									type="button"
									onClick={() => {
										setMode(m);
										setSelectedUser(null);
										setSelectedAgent(null);
										setError(null);
									}}
									className={`flex items-center justify-center gap-1.5 rounded-lg border p-2.5 text-xs font-medium transition-all ${
										isSelected
											? "border-primary/40 bg-primary/5 ring-1 ring-primary/20"
											: "border-border/60 hover:border-border hover:bg-muted/30"
									}`}
								>
									{m === "user" ? (
										<UserRound className="size-3.5" />
									) : (
										<Bot className="size-3.5" />
									)}
									{m === "user"
										? t("team.addMemberDialog.modeUser")
										: t("team.addMemberDialog.modeAgent")}
								</button>
							);
						})}
					</div>

					{mode === "agent" ? (
						<div className="space-y-1.5">
							<p className="text-sm font-medium">
								{t("team.addMemberDialog.agentLabel")}
							</p>
							{selectedAgent ? (
								<div className="flex items-center gap-3 rounded-lg border border-primary/40 bg-primary/5 px-3 py-2">
									<Avatar className="size-7 shrink-0">
										{selectedAgentAvatarUrl ? (
											<AvatarImage src={selectedAgentAvatarUrl} />
										) : null}
										<AvatarFallback className="text-xs bg-primary/10 text-primary font-semibold">
											<Bot className="size-3.5" />
										</AvatarFallback>
									</Avatar>
									<div className="min-w-0 flex-1">
										<span className="text-sm font-medium">
											{selectedAgent.name}
										</span>
										<span className="ml-2 text-xs text-muted-foreground">
											@{selectedAgent.handle}
										</span>
									</div>
									<button
										type="button"
										className="text-xs text-muted-foreground hover:text-foreground"
										onClick={() => setSelectedAgent(null)}
									>
										{t("team.addMemberDialog.change")}
									</button>
								</div>
							) : (
								<div className="rounded-lg border border-border overflow-hidden max-h-44 overflow-y-auto p-1">
									{isLoadingAgents ? (
										<div className="flex items-center justify-center py-4">
											<Loader2 className="size-4 animate-spin text-muted-foreground" />
										</div>
									) : availableAgents.length === 0 ? (
										<p className="py-4 text-center text-xs text-muted-foreground">
											{t("team.addMemberDialog.noAgentsAvailable")}
										</p>
									) : (
										availableAgents.map((agent) => {
											const avatarUrl = resolveAgentAvatarUrl(agent);
											return (
												<button
													key={agent.id}
													type="button"
													className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm transition-colors hover:bg-accent"
													onClick={() => setSelectedAgent(agent)}
												>
													<Avatar className="size-7 shrink-0">
														{avatarUrl ? <AvatarImage src={avatarUrl} /> : null}
														<AvatarFallback className="text-xs bg-primary/10 text-primary font-semibold">
															<Bot className="size-3.5" />
														</AvatarFallback>
													</Avatar>
													<div className="min-w-0 flex-1">
														<span className="font-medium truncate block">
															{agent.name}
														</span>
														<span className="text-xs text-muted-foreground truncate block">
															@{agent.handle}
														</span>
													</div>
												</button>
											);
										})
									)}
								</div>
							)}
						</div>
					) : (
						<div className="space-y-1.5">
							<p className="text-sm font-medium">
								{t("team.addMemberDialog.userLabel")}
							</p>
							{selectedUser ? (
								<div className="flex items-center gap-3 rounded-lg border border-primary/40 bg-primary/5 px-3 py-2">
									<Avatar className="size-7 shrink-0">
										{selectedUser.avatar_thumb_url ? (
											<AvatarImage src={selectedUser.avatar_thumb_url} />
										) : null}
										<AvatarFallback className="text-xs bg-primary/10 text-primary font-semibold">
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
										{t("team.addMemberDialog.change")}
									</button>
								</div>
							) : (
								<div className="rounded-lg border border-border overflow-hidden">
									<div className="flex items-center gap-2 px-3 py-2 border-b border-border/50">
										<Search className="size-3.5 shrink-0 text-muted-foreground" />
										<input
											ref={searchRef}
											className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
											placeholder={t("team.addMemberDialog.searchPlaceholder")}
											value={userSearch}
											onChange={(e) => {
												setUserSearch(e.target.value);
												applyUserSearch(e.target.value.trim());
											}}
											autoFocus
										/>
									</div>
									<div
										className="max-h-44 overflow-y-auto p-1"
										onScroll={handleUsersListScroll}
									>
										{isLoadingUsers ? (
											<div className="flex items-center justify-center py-4">
												<Loader2 className="size-4 animate-spin text-muted-foreground" />
											</div>
										) : filteredUsers.length === 0 ? (
											<p className="py-4 text-center text-xs text-muted-foreground">
												{userSearch
													? t("team.addMemberDialog.noUsersMatch")
													: t("team.addMemberDialog.noUsersAvailable")}
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
										{isFetchingNextPage && (
											<div className="flex items-center justify-center gap-1.5 py-2 text-xs text-muted-foreground">
												<Loader2 className="size-3 animate-spin" />
												{t("team.addMemberDialog.loadingMore")}
											</div>
										)}
									</div>
								</div>
							)}
						</div>
					)}

					{/* Role picker */}
					<div className="space-y-1.5">
						<p className="text-sm font-medium">
							{t("team.addMemberDialog.roleLabel")}
						</p>
						<RoleOptionList
							roles={roles}
							values={selectedRoleIds}
							onChange={(ids) => setSelectedRoleIds(ids)}
							label={t("team.addMemberDialog.roleLabel")}
							badgeClass={projectPermissionBadgeClass}
						/>
					</div>

					{/* Only for humans — an invited agent's description lives on the
					    agent itself, edited from the agent's own detail page. */}
					{mode === "user" && (
						<div className="space-y-1.5">
							<p className="text-sm font-medium">
								{t("team.descriptionChip.title")}
							</p>
							<Textarea
								value={description}
								onChange={(e) => setDescription(e.target.value)}
								placeholder={t("team.descriptionChip.placeholder")}
								rows={2}
								className="text-sm"
							/>
							<p className="text-xs text-muted-foreground">
								{t("team.descriptionChip.hint")}
							</p>
						</div>
					)}

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
						{t("team.addMemberDialog.cancel")}
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
						{t("team.addMemberDialog.addMember")}
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
	roles: Role[];
}) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const [open, setOpen] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const heldIds = member.roles.map((r) => r.id);

	const mutation = useMutation({
		mutationFn: (roleIds: string[]) =>
			replaceMemberRoles(projectId, member.id, roleIds),
		onMutate: async (roleIds) => {
			await queryClient.cancelQueries({
				queryKey: projectMembersQueryOptions(projectId).queryKey,
			});
			const previous = queryClient.getQueryData(
				projectMembersQueryOptions(projectId).queryKey,
			);
			queryClient.setQueryData(
				projectMembersQueryOptions(projectId).queryKey,
				(old: ProjectMember[] | undefined) =>
					old?.map((m) =>
						m.id === member.id
							? {
									...m,
									roles: roles
										.filter((r) => roleIds.includes(r.id))
										.map((r) => ({ id: r.id, name: r.name })),
								}
							: m,
					),
			);
			return { previous };
		},
		onError: (err: unknown, _roleIds, context) => {
			if (context?.previous) {
				queryClient.setQueryData(
					projectMembersQueryOptions(projectId).queryKey,
					context.previous,
				);
			}
			const code = getApiErrorCode(err);
			setError(
				code === ApiErrorCode.ProjectMemberNotFound
					? t("team.roleChip.errors.memberNotFound")
					: code === ApiErrorCode.Forbidden
						? t("team.roleChip.errors.roleForbidden")
						: code === ApiErrorCode.RoleNotAttachable ||
								code === ApiErrorCode.RoleNotFound
							? t("team.roleChip.errors.roleNotFound")
							: t("team.roleChip.errors.changeFailed"),
			);
		},
		onSuccess: () => {
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
				aria-label={t("team.roleChip.changeRole")}
				title={t("team.roleChip.changeRole")}
				disabled={mutation.isPending}
				className="flex min-w-0 max-w-full shrink-0 items-center gap-1.5 rounded-full border border-border/60 bg-secondary/40 py-1 pr-2 pl-1.5 text-xs font-medium text-secondary-foreground transition-all hover:border-border hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
			>
				{mutation.isPending ? (
					<Loader2 className="size-3 shrink-0 animate-spin text-muted-foreground" />
				) : (
					<Shield className="size-3 shrink-0 text-muted-foreground" />
				)}
				<RoleBadgeList
					roles={enrichRoles(member.roles, roles)}
					max={2}
					interactive={false}
				/>
				{!mutation.isPending && (
					<ChevronDown className="size-3 shrink-0 text-muted-foreground/70" />
				)}
			</PopoverTrigger>
			<PopoverContent
				className="w-80 max-w-[calc(100vw-2rem)] p-2.5"
				align="end"
				initialFocus={false}
			>
				<p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">
					{t("team.roleChip.changeRole")}
				</p>
				<RoleSelectPanel
					autoFocus
					roles={roles}
					values={heldIds}
					label={t("team.roleChip.changeRole")}
					badgeClass={projectPermissionBadgeClass}
					// A member keeps at least one role: with none they could do
					// nothing, and removing them is its own action.
					minSelected={1}
					disabled={mutation.isPending}
					onChange={(ids) => mutation.mutate(ids)}
				/>
				{error && (
					<p
						role="alert"
						className="rounded-md bg-destructive/10 px-2 py-1.5 text-xs text-destructive"
					>
						{error}
					</p>
				)}
			</PopoverContent>
		</Popover>
	);
}

// DescriptionChip lets a manager set a human member's Jev-facing
// description — shown to Jev (the AI decision API) when deciding whether to
// assign this member a task in Auto mode. Agent members don't get one here:
// their description lives on the Agent itself (edited from the agent's own
// detail page) since it doesn't vary per project.
function DescriptionChip({
	member,
	projectId,
}: {
	member: ProjectMember;
	projectId: string;
}) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const [open, setOpen] = useState(false);
	const [value, setValue] = useState(member.description);
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () =>
			updateProjectMemberRole(projectId, member.id, {
				description: value.trim(),
			}),
		onError: () => {
			setError(t("team.descriptionChip.saveFailed"));
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
				if (v) setValue(member.description);
				else setError(null);
			}}
		>
			<PopoverTrigger
				type="button"
				aria-label={t("team.descriptionChip.edit")}
				title={member.description || t("team.descriptionChip.edit")}
				className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
			>
				<PenLine className="size-3.5" />
			</PopoverTrigger>
			<PopoverContent className="w-72 p-3" align="end">
				<p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">
					{t("team.descriptionChip.title")}
				</p>
				<p className="mt-1 text-xs text-muted-foreground">
					{t("team.descriptionChip.hint")}
				</p>
				<Textarea
					value={value}
					onChange={(e) => setValue(e.target.value)}
					rows={3}
					className="mt-2 text-sm"
					placeholder={t("team.descriptionChip.placeholder")}
					disabled={mutation.isPending}
				/>
				{error && (
					<p className="mt-1.5 rounded-md bg-destructive/10 px-2 py-1.5 text-xs text-destructive">
						{error}
					</p>
				)}
				<div className="mt-2 flex justify-end">
					<Button
						size="sm"
						onClick={() => mutation.mutate()}
						disabled={mutation.isPending || value === member.description}
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : (
							t("team.descriptionChip.save")
						)}
					</Button>
				</div>
			</PopoverContent>
		</Popover>
	);
}

// ── Member Row ─────────────────────────────────────────────────────────────────

function MemberRow({
	member,
	projectId,
	roles,
	canManage,
	canAssignRoles,
	highlight = "",
	onRemove,
}: {
	member: ProjectMember;
	projectId: string;
	roles: Role[];
	canManage: boolean;
	/** Whether the viewer may change roles (roles:assign alone; not members:write). */
	canAssignRoles: boolean;
	/** Search text to highlight in the member's name and handle. */
	highlight?: string;
	onRemove: (member: ProjectMember) => void;
}) {
	const { t } = useTranslation("projects");
	const display = member.full_name || member.username;
	const isBot =
		member.member_type === "agent" ||
		member.username.startsWith("bot-") ||
		memberRoleNames(member).toLowerCase().includes("agent");
	const memberAvatarUrl = resolveMemberAvatarUrl(member);

	const memberRoles = enrichRoles(member.roles, roles);

	return (
		<div className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border border-border/50 bg-card px-4 py-3 transition-colors hover:bg-muted/30">
			<Avatar className="size-9 shrink-0">
				{memberAvatarUrl ? <AvatarImage src={memberAvatarUrl} alt="" /> : null}
				<AvatarFallback className="text-xs bg-primary/10 text-primary font-semibold">
					{isBot ? <Bot className="size-4" /> : getInitials(display)}
				</AvatarFallback>
			</Avatar>
			<div className="min-w-0 flex-1 basis-40">
				<p className="flex min-w-0 items-center gap-1.5 text-sm font-medium">
					<span className="truncate">
						<HighlightMatch text={display} query={highlight} />
					</span>
					{isBot ? (
						<span className="shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-[11px] font-medium leading-none text-muted-foreground">
							{t("team.memberRow.agentBadge")}
						</span>
					) : null}
				</p>
				<p className="text-xs text-muted-foreground truncate">
					@<HighlightMatch text={member.username} query={highlight} />
				</p>
			</div>
			<div className="ml-auto flex min-w-0 max-w-full items-center gap-1.5 max-sm:order-last max-sm:w-full max-sm:pl-12">
				{canManage && !isBot ? (
					<DescriptionChip member={member} projectId={projectId} />
				) : null}
				<div className="flex min-w-0 flex-1 justify-end max-sm:justify-start">
					{canAssignRoles ? (
						<RoleChip member={member} projectId={projectId} roles={roles} />
					) : (
						<RoleBadgeList roles={memberRoles} max={3} />
					)}
				</div>
				{canManage ? (
					<DropdownMenu>
						<DropdownMenuTrigger
							aria-label={t("team.memberRow.actions")}
							className="flex size-7 shrink-0 items-center justify-center rounded-md p-0 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
						>
							<MoreHorizontal className="size-4" />
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end" className="w-44">
							<DropdownMenuItem
								className="text-destructive focus:text-destructive focus:bg-destructive/10"
								onClick={() => onRemove(member)}
							>
								<Trash2 className="size-3.5 mr-2" />
								{t("team.memberRow.removeMember")}
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				) : null}
			</div>
		</div>
	);
}

// ── Page ───────────────────────────────────────────────────────────────────────

function TeamPage() {
	const { t } = useTranslation("projects");
	const { projectId } = Route.useParams();
	const queryClient = useQueryClient();
	const [addMemberOpen, setAddMemberOpen] = useState(false);
	const [search, setSearch] = useState("");
	const [roleFilter, setRoleFilter] = useState("");
	const [removingMember, setRemovingMember] = useState<ProjectMember | null>(
		null,
	);

	const { hasPermission, isLoading: isGlobalPermissionsLoading } =
		usePermissions();
	const { hasProjectPermission, isLoading: isProjectPermissionsLoading } =
		useProjectPermissions(projectId);
	// Wait for both permission sources — canReadMembers can come from either
	// one, so resolving just one of them isn't enough to know the real
	// answer yet.
	const isPermissionsLoading =
		isGlobalPermissionsLoading || isProjectPermissionsLoading;
	const { data: project } = useQuery(projectQueryOptions(projectId));
	const canReadMembers =
		hasPermission("project.members:read") ||
		hasProjectPermission("project.members:read");
	const {
		data: members,
		isLoading: isDataLoading,
		isError,
		error,
	} = useQuery({
		...projectMembersQueryOptions(projectId),
		enabled: canReadMembers,
	});
	// While permissions are still loading, canReadMembers defaults to false
	// same as a confirmed denial — guard on isPermissionsLoading (and fold
	// it into isLoading) so the page shows the skeleton instead of flashing
	// NoPermissionState first.
	const isLoading = isPermissionsLoading || isDataLoading;
	const noPermission =
		!isPermissionsLoading &&
		(!canReadMembers || (isError && isForbiddenError(error)));
	const { data: roles = [] } = useQuery(projectRolesQueryOptions(projectId));

	const canManageMembers =
		hasPermission("project.members:write") ||
		hasProjectPermission("project.members:write");
	// Handing out roles is a privilege of its own (roles:assign). Editing an
	// existing member's roles needs only that; project.members:write is not
	// required. The API judges every role added or removed on its own resource,
	// so the UI offers all roles and a refusal comes back as FORBIDDEN (shown by
	// the role error mapping).
	const { canAssignRoles } = useCanAssignProjectRole(projectId);
	// Adding a member is member management (members:write) and, because a new
	// member needs at least one role, also needs roles:assign.
	const canAddMembers = canManageMembers && canAssignRoles;

	const existingMemberIds = useMemo(
		() => new Set((members ?? []).map((m) => m.user_id)),
		[members],
	);
	const existingAgentIds = useMemo(
		() =>
			new Set(
				(members ?? [])
					.filter((m) => m.member_type === "agent" && m.agent_id)
					.map((m) => m.agent_id as string),
			),
		[members],
	);

	const visibleMembers = useMemo(
		() => filterProjectMembers(members ?? [], search, roleFilter),
		[members, search, roleFilter],
	);

	const roleChips = useMemo(
		() =>
			roles.map((r) => ({
				name: r.name,
				count: (members ?? []).filter((m) => m.roles.some((x) => x.id === r.id))
					.length,
			})),
		[roles, members],
	);
	const clearFilters = () => {
		setSearch("");
		setRoleFilter("");
	};

	const removeMutation = useMutation({
		mutationFn: () => {
			if (!removingMember) return Promise.resolve();
			return removeProjectMember(projectId, removingMember.id);
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
							{t("team.title")}
						</h1>
						<p className="mt-1 text-sm text-muted-foreground">
							{project?.name} · {t("team.subtitle")}
						</p>
					</div>
					{canAddMembers ? (
						<Button
							size="sm"
							className="gap-1.5 shadow-sm shadow-primary/20"
							onClick={() => setAddMemberOpen(true)}
						>
							<Plus className="size-3.5" />
							{t("team.addMember")}
						</Button>
					) : null}
				</div>
			</div>

			{/* Content */}
			<div className="p-6">
				{noPermission ? (
					<NoPermissionState
						icon={Users}
						title={t("team.noPermission.title")}
						description={t("team.noPermission.description")}
					/>
				) : isLoading ? (
					<div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
						{[...Array(4)].map((_, i) => (
							<div
								// biome-ignore lint/suspicious/noArrayIndexKey: static skeleton
								key={i}
								className="flex items-center gap-3 rounded-xl border border-border/50 bg-card px-4 py-3"
							>
								{/* Avatar */}
								<Skeleton className="size-9 rounded-full shrink-0" />
								{/* Name + username */}
								<div className="min-w-0 flex-1 space-y-1.5">
									<Skeleton className="h-3.5 w-32" />
									<Skeleton className="h-3 w-20" />
								</div>
								{/* Role chip */}
								<Skeleton className="h-6 w-24 rounded-full shrink-0" />
								{/* Action button */}
								<Skeleton className="size-7 rounded-md shrink-0" />
							</div>
						))}
					</div>
				) : !members?.length ? (
					<div className="flex flex-col items-center gap-3 rounded-xl border border-dashed border-border/60 bg-muted/10 py-14">
						<div className="flex size-12 items-center justify-center rounded-xl bg-primary/10">
							<Users className="size-6 text-primary" />
						</div>
						<div className="text-center">
							<p className="text-sm font-medium">{t("team.empty.title")}</p>
							<p className="mt-0.5 text-xs text-muted-foreground">
								{t("team.empty.description")}
							</p>
						</div>
						{canAddMembers ? (
							<Button
								size="sm"
								className="gap-1.5 mt-1"
								onClick={() => setAddMemberOpen(true)}
							>
								<Plus className="size-3.5" />
								{t("team.empty.addFirstMember")}
							</Button>
						) : null}
					</div>
				) : (
					<div>
						<MembersFilters
							search={search}
							roles={roleChips}
							totalCount={members.length}
							selectedRole={roleFilter}
							shownCount={visibleMembers.length}
							onSearchChange={setSearch}
							onRoleChange={setRoleFilter}
							onClear={clearFilters}
						/>
						<div className="mb-3 flex items-center justify-between">
							<p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">
								{t("team.memberCount", { count: visibleMembers.length })}
							</p>
						</div>
						{visibleMembers.length === 0 ? (
							<div className="flex flex-col items-center gap-3 rounded-xl border border-dashed border-border/60 bg-muted/10 py-14">
								<div className="flex size-12 items-center justify-center rounded-xl bg-muted">
									<Search className="size-6 text-muted-foreground/60" />
								</div>
								<div className="text-center">
									<p className="text-sm font-medium">
										{t("team.noMatch.title")}
									</p>
									<p className="mt-0.5 text-xs text-muted-foreground">
										{t("team.noMatch.description")}
									</p>
								</div>
								<Button size="sm" variant="outline" onClick={clearFilters}>
									{t("team.noMatch.clear")}
								</Button>
							</div>
						) : (
							<div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
								{visibleMembers.map((member) => (
									<MemberRow
										key={member.id}
										member={member}
										projectId={projectId}
										roles={roles}
										canManage={canManageMembers}
										canAssignRoles={canAssignRoles}
										highlight={search}
										onRemove={setRemovingMember}
									/>
								))}
							</div>
						)}
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
				existingAgentIds={existingAgentIds}
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
						<DialogTitle>{t("team.removeDialog.title")}</DialogTitle>
						<DialogDescription>
							{t("team.removeDialog.removePrefix")}{" "}
							<span className="font-medium text-foreground">
								{removingMember?.full_name || removingMember?.username}
							</span>{" "}
							{t("team.removeDialog.fromInfix")}{" "}
							<span className="font-medium text-foreground">
								{project?.name}
							</span>
							{t("team.removeDialog.confirmSuffix")}
						</DialogDescription>
					</DialogHeader>
					{removeMutation.isError ? (
						<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
							{t("team.removeDialog.removeFailed")}
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
							{t("team.removeDialog.cancel")}
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
							{t("team.removeDialog.remove")}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
