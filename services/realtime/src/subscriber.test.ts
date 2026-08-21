import { describe, expect, test } from "bun:test";
import type { Logger } from "pino";
import type { Server } from "socket.io";
import { routeEvent } from "./subscriber.ts";

describe("routeEvent", () => {
	test("routes an owner-private agent event only to the actor room", () => {
		const emitted: Array<{ room: string; event: string; body: unknown }> = [];
		const io = {
			to(room: string) {
				return {
					emit(event: string, body: unknown) {
						emitted.push({ room, event, body });
					},
				};
			},
		} as unknown as Server;
		const logger = { debug() {} } as unknown as Logger;

		routeEvent(
			io,
			{
				type: "agent.turn.finished",
				payload: {
					actor_user_id: "owner-1",
					project_id: "project-1",
					turn_id: "turn-1",
				},
			},
			logger,
		);

		expect(emitted).toHaveLength(1);
		expect(emitted[0]?.room).toBe("user:owner-1:agent-chat");
		expect(emitted[0]?.event).toBe("event");
	});

	for (const actorUserId of [undefined, ""]) {
		test(`drops an owner-private turn event with actor_user_id=${String(actorUserId)}`, () => {
			const emitted: Array<{ room: string; event: string; body: unknown }> = [];
			const io = {
				to(room: string) {
					return {
						emit(event: string, body: unknown) {
							emitted.push({ room, event, body });
						},
					};
				},
			} as unknown as Server;
			const logger = { debug() {} } as unknown as Logger;

			routeEvent(
				io,
				{
					type: "agent.turn.finished",
					payload: {
						actor_user_id: actorUserId,
						project_id: "project-1",
						turn_id: "turn-1",
						session_id: "session-1",
					},
				},
				logger,
			);

			expect(emitted).toHaveLength(0);
		});
	}
});
