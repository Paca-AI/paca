import { beforeEach, describe, expect, it } from "vitest";
import {
	captureOptions,
	DEFAULT_AUDIO_PROCESSING,
	loadAudioProcessing,
	saveAudioProcessing,
} from "./call-audio";

describe("call audio processing", () => {
	beforeEach(() => window.localStorage.clear());

	it("is all on by default", () => {
		expect(loadAudioProcessing()).toEqual(DEFAULT_AUDIO_PROCESSING);
	});

	it("remembers a choice", () => {
		saveAudioProcessing({
			...DEFAULT_AUDIO_PROCESSING,
			noiseSuppression: false,
		});
		expect(loadAudioProcessing().noiseSuppression).toBe(false);
		expect(loadAudioProcessing().echoCancellation).toBe(true);
	});

	it("ignores junk in storage", () => {
		window.localStorage.setItem("paca.call.audioProcessing", "{nope");
		expect(loadAudioProcessing()).toEqual(DEFAULT_AUDIO_PROCESSING);
		window.localStorage.setItem(
			"paca.call.audioProcessing",
			JSON.stringify({ echoCancellation: "no", autoGainControl: false }),
		);
		expect(loadAudioProcessing()).toEqual({
			...DEFAULT_AUDIO_PROCESSING,
			autoGainControl: false,
		});
	});

	it("maps to capture constraints", () => {
		expect(
			captureOptions({ ...DEFAULT_AUDIO_PROCESSING, noiseSuppression: false }),
		).toMatchObject({ noiseSuppression: false, voiceIsolation: false });
	});
});
