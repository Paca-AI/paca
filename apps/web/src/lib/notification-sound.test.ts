import { beforeEach, describe, expect, it } from "vitest";
import {
	isNotificationSoundMuted,
	setNotificationSoundMuted,
} from "./notification-sound";

describe("notification sound mute preference", () => {
	beforeEach(() => {
		window.localStorage.clear();
	});

	it("defaults to unmuted", () => {
		expect(isNotificationSoundMuted()).toBe(false);
	});

	it("persists muting", () => {
		setNotificationSoundMuted(true);
		expect(isNotificationSoundMuted()).toBe(true);
	});

	it("persists unmuting", () => {
		setNotificationSoundMuted(true);
		setNotificationSoundMuted(false);
		expect(isNotificationSoundMuted()).toBe(false);
	});
});
