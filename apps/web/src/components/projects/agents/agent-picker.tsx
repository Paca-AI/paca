import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Plus } from "lucide-react";
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
import { FolderCreateDialog } from "@/components/projects/environments/folder-create-dialog";
import { Button } from "@/components/ui/button";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectSeparator,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { usePermissions } from "@/hooks/use-permissions";
import {
	agentQueryOptions,
	agentsQueryOptions,
	chattableAgentsQueryOptions,
} from "@/lib/agent-api";
import type { Environment, EnvironmentFolder } from "@/lib/environment-api";
import { environmentsQueryOptions } from "@/lib/environment-api";

// ── Agent picker ──────────────────────────────────────────────────────────────
//
// Rendered inline in the composer's action row (`ThreadComponents.ComposerStart`)
// so picking an agent lives in the same box as the message input and send
// button — no separate "pick an agent first" step before the composer shows
// up. `ComposerStart` takes no props, so the picker's data is passed down via
// this context instead. Shared between the floating chat widget and the
// Conversations page's inline "new conversation" thread — same UX, same code.

export interface AgentPickerState {
	agents: { id: string; name: string }[];
	agentsLoading: boolean;
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

// Fetches the project's agents and owns the selected-agent state that feeds
// `AgentPickerState` — shared by the floating chat widget and the
// Conversations page's inline "new conversation" thread so neither duplicates
// the query + selection wiring (or the single-agent auto-select behavior
// below) on its own.
export function useAgentPicker(
	projectId: string,
	options?: { disabled?: boolean; enabled?: boolean },
) {
	const { data: agents = [], isLoading: agentsLoading } = useQuery({
		...agentsQueryOptions(projectId),
		enabled: options?.enabled ?? true,
	});
	const [agentId, setAgentId] = useState("");

	// Nothing to actually pick between — auto-select the project's only agent
	// instead of forcing an explicit choice before the composer will send.
	useEffect(() => {
		if (!agentId && agents.length === 1 && agents[0]) {
			setAgentId(agents[0].id);
		}
	}, [agents, agentId]);

	const disabled = options?.disabled;
	const pickerState = useMemo<AgentPickerState>(
		() => ({
			agents,
			agentsLoading,
			agentId,
			onAgentChange: setAgentId,
			disabled,
			emptyStateLink: {
				to: "/projects/$projectId/agents",
				params: { projectId },
				search: { create: true },
			},
		}),
		[agents, agentsLoading, agentId, disabled, projectId],
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
	const { hasPermission } = usePermissions();
	const { data: agents = [], isLoading: agentsLoading } = useQuery({
		...chattableAgentsQueryOptions,
		enabled: options?.enabled ?? true,
	});
	const [agentId, setAgentId] = useState("");

	useEffect(() => {
		if (!agentId && agents.length === 1 && agents[0]) {
			setAgentId(agents[0].id);
		}
	}, [agents, agentId]);

	const disabled = options?.disabled;
	const canCreate = hasPermission("agents.write");
	const pickerState = useMemo<AgentPickerState>(
		() => ({
			agents,
			agentsLoading,
			agentId,
			onAgentChange: setAgentId,
			disabled,
			emptyStateLink: canCreate
				? { to: "/admin/agents", search: { create: true } }
				: null,
		}),
		[agents, agentsLoading, agentId, disabled, canCreate],
	);

	return { agentId, setAgentId, agents, agentsLoading, pickerState };
}

export function AgentPickerInline() {
	const { t } = useTranslation("projects");
	const picker = useContext(AgentPickerContext);
	if (!picker) return null;
	const {
		agents,
		agentsLoading,
		agentId,
		onAgentChange,
		disabled,
		emptyStateLink,
	} = picker;

	if (agentsLoading) {
		return <div className="h-7 w-32 animate-pulse rounded-full bg-muted" />;
	}
	if (agents.length === 0) {
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

	return (
		<Select
			value={agentId}
			onValueChange={(v) => v && onAgentChange(v)}
			items={agents.map((a) => ({ value: a.id, label: a.name }))}
			disabled={disabled}
		>
			<SelectTrigger
				size="sm"
				className="h-7 rounded-full border-none bg-transparent pl-2.5 text-xs font-medium text-muted-foreground hover:bg-accent disabled:opacity-100"
			>
				<SelectValue placeholder={t("aiChat.selectAgentPlaceholder")} />
			</SelectTrigger>
			<SelectContent align="start">
				{agents.map((agent) => (
					<SelectItem key={agent.id} value={agent.id}>
						{agent.name}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
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

const NO_ENVIRONMENT = "__none__";
const CREATE_NEW_ENVIRONMENT = "__create_environment__";
const CREATE_NEW_FOLDER = "__create_folder__";

export interface EnvironmentPickerState {
	projectId: string;
	environments: Environment[];
	environmentsLoading: boolean;
	environmentId: string;
	onEnvironmentChange: (id: string) => void;
	folders: EnvironmentFolder[];
	folderId: string;
	onFolderChange: (id: string) => void;
	disabled?: boolean;
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
	const { data: environments = [], isLoading: environmentsLoading } = useQuery({
		...environmentsQueryOptions(projectId),
		enabled: queryEnabled,
	});
	// Fetched only to read default_environment_id — this agent is typically
	// already warm in the agent picker's own cache once chosen, so this is
	// usually an instant cache hit rather than a new request.
	const { data: agent } = useQuery({
		...agentQueryOptions(projectId, agentId),
		enabled: queryEnabled && !!agentId,
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
		if (
			!environmentId &&
			agent?.default_environment_id &&
			environments.some((e) => e.id === agent.default_environment_id)
		) {
			setEnvironmentIdState(agent.default_environment_id);
		}
	}, [agent?.default_environment_id, environments, environmentId]);

	const selectedEnvironment = environments.find((e) => e.id === environmentId);
	const folders = selectedEnvironment?.folders ?? [];

	// Exactly one folder — auto-select it instead of forcing an explicit
	// choice, mirroring useAgentPicker's single-option auto-select.
	useEffect(() => {
		if (!folderId && folders.length === 1 && folders[0]) {
			setFolderId(folders[0].id);
		}
	}, [folders, folderId]);

	const onEnvironmentChange = useCallback((id: string) => {
		setEnvironmentIdState(id);
		setFolderId("");
	}, []);

	const disabled = options?.disabled;
	const pickerState = useMemo<EnvironmentPickerState>(
		() => ({
			projectId,
			environments,
			environmentsLoading,
			environmentId,
			onEnvironmentChange,
			folders,
			folderId,
			onFolderChange: setFolderId,
			disabled,
		}),
		[
			projectId,
			environments,
			environmentsLoading,
			environmentId,
			onEnvironmentChange,
			folders,
			folderId,
			disabled,
		],
	);

	return { environmentId, folderId, environments, pickerState };
}

// EnvironmentPickerInline is the environment-only half — the folder half
// lives in FolderPickerInline below. Both render side by side with
// AgentPickerInline in the composer's action row (see
// new-conversation-thread.tsx's ComposerStartRow) — split into two
// components rather than one combined picker so FolderPickerInline can
// stay hidden until a real environment is actually selected.
export function EnvironmentPickerInline() {
	const { t } = useTranslation("projects");
	const picker = useContext(EnvironmentPickerContext);
	const [createOpen, setCreateOpen] = useState(false);
	if (!picker) return null;
	const {
		projectId,
		environments,
		environmentsLoading,
		environmentId,
		onEnvironmentChange,
		disabled,
	} = picker;

	// Nothing to pick from yet — this project hasn't created any static
	// environments, so stay invisible rather than showing an empty picker.
	if (environmentsLoading || environments.length === 0) {
		return null;
	}

	return (
		<>
			<Select
				value={environmentId || NO_ENVIRONMENT}
				onValueChange={(v) => {
					if (!v) return;
					if (v === CREATE_NEW_ENVIRONMENT) {
						setCreateOpen(true);
						return;
					}
					onEnvironmentChange(v === NO_ENVIRONMENT ? "" : v);
				}}
				items={[
					{ value: NO_ENVIRONMENT, label: t("environments.picker.temporary") },
					...environments.map((env) => ({ value: env.id, label: env.name })),
					{
						value: CREATE_NEW_ENVIRONMENT,
						label: t("environments.picker.createNew"),
					},
				]}
				disabled={disabled}
			>
				<SelectTrigger
					size="sm"
					className="h-7 rounded-full border-none bg-transparent pl-2.5 text-xs font-medium text-muted-foreground hover:bg-accent disabled:opacity-100"
				>
					<SelectValue placeholder={t("environments.picker.temporary")} />
				</SelectTrigger>
				<SelectContent align="start">
					<SelectItem value={NO_ENVIRONMENT}>
						{t("environments.picker.temporary")}
					</SelectItem>
					<SelectSeparator />
					{environments.map((env) => (
						<SelectItem key={env.id} value={env.id}>
							{env.name}
						</SelectItem>
					))}
					<SelectSeparator />
					<SelectItem value={CREATE_NEW_ENVIRONMENT}>
						<Plus className="size-3.5" />
						{t("environments.picker.createNew")}
					</SelectItem>
				</SelectContent>
			</Select>
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
	const { t } = useTranslation("projects");
	const picker = useContext(EnvironmentPickerContext);
	const [createOpen, setCreateOpen] = useState(false);
	if (!picker) return null;
	const {
		projectId,
		environments,
		environmentId,
		folders,
		folderId,
		onFolderChange,
		disabled,
	} = picker;

	if (!environmentId) return null;
	const selectedEnvironment = environments.find((e) => e.id === environmentId);
	if (!selectedEnvironment) return null;

	return (
		<>
			<Select
				value={folderId}
				onValueChange={(v) => {
					if (!v) return;
					if (v === CREATE_NEW_FOLDER) {
						setCreateOpen(true);
						return;
					}
					onFolderChange(v);
				}}
				items={[
					...folders.map((folder) => ({
						value: folder.id,
						label: folder.path,
					})),
					{
						value: CREATE_NEW_FOLDER,
						label: t("environments.picker.folderCreateNew"),
					},
				]}
				disabled={disabled}
			>
				<SelectTrigger
					size="sm"
					className="h-7 rounded-full border-none bg-transparent pl-2.5 text-xs font-medium text-muted-foreground hover:bg-accent disabled:opacity-100"
				>
					<SelectValue
						placeholder={t("environments.picker.folderPlaceholder")}
					/>
				</SelectTrigger>
				<SelectContent align="start">
					{folders.map((folder) => (
						<SelectItem key={folder.id} value={folder.id}>
							{folder.path}
						</SelectItem>
					))}
					{folders.length > 0 && <SelectSeparator />}
					<SelectItem value={CREATE_NEW_FOLDER}>
						<Plus className="size-3.5" />
						{t("environments.picker.folderCreateNew")}
					</SelectItem>
				</SelectContent>
			</Select>
			<FolderCreateDialog
				projectId={projectId}
				environmentId={environmentId}
				environmentStatus={selectedEnvironment.status}
				open={createOpen}
				onOpenChange={setCreateOpen}
				onCreated={(folder) => onFolderChange(folder.id)}
			/>
		</>
	);
}
