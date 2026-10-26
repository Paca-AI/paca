import { describe, expect, it } from "vitest";
import { linkTeamMentions } from "../../utils/converters.js";

const members = [
	{
		id: "m-admin",
		user_id: "u-admin",
		member_type: "user",
		username: "admin",
		full_name: "Admin",
	},
	{
		id: "m-bot",
		user_id: "u-bot",
		agent_id: "a-bot",
		member_type: "agent",
		username: "claude4",
		full_name: "Claude Agent 4",
	},
];

const para = (text: string, styles = {}) => [
	{
		type: "paragraph",
		content: [{ type: "text", text, styles }],
		children: [],
	},
];

describe("linkTeamMentions", () => {
	it("links @username to a teamMention, keeping surrounding text", () => {
		const [block] = linkTeamMentions(para("hi @admin, thanks"), members);
		expect(block.content).toEqual([
			{ type: "text", text: "hi ", styles: {} },
			{
				type: "teamMention",
				props: { id: "u-admin", name: "Admin", avatar: "" },
			},
			{ type: "text", text: ", thanks", styles: {} },
		]);
	});

	it("resolves '@Name (id: ...)' by member id and uses agent_id for agents", () => {
		const [block] = linkTeamMentions(
			para("@Claude Agent 4 (id: m-bot-0000-0000-0000-000000000000)"),
			members,
		);
		// id not a known member -> untouched
		expect(block.content[0].type).toBe("text");

		const uuidMembers = [
			{ ...members[1], id: "7252227a-8dd3-4ce2-8431-fa6c326b2f36" },
		];
		const [linked] = linkTeamMentions(
			para(
				"@Claude Agent 4 (id: 7252227a-8dd3-4ce2-8431-fa6c326b2f36) please look",
			),
			uuidMembers,
		);
		expect(linked.content[0]).toEqual({
			type: "teamMention",
			props: { id: "a-bot", name: "Claude Agent 4", avatar: "" },
		});
		expect(linked.content[1].text).toBe(" please look");
	});

	it("leaves unknown names, emails, code and code blocks alone", () => {
		expect(linkTeamMentions(para("@nobody"), members)[0].content[0].type).toBe(
			"text",
		);
		expect(
			linkTeamMentions(para("a@admin.com"), members)[0].content[0].type,
		).toBe("text");
		expect(
			linkTeamMentions(para("@admin", { code: true }), members)[0].content[0]
				.type,
		).toBe("text");
		const code = [
			{ type: "codeBlock", content: [{ type: "text", text: "@admin" }] },
		];
		expect(linkTeamMentions(code, members)).toEqual(code);
	});
});
