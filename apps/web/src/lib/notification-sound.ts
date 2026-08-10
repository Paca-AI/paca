// Plays a short two-tone chime for incoming notifications using the Web
// Audio API directly, rather than an <audio> element backed by a shipped
// sound file — no binary asset to add, license, or fetch.

const MUTE_STORAGE_KEY = "paca:notification-sound-muted";

/** Whether the user has muted the notification chime (persisted per-browser
 * via localStorage, mirroring the "paca:"-prefixed keys used elsewhere for
 * device-local UI preferences, e.g. app-sidebar.tsx's collapsed-section
 * state). Defaults to unmuted. */
export function isNotificationSoundMuted(): boolean {
	if (typeof window === "undefined") return false;
	return window.localStorage.getItem(MUTE_STORAGE_KEY) === "true";
}

export function setNotificationSoundMuted(muted: boolean): void {
	if (typeof window === "undefined") return;
	window.localStorage.setItem(MUTE_STORAGE_KEY, String(muted));
}

let audioContext: AudioContext | null = null;

// Safari < 14.1 only exposes the constructor under this prefixed name.
interface WindowWithWebkitAudio {
	webkitAudioContext?: typeof AudioContext;
}

function getAudioContext(): AudioContext | null {
	if (typeof window === "undefined") return null;
	const Ctor =
		window.AudioContext ??
		(window as unknown as WindowWithWebkitAudio).webkitAudioContext;
	if (!Ctor) return null;
	if (!audioContext) {
		try {
			audioContext = new Ctor();
		} catch {
			// Construction can throw under hardened privacy settings or once a
			// browser's context limit is hit — treat that the same as "Web
			// Audio unavailable".
			return null;
		}
	}
	return audioContext;
}

const TONES = [
	{ freq: 880, start: 0 }, // A5
	{ freq: 1108.73, start: 0.09 }, // C#6 — ascending major third
];
const TONE_DURATION = 0.15;
const PEAK_GAIN = 0.2;
// A burst of near-simultaneous notifications (e.g. bulk-assign) would
// otherwise stack overlapping oscillators into a garbled chord — skip
// re-triggering the chime until the current one has finished.
const CHIME_DURATION_MS =
	(TONES[TONES.length - 1].start + TONE_DURATION + 0.02) * 1000;
let chimeEndsAt = 0;

/** Plays the notification chime. No-ops if the user has muted it, the Web
 * Audio API is unavailable, the AudioContext can't be resumed (e.g. no user
 * gesture has occurred on the page yet), or a chime is already playing. */
export function playNotificationSound(): void {
	if (isNotificationSoundMuted()) return;

	const nowMs = Date.now();
	if (nowMs < chimeEndsAt) return;

	const ctx = getAudioContext();
	if (!ctx) return;
	if (ctx.state === "suspended") ctx.resume().catch(() => {});

	chimeEndsAt = nowMs + CHIME_DURATION_MS;
	const now = ctx.currentTime;
	for (const { freq, start } of TONES) {
		const oscillator = ctx.createOscillator();
		const gain = ctx.createGain();
		oscillator.type = "sine";
		oscillator.frequency.value = freq;

		const startTime = now + start;
		gain.gain.setValueAtTime(0, startTime);
		gain.gain.linearRampToValueAtTime(PEAK_GAIN, startTime + 0.015);
		gain.gain.exponentialRampToValueAtTime(0.001, startTime + TONE_DURATION);

		oscillator.connect(gain);
		gain.connect(ctx.destination);
		oscillator.start(startTime);
		oscillator.stop(startTime + TONE_DURATION + 0.02);
	}
}
