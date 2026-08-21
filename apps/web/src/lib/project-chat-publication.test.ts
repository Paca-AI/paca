import { describe, expect, it } from "vitest";
import type { ConclusionPublication } from "./agent-api";
import { projectChatPublicationSource } from "./project-chat-publication";

function publication(
	overrides: Record<string, unknown>,
): ConclusionPublication {
	return {
		id: "publication-1",
		target_task_id: "task-1",
		published_by_user_id: "user-1",
		published_by_member_id: "member-1",
		generated_by_agent_id: "agent-1",
		kind: "published",
		summary: "Frozen summary",
		summary_version: 1,
		summary_sha256: "summary-sha",
		description_updated: false,
		created_at: "2026-08-18T00:00:00Z",
		source_accessible: false,
		...overrides,
	} as ConclusionPublication;
}

describe("projectChatPublicationSource", () => {
	it("drops malicious private ids when source_accessible is false", () => {
		const malformed = publication({
			source_accessible: false,
			source_session_id: "private-session",
			source_turn_id: "private-turn",
		});

		expect(projectChatPublicationSource(malformed)).toBeNull();
	});

	it("links only an explicitly accessible complete source", () => {
		expect(
			projectChatPublicationSource(
				publication({
					source_accessible: true,
					source_session_id: "session-1",
					source_turn_id: "turn-1",
				}),
			),
		).toEqual({ sessionId: "session-1", turnId: "turn-1" });
		expect(
			projectChatPublicationSource(
				publication({
					source_accessible: true,
					source_session_id: "session-1",
				}),
			),
		).toBeNull();
	});
});
