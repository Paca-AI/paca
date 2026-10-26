// Plays Paca's signature two-note chime for incoming notifications using the
// Web Audio API directly, rather than an <audio> element backed by a shipped
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

// Each syllable is a consonant burst (band-passed noise) followed by a vowel
// "a" (a sawtooth run through two formant filters). "Pa" uses a low, soft
// labial burst; "ka" a brighter, longer velar burst and a higher pitch.
// Paca's signature motif: a two-note fall (G4 → D4) that follows the tones
// of "pá kà" — a high note, then a lower one. Each note is struck like a
// wooden mallet instrument (marimba / wood block): inharmonic bar-style
// overtones that die away much faster than the fundamental, with a light
// echo for a bit of space.
const NOTES = [
	{ freq: 392, start: 0, duration: 0.22 }, // G4 — "pá"
	{ freq: 293.66, start: 0.15, duration: 0.5 }, // D4 — "kà"
];
const PARTIALS = [
	{ multiple: 1, gain: 1 },
	{ multiple: 2.76, gain: 0.4 },
	{ multiple: 5.4, gain: 0.15 },
];
const ATTACK = 0.008;
const PEAK_GAIN = 0.22;
const ECHO_DELAY = 0.19;
const ECHO_FEEDBACK = 0.3;
const ECHO_WET = 0.2;
const ECHO_TAIL = 0.4;
// A burst of near-simultaneous notifications (e.g. bulk-assign) would
// otherwise stack overlapping notes into a garbled chord — skip re-triggering
// until the current chime has finished.
const CHIME_DURATION_MS =
	(Math.max(...NOTES.map((note) => note.start + note.duration)) +
		ECHO_TAIL +
		0.02) *
	1000;
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

	const master = ctx.createGain();
	master.connect(ctx.destination);
	const echo = ctx.createDelay(1);
	echo.delayTime.value = ECHO_DELAY;
	const feedback = ctx.createGain();
	feedback.gain.value = ECHO_FEEDBACK;
	const wet = ctx.createGain();
	wet.gain.value = ECHO_WET;
	master.connect(echo);
	echo.connect(feedback);
	feedback.connect(echo);
	echo.connect(wet);
	wet.connect(ctx.destination);

	for (const { freq, start, duration } of NOTES) {
		const startTime = now + start;
		for (const { multiple, gain: partialGain } of PARTIALS) {
			// Overtones fade faster than the fundamental, like a struck bar.
			const partialDuration = duration / multiple;
			const oscillator = ctx.createOscillator();
			const gain = ctx.createGain();
			oscillator.type = "sine";
			oscillator.frequency.value = freq * multiple;
			gain.gain.setValueAtTime(0, startTime);
			gain.gain.linearRampToValueAtTime(
				PEAK_GAIN * partialGain,
				startTime + ATTACK,
			);
			gain.gain.exponentialRampToValueAtTime(
				0.001,
				startTime + partialDuration,
			);
			oscillator.connect(gain);
			gain.connect(master);
			oscillator.start(startTime);
			oscillator.stop(startTime + partialDuration + 0.02);
		}
	}
}
