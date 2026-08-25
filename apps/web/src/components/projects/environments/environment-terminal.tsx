import { Loader2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import "xterm/css/xterm.css";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import {
	ENVIRONMENT_HEARTBEAT_INTERVAL_MS,
	getTerminalTicket,
	heartbeatEnvironment,
} from "@/lib/environment-api";

// In-browser terminal for a running static environment — Phase A of
// docs/ai-agent/environment-management.md's Terminal / SSH Access section.
// Opens a WebSocket at the ticket-authenticated `ws_url` returned by
// `getTerminalTicket` and speaks a small binary frame protocol:
//
//   outgoing stdin:  [0x01, ...utf8Bytes]
//   outgoing resize: [0x02, rowsHi, rowsLo, colsHi, colsLo]  (big-endian u16)
//   incoming data:   [0x01, ...ptyOutputBytes] -> term.write(frame.slice(1))
//
// A periodic heartbeat (POST .../heartbeat) keeps the environment's idle
// timer from expiring out from under an actively-open terminal session —
// the agent-runner idle-reaper only sees conversation activity and this
// heartbeat, not the WebSocket connection itself.

const FRAME_DATA = 0x01;
const FRAME_RESIZE = 0x02;

function encodeStdinFrame(data: string): Uint8Array {
	const bytes = new TextEncoder().encode(data);
	const frame = new Uint8Array(bytes.length + 1);
	frame[0] = FRAME_DATA;
	frame.set(bytes, 1);
	return frame;
}

function encodeResizeFrame(rows: number, cols: number): Uint8Array {
	return new Uint8Array([
		FRAME_RESIZE,
		(rows >> 8) & 0xff,
		rows & 0xff,
		(cols >> 8) & 0xff,
		cols & 0xff,
	]);
}

/**
 * Resolves a `ws_url` from the terminal-ticket response against the current
 * origin — the same "treat window.location as the connection's base"
 * convention socket-client.ts uses for its Socket.IO origin. Already-absolute
 * ws(s):// URLs (the expected shape) pass through untouched; a bare path is
 * resolved against the page's own host, swapping http(s) for the matching
 * ws(s) scheme.
 */
function resolveWsUrl(wsUrl: string): string {
	if (/^wss?:\/\//i.test(wsUrl)) return wsUrl;
	const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
	const path = wsUrl.startsWith("/") ? wsUrl : `/${wsUrl}`;
	return `${proto}//${window.location.host}${path}`;
}

type ConnectionState = "connecting" | "connected" | "error" | "closed";

export function EnvironmentTerminal({
	projectId,
	environmentId,
}: {
	projectId: string;
	environmentId: string;
}) {
	const { t } = useTranslation("projects");
	const containerRef = useRef<HTMLDivElement | null>(null);
	const [state, setState] = useState<ConnectionState>("connecting");

	useEffect(() => {
		let cancelled = false;
		let ws: WebSocket | null = null;
		let heartbeatTimer: ReturnType<typeof setInterval> | null = null;
		let resizeObserver: ResizeObserver | null = null;
		let connectTimeoutTimer: ReturnType<typeof setTimeout> | null = null;

		const term = new Terminal({
			cursorBlink: true,
			fontSize: 13,
			fontFamily:
				"ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace",
			theme: {
				background: "#00000000",
			},
		});
		const fitAddon = new FitAddon();
		term.loadAddon(fitAddon);

		if (containerRef.current) {
			term.open(containerRef.current);
		}

		// FitAddon.fit() throws "Cannot read properties of undefined
		// (reading 'dimensions')" if it runs before xterm's own internal
		// renderer has finished its first layout pass — a well-known
		// xterm.js race, not specific to this component. Left unguarded,
		// that throw is uncaught (fit() is called from a ResizeObserver
		// callback and a raw WebSocket handler, neither wrapped by React),
		// which both spams the console and — since it happens after
		// setState("connected") but interrupts sendCurrentSize()/the
		// resize-observer setup right after it in onopen — can leave the
		// session half-initialized. Swallowing a failed fit here just means
		// the next real resize/reconnect retries it instead of crashing.
		const safeFit = () => {
			try {
				fitAddon.fit();
			} catch {
				// Ignored — see comment above.
			}
		};

		const sendCurrentSize = () => {
			if (ws?.readyState === WebSocket.OPEN) {
				ws.send(encodeResizeFrame(term.rows, term.cols));
			}
		};

		term.onResize(({ rows, cols }) => {
			if (ws?.readyState === WebSocket.OPEN) {
				ws.send(encodeResizeFrame(rows, cols));
			}
		});

		const dataDisposable = term.onData((data) => {
			if (ws?.readyState === WebSocket.OPEN) {
				ws.send(encodeStdinFrame(data));
			}
		});

		async function connect() {
			try {
				const ticket = await getTerminalTicket(projectId, environmentId);
				if (cancelled) return;

				const socket = new WebSocket(resolveWsUrl(ticket.ws_url));
				socket.binaryType = "arraybuffer";
				ws = socket;

				// A WebSocket that never fires onopen/onerror/onclose (a
				// stuck proxy, a silently dropped connection) would
				// otherwise leave the UI showing "connecting" forever with
				// no way out — this is what that looked like before this
				// fix, compounded by the uncaught fit() error above
				// interrupting onopen's own completion.
				connectTimeoutTimer = setTimeout(() => {
					if (!cancelled && socket.readyState === WebSocket.CONNECTING) {
						setState("error");
						socket.close();
					}
				}, 15_000);

				socket.onopen = () => {
					if (cancelled) return;
					if (connectTimeoutTimer) clearTimeout(connectTimeoutTimer);
					setState("connected");
					safeFit();
					sendCurrentSize();

					// Only start reacting to real size changes once the
					// terminal is confirmed open — a ResizeObserver set up
					// any earlier delivers its first notification almost
					// immediately upon observe(), racing xterm's own
					// initial layout and triggering the exact "dimensions"
					// crash this fix addresses.
					if (containerRef.current) {
						resizeObserver = new ResizeObserver(() => safeFit());
						resizeObserver.observe(containerRef.current);
					}
				};

				socket.onmessage = (event) => {
					if (!(event.data instanceof ArrayBuffer)) return;
					const bytes = new Uint8Array(event.data);
					if (bytes.length === 0) return;
					if (bytes[0] === FRAME_DATA) {
						term.write(bytes.slice(1));
					}
				};

				socket.onerror = () => {
					if (connectTimeoutTimer) clearTimeout(connectTimeoutTimer);
					if (!cancelled) setState("error");
				};

				socket.onclose = () => {
					if (connectTimeoutTimer) clearTimeout(connectTimeoutTimer);
					if (!cancelled) setState((s) => (s === "error" ? s : "closed"));
				};

				// Keeps the environment's idle timer from expiring while this
				// terminal session is open — see ENVIRONMENT_HEARTBEAT_INTERVAL_MS's
				// doc comment in environment-api.ts.
				heartbeatTimer = setInterval(() => {
					heartbeatEnvironment(projectId, environmentId).catch(() => {
						// Best-effort — a missed heartbeat just means the idle
						// reaper might catch this environment on its next sweep;
						// the next tick will retry.
					});
				}, ENVIRONMENT_HEARTBEAT_INTERVAL_MS);
			} catch {
				if (!cancelled) setState("error");
			}
		}

		connect();

		return () => {
			cancelled = true;
			if (connectTimeoutTimer) clearTimeout(connectTimeoutTimer);
			if (heartbeatTimer) clearInterval(heartbeatTimer);
			resizeObserver?.disconnect();
			dataDisposable.dispose();
			ws?.close();
			term.dispose();
		};
	}, [projectId, environmentId]);

	return (
		<div className="flex flex-col gap-2 h-full min-h-0">
			<div className="flex items-center gap-2 text-xs text-muted-foreground shrink-0">
				{state === "connecting" && (
					<>
						<Loader2 className="size-3.5 animate-spin" />
						{t("environments.detail.terminal.connecting")}
					</>
				)}
				{state === "connected" && (
					<>
						<span className="size-1.5 rounded-full bg-emerald-500" />
						{t("environments.detail.terminal.connected")}
					</>
				)}
				{state === "error" && (
					<>
						<span className="size-1.5 rounded-full bg-destructive" />
						{t("environments.detail.terminal.connectionFailed")}
					</>
				)}
				{state === "closed" && (
					<>
						<span className="size-1.5 rounded-full bg-muted-foreground/40" />
						{t("environments.detail.terminal.disconnected")}
					</>
				)}
			</div>
			<div className="flex-1 min-h-0 rounded-lg border border-border/60 bg-[#0d1117] p-2 overflow-hidden">
				<div ref={containerRef} className="h-full" />
			</div>
		</div>
	);
}
