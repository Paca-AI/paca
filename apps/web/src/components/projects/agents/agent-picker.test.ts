import { describe, expect, it } from "vitest";
import type { Agent } from "@/lib/agent-api";
import { privateChatAgentOptions } from "./agent-picker";

function agent(id: string, agentType: "llm" | "acp"): Agent {
	return {
		id,
		name: id,
		agent_type: agentType,
	} as Agent;
}

describe("privateChatAgentOptions", () => {
	it("keeps ACP visible but unselectable only in the private Chats adapter", () => {
		const llm = agent("llm-1", "llm");
		const acp = agent("acp-1", "acp");
		const options = privateChatAgentOptions([llm, acp]);

		expect(options[0]).toEqual(expect.objectContaining({ id: "llm-1" }));
		expect(options[0]).not.toHaveProperty("disabledReason");
		expect(options[1]).toEqual(
			expect.objectContaining({
				id: "acp-1",
				disabledReason: "acp_private_unavailable",
			}),
		);
		expect(acp).not.toHaveProperty("disabledReason");
	});
});
