import { describe, expect, it } from "vitest";
import { chatErrorKey } from "./chat-errors";

const withCode = (code: string) => ({
	response: { data: { error_code: code } },
});

describe("chatErrorKey", () => {
	it("maps the chat codes the API returns, including the unregistered ones", () => {
		expect(chatErrorKey(withCode("CHANNEL_NAME_TAKEN"))).toBe("nameTaken");
		expect(chatErrorKey(withCode("MESSAGE_TOO_LONG"))).toBe("messageTooLong");
		expect(chatErrorKey(withCode("DM_SELF"))).toBe("dmSelf");
		expect(chatErrorKey(withCode("DM_PEER_NOT_FOUND"))).toBe("dmPeerNotFound");
		expect(chatErrorKey(withCode("FORBIDDEN"))).toBe("forbidden");
	});

	it("falls back to the generic message for anything else", () => {
		expect(chatErrorKey(withCode("SOMETHING_NEW"))).toBe("generic");
		expect(chatErrorKey(new Error("network"))).toBe("generic");
		expect(chatErrorKey(undefined)).toBe("generic");
	});
});
