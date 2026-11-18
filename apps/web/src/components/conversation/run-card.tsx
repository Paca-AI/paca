import { Link } from "@tanstack/react-router";
import { Check, CircleAlert, Loader2, Pause, Square } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export type RunStatus =
	| "queued"
	| "running"
	| "paused"
	| "finished"
	| "failed"
	| "stopped";

const TONE: Record<RunStatus, string> = {
	queued: "text-muted-foreground",
	running: "text-primary",
	paused: "text-muted-foreground",
	finished: "text-emerald-600 dark:text-emerald-400",
	failed: "text-destructive",
	stopped: "text-muted-foreground",
};

/** An agent run's state as an icon. A run that is working spins. */
export function RunStatusIcon({
	status,
	className,
}: {
	status: string;
	className?: string;
}) {
	const s = (status in TONE ? status : "stopped") as RunStatus;
	const cls = cn("size-3.5 shrink-0", TONE[s], className);
	switch (s) {
		case "queued":
		case "running":
			return <Loader2 className={cn(cls, "animate-spin")} aria-hidden />;
		case "finished":
			return <Check className={cls} aria-hidden />;
		case "failed":
			return <CircleAlert className={cls} aria-hidden />;
		case "paused":
			return <Pause className={cls} aria-hidden />;
		default:
			return <Square className={cls} aria-hidden />;
	}
}

/**
 * A pointer from a message to the agent run behind it ("View run"), for the
 * replies an agent wrote in a direct message. The run itself opens in the full
 * conversation view, with its reasoning and tool calls.
 */
export function RunLink({
	runId,
	status,
	children,
}: {
	runId: string;
	status?: string;
	children: ReactNode;
}) {
	return (
		<Link
			to="/conversations/$conversationId"
			params={{ conversationId: runId }}
			className="mt-1 inline-flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-primary hover:underline"
		>
			{status ? <RunStatusIcon status={status} /> : null}
			{children}
		</Link>
	);
}
