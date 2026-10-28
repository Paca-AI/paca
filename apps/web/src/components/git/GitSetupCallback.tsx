import { useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	beginAgentGitAccess,
	completeGitAccess,
	completeGitSetup,
	completeProjectGitSetup,
	followGitStep,
	forgetGitFlow,
	GIT_FALLBACK_PATH,
	type GitAccessCompletion,
	type GitSetupCompletion,
	gitErrorKey,
	gitQueriesKey,
	projectGitSettingsPath,
	readGitFlow,
	rememberGitFlow,
	returnPathOf,
	takeAgentInstall,
} from "@/lib/git-api";

/**
 * One call per distinct callback URL. A setup state can be completed once per
 * leg, so a second call (React StrictMode mounts effects twice in development,
 * a user reloading the tab) must reuse the first call's outcome instead of
 * sending the one-time parameters again.
 */
type Outcome =
	| { flow: "identity"; result: GitSetupCompletion }
	| { flow: "access"; result: GitAccessCompletion };

const inflight = new Map<string, Promise<Outcome>>();

/** Reads the callback's parameters from the raw query string. */
export function parseCallback(search: string): {
	state: string;
	params: Record<string, string>;
} {
	const all = new URLSearchParams(search);
	const state = all.get("state") ?? "";
	const params: Record<string, string> = {};
	for (const [k, v] of all) {
		if (k !== "state") params[k] = v;
	}
	return { state, params };
}

/**
 * Completes whichever flow started this round trip: a project's access grant
 * (installing the App on a repository owner's account), a project's own
 * connection, or a workspace connection or agent identity.
 * The browser's note of the flow only routes the call; the API checks the
 * state, its owner and its project itself.
 */
function complete(search: string): Promise<Outcome> {
	const existing = inflight.get(search);
	if (existing) return existing;
	const { state, params } = parseCallback(search);
	const hint = readGitFlow(state);
	const identity = (call: Promise<GitSetupCompletion>) =>
		call.then((result) => ({ flow: "identity" as const, result }));
	let p: Promise<Outcome>;
	switch (hint.kind) {
		case "access":
			p = completeGitAccess(hint.projectId, state, params).then((result) => ({
				flow: "access" as const,
				result,
			}));
			break;
		case "project-identity":
			p = identity(completeProjectGitSetup(hint.projectId, state, params));
			break;
		default:
			p = identity(completeGitSetup(state, params));
	}
	inflight.set(search, p);
	return p;
}

/**
 * Where a Git platform sends the browser back after the user approved
 * something there (GitHub: creating the App, or installing it on an account).
 * It hands the redirect's parameters to the API, which runs the provider's next
 * step, and then either continues to the platform (another leg) or returns the
 * user to where they started.
 *
 * The parameters are read from the raw query string, not from the router's
 * parsed search: the router would turn a code like "1e5" into a number.
 */
export function GitSetupCallback({ search }: { search: string }) {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const [errorKey, setErrorKey] = useState<string | null>(null);
	// Where "back" goes when something fails: the project the flow started from.
	const hint = readGitFlow(parseCallback(search).state);
	const backTo =
		hint.kind === "identity"
			? GIT_FALLBACK_PATH
			: projectGitSettingsPath(hint.projectId);

	useEffect(() => {
		let cancelled = false;
		if (!parseCallback(search).state) {
			setErrorKey("setupInvalid");
			return;
		}
		complete(search)
			.then((out) => {
				if (cancelled) return;
				forgetGitFlow(parseCallback(search).state);
				if (out.flow === "access") {
					window.location.replace(returnPathOf(out.result.return_url));
					return;
				}
				void queryClient.invalidateQueries({ queryKey: gitQueriesKey });
				if (out.result.next) {
					if (!followGitStep(out.result.next)) setErrorKey("generic");
					return;
				}
				const back = () =>
					window.location.replace(returnPathOf(out.result.return_url));
				// An agent's App serves one project, so the user installs it now
				// instead of having to remember to later. If that cannot start,
				// the agent tab still offers the install.
				const pending =
					out.result.identity.scope === "agent" ? takeAgentInstall() : null;
				if (!pending) {
					back();
					return;
				}
				beginAgentGitAccess(pending.projectId, pending.agentId, {
					provider_id: out.result.identity.provider_id,
					owner: "",
					return_url: out.result.return_url,
				})
					.then((step) => {
						rememberGitFlow(step.state, {
							kind: "access",
							projectId: pending.projectId,
						});
						if (!followGitStep(step)) back();
					})
					.catch(back);
			})
			.catch((err: unknown) => {
				if (!cancelled) setErrorKey(gitErrorKey(err));
			});
		return () => {
			cancelled = true;
		};
	}, [search, queryClient]);

	if (errorKey) {
		return (
			<div className="flex flex-1 items-center justify-center p-6">
				<div
					role="alert"
					className="flex max-w-md flex-col items-center gap-3 text-center"
				>
					<AlertCircle className="size-8 text-destructive" />
					<h1 className="text-lg font-semibold">
						{t("git.callback.failedTitle")}
					</h1>
					<p className="text-sm text-muted-foreground">
						{t(`git.errors.${errorKey}` as never)}
					</p>
					<Button onClick={() => window.location.assign(backTo)}>
						{t("git.callback.back")}
					</Button>
				</div>
			</div>
		);
	}
	return (
		<div className="flex flex-1 items-center justify-center p-6">
			<div className="flex flex-col items-center gap-3 text-center">
				<Loader2 className="size-8 animate-spin text-primary" />
				<h1 className="text-lg font-semibold">{t("git.callback.working")}</h1>
				<p className="text-sm text-muted-foreground">
					{t("git.callback.workingHint")}
				</p>
			</div>
		</div>
	);
}
