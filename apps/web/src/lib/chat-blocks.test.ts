import { describe, expect, it } from "vitest";
import type { ChatBlock, ChatInline } from "./chat-api";
import { isEmptyMessage, parseCommand, prepareForSend } from "./chat-blocks";

const para = (...content: ChatInline[]): ChatBlock => ({
	type: "paragraph",
	content,
});
const text = (t: string) => ({ type: "text", text: t, styles: {} });

describe("chat blocks", () => {
	it("treats only empty paragraphs as an empty message", () => {
		expect(isEmptyMessage([para(), para(text("  "))])).toBe(true);
		expect(isEmptyMessage([para(text("hi"))])).toBe(false);
		expect(
			isEmptyMessage([para({ type: "taskReference", props: { id: "t" } })]),
		).toBe(false);
	});

	it("drops the trailing empty paragraph and keeps the rest", () => {
		const out = prepareForSend([para(text("a")), para(), para()]);
		expect(out).toHaveLength(1);
	});

	it("reduces chips to ids, so a title is never stored", () => {
		const out = prepareForSend([
			para(
				{
					type: "taskReference",
					props: {
						id: "t1",
						projectId: "p1",
						number: 7,
						title: "Secret",
						status: "open",
					},
				},
				{
					type: "docReference",
					props: { id: "d1", projectId: "p1", title: "Private doc" },
				},
				{ type: "teamMention", props: { id: "u1", name: "Mina", avatar: "x" } },
			),
		]);
		expect(out[0]?.content).toEqual([
			{
				type: "taskReference",
				props: { id: "t1", projectId: "p1", number: 7 },
			},
			{ type: "docReference", props: { id: "d1", projectId: "p1" } },
			{ type: "teamMention", props: { id: "u1", name: "Mina" } },
		]);
		expect(JSON.stringify(out)).not.toMatch(/Secret|Private doc/);
	});

	it("reads a slash command and its arguments", () => {
		expect(parseCommand([para(text("/task Fix the login"))])).toEqual({
			name: "task",
			args: "Fix the login",
		});
		expect(parseCommand([para(text("/new")), para()])).toEqual({
			name: "new",
			args: "",
		});
		expect(parseCommand([para(text("and/or maybe"))])).toBeNull();
		expect(parseCommand([para(text("see /task later"))])).toBeNull();
	});
});
