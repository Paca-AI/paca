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
        self._send_lock = asyncio.Lock()

    async def _send(self, message: dict[str, Any]) -> None:
        if self._ws is None:
            logger.warning("Dropping message — not connected: %s", message.get("type"))
            return
        async with self._send_lock:
            await self._ws.send(json.dumps(message))

    async def run_forever(self) -> None:
        backoff = _INITIAL_BACKOFF_SECONDS
        while True:
            try:
                await self._connect_once()
                backoff = _INITIAL_BACKOFF_SECONDS
            except asyncio.CancelledError:
                raise
            except Exception as exc:
                logger.warning("Bridge connection lost (%s) — reconnecting in %ss", exc, backoff)
            await asyncio.sleep(backoff)
            backoff = min(backoff * 2, _MAX_BACKOFF_SECONDS)

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
