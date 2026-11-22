import type { AudioCaptureOptions } from "livekit-client";

/**
 * How the browser cleans up this person's microphone before it is sent.
 * All three are on by default: echo cancellation stops a speaker's sound from
 * going back out through the microphone, noise suppression removes steady
 * background noise, auto gain keeps the level even.
 */
export interface AudioProcessing {
	echoCancellation: boolean;
	noiseSuppression: boolean;
	autoGainControl: boolean;
}

export const DEFAULT_AUDIO_PROCESSING: AudioProcessing = {
	echoCancellation: true,
	noiseSuppression: true,
	autoGainControl: true,
};

const STORAGE_KEY = "paca.call.audioProcessing";

/** The saved choice. Storage can be blocked or hold junk: fall back per field. */
export function loadAudioProcessing(): AudioProcessing {
	try {
		const raw = window.localStorage.getItem(STORAGE_KEY);
		if (!raw) return DEFAULT_AUDIO_PROCESSING;
		const v = JSON.parse(raw) as Partial<
			Record<keyof AudioProcessing, unknown>
		>;
		const pick = (k: keyof AudioProcessing) =>
			typeof v?.[k] === "boolean" ? (v[k] as boolean) : true;
		return {
			echoCancellation: pick("echoCancellation"),
			noiseSuppression: pick("noiseSuppression"),
			autoGainControl: pick("autoGainControl"),
		};
	} catch {
		return DEFAULT_AUDIO_PROCESSING;
	}
}

export function saveAudioProcessing(v: AudioProcessing): void {
	try {
		window.localStorage.setItem(STORAGE_KEY, JSON.stringify(v));
	} catch {
		// not remembered; still applies to this call
	}
}

/** The capture options to hand LiveKit. voiceIsolation is the browser's
 * stronger noise filter where it exists, and is ignored elsewhere. */
export function captureOptions(v: AudioProcessing): AudioCaptureOptions {
	return {
		echoCancellation: v.echoCancellation,
		noiseSuppression: v.noiseSuppression,
		autoGainControl: v.autoGainControl,
		voiceIsolation: v.noiseSuppression,
	};
}
