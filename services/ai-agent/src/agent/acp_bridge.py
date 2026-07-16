"""Registry for local ACP bridge connections.

An ACP-type agent's "sandbox" is a WebSocket connection from a daemon the
user runs on their own machine (apps/acp-bridge) — see routes/bridge.py for
the connection handshake and acp_dispatch.py for how triggers get routed
here. Presence and dispatch both go through Valkey so any ai-agent replica
can tell whether an agent's bridge is connected (possibly to a *different*
replica) and reach it: PUBLISH on a per-agent channel is delivered to every
subscriber across all replicas, regardless of which process holds the
WebSocket — the same cross-replica pattern already used for
`paca.events` (see core/streams.py).
"""

from __future__ import annotations

import asyncio
import json
import logging
from typing import Any, Protocol

from ..core import streams as stream_store
from ..core.streams import get_client

logger = logging.getLogger(__name__)

_PRESENCE_PREFIX = "paca:acp-bridge:online:"
_DISPATCH_PREFIX = "paca:acp-bridge:dispatch:"
# Presence TTL — the daemon must ping (see routes/bridge.py's "ping" handling)
# well within this window; comfortably longer than the daemon's own ~20s
# heartbeat interval so a couple of missed/delayed pings don't flap presence.
_PRESENCE_TTL_SECONDS = 45
# Backoff for re-subscribing after the forwarder's Pub/Sub connection drops
# (e.g. a Valkey restart) — keeps the WebSocket connection usable instead of
# silently losing dispatch delivery for the rest of its lifetime.
_RECONNECT_BACKOFF_SECONDS = 2


class _SendsJSON(Protocol):
    async def send_json(self, data: Any) -> None: ...


# Connections + their forwarding tasks, keyed by agent_id — only valid on
# *this* replica; presence/dispatch below is what makes routing work when the
# connection lives on a different replica.
_connections: dict[str, _SendsJSON] = {}
_forward_tasks: dict[str, asyncio.Task] = {}


def _presence_key(agent_id: str) -> str:
    return f"{_PRESENCE_PREFIX}{agent_id}"


def _dispatch_channel(agent_id: str) -> str:
    return f"{_DISPATCH_PREFIX}{agent_id}"


async def _forward_dispatched_messages(agent_id: str, ws: _SendsJSON) -> None:
    """Subscribe to the per-agent dispatch channel and forward each message
    to the connected WebSocket until cancelled (on unregister/disconnect).

    Reconnects with backoff on a dropped Pub/Sub connection (e.g. a Valkey
    restart) rather than giving up for the rest of the WebSocket's lifetime —
    the outer loop only exits on cancellation.
    """
    while True:
        # get_pubsub_client() is a dedicated, no-read-timeout connection —
        # get_client()'s regular 5s socket_timeout is right for short
        # request/response commands but would spuriously kill this
        # long-idle .listen() loop the first time nothing arrives in 5s.
        pubsub = stream_store.get_pubsub_client().pubsub()
        try:
            await pubsub.subscribe(_dispatch_channel(agent_id))
            async for message in pubsub.listen():
                if message.get("type") != "message":
                    continue
                try:
                    payload = json.loads(message["data"])
                except (TypeError, ValueError, KeyError):
                    logger.warning(
                        "Dropping malformed ACP bridge dispatch for agent %s", agent_id
                    )
                    continue
                await ws.send_json(payload)
            return
        except asyncio.CancelledError:
            raise
        except Exception:
            logger.exception(
                "ACP bridge forwarder for agent %s lost its connection — "
                "reconnecting in %ss",
                agent_id,
                _RECONNECT_BACKOFF_SECONDS,
            )
        finally:
            try:
                await pubsub.unsubscribe(_dispatch_channel(agent_id))
                await pubsub.aclose()
            except Exception:
                logger.debug("Failed to clean up pubsub for agent %s", agent_id, exc_info=True)
        await asyncio.sleep(_RECONNECT_BACKOFF_SECONDS)


async def register(agent_id: str, project_id: str, ws: _SendsJSON) -> None:
    """Mark an agent's local bridge as connected on this replica.

    Publishes an "agent.acp_bridge.status" realtime event so the frontend
    can react immediately instead of polling the status endpoint — see
    src/hooks/use-project-realtime.ts on the frontend.
    """
    client = get_client()
    await client.set(_presence_key(agent_id), "1", ex=_PRESENCE_TTL_SECONDS)
    _connections[agent_id] = ws
    _forward_tasks[agent_id] = asyncio.create_task(_forward_dispatched_messages(agent_id, ws))
    await _publish_status(agent_id, project_id, connected=True)


async def unregister(agent_id: str, project_id: str) -> None:
    """Tear down a bridge connection — called on WebSocket disconnect."""
    task = _forward_tasks.pop(agent_id, None)
    if task is not None:
        task.cancel()
        try:
            await task
        except asyncio.CancelledError:
            pass
    _connections.pop(agent_id, None)
    client = get_client()
    await client.delete(_presence_key(agent_id))
    await _publish_status(agent_id, project_id, connected=False)


async def _publish_status(agent_id: str, project_id: str, *, connected: bool) -> None:
    try:
        await stream_store.publish_realtime(
            project_id=project_id,
            conversation_id="",
            event_type="agent.acp_bridge.status",
            extra_payload={"agent_id": agent_id, "connected": connected},
        )
    except Exception:
        logger.warning(
            "Failed to publish ACP bridge status for agent %s", agent_id, exc_info=True
        )


async def heartbeat(agent_id: str) -> None:
    """Refresh an agent's presence TTL — called on each "ping" from the daemon."""
    client = get_client()
    await client.expire(_presence_key(agent_id), _PRESENCE_TTL_SECONDS)


async def is_online(agent_id: str) -> bool:
    """Whether *any* replica currently holds a live bridge connection for this agent."""
    client = get_client()
    return bool(await client.exists(_presence_key(agent_id)))


async def dispatch(agent_id: str, message: dict[str, Any]) -> bool:
    """Publish a message (start_turn/stop_turn/pause_turn) to the agent's bridge.

    Returns False without publishing if the agent isn't currently connected
    (checked first — Valkey Pub/Sub drops messages with no subscriber rather
    than queuing them, so callers must not rely on eventual delivery).
    """
    if not await is_online(agent_id):
        return False
    client = get_client()
    await client.publish(_dispatch_channel(agent_id), json.dumps(message))
    return True
