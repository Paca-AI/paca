import { describe, expect, it } from "vitest";

import { splitShellCommand } from "./shell-command";

describe("splitShellCommand", () => {
	it("splits on plain whitespace", () => {
		expect(splitShellCommand("npx -y my-acp-server")).toEqual([
			"npx",
			"-y",
			"my-acp-server",
		]);
	});

	it("keeps a double-quoted argument together", () => {
		expect(splitShellCommand('my-cli --arg "hello world"')).toEqual([
			"my-cli",
			"--arg",
			"hello world",
		]);
	});

	it("keeps a single-quoted argument together", () => {
		expect(splitShellCommand("my-cli --arg 'hello world'")).toEqual([
			"my-cli",
			"--arg",
			"hello world",
		]);
	});

	it("collapses repeated whitespace and trims", () => {
		expect(splitShellCommand("  npx   -y   my-acp-server  ")).toEqual([
			"npx",
			"-y",
			"my-acp-server",
		]);
	});

	it("returns an empty array for blank input", () => {
		expect(splitShellCommand("")).toEqual([]);
		expect(splitShellCommand("   ")).toEqual([]);
	});

	it("supports escaped spaces outside quotes", () => {
		expect(splitShellCommand("my-cli --path foo\\ bar")).toEqual([
			"my-cli",
			"--path",
			"foo bar",
		]);
	});
});
