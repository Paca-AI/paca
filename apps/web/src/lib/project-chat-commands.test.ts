import { describe, expect, it } from "vitest";
import {
	isProjectChatCommandQuery,
	parseProjectChatCommand,
	projectChatCommandToken,
} from "./project-chat-commands";

describe("project chat commands", () => {
	it("parses localized and stable command tokens with optional instructions", () => {
		expect(parseProjectChatCommand("/更新描述 保留验收标准")).toEqual({
			kind: "update-description",
			token: "/更新描述",
			argument: "保留验收标准",
		});
		expect(parseProjectChatCommand("/record-conclusion")).toEqual({
			kind: "record-conclusion",
			token: "/record-conclusion",
			argument: "",
		});
		expect(parseProjectChatCommand("please /更新描述")).toBeNull();
	});

	it("uses stable protocol tokens independently of the display language", () => {
		expect(projectChatCommandToken("update-description")).toBe(
			"/update-description",
		);
		expect(projectChatCommandToken("record-conclusion")).toBe(
			"/record-conclusion",
		);
	});

	it("only treats the first slash token as an active menu query", () => {
		expect(isProjectChatCommandQuery("/")).toBe(true);
		expect(isProjectChatCommandQuery("/更新")).toBe(true);
		expect(isProjectChatCommandQuery("/更新描述 ")).toBe(false);
	});
});
