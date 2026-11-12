import { describe, expect, it } from "vitest";
import { getToolCommand, summarizeToolArgs } from "./tool-args";

describe("tool-args", () => {
	it("extracts a bash command", () => {
		const args = JSON.stringify({
			command: "ls -la\necho hi",
			description: "x",
		});
		expect(getToolCommand(args)).toBe("ls -la\necho hi");
		expect(summarizeToolArgs(args)).toBe("ls -la echo hi");
	});

	it("joins an argv-style command", () => {
		expect(getToolCommand(JSON.stringify({ command: ["git", "status"] }))).toBe(
			"git status",
		);
	});

	it("falls back to common keys, then raw text", () => {
		expect(summarizeToolArgs(JSON.stringify({ file_path: "/a/b.ts" }))).toBe(
			"/a/b.ts",
		);
		expect(summarizeToolArgs("npm test")).toBe("npm test");
		expect(summarizeToolArgs("")).toBeUndefined();
		expect(summarizeToolArgs("{}")).toBeUndefined();
	});

	it("truncates long input", () => {
		const out = summarizeToolArgs(JSON.stringify({ command: "a".repeat(300) }));
		expect(out?.length).toBe(101);
		expect(out?.endsWith("…")).toBe(true);
	});
});
