import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
	type Participant,
	type RemoteTrack,
	Track,
	type TrackPublication,
} from "livekit-client";
import {
	Mic,
	MicOff,
	Monitor,
	MonitorOff,
	MoreVertical,
	PhoneOff,
	Video,
	VideoOff,
} from "lucide-react";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { callKeys, muteParticipant, removeParticipant } from "@/lib/call-api";
import { chatErrorKey } from "@/lib/chat-errors";
import { cn } from "@/lib/utils";
import { useCall } from "./call-provider";

function initials(name: string): string {
	const parts = name.trim().split(/\s+/).filter(Boolean);
	return (
		(parts[0]?.[0] ?? "?") + (parts.length > 1 ? (parts.at(-1)?.[0] ?? "") : "")
	).toUpperCase();
}

function VideoView({ track }: { track: Track | undefined }) {
	const ref = useRef<HTMLVideoElement>(null);
	useEffect(() => {
		const el = ref.current;
		if (!el || !track) return;
		track.attach(el);
		return () => {
			track.detach(el);
		};
	}, [track]);
	return (
		<video
			ref={ref}
			autoPlay
			playsInline
			muted
			className="size-full object-cover"
		/>
	);
}

/** Plays a remote microphone. Muted when the browser has not allowed sound yet. */
function AudioView({ track }: { track: RemoteTrack }) {
	const ref = useRef<HTMLAudioElement>(null);
	useEffect(() => {
		const el = ref.current;
		if (!el) return;
		track.attach(el);
		return () => {
			track.detach(el);
		};
	}, [track]);
	// A live call has no captions to offer: this is the other person's voice.
	// biome-ignore lint/a11y/useMediaCaption: live call audio
	return <audio ref={ref} autoPlay />;
}

function trackOf(
	p: Participant,
	source: Track.Source,
): { pub: TrackPublication | undefined; track: Track | undefined } {
	const pub = p.getTrackPublication(source);
	return { pub, track: pub?.track };
}

function Tile({
	participant,
	local,
	label,
	source,
	canModerate,
	onMute,
	onRemove,
}: {
	participant: Participant;
	local: boolean;
	label: string;
	source: Track.Source.Camera | Track.Source.ScreenShare;
	canModerate: boolean;
	onMute: () => void;
	onRemove: () => void;
}) {
	const { t } = useTranslation("chat");
	const { pub, track } = trackOf(participant, source);
	const showVideo = !!track && !pub?.isMuted;
	const mic = participant.getTrackPublication(Track.Source.Microphone);
	const muted = !mic || mic.isMuted;
	const name = participant.name || participant.identity;
	return (
		<div
			className={cn(
				"relative aspect-video overflow-hidden rounded-lg border bg-muted",
				participant.isSpeaking && "ring-2 ring-emerald-500",
			)}
		>
			{showVideo ? (
				<VideoView track={track} />
			) : (
				<div className="flex size-full items-center justify-center">
					<span
						aria-hidden
						className="flex size-14 items-center justify-center rounded-full bg-primary/15 text-lg font-semibold"
					>
						{initials(name)}
					</span>
				</div>
			)}
			<div className="absolute inset-x-0 bottom-0 flex items-center gap-1.5 bg-gradient-to-t from-black/60 to-transparent px-2 py-1.5 text-xs text-white">
				{source === Track.Source.Camera && muted ? (
					<MicOff className="size-3.5" aria-label={t("call.micOff")} />
				) : null}
				<span className="truncate">{label}</span>
			</div>
			{!local && canModerate && source === Track.Source.Camera ? (
				<DropdownMenu>
					<DropdownMenuTrigger
						render={
							<Button
								size="icon-sm"
								variant="secondary"
								className="absolute right-1.5 top-1.5 opacity-80"
								aria-label={t("call.moderate", { name })}
							/>
						}
					>
						<MoreVertical />
					</DropdownMenuTrigger>
					<DropdownMenuContent align="end">
						<DropdownMenuItem onClick={onMute}>
							{t("call.muteParticipant", { name })}
						</DropdownMenuItem>
						<DropdownMenuItem variant="destructive" onClick={onRemove}>
							{t("call.removeParticipant", { name })}
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenu>
			) : null}
		</div>
	);
}

/**
 * The call in a channel: tiles for everyone connected, and the controls.
 * Rendered only for the channel the person is in a call in; CallProvider owns
 * the room, so leaving this page does not end the call.
 */
export function CallPanel({
	projectId,
	channelId,
	canModerate,
	onError,
}: {
	projectId: string;
	channelId: string;
	/** The caller may mute and remove others (calls:moderate). */
	canModerate: boolean;
	onError: (message: string | null) => void;
}) {
	const { t } = useTranslation("chat");
	const queryClient = useQueryClient();
	const call = useCall();
	const { room } = call;

	const moderate = useMutation({
		mutationFn: (v: { accountId: string; remove: boolean }) =>
			v.remove
				? removeParticipant(projectId, channelId, v.accountId)
				: muteParticipant(projectId, channelId, v.accountId),
		onError: (e) => onError(t(`errors.${chatErrorKey(e)}`)),
		onSuccess: () => {
			onError(null);
			void queryClient.invalidateQueries({
				queryKey: callKeys.channel(projectId, channelId),
			});
		},
	});

	if (!room || call.target?.channelId !== channelId || call.status === "idle") {
		return null;
	}

	const everyone: Participant[] = [
		room.localParticipant,
		...Array.from(room.remoteParticipants.values()),
	];
	const sharing = everyone.filter(
		(p) => trackOf(p, Track.Source.ScreenShare).track,
	);
	const lp = room.localParticipant;

	return (
		<section
			aria-label={t("call.panelLabel")}
			className="border-b bg-background p-3"
		>
			{/* Remote microphones: heard even when nothing is on screen for them. */}
			{Array.from(room.remoteParticipants.values()).flatMap((p) =>
				[Track.Source.Microphone, Track.Source.ScreenShareAudio].map((src) => {
					const track = p.getTrackPublication(src)?.track;
					return track ? (
						<AudioView
							key={`${p.identity}-${src}`}
							track={track as RemoteTrack}
						/>
					) : null;
				}),
			)}
			<div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
				{sharing.map((p) => (
					<Tile
						key={`${p.identity}-screen`}
						participant={p}
						local={p === lp}
						label={t("call.screenOf", {
							name: p === lp ? t("call.you") : p.name || p.identity,
						})}
						source={Track.Source.ScreenShare}
						canModerate={false}
						onMute={() => undefined}
						onRemove={() => undefined}
					/>
				))}
				{everyone.map((p) => (
					<Tile
						key={p.identity}
						participant={p}
						local={p === lp}
						label={p === lp ? t("call.you") : p.name || p.identity}
						source={Track.Source.Camera}
						canModerate={canModerate}
						onMute={() =>
							moderate.mutate({ accountId: p.identity, remove: false })
						}
						onRemove={() =>
							moderate.mutate({ accountId: p.identity, remove: true })
						}
					/>
				))}
			</div>
			{call.mediaBlocked ? (
				<p role="alert" className="mt-2 text-xs text-destructive">
					{t("call.mediaBlocked")}
				</p>
			) : null}
			<div className="mt-3 flex items-center justify-center gap-2">
				<Button
					size="icon"
					variant={lp.isMicrophoneEnabled ? "secondary" : "destructive"}
					aria-label={
						lp.isMicrophoneEnabled ? t("call.mute") : t("call.unmute")
					}
					aria-pressed={!lp.isMicrophoneEnabled}
					onClick={call.toggleMic}
				>
					{lp.isMicrophoneEnabled ? <Mic /> : <MicOff />}
				</Button>
				<Button
					size="icon"
					variant="secondary"
					aria-label={
						lp.isCameraEnabled ? t("call.cameraOff") : t("call.cameraOn")
					}
					aria-pressed={lp.isCameraEnabled}
					onClick={call.toggleCamera}
				>
					{lp.isCameraEnabled ? <Video /> : <VideoOff />}
				</Button>
				<Button
					size="icon"
					variant="secondary"
					aria-label={
						lp.isScreenShareEnabled
							? t("call.stopShare")
							: t("call.shareScreen")
					}
					aria-pressed={lp.isScreenShareEnabled}
					onClick={call.toggleScreen}
				>
					{lp.isScreenShareEnabled ? <MonitorOff /> : <Monitor />}
				</Button>
				<Button
					size="icon"
					variant="destructive"
					aria-label={t("call.leave")}
					onClick={call.leave}
				>
					<PhoneOff />
				</Button>
			</div>
		</section>
	);
}
