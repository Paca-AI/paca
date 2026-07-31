import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { createContext, useContext, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { agentsQueryOptions } from "@/lib/agent-api";

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
	/** Needed to link to the Agents page when the project has none yet. */
	projectId: string;
}

export const AgentPickerContext = createContext<AgentPickerState | null>(null);

// Fetches the project's agents and owns the selected-agent state that feeds
// `AgentPickerState` — shared by the floating chat widget and the
// Conversations page's inline "new conversation" thread so neither duplicates
// the query + selection wiring (or the single-agent auto-select behavior
// below) on its own.
export function useAgentPicker(
	projectId: string,
	options?: { disabled?: boolean },
) {
	const { data: agents = [], isLoading: agentsLoading } = useQuery(
		agentsQueryOptions(projectId),
	);
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
			projectId,
		}),
		[agents, agentsLoading, agentId, disabled, projectId],
	);

	return { agentId, setAgentId, agents, agentsLoading, pickerState };
}

export function AgentPickerInline() {
	const { t } = useTranslation("projects");
	const picker = useContext(AgentPickerContext);
	if (!picker) return null;
	const { agents, agentsLoading, agentId, onAgentChange, disabled, projectId } =
		picker;

	if (agentsLoading) {
		return <div className="h-7 w-32 animate-pulse rounded-full bg-muted" />;
	}
	if (agents.length === 0) {
		return (
			<Button
				size="sm"
				variant="outline"
				className="h-7 gap-1.5 rounded-full text-xs"
				nativeButton={false}
				render={
					<Link
						to="/projects/$projectId/agents"
						params={{ projectId }}
						search={{ create: true }}
					/>
				}
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
