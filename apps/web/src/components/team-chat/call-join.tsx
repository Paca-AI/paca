import { useQuery } from "@tanstack/react-query";
import { Headphones, Phone, Volume2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { currentUserQueryOptions } from "@/lib/auth-api";
import {
	callConfigQueryOptions,
	channelCallQueryOptions,
} from "@/lib/call-api";
import { openCallTab } from "@/lib/call-window";
import type { Channel } from "@/lib/chat-api";

/** What a channel's call looks like to someone who has not joined it. */
export function useChannelCall(
	scope: string,
	channel: Channel | undefined,
	canJoin: boolean,
) {
	const enabled =
		useQuery({ ...callConfigQueryOptions, enabled: canJoin }).data === true;
	const open = enabled && !!channel && !channel.archived_at;
	const participants =
		useQuery(channelCallQueryOptions(scope, channel?.id ?? "", open)).data
			?.participants ?? [];
	const meId = useQuery(currentUserQueryOptions).data?.id;
	// Already in it, from this or another tab: the button then goes back to it.
	const inCall = participants.some((p) => p.account_id === meId);
	const join = () => {
		if (!channel) return;
		openCallTab(scope, channel.id, channel.peer?.name ?? channel.name);
	};
	return {
		/** Calls are on, the channel is open and the caller may join. */
		available: open && canJoin,
		participants,
		inCall,
		join,
	};
}

type ChannelCall = ReturnType<typeof useChannelCall>;

/** The channel header's button: start a huddle, or join the one running. */
export function CallHeaderButton({
	call,
	voice,
}: {
	call: ChannelCall;
	voice: boolean;
}) {
	const { t } = useTranslation("chat");
	if (!call.available) return null;
	const running = call.participants.length > 0;
	return (
		<Button
			size="sm"
			variant={running ? "default" : "outline"}
			onClick={call.join}
		>
			{voice ? <Volume2 /> : <Headphones />}
			{call.inCall
				? t("call.backToCall")
				: voice
					? t("call.joinVoice")
					: running
						? t("call.join")
						: t("call.startHuddle")}
		</Button>
	);
}

/** Above the messages: who is in the call, with a way to join. */
export function CallBanner({
	call,
	voice,
}: {
	call: ChannelCall;
	voice: boolean;
}) {
	const { t } = useTranslation("chat");
	if (!call.available) return null;
	const running = call.participants.length > 0;
	// A voice channel always says so, even empty; a huddle only while it runs.
	if (!running && !voice) return null;
	return (
		<div className="flex items-center gap-3 border-b bg-muted/40 px-4 py-2 text-sm">
			<Phone className="size-4 shrink-0 text-emerald-600" aria-hidden />
			<p className="min-w-0 flex-1 truncate">
				{running
					? t("call.inCallCount", { count: call.participants.length }) +
						": " +
						call.participants.map((p) => p.name).join(", ")
					: t("call.voiceEmpty")}
			</p>
			<Button size="sm" onClick={call.join}>
				{call.inCall ? t("call.backToCall") : t("call.join")}
			</Button>
		</div>
	);
}
