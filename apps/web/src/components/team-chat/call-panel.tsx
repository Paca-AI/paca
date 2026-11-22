import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
	type Participant,
	type RemoteTrack,
	Track,
	type TrackPublication,
} from "livekit-client";
import {
	ChevronUp,
	Mic,
	MicOff,
	Monitor,
	MonitorOff,
	MoreVertical,
	PhoneOff,
	Sparkles,
	Users,
	Video,
	VideoOff,
	X,
} from "lucide-react";
import { type ComponentProps, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuCheckboxItem,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { callKeys, muteParticipant, removeParticipant } from "@/lib/call-api";
import { DM_SCOPE } from "@/lib/chat-api";
import { chatErrorKey } from "@/lib/chat-errors";
import { cn } from "@/lib/utils";
import {
	AssistantSection,
	CaptionsStrip,
	ProposalsSection,
} from "./call-assistant";
import { useCall } from "./call-provider";
import { RecordButton, RecordingNotice } from "./call-recording";

type SidePanel = "people" | "assistant";

const AVATAR_COLORS = [
	"bg-sky-700",
	"bg-emerald-700",
	"bg-violet-700",
	"bg-rose-700",
	"bg-amber-700",
	"bg-teal-700",
	"bg-indigo-700",
	"bg-fuchsia-700",
];

/** The same person always gets the same colour, so tiles are easy to tell apart. */
function avatarColor(name: string): string {
	let h = 0;
	for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
	return AVATAR_COLORS[h % AVATAR_COLORS.length] as string;
}

function initials(name: string): string {
	const parts = name.trim().split(/\s+/).filter(Boolean);
	return (
		(parts[0]?.[0] ?? "?") + (parts.length > 1 ? (parts.at(-1)?.[0] ?? "") : "")
	).toUpperCase();
}

function Avatar({ name, className }: { name: string; className?: string }) {
	return (
		<span
			aria-hidden
			className={cn(
				"flex shrink-0 items-center justify-center rounded-full font-medium text-white",
				avatarColor(name),
				className,
			)}
		>
			{initials(name)}
		</span>
	);
}

function VideoView({
	track,
	mirror,
	contain,
}: {
	track: Track | undefined;
	mirror?: boolean;
	contain?: boolean;
}) {
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
			className={cn(
				"size-full",
				contain ? "object-contain" : "object-cover",
				mirror && "-scale-x-100",
			)}
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

function isMicOff(p: Participant): boolean {
	const mic = p.getTrackPublication(Track.Source.Microphone);
	return !mic || mic.isMuted;
}

/** Mute / remove, for someone with calls:moderate. */
function ModerationMenu({
	name,
	className,
	onMute,
	onRemove,
}: {
	name: string;
	className?: string;
	onMute: () => void;
	onRemove: () => void;
}) {
	const { t } = useTranslation("chat");
	return (
		<DropdownMenu>
			<DropdownMenuTrigger
				render={
					<Button
						size="icon-sm"
						variant="secondary"
						className={cn(
							"rounded-full bg-transparent hover:bg-muted",
							className,
						)}
						aria-label={t("call.moderate", { name })}
					/>
				}
			>
				<MoreVertical />
			</DropdownMenuTrigger>
			<DropdownMenuContent align="end" className="dark">
				<DropdownMenuItem onClick={onMute}>
					{t("call.muteParticipant", { name })}
				</DropdownMenuItem>
				<DropdownMenuItem variant="destructive" onClick={onRemove}>
					{t("call.removeParticipant", { name })}
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

function Tile({
	participant,
	local,
	label,
	source,
	menu,
	className,
}: {
	participant: Participant;
	local: boolean;
	label: string;
	source: Track.Source.Camera | Track.Source.ScreenShare;
	/** Moderation controls, shown on hover. */
	menu?: React.ReactNode;
	className?: string;
}) {
	const { t } = useTranslation("chat");
	const { pub, track } = trackOf(participant, source);
	const showVideo = !!track && !pub?.isMuted;
	const camera = source === Track.Source.Camera;
	const micOff = camera && isMicOff(participant);
	const name = participant.name || participant.identity;
	const speaking = camera && participant.isSpeaking;
	return (
		<div
			className={cn(
				"group relative min-h-0 min-w-0 overflow-hidden rounded-2xl bg-neutral-800 transition-shadow [container-type:size]",
				speaking && "ring-[3px] ring-sky-400",
				!camera && "bg-black",
				className,
			)}
		>
			{showVideo ? (
				<VideoView track={track} mirror={local && camera} contain={!camera} />
			) : (
				<div className="flex size-full items-center justify-center">
					<Avatar
						name={name}
						className="size-[clamp(3.5rem,18cqmin,8rem)] text-[clamp(1.25rem,7cqmin,3rem)]"
					/>
				</div>
			)}
			<div className="pointer-events-none absolute inset-x-0 bottom-0 flex items-center gap-1.5 bg-gradient-to-t from-black/60 to-transparent px-3 pb-2 pt-6 text-sm text-white">
				<span className="truncate font-medium drop-shadow">{label}</span>
			</div>
			{micOff ? (
				<span
					role="img"
					aria-label={t("call.micOff")}
					className="absolute right-2 top-2 flex size-7 items-center justify-center rounded-full bg-black/60 text-white"
				>
					<MicOff className="size-4" />
				</span>
			) : speaking ? (
				<span
					aria-hidden
					className="absolute right-2 top-2 flex size-7 items-center justify-center rounded-full bg-sky-500 text-white"
				>
					<Mic className="size-4" />
				</span>
			) : null}
			{menu ? (
				<div className="absolute left-2 top-2 text-white opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100">
					{menu}
				</div>
			) : null}
		</div>
	);
}

/** Columns for a gallery of n tiles: one person fills the stage, as in Meet. */
function galleryColumns(n: number): number {
	if (n <= 1) return 1;
	if (n <= 4) return 2;
	if (n <= 9) return 3;
	return 4;
}

function Clock() {
	const [now, setNow] = useState(() => new Date());
	useEffect(() => {
		const id = window.setInterval(() => setNow(new Date()), 15_000);
		return () => window.clearInterval(id);
	}, []);
	return (
		<time dateTime={now.toISOString()} className="tabular-nums">
			{now.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })}
		</time>
	);
}

/** The People / Assistant buttons: light on the dark bar, filled while open. */
function panelButtonClass(active: boolean): string {
	return cn(
		"relative",
		active
			? "bg-sky-300 text-neutral-900 hover:bg-sky-200 hover:text-neutral-900"
			: "bg-transparent text-white hover:bg-white/10 hover:text-white",
	);
}

/** A round control in the bottom bar. */
function BarButton({
	off,
	className,
	...props
}: ComponentProps<typeof Button> & { off?: boolean }) {
	return (
		<Button
			size="icon"
			variant={off ? "destructive" : "secondary"}
			className={cn(
				"size-10 rounded-full md:size-12 [&_svg:not([class*='size-'])]:size-5",
				className,
			)}
			{...props}
		/>
	);
}

/**
 * The call in a channel, laid out like Google Meet: the people fill a dark
 * stage, the controls sit in a bar along the bottom, and the assistant and the
 * list of people open in a panel on the right. Rendered only for the channel
 * the person is in a call in; CallProvider owns the room.
 */
export function CallPanel({
	projectId,
	channelId,
	title,
	canModerate,
	canInviteAssistant = false,
	canRecord = false,
}: {
	projectId: string;
	channelId: string;
	/** Shown at the left of the bottom bar, next to the time. */
	title?: string;
	/** The caller may mute and remove others (calls:moderate). */
	canModerate: boolean;
	/** The caller may bring a meeting assistant into the call (calls:invite_agent). */
	canInviteAssistant?: boolean;
	/** The caller may record the call (calls:record). */
	canRecord?: boolean;
}) {
	const { t } = useTranslation("chat");
	const queryClient = useQueryClient();
	const call = useCall();
	const { room } = call;
	const [panel, setPanel] = useState<SidePanel | null>(null);
	// A failed mute or remove (the person already left, access was revoked,
	// LiveKit is unreachable) is shown until the next attempt works.
	const [moderationError, setModerationError] = useState<string | null>(null);

	// A proposal for this person to review opens the assistant panel.
	const arrivalCount = call.arrivals.length;
	const seenArrivals = useRef(arrivalCount);
	const [unseen, setUnseen] = useState(false);
	const panelRef = useRef(panel);
	panelRef.current = panel;
	useEffect(() => {
		if (arrivalCount > seenArrivals.current) {
			const open = panelRef.current;
			if (open === null) setPanel("assistant");
			// Already looking at it: nothing is unseen.
			else if (open !== "assistant") setUnseen(true);
		}
		seenArrivals.current = arrivalCount;
	}, [arrivalCount]);
	useEffect(() => {
		if (panel === "assistant") setUnseen(false);
	}, [panel]);

	const moderate = useMutation({
		mutationFn: (v: { accountId: string; remove: boolean }) =>
			v.remove
				? removeParticipant(projectId, channelId, v.accountId)
				: muteParticipant(projectId, channelId, v.accountId),
		onError: (e) => setModerationError(t(`errors.${chatErrorKey(e)}`)),
		onSuccess: () => {
			setModerationError(null);
			void queryClient.invalidateQueries({
				queryKey: callKeys.channel(projectId, channelId),
			});
		},
	});

	if (!room || call.target?.channelId !== channelId || call.status === "idle") {
		return null;
	}

	const lp = room.localParticipant;
	const remotes = Array.from(room.remoteParticipants.values());
	const everyone: Participant[] = [lp, ...remotes];
	const sharing = everyone.filter(
		(p) => trackOf(p, Track.Source.ScreenShare).track,
	);
	const displayName = (p: Participant) =>
		p === lp ? t("call.you") : p.name || p.identity;
	// Who said what, for captions and the proposal chips.
	const names = new Map<string, string>(
		everyone.map((p) => [p.identity, displayName(p)]),
	);
	// A direct message has no project, so no assistants.
	const hasProject = projectId !== DM_SCOPE;
	const togglePanel = (which: SidePanel) =>
		setPanel((p) => (p === which ? null : which));

	const menuFor = (p: Participant) =>
		canModerate && p !== lp ? (
			<ModerationMenu
				name={p.name || p.identity}
				className="bg-black/50 hover:bg-black/70"
				onMute={() => moderate.mutate({ accountId: p.identity, remove: false })}
				onRemove={() =>
					moderate.mutate({ accountId: p.identity, remove: true })
				}
			/>
		) : undefined;

	const cameraTile = (p: Participant, className?: string) => (
		<Tile
			key={p.identity}
			participant={p}
			local={p === lp}
			label={displayName(p)}
			source={Track.Source.Camera}
			menu={menuFor(p)}
			className={className}
		/>
	);
	const screenTile = (p: Participant, className?: string) => (
		<Tile
			key={`${p.identity}-screen`}
			participant={p}
			local={p === lp}
			label={t("call.screenOf", { name: displayName(p) })}
			source={Track.Source.ScreenShare}
			className={className}
		/>
	);

	const cols = galleryColumns(everyone.length);
	const rows = Math.ceil(everyone.length / cols);
	const [presented, ...otherShares] = sharing;

	return (
		<section
			aria-label={t("call.panelLabel")}
			className="dark relative flex size-full min-h-0 flex-col bg-neutral-950 text-white"
		>
			{/* Remote microphones: heard even when nothing is on screen for them. */}
			{remotes.flatMap((p) =>
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

			<div className="relative flex min-h-0 flex-1 gap-3 p-3 pb-0">
				<div className="relative min-h-0 min-w-0 flex-1">
					{presented ? (
						<div className="flex size-full min-h-0 flex-col gap-3 md:flex-row">
							{screenTile(presented, "flex-1")}
							<div className="flex shrink-0 gap-3 overflow-auto md:w-60 md:flex-col">
								{otherShares.map((p) =>
									screenTile(p, "aspect-video w-48 shrink-0 md:w-full"),
								)}
								{everyone.map((p) =>
									cameraTile(p, "aspect-video w-48 shrink-0 md:w-full"),
								)}
							</div>
						</div>
					) : (
						<div
							className="grid size-full gap-3"
							style={{
								gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`,
								gridTemplateRows: `repeat(${rows}, minmax(0, 1fr))`,
							}}
						>
							{everyone.map((p) => cameraTile(p))}
						</div>
					)}

					<div className="pointer-events-none absolute inset-x-0 top-3 flex flex-col items-start gap-2 px-3">
						{hasProject ? (
							<RecordingNotice
								projectId={projectId}
								channelId={channelId}
								className="pointer-events-auto mb-0 bg-black/70 text-red-300"
							/>
						) : null}
						{moderationError ? (
							<p
								role="alert"
								className="pointer-events-auto rounded-md bg-black/70 px-3 py-1.5 text-xs text-red-300"
							>
								{moderationError}
							</p>
						) : null}
						{call.mediaBlocked ? (
							<p
								role="alert"
								className="pointer-events-auto max-w-md rounded-md bg-black/70 px-3 py-1.5 text-xs text-red-300"
							>
								{t("call.mediaBlocked")}
							</p>
						) : null}
					</div>

					{hasProject ? (
						<div className="pointer-events-none absolute inset-x-0 bottom-3 flex justify-center px-3">
							<CaptionsStrip
								names={names}
								className="pointer-events-auto w-full max-w-2xl rounded-xl bg-black/75 px-4 py-2.5 backdrop-blur"
							/>
						</div>
					) : null}
				</div>

				{panel ? (
					<aside
						aria-label={t(
							panel === "people" ? "call.people" : "assistant.title",
						)}
						className="absolute inset-0 z-10 flex min-h-0 flex-col overflow-hidden bg-card text-card-foreground md:static md:inset-auto md:w-96 md:shrink-0 md:rounded-2xl"
					>
						<header className="flex items-center gap-2 px-4 py-3">
							<h2 className="flex-1 text-lg font-medium">
								{t(panel === "people" ? "call.people" : "assistant.title")}
							</h2>
							<Button
								size="icon-sm"
								variant="ghost"
								className="rounded-full"
								aria-label={t("call.closePanel")}
								onClick={() => setPanel(null)}
							>
								<X />
							</Button>
						</header>
						<div className="min-h-0 flex-1 overflow-y-auto px-4 pb-4">
							{panel === "people" ? (
								<ul className="space-y-1">
									{everyone.map((p) => (
										<li
											key={p.identity}
											className="flex items-center gap-3 rounded-lg px-1 py-1.5"
										>
											<Avatar
												name={p.name || p.identity}
												className="size-9 text-sm"
											/>
											<span className="min-w-0 flex-1 truncate text-sm">
												{displayName(p)}
											</span>
											{isMicOff(p) ? (
												<MicOff
													className="size-4 text-muted-foreground"
													aria-label={t("call.micOff")}
												/>
											) : (
												<Mic
													className="size-4 text-muted-foreground"
													aria-hidden
												/>
											)}
											{canModerate && p !== lp ? (
												<ModerationMenu
													name={p.name || p.identity}
													onMute={() =>
														moderate.mutate({
															accountId: p.identity,
															remove: false,
														})
													}
													onRemove={() =>
														moderate.mutate({
															accountId: p.identity,
															remove: true,
														})
													}
												/>
											) : null}
										</li>
									))}
								</ul>
							) : (
								<>
									<AssistantSection
										projectId={projectId}
										channelId={channelId}
										canInvite={canInviteAssistant}
										canModerate={canModerate}
									/>
									<ProposalsSection
										projectId={projectId}
										channelId={channelId}
										names={names}
									/>
								</>
							)}
						</div>
					</aside>
				) : null}
			</div>

			<footer className="flex flex-wrap items-center justify-center gap-2 px-2 py-3 md:grid md:grid-cols-[1fr_auto_1fr] md:gap-3 md:px-4">
				<div className="hidden min-w-0 items-center gap-3 text-sm md:flex">
					<Clock />
					{title ? (
						<>
							<span aria-hidden className="h-4 w-px bg-white/30" />
							<span className="truncate font-medium">{title}</span>
						</>
					) : null}
				</div>

				<div className="flex items-center gap-2 md:col-start-2">
					<div className="flex items-center rounded-full bg-secondary">
						<AudioSettingsMenu />
						<BarButton
							off={!lp.isMicrophoneEnabled}
							aria-label={
								lp.isMicrophoneEnabled ? t("call.mute") : t("call.unmute")
							}
							aria-pressed={!lp.isMicrophoneEnabled}
							onClick={call.toggleMic}
						>
							{lp.isMicrophoneEnabled ? <Mic /> : <MicOff />}
						</BarButton>
					</div>
					<BarButton
						off={!lp.isCameraEnabled}
						aria-label={
							lp.isCameraEnabled ? t("call.cameraOff") : t("call.cameraOn")
						}
						aria-pressed={lp.isCameraEnabled}
						onClick={call.toggleCamera}
					>
						{lp.isCameraEnabled ? <Video /> : <VideoOff />}
					</BarButton>
					<BarButton
						className={
							lp.isScreenShareEnabled
								? "bg-sky-300 text-neutral-900 hover:bg-sky-200"
								: undefined
						}
						aria-label={
							lp.isScreenShareEnabled
								? t("call.stopShare")
								: t("call.shareScreen")
						}
						aria-pressed={lp.isScreenShareEnabled}
						onClick={call.toggleScreen}
					>
						{lp.isScreenShareEnabled ? <MonitorOff /> : <Monitor />}
					</BarButton>
					{hasProject && canRecord ? (
						<RecordButton
							projectId={projectId}
							channelId={channelId}
							buttonClassName="size-10 rounded-full md:size-12 [&_svg:not([class*='size-'])]:size-5"
						/>
					) : null}
					<BarButton
						variant="destructive"
						className="w-14 bg-red-600 md:w-16 text-white hover:bg-red-500"
						aria-label={t("call.leave")}
						onClick={call.leave}
					>
						<PhoneOff />
					</BarButton>
				</div>

				<div className="flex items-center justify-end gap-1 md:col-start-3">
					<BarButton
						variant="secondary"
						className={panelButtonClass(panel === "people")}
						aria-label={t("call.people")}
						aria-pressed={panel === "people"}
						onClick={() => togglePanel("people")}
					>
						<Users />
						<span className="absolute -right-0.5 -top-0.5 flex min-w-5 items-center justify-center rounded-full bg-neutral-700 px-1 text-[11px] leading-5 text-white">
							{everyone.length}
						</span>
					</BarButton>
					{hasProject ? (
						<BarButton
							variant="secondary"
							className={panelButtonClass(panel === "assistant")}
							aria-label={t("assistant.title")}
							aria-pressed={panel === "assistant"}
							onClick={() => togglePanel("assistant")}
						>
							<Sparkles />
							{unseen && panel !== "assistant" ? (
								<span
									aria-hidden
									className="absolute right-1.5 top-1.5 size-2.5 rounded-full bg-sky-400 ring-2 ring-neutral-950"
								/>
							) : null}
						</BarButton>
					) : null}
				</div>
			</footer>
		</section>
	);
}

/** Echo cancellation, noise suppression and auto gain for the microphone. */
function AudioSettingsMenu() {
	const { t } = useTranslation("chat");
	const { audioProcessing, setAudioProcessing } = useCall();
	const items = [
		["echoCancellation", t("call.audio.echoCancellation")],
		["noiseSuppression", t("call.audio.noiseSuppression")],
		["autoGainControl", t("call.audio.autoGainControl")],
	] as const;
	return (
		<DropdownMenu>
			<DropdownMenuTrigger
				render={
					<Button
						size="icon"
						variant="secondary"
						className="h-10 w-8 rounded-l-full md:h-12 rounded-r-none bg-transparent pl-1 hover:bg-white/10"
						aria-label={t("call.audio.settings")}
					/>
				}
			>
				<ChevronUp />
			</DropdownMenuTrigger>
			<DropdownMenuContent
				align="start"
				side="top"
				sideOffset={8}
				className="dark w-64"
			>
				<DropdownMenuGroup>
					<DropdownMenuLabel>{t("call.audio.heading")}</DropdownMenuLabel>
					{items.map(([key, label]) => (
						<DropdownMenuCheckboxItem
							key={key}
							checked={audioProcessing[key]}
							onCheckedChange={(on) => setAudioProcessing({ [key]: on })}
						>
							{label}
						</DropdownMenuCheckboxItem>
					))}
					<p className="px-2 py-1.5 text-xs text-muted-foreground">
						{t("call.audio.hint")}
					</p>
				</DropdownMenuGroup>
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
