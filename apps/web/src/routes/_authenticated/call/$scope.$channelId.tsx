import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { RouteErrorComponent } from "@/components/route-error-boundary";
import { CallPanel } from "@/components/team-chat/call-panel";
import { CallProvider, useCall } from "@/components/team-chat/call-provider";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { useChatPermissions } from "@/hooks/use-chat-permissions";
import { channelsQueryOptions } from "@/lib/chat-api";

export const Route = createFileRoute("/_authenticated/call/$scope/$channelId")({
	validateSearch: (search: Record<string, unknown>): { name?: string } => ({
		name: typeof search.name === "string" ? search.name : undefined,
	}),
	errorComponent: ({ error }) => <RouteErrorComponent error={error} />,
	component: CallRoute,
});

function CallRoute() {
	return (
		<CallProvider>
			<CallTab />
		</CallProvider>
	);
}

/**
 * The call, full screen in its own browser tab (opened from the channel
 * header). It joins on arrival and closes itself when the person leaves.
 */
function CallTab() {
	const { t } = useTranslation("chat");
	const { scope, channelId } = Route.useParams();
	const { name } = Route.useSearch();
	const call = useCall();
	const { can } = useChatPermissions(scope);
	const channel = useQuery(channelsQueryOptions(scope)).data?.find(
		(c) => c.id === channelId,
	);
	const title = name ?? channel?.peer?.name ?? channel?.name ?? "";

	const started = useRef(false);
	// biome-ignore lint/correctness/useExhaustiveDependencies: join once per channel
	useEffect(() => {
		if (started.current) return;
		started.current = true;
		void call.join({ scope, channelId, channelName: title });
	}, [scope, channelId]);

	useEffect(() => {
		if (title) document.title = title;
	}, [title]);

	// Left (or removed): this tab has nothing more to show.
	const wasIn = useRef(false);
	useEffect(() => {
		if (call.status === "connected") wasIn.current = true;
		if (call.status === "idle" && wasIn.current) window.close();
	}, [call.status]);

	return (
		<div className="fixed inset-0 z-50 flex flex-col bg-background">
			<header className="flex items-center gap-2 border-b px-4 py-2">
				<h1 className="min-w-0 flex-1 truncate text-sm font-semibold">
					{title}
				</h1>
			</header>
			{/* Scrolls when the assistant's captions and suggestion cards make the call
			    taller than the window; my-auto keeps a short call centred without
			    clipping the top of a tall one (justify-center would). */}
			<div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
				<div className="my-auto">
					{call.error ? (
						<div className="flex flex-col items-center gap-3 p-6 text-sm">
							<p role="alert" className="text-destructive">
								{t(`errors.${call.error}`)}
							</p>
							<Button variant="outline" onClick={() => window.close()}>
								{t("call.leave")}
							</Button>
						</div>
					) : call.status === "connected" ? (
						<CallPanel
							projectId={scope}
							channelId={channelId}
							canModerate={scope !== "dm" && can("calls:moderate")}
							canInviteAssistant={scope !== "dm" && can("calls:invite_agent")}
						/>
					) : call.status === "idle" && wasIn.current ? (
						<p className="p-6 text-center text-sm text-muted-foreground">
							{t("call.ended")}
						</p>
					) : (
						<div className="flex items-center justify-center gap-2 p-6 text-sm text-muted-foreground">
							<Spinner /> {t("call.connecting")}
						</div>
					)}
				</div>
			</div>
		</div>
	);
}
