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

	const centred =
		"flex size-full flex-col items-center justify-center gap-3 p-6 text-sm";
	return (
		<div className="dark fixed inset-0 z-50 bg-neutral-950 text-white">
			{call.error ? (
				<div className={centred}>
					<p role="alert" className="text-red-300">
						{t(`errors.${call.error}`)}
					</p>
					<Button variant="secondary" onClick={() => window.close()}>
						{t("call.leave")}
					</Button>
				</div>
			) : call.status === "connected" ? (
				<CallPanel
					projectId={scope}
					channelId={channelId}
					title={title}
					canModerate={scope !== "dm" && can("calls:moderate")}
					canInviteAssistant={scope !== "dm" && can("calls:invite_agent")}
					canRecord={scope !== "dm" && can("calls:record")}
				/>
			) : call.status === "idle" && wasIn.current ? (
				<div className={centred}>
					<p className="text-neutral-300">{t("call.ended")}</p>
				</div>
			) : (
				<div className={centred}>
					<Spinner className="size-6" />
					{title ? <p className="text-lg font-medium">{title}</p> : null}
					<p className="text-neutral-400">{t("call.connecting")}</p>
				</div>
			)}
		</div>
	);
}
