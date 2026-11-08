import { expect } from "@playwright/test";
import { BASE_URL } from "./e2e-api";

type BridgeMessage = Record<string, unknown> & { type: string };

/** A minimal stand-in for the local paca-acp-bridge daemon. */
export class FakeBridge {
	private socket: WebSocket;
	private received: BridgeMessage[] = [];
	private pingTimer?: ReturnType<typeof setInterval>;

	private constructor(socket: WebSocket) {
		this.socket = socket;
		socket.addEventListener("message", (event) => {
			this.received.push(JSON.parse(String(event.data)) as BridgeMessage);
		});
	}

	static async connect(agentId: string, token: string): Promise<FakeBridge> {
		const url = `${BASE_URL.replace(/^http/, "ws")}/agent-bridge/ws`;
		const socket = new WebSocket(url);
		await new Promise<void>((resolve, reject) => {
			socket.addEventListener("open", () => resolve(), { once: true });
			socket.addEventListener(
				"error",
				() => reject(new Error(`could not open ${url}`)),
				{ once: true },
			);
		});
		const bridge = new FakeBridge(socket);
		socket.send(JSON.stringify({ type: "hello", agent_id: agentId, token }));
		await bridge.next("hello_ack");
		// The server drops a bridge whose presence key (45 s TTL) is not
		// refreshed; the real daemon pings every 20 s.
		bridge.pingTimer = setInterval(
			() => socket.send(JSON.stringify({ type: "ping" })),
			20_000,
		);
		return bridge;
	}

	/** Waits for (and consumes) the next message of the given type. */
	async next(type: string, conversationId?: string): Promise<BridgeMessage> {
		let found: BridgeMessage | undefined;
		await expect
			.poll(
				() => {
					const idx = this.received.findIndex(
						(m) =>
							m.type === type &&
							(conversationId === undefined ||
								m.conversation_id === conversationId),
					);
					if (idx >= 0) found = this.received.splice(idx, 1)[0];
					return found !== undefined;
				},
				{ message: `bridge never received ${type}`, timeout: 20_000 },
			)
			.toBe(true);
		return found as BridgeMessage;
	}

	/** Reports a turn status back to Paca, as the real daemon would. */
	reportTurnStatus(conversationId: string, status: string): void {
		this.socket.send(
			JSON.stringify({
				type: "turn_status",
				conversation_id: conversationId,
				status,
			}),
		);
	}

	close(): void {
		clearInterval(this.pingTimer);
		this.socket.close();
	}
}
