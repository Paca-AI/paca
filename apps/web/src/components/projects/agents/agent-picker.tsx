import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronDown, Plus } from "lucide-react";
import {
	createContext,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import { EnvironmentCreateDialog } from "@/components/projects/environments/environment-create-dialog";
import {
	PinnedRow,
	useFolderPath,
} from "@/components/projects/environments/environment-folder-select";
import { FolderCreateDialog } from "@/components/projects/environments/folder-create-dialog";
import { SearchableList } from "@/components/projects/interactions/task-detail/property-field/searchable-list";
import { MiddleTruncate } from "@/components/shared/middle-truncate";
import { RemoteSelect } from "@/components/shared/remote-select";
import { Button } from "@/components/ui/button";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { useAgentSearch } from "@/hooks/use-agent-search";
import {
	useEnvironmentFolderSearch,
	useEnvironmentSearch,
} from "@/hooks/use-environment-search";
import { useJevEnabled } from "@/hooks/use-jev-enabled";
import { usePermissions } from "@/hooks/use-permissions";
import { agentQueryOptions } from "@/lib/agent-api";
import { useAgentDirectory } from "@/lib/agent-directory";
import {
	type Environment,
	environmentFolderQueryOptions,
	environmentFoldersInfiniteQueryOptions,
	environmentQueryOptions,
	environmentsInfiniteQueryOptions,
} from "@/lib/environment-api";

// ── Agent picker ──────────────────────────────────────────────────────────────
//
// Rendered inline in the composer's action row (`ThreadComponents.ComposerStart`)
// so picking an agent lives in the same box as the message input and send
// button — no separate "pick an agent first" step before the composer shows
// up. `ComposerStart` takes no props, so the picker's data is passed down via
// this context instead. Shared between the floating chat widget and the
// Conversations page's inline "new conversation" thread — same UX, same code.

export interface AgentPickerState {
	/** The options currently listed: the synthetic Auto entry (when offered)
	 *  followed by the loaded page(s) of agents matching the search. */
	agents: { id: string; name: string }[];
	agentsLoading: boolean;
	/** Name to show for the selected agent when it isn't among `agents` (e.g.
	 *  the list is filtered by a search), resolved by id. */
	selectedName?: string;
	/** Server-side search and paging of the option list; without it the list
	 *  is static. */
	search?: {
		query: string;
		onQueryChange: (query: string) => void;
		hasMore: boolean;
		isLoadingMore: boolean;
		isError?: boolean;
		onLoadMore: () => void;
	};
	agentId: string;
	onAgentChange: (id: string) => void;
	/** Once a conversation has started, the agent is fixed for its lifetime —
	 * lock the picker instead of implying a switch would do anything. */
	disabled?: boolean;
	/** Link shown in the empty state ("no agents yet, create one") — null
	 * hides that button entirely (e.g. a global-scope user without
	 * permission to create a global agent has nowhere useful to send them). */
	emptyStateLink: {
		to: string;
		params?: Record<string, string>;
		search?: Record<string, unknown>;
	} | null;
}

export const AgentPickerContext = createContext<AgentPickerState | null>(null);

// AUTO_AGENT_ID is the picker's sentinel "Auto" selection — Jev (the AI
// decision API) picks which real agent handles the conversation, based on
// the user's opening message, at send time. Not a real agent id: whichever
// send handler owns the picker (new-conversation-thread.tsx, the floating
// chat widget) must resolve it to a real id via resolveAutoAgent/
// resolveGlobalAutoAgent before calling startChatSession — see those
// handlers' own "Auto" branch.
export const AUTO_AGENT_ID = "__auto__";

// Fetches the project's agents and owns the selected-agent state that feeds
// `AgentPickerState` — shared by the floating chat widget and the
// Conversations page's inline "new conversation" thread so neither duplicates
// the query + selection wiring (or the single-agent auto-select behavior
// below) on its own.
export function useAgentPicker(
	projectId: string,
	options?: { disabled?: boolean; enabled?: boolean },
) {
	const { t } = useTranslation("projects");
	const jevEnabled = useJevEnabled(projectId);
	const search = useAgentSearch(projectId, options?.enabled ?? true);
	const getAgent = useAgentDirectory(projectId);
	// The server only lists agents the caller may use, so every one is a choice.
	const realAgents = search.agents;
	const agentsLoading = search.isLoading;
	// Auto-selection and the Auto option reflect the project's agents as a
	// whole, so they only apply while the list isn't narrowed by a search.
	const browsing = search.query.trim() === "";
	// "Auto" is prepended as a synthetic first option — only when Jev is
	// configured AND there's at least one real agent to resolve it to
	// (otherwise it'd be a dead end: picking it just 404s at send time).
	const agents = useMemo(
		() =>
			jevEnabled && realAgents.length > 0 && browsing
				? [{ id: AUTO_AGENT_ID, name: t("aiChat.autoAgent") }, ...realAgents]
				: realAgents,
		[realAgents, jevEnabled, browsing, t],
	);
	const [agentId, setAgentId] = useState("");

	// Default to Auto once Jev is available; otherwise fall back to the
	// pre-Auto behavior — auto-select the project's only agent instead of
	// forcing an explicit choice before the composer will send.
	useEffect(() => {
		if (agentId || !browsing) return;
		if (jevEnabled && realAgents.length > 0) {
			setAgentId(AUTO_AGENT_ID);
		} else if (realAgents.length === 1 && !search.hasMore && realAgents[0]) {
			setAgentId(realAgents[0].id);
		}
	}, [realAgents, agentId, jevEnabled, browsing, search.hasMore]);

	const disabled = options?.disabled;
	const selectedName =
		agentId === AUTO_AGENT_ID ? t("aiChat.autoAgent") : getAgent(agentId)?.name;
	const pickerState = useMemo<AgentPickerState>(
		() => ({
			agents,
			agentsLoading,
			selectedName,
			search: {
				query: search.query,
				onQueryChange: search.onQueryChange,
				hasMore: search.hasMore,
				isLoadingMore: search.isLoadingMore,
				isError: search.isError,
				onLoadMore: search.onLoadMore,
			},
			agentId,
			onAgentChange: setAgentId,
			disabled,
			emptyStateLink: {
				to: "/projects/$projectId/agents",
				params: { projectId },
				search: { create: true },
			},
		}),
		[
			agents,
			agentsLoading,
			selectedName,
			search.query,
			search.onQueryChange,
			search.hasMore,
			search.isLoadingMore,
			search.isError,
			search.onLoadMore,
			agentId,
			disabled,
			projectId,
		],
	);

	return { agentId, setAgentId, agents, agentsLoading, pickerState };
}

// Global-scope sibling of useAgentPicker: sources from every agent any
// authenticated user may chat with (GET /agents, no project) instead of a
// single project's roster. Used by the global AIChatFloat (home/admin pages).
export function useGlobalAgentPicker(options?: {
	disabled?: boolean;
	enabled?: boolean;
}) {
	const { t } = useTranslation("projects");
	// No project in scope here (this is the global, cross-project picker), so
	// Jev is always treated as unconfigured — matches ResolveGlobalAutoAgent's
	// own graceful nil-project fallback on the backend.
	const jevEnabled = useJevEnabled(undefined);
	const { hasPermission } = usePermissions();
	const search = useAgentSearch(null, options?.enabled ?? true);
	const getAgent = useAgentDirectory(null);
	const realAgents = search.agents;
	const agentsLoading = search.isLoading;
	const browsing = search.query.trim() === "";
	const agents = useMemo(
		() =>
			jevEnabled && realAgents.length > 0 && browsing
				? [{ id: AUTO_AGENT_ID, name: t("aiChat.autoAgent") }, ...realAgents]
				: realAgents,
		[realAgents, jevEnabled, browsing, t],
	);
	const [agentId, setAgentId] = useState("");

	useEffect(() => {
		if (agentId || !browsing) return;
		if (jevEnabled && realAgents.length > 0) {
			setAgentId(AUTO_AGENT_ID);
		} else if (realAgents.length === 1 && !search.hasMore && realAgents[0]) {
			setAgentId(realAgents[0].id);
		}
	}, [realAgents, agentId, jevEnabled, browsing, search.hasMore]);

	const disabled = options?.disabled;
	const canCreate = hasPermission("agents:write");
	const selectedName =
		agentId === AUTO_AGENT_ID ? t("aiChat.autoAgent") : getAgent(agentId)?.name;
	const pickerState = useMemo<AgentPickerState>(
		() => ({
			agents,
			agentsLoading,
			selectedName,
			search: {
				query: search.query,
				onQueryChange: search.onQueryChange,
				hasMore: search.hasMore,
				isLoadingMore: search.isLoadingMore,
				isError: search.isError,
				onLoadMore: search.onLoadMore,
			},
			agentId,
			onAgentChange: setAgentId,
			disabled,
			emptyStateLink: canCreate
				? { to: "/admin/agents", search: { create: true } }
				: null,
		}),
		[
			agents,
			agentsLoading,
			selectedName,
			search.query,
			search.onQueryChange,
			search.hasMore,
			search.isLoadingMore,
			search.isError,
			search.onLoadMore,
			agentId,
			disabled,
			canCreate,
		],
	);

	return { agentId, setAgentId, agents, agentsLoading, pickerState };
}

export function AgentPickerInline() {
	const picker = useContext(AgentPickerContext);
	if (!picker) return null;
	return <AgentPickerPopover picker={picker} />;
}

function AgentPickerPopover({ picker }: { picker: AgentPickerState }) {
	const { t } = useTranslation("projects");
	const [open, setOpen] = useState(false);
	const {
		agents,
		agentsLoading,
		agentId,
		onAgentChange,
		disabled,
		emptyStateLink,
		search,
		selectedName,
	} = picker;

	if (agentsLoading) {
		return <div className="h-7 w-32 animate-pulse rounded-full bg-muted" />;
	}
	// An empty list while searching just means no match — only an empty,
	// unsearched list means the project has no agents at all.
	if (agents.length === 0 && !search?.query.trim()) {
		if (!emptyStateLink) {
			return (
				<span className="text-xs text-muted-foreground px-2">
					{t("aiChat.noAgentsAvailable")}
				</span>
			);
		}
		return (
			<Button
				size="sm"
				variant="outline"
				className="h-7 gap-1.5 rounded-full text-xs"
				nativeButton={false}
				render={<Link {...emptyStateLink} />}
			>
				<Plus className="size-3.5" />
				{t("agents.page.newAgent")}
			</Button>
		);
	}

	const triggerLabel =
		agents.find((a) => a.id === agentId)?.name ?? selectedName;

	return (
		<Popover open={open} onOpenChange={setOpen}>
			<PopoverTrigger
				type="button"
				disabled={disabled}
				className="flex h-7 items-center gap-1 rounded-full border-none bg-transparent pl-2.5 pr-1.5 text-xs font-medium text-muted-foreground outline-none transition-colors hover:bg-accent focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-100"
			>
				<MiddleTruncate
					className="max-w-40"
					text={triggerLabel ?? t("aiChat.selectAgentPlaceholder")}
				/>
				<ChevronDown className="size-3.5 shrink-0 opacity-60" />
			</PopoverTrigger>
			<PopoverContent
				align="start"
				className="w-64 p-1 rounded-xl border border-border/40 shadow-lg"
			>
				<SearchableList
					remote={search}
					items={agents.map((a) => ({
						key: a.id,
						label: a.name,
						selected: a.id === agentId,
						onSelect: () => {
							onAgentChange(a.id);
							setOpen(false);
						},
						content: (
							<MiddleTruncate className="flex-1 text-left" text={a.name} />
						),
					}))}
				/>
			</PopoverContent>
		</Popover>
	);
}

// ── Environment picker ────────────────────────────────────────────────────────
//
// Two components sharing one EnvironmentPickerContext, both docked in the
// composer's action row alongside AgentPickerInline (see
// new-conversation-thread.tsx's ComposerStartRow): EnvironmentPickerInline
// (the environment Select) and FolderPickerInline (the folder Select, only
// meaningful once a real environment is selected). Together they pick a
// static environment + folder to attach the new conversation to, instead
// of the default ephemeral per-conversation sandbox. Fully additive:
// leaving the environment on its default "temporary" value preserves
// today's behavior exactly, since new-conversation-thread.tsx only sends
// environment_id/folder_id when they're actually set. See
// docs/ai-agent/environment-management.md's Frontend section.

export interface EnvironmentPickerState {
	projectId: string;
	/** Whether the project has any environment at all; the picker stays
	 *  hidden when it has none. The options themselves are searched and paged
	 *  on the server inside the popover. */
	hasEnvironments: boolean;
	environmentsLoading: boolean;
	environmentId: string;
	onEnvironmentChange: (id: string) => void;
	/** The selected environment, resolved by id. */
	environment?: Environment;
	folderId: string;
	onFolderChange: (id: string) => void;
	/** Applies to both the environment and folder pickers (e.g. a
	 * conversation already in progress, whose attachment is fixed for its
	 * lifetime). */
	disabled?: boolean;
	/** Environment-only lock, layered on top of disabled above — see
	 * useEnvironmentPicker's own doc comment on why a provider_cli agent
	 * locks environment but leaves folder pickable. */
	environmentDisabled?: boolean;
	/** True for an ACP-type agent — its sandboxing is owned entirely by the
	 * user's own local ACP client, so no static environment/folder ever
	 * applies (mirrors agent-detail.tsx's own `!isAcp` gate on its
	 * Environment section). Both pickers stay hidden entirely rather than
	 * merely disabled. */
	hidden?: boolean;
}

export const EnvironmentPickerContext =
	createContext<EnvironmentPickerState | null>(null);

// Environments are project-scoped only (no global-chat equivalent) — callers
// at global scope simply never mount EnvironmentPickerContext.Provider, and
// EnvironmentPickerInline no-ops when there's no context to read.
export function useEnvironmentPicker(
	projectId: string,
	agentId: string,
	options?: { disabled?: boolean; enabled?: boolean },
) {
	const queryEnabled = (options?.enabled ?? true) && !!projectId;
	// First page only — to know whether the project has any environment. The
	// picker's popover searches and pages the full list itself.
	const { data: firstPage, isLoading: environmentsLoading } = useInfiniteQuery({
		...environmentsInfiniteQueryOptions(projectId),
		enabled: queryEnabled,
	});
	const hasEnvironments = (firstPage?.pages[0]?.items.length ?? 0) > 0;
	// Fetched only to read default_environment_id — this agent is typically
	// already warm in the agent picker's own cache once chosen, so this is
	// usually an instant cache hit rather than a new request.
	const { data: agent } = useQuery({
		...agentQueryOptions(projectId, agentId),
		enabled: queryEnabled && !!agentId,
	});

	// The agent's default environment, resolved by id — null/missing when it
	// was deleted after being set, so a dangling id is never selected.
	const { data: defaultEnvironment } = useQuery({
		...environmentQueryOptions(projectId, agent?.default_environment_id ?? ""),
		enabled: queryEnabled && !!agent?.default_environment_id,
		retry: false,
	});

	const [environmentId, setEnvironmentIdState] = useState("");
	const [folderId, setFolderId] = useState("");
	const prevAgentIdRef = useRef(agentId);

	// Switching to a different agent starts over — clear whatever was picked
	// for the previous one instead of carrying a stale selection across.
	useEffect(() => {
		if (prevAgentIdRef.current !== agentId) {
			prevAgentIdRef.current = agentId;
			setEnvironmentIdState("");
			setFolderId("");
		}
	}, [agentId]);

	// Nothing explicitly picked yet — default to the agent's own default
	// environment once it's known, the same "auto-select" convention
	// useAgentPicker uses for a project's single agent. Guarded against a
	// default_environment_id that no longer resolves (the environment was
	// deleted after being set as default) — without this, a dangling id
	// gets set into state and silently posted as environment_id on
	// startChatSession with nothing in the picker's own list to show for
	// it.
	useEffect(() => {
		if (!environmentId && agent?.default_environment_id && defaultEnvironment) {
			setEnvironmentIdState(agent.default_environment_id);
		}
	}, [agent?.default_environment_id, defaultEnvironment, environmentId]);

	const { data: environment } = useQuery({
		...environmentQueryOptions(projectId, environmentId),
		enabled: queryEnabled && !!environmentId,
	});
	// First page of the selected environment's folders — enough to tell
	// whether it has exactly one (auto-selected below).
	const { data: folderPages } = useInfiniteQuery({
		...environmentFoldersInfiniteQueryOptions(projectId, environmentId),
		enabled: queryEnabled && !!environmentId,
	});
	const onlyFolder =
		folderPages?.pages[0]?.total_count === 1
			? folderPages.pages[0].items[0]
			: undefined;
	const { data: defaultFolder } = useQuery({
		...environmentFolderQueryOptions(
			projectId,
			environmentId,
			agent?.default_folder_id ?? "",
		),
		enabled:
			queryEnabled &&
			!!environmentId &&
			environmentId === agent?.default_environment_id &&
			!!agent?.default_folder_id,
	});

	// The agent's own default folder — only applies once environmentId has
	// resolved to the agent's own default_environment_id (never a
	// manually-picked, possibly different one; see onEnvironmentChange
	// below, which clears folderId on every environment switch so this
	// can't leak a stale folder across environments), and only when that
	// folder still exists in the environment's current list — same
	// dangling-reference guard the default_environment_id effect above
	// uses.
	useEffect(() => {
		if (
			!folderId &&
			environmentId &&
			environmentId === agent?.default_environment_id &&
			agent?.default_folder_id &&
			defaultFolder
		) {
			setFolderId(agent.default_folder_id);
		}
	}, [
		agent?.default_environment_id,
		agent?.default_folder_id,
		environmentId,
		defaultFolder,
		folderId,
	]);

	// Exactly one folder — auto-select it instead of forcing an explicit
	// choice, mirroring useAgentPicker's single-option auto-select. Runs
	// after the default-folder effect above so an agent's own default
	// (when still valid) always wins over this weaker heuristic.
	useEffect(() => {
		if (!folderId && onlyFolder) {
			setFolderId(onlyFolder.id);
		}
	}, [onlyFolder, folderId]);

	const onEnvironmentChange = useCallback((id: string) => {
		setEnvironmentIdState(id);
		setFolderId("");
	}, []);

	const disabled = options?.disabled;
	// A provider_cli agent's underlying CLI persists its login state on disk
	// inside its own default environment (see internal/executor/providercli's
	// symlink bootstrap, services/agent-runner) — switching to a different
	// environment for one conversation would silently lose that login, so
	// *environment* choice is locked to whatever default_environment_id the
	// agent itself carries (already auto-selected above) rather than left
	// open to a choice that only breaks things. Folder is deliberately NOT
	// included in this lock — picking a different working directory inside
	// the same, still-logged-in environment is harmless, and still something
	// a user legitimately wants to do per conversation. Combined with the
	// caller's own disabled (via the || below), not replacing it — a
	// caller-locked picker (e.g. an already-started conversation) must stay
	// locked regardless of agent type.
	const environmentDisabled = disabled || agent?.agent_type === "provider_cli";
	// An ACP agent runs entirely through the user's own local ACP client
	// (apps/acp-bridge) — it never attaches to one of this project's static
	// environments, so both pickers are hidden outright instead of merely
	// disabled (same `!isAcp` gate agent-detail.tsx's Environment section
	// already uses).
	const hidden = agent?.agent_type === "acp";
	const pickerState = useMemo<EnvironmentPickerState>(
		() => ({
			projectId,
			hasEnvironments,
			environmentsLoading,
			environmentId,
			onEnvironmentChange,
			environment,
			folderId,
			onFolderChange: setFolderId,
			disabled,
			environmentDisabled,
			hidden,
		}),
		[
			projectId,
			hasEnvironments,
			environmentsLoading,
			environmentId,
			onEnvironmentChange,
			environment,
			folderId,
			disabled,
			environmentDisabled,
			hidden,
		],
	);

	return { environmentId, folderId, pickerState };
}

const PILL_TRIGGER =
	"flex h-7 items-center gap-1 rounded-full border-none bg-transparent pl-2.5 pr-1.5 text-xs font-medium text-muted-foreground hover:bg-accent disabled:opacity-100";

// EnvironmentPickerInline is the environment-only half — the folder half
// lives in FolderPickerInline below. Both render side by side with
// AgentPickerInline in the composer's action row (see
// new-conversation-thread.tsx's ComposerStartRow) — split into two
// components rather than one combined picker so FolderPickerInline can
// stay hidden until a real environment is actually selected.
export function EnvironmentPickerInline() {
	const picker = useContext(EnvironmentPickerContext);
	if (!picker) return null;
	return <EnvironmentPickerBody picker={picker} />;
}

function EnvironmentPickerBody({ picker }: { picker: EnvironmentPickerState }) {
	const { t } = useTranslation("projects");
	const [open, setOpen] = useState(false);
	const [createOpen, setCreateOpen] = useState(false);
	const {
		projectId,
		hasEnvironments,
		environmentsLoading,
		environmentId,
		onEnvironmentChange,
		environment,
		disabled,
		environmentDisabled,
		hidden,
	} = picker;
	const isDisabled = !!(disabled || environmentDisabled);
	const search = useEnvironmentSearch(projectId, open && !isDisabled);

	// Hidden outright for an ACP-type agent (see EnvironmentPickerState.hidden),
	// or when this project hasn't created any static environments yet — either
	// way, stay invisible rather than showing an empty/meaningless picker.
	if (hidden || environmentsLoading || !hasEnvironments) {
		return null;
	}

	return (
		<>
			<RemoteSelect
				open={open}
				onOpenChange={setOpen}
				disabled={isDisabled}
				triggerClassName={PILL_TRIGGER}
				label={
					environmentId ? environment?.name : t("environments.picker.temporary")
				}
				placeholder={t("environments.picker.temporary")}
			>
				<SearchableList
					remote={search}
					pinned={
						<>
							<PinnedRow
								onSelect={() => {
									onEnvironmentChange("");
									setOpen(false);
								}}
							>
								{t("environments.picker.temporary")}
							</PinnedRow>
							<PinnedRow
								onSelect={() => {
									setOpen(false);
									setCreateOpen(true);
								}}
							>
								<Plus className="size-3.5" />
								{t("environments.picker.createNew")}
							</PinnedRow>
						</>
					}
					items={search.environments.map((env) => ({
						key: env.id,
						label: env.name,
						selected: env.id === environmentId,
						onSelect: () => {
							onEnvironmentChange(env.id);
							setOpen(false);
						},
						content: (
							<span className="flex-1 truncate text-left">{env.name}</span>
						),
					}))}
				/>
			</RemoteSelect>
			<EnvironmentCreateDialog
				projectId={projectId}
				open={createOpen}
				onOpenChange={setCreateOpen}
				onCreated={(env) => onEnvironmentChange(env.id)}
			/>
		</>
	);
}

// FolderPickerInline is the folder half — rendered next to
// AgentPickerInline in the composer's action row (see
// new-conversation-thread.tsx's ComposerStartRow). Only meaningful once a
// real (non-temporary) environment is selected.
export function FolderPickerInline() {
	const picker = useContext(EnvironmentPickerContext);
	if (!picker || picker.hidden || !picker.environmentId || !picker.environment)
		return null;
	return <FolderPickerBody picker={picker} environment={picker.environment} />;
}

function FolderPickerBody({
	picker,
	environment,
}: {
	picker: EnvironmentPickerState;
	environment: Environment;
}) {
	const { t } = useTranslation("projects");
	const [open, setOpen] = useState(false);
	const [createOpen, setCreateOpen] = useState(false);
	const { projectId, folderId, onFolderChange, disabled } = picker;
	const search = useEnvironmentFolderSearch(
		projectId,
		environment.id,
		open && !disabled,
	);
	const path = useFolderPath(projectId, environment.id, folderId);

	return (
		<>
			<RemoteSelect
				open={open}
				onOpenChange={setOpen}
				disabled={disabled}
				triggerClassName={PILL_TRIGGER}
				label={folderId ? path : undefined}
				placeholder={t("environments.picker.folderPlaceholder")}
			>
				<SearchableList
					remote={search}
					pinned={
						<PinnedRow
							onSelect={() => {
								setOpen(false);
								setCreateOpen(true);
							}}
						>
							<Plus className="size-3.5" />
							{t("environments.picker.folderCreateNew")}
						</PinnedRow>
					}
					items={search.folders.map((folder) => ({
						key: folder.id,
						label: folder.path,
						selected: folder.id === folderId,
						onSelect: () => {
							onFolderChange(folder.id);
							setOpen(false);
						},
						content: (
							<span className="flex-1 truncate text-left font-mono text-xs">
								{folder.path}
							</span>
						),
					}))}
				/>
			</RemoteSelect>
			<FolderCreateDialog
				projectId={projectId}
				environmentId={environment.id}
				environmentStatus={environment.status}
				open={createOpen}
				onOpenChange={setCreateOpen}
				onCreated={(folder) => onFolderChange(folder.id)}
			/>
		</>
	);
}
