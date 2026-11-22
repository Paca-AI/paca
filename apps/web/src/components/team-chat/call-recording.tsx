import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Circle, Square } from "lucide-react";
import { useState } from "react";
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
	callFeaturesQueryOptions,
	callKeys,
	channelCallQueryOptions,
	startRecording,
	stopRecording,
} from "@/lib/call-api";
import { chatErrorKey } from "@/lib/chat-errors";

/** Whether the call is being recorded, from the call itself: everyone in it sees
 * the same, kept current by the realtime announcement. */
export function useCallRecording(projectId: string, channelId: string) {
	const call = useQuery(channelCallQueryOptions(projectId, channelId)).data;
	const rec = call?.recording ?? null;
	return rec && rec.status !== "complete" && rec.status !== "failed"
		? rec
		: null;
}

/** The notice everyone in a recorded call sees; it cannot be dismissed. */
export function RecordingNotice({
	projectId,
	channelId,
}: {
	projectId: string;
	channelId: string;
}) {
	const { t } = useTranslation("chat");
	const rec = useCallRecording(projectId, channelId);
	if (!rec) return null;
	return (
		<p
			role="status"
			className="mb-2 flex items-center gap-2 rounded-md bg-destructive/10 px-3 py-1.5 text-sm font-medium text-destructive"
		>
			<span
				aria-hidden
				className="size-2.5 shrink-0 animate-pulse rounded-full bg-destructive"
			/>
			{rec.status === "stopping"
				? t("recording.stopping")
				: t("recording.notice")}
		</p>
	);
}

/**
 * Start or stop recording (calls:record). Starting asks first, because everyone
 * in the call is told and the file is kept.
 */
export function RecordButton({
	projectId,
	channelId,
}: {
	projectId: string;
	channelId: string;
}) {
	const { t } = useTranslation("chat");
	const qc = useQueryClient();
	const features = useQuery(callFeaturesQueryOptions).data;
	const rec = useCallRecording(projectId, channelId);
	const [confirm, setConfirm] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const refresh = () =>
		void qc.invalidateQueries({
			queryKey: callKeys.channel(projectId, channelId),
		});
	const start = useMutation({
		mutationFn: () => startRecording(projectId, channelId),
		onSuccess: () => {
			setError(null);
			setConfirm(false);
			refresh();
		},
		onError: (e) => {
			setConfirm(false);
			setError(t(`errors.${chatErrorKey(e)}`));
		},
	});
	const stop = useMutation({
		mutationFn: () => stopRecording(projectId, channelId),
		onSuccess: () => {
			setError(null);
			refresh();
			void qc.invalidateQueries({
				queryKey: ["calls", projectId, "recordings", channelId],
			});
		},
		onError: (e) => setError(t(`errors.${chatErrorKey(e)}`)),
	});

	if (!features?.recording) return null;
	return (
		<>
			{rec ? (
				<Button
					size="icon"
					variant="destructive"
					aria-label={t("recording.stop")}
					disabled={stop.isPending || rec.status === "stopping"}
					onClick={() => stop.mutate()}
				>
					<Square />
				</Button>
			) : (
				<Button
					size="icon"
					variant="secondary"
					aria-label={t("recording.start")}
					onClick={() => setConfirm(true)}
				>
					<Circle className="text-destructive" />
				</Button>
			)}
			{error ? (
				<p
					role="alert"
					className="basis-full text-center text-xs text-destructive"
				>
					{error}
				</p>
			) : null}
			<Dialog open={confirm} onOpenChange={setConfirm}>
				<DialogContent>
					<DialogHeader>
						<DialogTitle>{t("recording.confirmTitle")}</DialogTitle>
						<DialogDescription>{t("recording.confirmBody")}</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button variant="ghost" onClick={() => setConfirm(false)}>
							{t("recording.cancel")}
						</Button>
						<Button
							variant="destructive"
							disabled={start.isPending}
							onClick={() => start.mutate()}
						>
							{t("recording.confirm")}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</>
	);
}
