import { describe, expect, it } from "vitest";
import { shouldShowPermanentStop } from "./conversation-control-state";

describe("shouldShowPermanentStop", () => {
	it("keeps the permanent stop action available for a running ACP conversation", () => {
		expect(shouldShowPermanentStop("running")).toBe(true);
	});

	it("hides the permanent stop action after a terminal status", () => {
		expect(shouldShowPermanentStop("stopped")).toBe(false);
	});
});
