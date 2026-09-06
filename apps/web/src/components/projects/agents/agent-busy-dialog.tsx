import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import {
	ApiErrorCode,
	getApiErrorCode,
	getApiErrorDetails,
} from "@/lib/api-error";

type OnBusyDecision = "queue" | "force";

// Mirrors the server's two independent reasons a dispatch can be held back
// — see services/api's checkDispatchCapacity: the agent's own
// parallelism_limit, or (regardless of that agent's own capacity) another
// conversation, from any agent, already running in the same environment
// folder. Both offer the identical on_busy=queue|force retry; only the
// dialog copy differs. PromptInput is what `ask` takes (everything but the
// resolver, which it attaches itself) — kept as its own explicit union
// rather than `Omit<PendingPrompt, "resolve">`, since Omit over a
// discriminated union isn't distributive and would otherwise flatten the
// two branches into one type where every field is optional.
type Resolve = (decision: OnBusyDecision | "cancel") => void;
type PromptInput =
	| { reason: "parallelism"; running: number; limit: number }
	| { reason: "folder" };
type PendingPrompt = PromptInput & { resolve: Resolve };

/**
 * Drives the "agent is busy" confirm dialog for the chat composer's send
 * flow. Call `send(fn)` instead of `fn()` directly wherever a chat message
 * is dispatched (sendChatMessage/startChatSession and their global-chat
 * siblings): it tries `fn()` first, and only if that rejects with
 * AGENT_PARALLELISM_LIMIT_REACHED (the agent is already at its
 * parallelism_limit of running conversations) or AGENT_ENVIRONMENT_FOLDER_BUSY
 * (another conversation, from any agent, is already running in the same
 * environment folder — see services/api's checkFolderCapacity) does it show
 * a dialog asking whether to queue the message or send it anyway, then
 * retries `fn` with the chosen on_busy value. Cancelling rejects with a
 * plain Error so the caller's own onNew sees a normal failed send rather
 * than silently swallowing the message.
 *
 * Render the returned `dialog` element once, anywhere in the same
 * component — it's inert (closed) until `send` actually needs it.
 */
export function useAgentBusyPrompt() {
	const { t } = useTranslation("projects");
	const [pending, setPending] = useState<PendingPrompt | null>(null);

	const ask = useCallback(
		(prompt: PromptInput) =>
			new Promise<OnBusyDecision | "cancel">((resolve) => {
				setPending({
					...prompt,
					resolve: (decision) => {
						resolve(decision);
						setPending(null);
					},
				});
			}),
		[],
	);

	const send = useCallback(
		async <T,>(fn: (onBusy?: OnBusyDecision) => Promise<T>): Promise<T> => {
			try {
				return await fn();
			} catch (err) {
				const code = getApiErrorCode(err);
				let decision: OnBusyDecision | "cancel";
				if (code === ApiErrorCode.AgentParallelismLimitReached) {
					const details = getApiErrorDetails(err);
					decision = await ask({
						reason: "parallelism",
						running: Number(details?.running ?? 0),
						limit: Number(details?.limit ?? 1),
					});
				} else if (code === ApiErrorCode.AgentEnvironmentFolderBusy) {
					decision = await ask({ reason: "folder" });
				} else {
					throw err;
				}
				if (decision === "cancel") {
					throw new Error(t("agents.busyDialog.cancelledError"));
				}
				return fn(decision);
			}
		},
		[ask, t],
	);

	const dialog = (
		<Dialog
			open={!!pending}
			onOpenChange={(open) => {
				if (!open) pending?.resolve("cancel");
			}}
		>
			<DialogContent className="sm:max-w-sm">
				<DialogHeader>
					<DialogTitle>{t("agents.busyDialog.title")}</DialogTitle>
					<DialogDescription>
						{pending?.reason === "parallelism"
							? t("agents.busyDialog.description", {
									running: pending.running,
									limit: pending.limit,
								})
							: pending?.reason === "folder"
								? t("agents.busyDialog.folderDescription")
								: null}
					</DialogDescription>
				</DialogHeader>
				<DialogFooter>
					<Button variant="outline" onClick={() => pending?.resolve("cancel")}>
						{t("agents.busyDialog.cancel")}
					</Button>
					<Button variant="outline" onClick={() => pending?.resolve("queue")}>
						{t("agents.busyDialog.queue")}
					</Button>
					<Button onClick={() => pending?.resolve("force")}>
						{t("agents.busyDialog.force")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);

	return { dialog, send };
}
