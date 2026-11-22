import { useQuery } from "@tanstack/react-query";
import { Download, History, Video } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import {
	type CallRecording,
	callFeaturesQueryOptions,
	channelRecordingsQueryOptions,
	recordingUrl,
} from "@/lib/call-api";
import {
	channelMeetingsQueryOptions,
	downloadTranscript,
	type Meeting,
	meetingConfigQueryOptions,
	meetingTranscriptQueryOptions,
} from "@/lib/meeting-api";

function when(iso: string): string {
	return new Date(iso).toLocaleString(undefined, {
		dateStyle: "medium",
		timeStyle: "short",
	});
}

function clock(iso: string): string {
	return new Date(iso).toLocaleTimeString(undefined, {
		hour: "2-digit",
		minute: "2-digit",
		second: "2-digit",
	});
}

/** One call's transcript: who said what, in order. */
export function TranscriptView({
	projectId,
	channelId,
	meeting,
}: {
	projectId: string;
	channelId: string;
	meeting: Meeting;
}) {
	const { t } = useTranslation("chat");
	const live = meeting.ended_at === null;
	const lines = useQuery(
		meetingTranscriptQueryOptions(projectId, channelId, meeting.call_id, live),
	);
	if (lines.isLoading) return <Skeleton className="h-32 w-full" />;
	if (lines.isError) {
		return (
			<p role="alert" className="text-sm text-destructive">
				{t("meetings.loadFailed")}
			</p>
		);
	}
	const data = lines.data ?? [];
	if (data.length === 0) {
		return (
			<p className="text-sm text-muted-foreground">{t("meetings.empty")}</p>
		);
	}
	return (
		<ol aria-label={t("meetings.transcript")} className="space-y-2 text-sm">
			{data.map((l) => (
				<li
					key={`${l.spoken_at}-${l.speaker_id}-${l.text}`}
					className="flex gap-2"
				>
					<time
						dateTime={l.spoken_at}
						className="w-16 shrink-0 text-xs tabular-nums text-muted-foreground"
					>
						{clock(l.spoken_at)}
					</time>
					<span>
						<span className="font-medium">
							{l.speaker_name || t("assistant.someone")}:
						</span>{" "}
						{l.text}
					</span>
				</li>
			))}
		</ol>
	);
}

/** One recording of a call: play it in the page, or save the file. The link is
 * fetched on demand and lasts an hour, so nothing sensitive sits in the page. */
function RecordingItem({
	projectId,
	channelId,
	rec,
}: {
	projectId: string;
	channelId: string;
	rec: CallRecording;
}) {
	const { t } = useTranslation("chat");
	const [src, setSrc] = useState<string | null>(null);
	const [busy, setBusy] = useState(false);
	const [failed, setFailed] = useState(false);
	const open = async (download: boolean) => {
		setBusy(true);
		setFailed(false);
		try {
			const url = await recordingUrl(projectId, channelId, rec.id, download);
			if (download) window.open(url, "_blank", "noopener");
			else setSrc(url);
		} catch {
			setFailed(true);
		} finally {
			setBusy(false);
		}
	};
	const mins = Math.max(1, Math.round(rec.duration_ms / 60000));
	if (rec.status === "failed") {
		return <p className="text-xs text-destructive">{t("recording.failed")}</p>;
	}
	if (rec.status !== "complete") {
		return (
			<p className="text-xs text-muted-foreground">
				{t("recording.processing")}
			</p>
		);
	}
	return (
		<div className="space-y-2">
			<div className="flex flex-wrap items-center gap-2">
				<Button
					size="sm"
					variant="outline"
					disabled={busy}
					onClick={() => void open(false)}
				>
					<Video />
					{t("recording.play", { minutes: mins })}
				</Button>
				<Button
					size="sm"
					variant="ghost"
					disabled={busy}
					onClick={() => void open(true)}
				>
					<Download />
					{t("recording.download")}
				</Button>
			</div>
			{failed ? (
				<p role="alert" className="text-xs text-destructive">
					{t("recording.unavailable")}
				</p>
			) : null}
			{src ? (
				// biome-ignore lint/a11y/useMediaCaption: a call recording has no caption track
				<video src={src} controls className="w-full rounded-md bg-black" />
			) : null}
		</div>
	);
}

/**
 * The transcribed calls of a channel, newest first, with each one's transcript
 * and a way to save it. Shown to anyone who can read the channel, like its
 * messages; hidden when there is nothing to show.
 */
export function MeetingHistoryButton({
	projectId,
	channelId,
}: {
	projectId: string;
	channelId: string;
}) {
	const { t } = useTranslation("chat");
	const assistants = useQuery(meetingConfigQueryOptions).data === true;
	const recordable =
		useQuery(callFeaturesQueryOptions).data?.recording === true;
	const meetings = useQuery(
		channelMeetingsQueryOptions(
			projectId,
			channelId,
			assistants && !!projectId,
		),
	).data;
	// Recording does not depend on the assistant, so neither does its playback.
	const recordings = useQuery(
		channelRecordingsQueryOptions(
			projectId,
			channelId,
			recordable && !!projectId,
		),
	).data;
	const [open, setOpen] = useState(false);
	const [picked, setPicked] = useState<string | null>(null);
	const [saving, setSaving] = useState(false);
	// Calls that were recorded but not transcribed (the assistant is off, or was
	// never invited) still get an entry, with just the recording.
	const transcribed = assistants ? (meetings ?? []) : [];
	const known = new Set(transcribed.map((m) => m.call_id));
	const recordedOnly: Meeting[] = [];
	for (const r of recordable ? (recordings ?? []) : []) {
		if (known.has(r.call_id)) continue;
		known.add(r.call_id);
		recordedOnly.push({
			call_id: r.call_id,
			started_at: r.started_at,
			ended_at: r.ended_at ?? r.started_at,
			segments: 0,
			speakers: [],
		});
	}
	const entries = [...transcribed, ...recordedOnly].sort((a, b) =>
		b.started_at.localeCompare(a.started_at),
	);
	if (entries.length === 0) return null;
	const current = entries.find((m) => m.call_id === picked) ?? entries[0];
	const hasTranscript = assistants && current.segments > 0;

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger
				render={
					<Button
						size="icon-sm"
						variant="ghost"
						aria-label={t("meetings.open")}
					/>
				}
			>
				<History />
			</DialogTrigger>
			<DialogContent className="sm:max-w-3xl">
				<DialogHeader>
					<DialogTitle>{t("meetings.title")}</DialogTitle>
					<DialogDescription>{t("meetings.description")}</DialogDescription>
				</DialogHeader>
				<div className="grid min-h-0 gap-4 sm:grid-cols-[14rem_1fr]">
					<ul aria-label={t("meetings.list")} className="space-y-1">
						{entries.map((m) => (
							<li key={m.call_id}>
								<button
									type="button"
									aria-current={m.call_id === current?.call_id}
									onClick={() => setPicked(m.call_id)}
									className="w-full rounded-md border p-2 text-left text-sm hover:bg-muted aria-[current=true]:bg-muted"
								>
									<span className="block font-medium">
										{when(m.started_at)}
									</span>
									<span className="block text-xs text-muted-foreground">
										{m.ended_at === null
											? t("meetings.live")
											: m.segments > 0
												? t("meetings.lines", { count: m.segments })
												: t("meetings.recordedOnly")}
									</span>
									{m.speakers.length > 0 ? (
										<span className="block truncate text-xs text-muted-foreground">
											{m.speakers.join(", ")}
										</span>
									) : null}
								</button>
							</li>
						))}
					</ul>
					<div className="max-h-[60vh] min-w-0 space-y-3 overflow-y-auto">
						{current ? (
							<>
								{hasTranscript || current.ended_at === null ? (
									<div className="flex justify-end">
										<Button
											size="sm"
											variant="outline"
											disabled={saving}
											onClick={() => {
												setSaving(true);
												void downloadTranscript(
													projectId,
													channelId,
													current.call_id,
												).finally(() => setSaving(false));
											}}
										>
											<Download />
											{t("meetings.download")}
										</Button>
									</div>
								) : null}
								{(recordings ?? [])
									.filter((r) => r.call_id === current.call_id)
									.map((r) => (
										<RecordingItem
											key={r.id}
											projectId={projectId}
											channelId={channelId}
											rec={r}
										/>
									))}
								{hasTranscript || current.ended_at === null ? (
									<TranscriptView
										projectId={projectId}
										channelId={channelId}
										meeting={current}
									/>
								) : null}
							</>
						) : null}
					</div>
				</div>
			</DialogContent>
		</Dialog>
	);
}
