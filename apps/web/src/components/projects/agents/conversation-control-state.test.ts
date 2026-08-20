import { describe, expect, it } from "vitest";
import { shouldShowConversationStop } from "./conversation-control-state";

describe("shouldShowConversationStop", () => {
	it("keeps the durable stop action available for a running ACP conversation", () => {
		expect(shouldShowConversationStop("running")).toBe(true);
	});

	it("hides the durable stop action after a terminal status", () => {
		expect(shouldShowConversationStop("stopped")).toBe(false);
	});
});
