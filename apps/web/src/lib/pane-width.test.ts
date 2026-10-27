import { beforeEach, describe, expect, it } from "vitest";
import {
	clampPaneWidth,
	ROADMAP_TASK_PANE,
	readStoredPaneWidth,
	SIDEBAR_PANE,
	storePaneWidth,
} from "./pane-width";

describe("pane width", () => {
	beforeEach(() => localStorage.clear());

	it("clamps to the min and to the viewport-relative max", () => {
		expect(clampPaneWidth(SIDEBAR_PANE, 10, 1600)).toBe(SIDEBAR_PANE.minWidth);
		expect(clampPaneWidth(SIDEBAR_PANE, 9999, 1600)).toBe(
			SIDEBAR_PANE.maxWidth,
		);
		expect(clampPaneWidth(SIDEBAR_PANE, 9999, 800)).toBe(320);
	});

	it("falls back to the default when nothing valid is stored", () => {
		expect(readStoredPaneWidth(ROADMAP_TASK_PANE, 1600)).toBe(280);
		localStorage.setItem(ROADMAP_TASK_PANE.storageKey, "junk");
		expect(readStoredPaneWidth(ROADMAP_TASK_PANE, 1600)).toBe(280);
	});

	it("round-trips a stored width and clears it at the default", () => {
		storePaneWidth(ROADMAP_TASK_PANE, 400);
		expect(readStoredPaneWidth(ROADMAP_TASK_PANE, 1600)).toBe(400);
		storePaneWidth(ROADMAP_TASK_PANE, ROADMAP_TASK_PANE.defaultWidth);
		expect(localStorage.getItem(ROADMAP_TASK_PANE.storageKey)).toBeNull();
	});
});
