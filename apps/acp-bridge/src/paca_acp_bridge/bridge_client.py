"""WebSocket client: connects to Paca's ACP bridge endpoint, reconnects with
backoff, and dispatches start_turn/stop_turn/pause_turn messages to a
ConversationRunner. See services/ai-agent/src/routes/bridge.py for the
server-side counterpart.
"""

from __future__ import annotations

import asyncio
import json
import logging
from typing import Any
from urllib.parse import urlsplit, urlunsplit

import websockets

from .runner import ConversationRunner

logger = logging.getLogger(__name__)

_HEARTBEAT_INTERVAL_SECONDS = 20
_INITIAL_BACKOFF_SECONDS = 1
_MAX_BACKOFF_SECONDS = 30
# Delay between retries of the head-of-line message in the outbox while
# disconnected (or while a send attempt just failed) — short enough that a
# reconnect is picked up quickly, long enough not to spin tightly.
_SEND_RETRY_SECONDS = 0.5


def to_bridge_ws_url(server: str) -> str:
    """Turn a Paca base URL (http(s)://host) into the bridge WebSocket URL."""
    parts = urlsplit(server)
    scheme = "wss" if parts.scheme in ("https", "wss") else "ws"
    return urlunsplit((scheme, parts.netloc, "/agent-bridge/ws", "", ""))


class BridgeClient:
    def __init__(self, server: str, agent_id: str, token: str, workspace: str) -> None:
        self._url = to_bridge_ws_url(server)
        self._agent_id = agent_id
        self._token = token
        self._runner = ConversationRunner(workspace=workspace, send=self._send)
        self._ws: Any = None
        # Outbound messages (events + turn_status) are queued rather than sent
        # directly — _sender_loop is the only thing that ever calls ws.send(),
        # so a message queued while disconnected (or mid-reconnect) waits here
        # instead of being silently dropped, which previously meant a final
        # turn_status could vanish and leave the server-side conversation
        # stuck at RUNNING forever (see acp_dispatch.py's watchdog for the
        # server-side backstop this complements).
        self._outbox: asyncio.Queue[dict[str, Any]] = asyncio.Queue()
        self._sender_task: asyncio.Task | None = None

    async def _send(self, message: dict[str, Any]) -> None:
        await self._outbox.put(message)

    async def _sender_loop(self) -> None:
        """Drains the outbox in order, retrying a message across reconnects
        instead of dropping it. Runs for the lifetime of the process — one
        instance survives every individual WebSocket connection.
        """
        while True:
            message = await self._outbox.get()
            while True:
                ws = self._ws
                if ws is None:
                    await asyncio.sleep(_SEND_RETRY_SECONDS)
                    continue
                try:
                    await ws.send(json.dumps(message))
                    break
                except asyncio.CancelledError:
                    raise
                except Exception:
                    logger.warning(
                        "Failed to send %s — will retry once reconnected",
                        message.get("type"),
                        exc_info=True,
                    )
                    await asyncio.sleep(_SEND_RETRY_SECONDS)

    async def run_forever(self) -> None:
        self._sender_task = asyncio.create_task(self._sender_loop())
        try:
            backoff = _INITIAL_BACKOFF_SECONDS
            while True:
                try:
                    await self._connect_once()
                    backoff = _INITIAL_BACKOFF_SECONDS
                except asyncio.CancelledError:
                    raise
                except Exception as exc:
                    logger.warning(
                        "Bridge connection lost (%s) — reconnecting in %ss", exc, backoff
                    )
                await asyncio.sleep(backoff)
                backoff = min(backoff * 2, _MAX_BACKOFF_SECONDS)
        finally:
            self._sender_task.cancel()

    async def _connect_once(self) -> None:
        logger.info("Connecting to %s", self._url)
        async with websockets.connect(self._url) as ws:
            self._ws = ws
            await ws.send(
                json.dumps({"type": "hello", "agent_id": self._agent_id, "token": self._token})
            )
            ack = json.loads(await ws.recv())
            if ack.get("type") != "hello_ack":
                raise RuntimeError(f"Bridge rejected connection: {ack}")
            logger.info("Connected — serving ACP conversations from %s", self._runner.workspace)

            heartbeat_task = asyncio.create_task(self._heartbeat_loop())
            try:
                async for raw in ws:
                    await self._handle_message(json.loads(raw))
            finally:
                heartbeat_task.cancel()
                self._ws = None

    async def _heartbeat_loop(self) -> None:
        while True:
            await asyncio.sleep(_HEARTBEAT_INTERVAL_SECONDS)
            await self._send({"type": "ping"})

    async def _handle_message(self, data: dict[str, Any]) -> None:
        msg_type = data.get("type")
        if msg_type == "start_turn":
            await self._runner.start_turn(data)
        elif msg_type in ("stop_turn", "pause_turn"):
            self._runner.interrupt(data.get("conversation_id"))
        elif msg_type == "pong":
            pass
        else:
            logger.warning("Unknown message type from server: %r", msg_type)
