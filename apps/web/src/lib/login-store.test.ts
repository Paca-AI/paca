import { beforeEach, describe, expect, it, vi } from "vitest";
import {
	loginExampleStore,
	markLoginSubmit,
	setUsernamePreview,
} from "./login-store";

describe("loginExampleStore", () => {
	beforeEach(() => {
		loginExampleStore.setState(() => ({
			usernamePreview: "",
			submitCount: 0,
			lastSubmittedAt: "",
		}));
	});

	it("updates username preview", () => {
		setUsernamePreview("alice");

		expect(loginExampleStore.state.usernamePreview).toBe("alice");
		expect(loginExampleStore.state.submitCount).toBe(0);
	});

	it("increments submit count and stamps timestamp", () => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date("2026-03-28T10:11:12.000Z"));

		markLoginSubmit();

		expect(loginExampleStore.state.submitCount).toBe(1);
		expect(loginExampleStore.state.lastSubmittedAt).toBe(
			"2026-03-28T10:11:12.000Z",
		);

		vi.useRealTimers();
	});
});
