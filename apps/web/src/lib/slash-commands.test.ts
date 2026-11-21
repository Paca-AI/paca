import { describe, expect, it } from "vitest";
import type { ChatBlock } from "./chat-api";
import { findCommand, type SlashCommand } from "./slash-commands";

const cmd = (name: string): SlashCommand => ({
	name,
	description: name,
	run: async () => {},
});
const msg = (text: string): ChatBlock[] => [
	{ type: "paragraph", content: [{ type: "text", text, styles: {} }] },
];

describe("findCommand", () => {
	const commands = [cmd("task"), cmd("doc")];

	it("finds a command and its arguments", () => {
		const hit = findCommand(commands, msg("/task Fix the login"));
		expect(hit?.command.name).toBe("task");
		expect(hit?.args).toBe("Fix the login");
	});

	it("is not a command for plain text or an unknown name", () => {
		expect(findCommand(commands, msg("hello /task"))).toBeNull();
		expect(findCommand(commands, msg("/nope x"))).toBeNull();
		expect(findCommand(undefined, msg("/task x"))).toBeNull();
	});
});
