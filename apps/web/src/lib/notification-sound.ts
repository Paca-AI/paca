// Plays a short two-tone chime for incoming notifications using the Web
// Audio API directly, rather than an <audio> element backed by a shipped
// sound file — no binary asset to add, license, or fetch.

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
	audioContext ??= new Ctor();
	return audioContext;
}

const TONES = [
	{ freq: 880, start: 0 }, // A5
	{ freq: 1108.73, start: 0.09 }, // C#6 — ascending major third
];
const TONE_DURATION = 0.15;
const PEAK_GAIN = 0.2;

/** Plays the notification chime. No-ops if the Web Audio API is unavailable
 * or the AudioContext can't be resumed (e.g. no user gesture has occurred
 * on the page yet). */
export function playNotificationSound(): void {
	const ctx = getAudioContext();
	if (!ctx) return;
	if (ctx.state === "suspended") void ctx.resume();

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
