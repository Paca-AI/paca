import { Link } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { createContext, useContext } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";

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
