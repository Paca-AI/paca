import { beforeEach, describe, expect, it } from "vitest";
import {
	clearOwnOps,
	collabSocketId,
	isOwnOp,
	newClientOp,
	rememberOwnOp,
	setCollabSocketProvider,
} from "./client-ops";

describe("client ops", () => {
	beforeEach(clearOwnOps);

	it("makes header-safe ids that differ", () => {
		const a = newClientOp();
		expect(a).toMatch(/^[0-9a-f]{24}$/);
		expect(newClientOp()).not.toBe(a);
	});

	it("knows the ids it sent, and nothing else", () => {
		rememberOwnOp("a");
		expect(isOwnOp("a")).toBe(true);
		expect(isOwnOp("b")).toBe(false);
		expect(isOwnOp(undefined)).toBe(false);
		expect(isOwnOp("")).toBe(false);
	});

	it("forgets an id after a minute", () => {
		rememberOwnOp("a", 1_000);
		expect(isOwnOp("a", 1_000 + 59_000)).toBe(true);
		expect(isOwnOp("a", 1_000 + 61_000)).toBe(false);
	});

	it("reports the connected socket id, and nothing when there is none", () => {
		setCollabSocketProvider(null);
		expect(collabSocketId()).toBeUndefined();
		setCollabSocketProvider(() => "");
		expect(collabSocketId()).toBeUndefined();
		setCollabSocketProvider(() => "sock-1");
		expect(collabSocketId()).toBe("sock-1");
		setCollabSocketProvider(null);
	});

	it("keeps a bounded number of ids, dropping the oldest", () => {
		for (let i = 0; i < 600; i++) rememberOwnOp(`op${i}`);
		expect(isOwnOp("op0")).toBe(false);
		expect(isOwnOp("op599")).toBe(true);
	});
});
