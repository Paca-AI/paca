"""WebSocket endpoint for local ACP bridge daemons (apps/acp-bridge).

The daemon connects outbound from the user's machine, authenticates with a
per-agent bridge token, and stays connected for as long as `paca-acp-bridge
run` keeps running. Once connected it: (1) receives dispatched turns/controls
published by acp_dispatch.py via acp_bridge.dispatch(), and (2) reports SDK
events + turn status back, persisted through the same
executor.persist_conversation_event() path cloud-sandboxed conversations use
— so the frontend renders either source identically.

The agent id and bridge token travel as a first WebSocket frame ("hello"),
sent right after the client completes the handshake — *not* as connection
headers validated before accept(). Headers would let an invalid token be
rejected slightly earlier, but the daemon (apps/acp-bridge) is distributed
via `uvx paca-acp-bridge`, which always resolves the latest published
package — a client on an older release than the server would never send
those headers and would be rejected outright with no way to self-diagnose
why (a bad token and a protocol mismatch both surface identically as the
WebSocket library's generic "server rejected connection" error). The hello
frame is a wire-compatible, self-describing handshake that doesn't have that
failure mode; _HELLO_TIMEOUT_SECONDS bounds how long an unauthenticated
socket can sit open waiting for one, which was the actual concern with
accepting first.
"""

from __future__ import annotations

import asyncio
import hashlib
import logging
import secrets

from fastapi import APIRouter, Depends, Header, HTTPException, WebSocket, WebSocketDisconnect

from ..agent import acp_bridge
from ..agent.executor import persist_conversation_event
from ..config import settings
from ..core import streams as stream_store
from ..models.conversation_status import ConversationStatus
from ..repositories import conversation_repository
from ..repositories.agent_repository import find_agent_by_bridge_token_hash

logger = logging.getLogger(__name__)

router = APIRouter(prefix="/agent-bridge")

# How long to wait for the client's "hello" frame after accepting the
# WebSocket before giving up — bounds how long an unauthenticated connection
# can be held open (e.g. by a client that never sends anything).
_HELLO_TIMEOUT_SECONDS = 10


def _hash_token(token: str) -> str:
    return hashlib.sha256(token.encode()).hexdigest()


@router.websocket("/ws")
async def bridge_ws(websocket: WebSocket) -> None:
    await websocket.accept()

    try:
        hello = await asyncio.wait_for(websocket.receive_json(), timeout=_HELLO_TIMEOUT_SECONDS)
    except Exception:
        await websocket.close(code=4400)
        return
    if hello.get("type") != "hello" or not hello.get("agent_id") or not hello.get("token"):
        await websocket.close(code=4400)
        return

    token_hash = _hash_token(hello["token"])
    resolved = await find_agent_by_bridge_token_hash(token_hash)
    if resolved is None or resolved[0] != hello["agent_id"]:
        await websocket.close(code=4401)
        return

    agent_id, project_id = resolved
    session_id = await acp_bridge.register(agent_id, project_id, websocket)
    logger.info("ACP bridge connected for agent %s", agent_id)
    await websocket.send_json({"type": "hello_ack"})

    try:
        while True:
            data = await websocket.receive_json()
            msg_type = data.get("type")

            if msg_type == "event":
                conversation_id = data.get("conversation_id")
                project_id = data.get("project_id")
                if not conversation_id or not project_id:
                    continue
                event_index = await conversation_repository.get_next_event_index(conversation_id)
                await persist_conversation_event(
                    conversation_id=conversation_id,
                    project_id=project_id,
                    event_type=data.get("event_type", "ACPEvent"),
                    event_source=data.get("event_source", "agent"),
                    event_index=event_index,
                    payload=data.get("payload", "{}"),
                )

            elif msg_type == "turn_status":
                conversation_id = data.get("conversation_id")
                project_id = data.get("project_id")
                status_str = data.get("status")
                if not conversation_id or not project_id or status_str is None:
                    continue
                try:
                    status = ConversationStatus(status_str)
                except ValueError:
                    logger.warning(
                        "Dropping unknown turn_status %r from agent %s", status_str, agent_id
                    )
                    continue
                await conversation_repository.update_conversation_status(
                    conversation_id, status, error_message=data.get("error_message")
                )
                await stream_store.publish_realtime(
                    project_id=project_id,
                    conversation_id=conversation_id,
                    event_type=f"agent.conversation.{status.value}",
                )

            elif msg_type == "ping":
                await acp_bridge.heartbeat(agent_id)
                await websocket.send_json({"type": "pong"})

            else:
                logger.warning(
                    "Unknown ACP bridge message type %r from agent %s", msg_type, agent_id
                )

    except WebSocketDisconnect:
        pass
    except Exception:
        logger.exception("ACP bridge connection for agent %s crashed", agent_id)
    finally:
        await acp_bridge.unregister(agent_id, project_id, session_id)
        logger.info("ACP bridge disconnected for agent %s", agent_id)


def _require_internal_key(x_internal_token: str = Header(default="")) -> None:
    """Dependency that rejects requests missing the correct internal API token.

    Duplicated from routes/conversations.py rather than imported — both are
    tiny and importing across route modules for a two-line check isn't worth
    the coupling.
    """
    if not secrets.compare_digest(x_internal_token, settings.internal_api_key):
        raise HTTPException(status_code=401, detail="Unauthorized")


@router.get("/status/{agent_id}", dependencies=[Depends(_require_internal_key)])
async def bridge_status(agent_id: str) -> dict:
    """Internal endpoint proxied by services/api's GetACPBridgeStatus."""
    return {"connected": await acp_bridge.is_online(agent_id)}


@router.post("/disconnect/{agent_id}", dependencies=[Depends(_require_internal_key)])
async def bridge_disconnect(agent_id: str) -> dict:
    """Internal endpoint: force-close any currently connected bridge session
    for this agent. Called by services/api right after a bridge token is
    regenerated, so a daemon still holding the old token can't stay
    connected indefinitely (see agent_handler.go's GenerateACPBridgeToken).
    """
    await acp_bridge.evict(agent_id)
    return {"ok": True}
