import { Link } from "@tanstack/react-router";
import { History, Search } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { RunStatusIcon } from "@/components/conversation/run-card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import type { AgentRun } from "@/lib/chat-api";
import { timeAgo } from "@/lib/time-ago";
import { cn } from "@/lib/utils";

/** Which runs match a search and a status filter, newest first as given. */
export function filterRuns(
	runs: AgentRun[],
	query: string,
	status: AgentRun["status"] | "all",
): AgentRun[] {
	const q = query.trim().toLowerCase();
	return runs.filter(
		(r) =>
			(status === "all" || r.status === status) &&
			(q === "" || r.title.toLowerCase().includes(q)),
	);
}

/**
 * The runs behind a direct message with an agent, kept out of the way: the
 * conversation itself is one continuous thread, and this is where to find an
 * earlier piece of work. Each run opens in the full conversation view.
 */
export function AgentRunsPanel({
	runs,
	agentName,
}: {
	runs: AgentRun[];
	agentName: string;
}) {
	const { t } = useTranslation("chat");
	const { t: tCommon } = useTranslation("common");
	const [query, setQuery] = useState("");
	const [status, setStatus] = useState<AgentRun["status"] | "all">("all");
	const present = useMemo(
		() => [...new Set(runs.map((r) => r.status))],
		[runs],
	);
	const shown = filterRuns(runs, query, status);

	return (
		<Popover>
			<PopoverTrigger
				render={
					<Button size="sm" variant="ghost" aria-label={t("agent.runsTitle")} />
				}
			>
				<History />
				{t("agent.runs", { count: runs.length })}
			</PopoverTrigger>
			<PopoverContent align="end" className="w-80 gap-2 p-2">
				{runs.length === 0 ? (
					<p className="px-2 py-4 text-center text-sm text-muted-foreground">
						{t("agent.empty", { name: agentName })}
					</p>
				) : (
					<>
						<div className="relative">
							<Search className="pointer-events-none absolute top-2 left-2 size-4 text-muted-foreground" />
							<Input
								value={query}
								onChange={(e) => setQuery(e.target.value)}
								placeholder={t("agent.search")}
								aria-label={t("agent.search")}
								className="pl-8"
							/>
						</div>
						{present.length > 1 ? (
							<div className="flex flex-wrap gap-1">
								{(["all", ...present] as const).map((s) => (
									<button
										key={s}
										type="button"
										aria-pressed={status === s}
										onClick={() => setStatus(s)}
										className={cn(
											"rounded-full border px-2 py-0.5 text-xs",
											status === s
												? "border-primary bg-primary text-primary-foreground"
												: "text-muted-foreground hover:text-foreground",
										)}
									>
										{s === "all" ? t("agent.all") : t(`agent.status.${s}`)}
									</button>
								))}
							</div>
						) : null}
						<ul className="max-h-72 overflow-y-auto">
							{shown.map((r) => (
								<li key={r.id}>
									<Link
										to="/conversations/$conversationId"
										params={{ conversationId: r.id }}
										aria-label={`${t("agent.open")}: ${r.title || t("agent.untitled")}`}
										className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-muted"
									>
										<RunStatusIcon status={r.status} />
										<span className="min-w-0 flex-1 truncate">
											{r.title || t("agent.untitled")}
											{r.channel_name ? (
												<span className="ml-1.5 rounded bg-muted px-1 text-[10px] text-muted-foreground">
													{t("agent.fromChannel", { channel: r.channel_name })}
												</span>
											) : null}
										</span>
										<span className="shrink-0 text-xs text-muted-foreground">
											{timeAgo(r.created_at, tCommon)}
										</span>
									</Link>
								</li>
							))}
							{shown.length === 0 ? (
								<li className="px-2 py-3 text-center text-sm text-muted-foreground">
									{t("agent.noMatch")}
								</li>
							) : null}
						</ul>
					</>
				)}
			</PopoverContent>
		</Popover>
	);
}
