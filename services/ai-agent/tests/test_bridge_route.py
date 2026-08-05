"""Tests for the ACP bridge WebSocket route and its internal HTTP endpoints
(src/routes/bridge.py) — no live services.
"""

from __future__ import annotations

from unittest.mock import ANY, AsyncMock, patch

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from httpx import ASGITransport, AsyncClient
from starlette.websockets import WebSocketDisconnect

from src.routes.bridge import router as bridge_router

_app = FastAPI()
_app.include_router(bridge_router)

# Internal token value from conftest.py's INTERNAL_API_KEY env setup.
INTERNAL_TOKEN = "test-internal-key"


@pytest.fixture
async def client():
    async with AsyncClient(transport=ASGITransport(app=_app), base_url="http://test") as c:
        yield c


# ─── hello handshake ───────────────────────────────────────────────────────


def test_hello_missing_token_closes_4400():
    with patch(
        "src.routes.bridge.find_agent_by_bridge_token_hash", new_callable=AsyncMock
    ) as mock_lookup:
        with TestClient(_app).websocket_connect("/agent-bridge/ws") as ws:
            ws.send_json({"type": "hello", "agent_id": "agent-1"})
            with pytest.raises(WebSocketDisconnect) as exc_info:
                ws.receive_json()
        assert exc_info.value.code == 4400
        mock_lookup.assert_not_called()


def test_hello_invalid_token_closes_4401():
    with patch(
        "src.routes.bridge.find_agent_by_bridge_token_hash",
        new_callable=AsyncMock,
        return_value=None,
    ):
        with TestClient(_app).websocket_connect("/agent-bridge/ws") as ws:
            ws.send_json({"type": "hello", "agent_id": "agent-1", "token": "wrong"})
            with pytest.raises(WebSocketDisconnect) as exc_info:
                ws.receive_json()
        assert exc_info.value.code == 4401


def test_hello_token_belongs_to_different_agent_closes_4401():
    # find_agent_by_bridge_token_hash resolved a real agent, but not the one
    # the client claimed in its hello frame — must not authenticate as it.
    with patch(
        "src.routes.bridge.find_agent_by_bridge_token_hash",
        new_callable=AsyncMock,
        return_value=("agent-other", "proj-1"),
    ):
        with TestClient(_app).websocket_connect("/agent-bridge/ws") as ws:
            ws.send_json({"type": "hello", "agent_id": "agent-1", "token": "someone-elses"})
            with pytest.raises(WebSocketDisconnect) as exc_info:
                ws.receive_json()
        assert exc_info.value.code == 4401


def test_hello_success_registers_and_sends_ack():
    with (
        patch(
            "src.routes.bridge.find_agent_by_bridge_token_hash",
            new_callable=AsyncMock,
            return_value=("agent-1", "proj-1"),
        ),
        patch(
            "src.agent.acp_bridge.register",
            new_callable=AsyncMock,
            return_value="session-1",
        ) as mock_register,
        patch("src.agent.acp_bridge.unregister", new_callable=AsyncMock),
    ):
        with TestClient(_app).websocket_connect("/agent-bridge/ws") as ws:
            ws.send_json({"type": "hello", "agent_id": "agent-1", "token": "correct"})
            ack = ws.receive_json()
        assert ack == {"type": "hello_ack"}
        mock_register.assert_awaited_once_with("agent-1", "proj-1", ANY)


# ─── event / turn_status ownership checks ──────────────────────────────────
#
# The bridge previously trusted a client-supplied conversation_id/project_id
# on every "event"/"turn_status" message with no check that the connected
# agent actually owns that conversation — a bridge holding one agent's token
# could inject events into, or forge the terminal status of, any
# conversation it named. These are regression tests for that fix.


def _connected_ws(agent_id="agent-1", project_id="proj-1"):
    """Context manager yielding a websocket already past the hello handshake,
    with register/unregister mocked so no real registry state is touched.
    """
    patches = (
        patch(
            "src.routes.bridge.find_agent_by_bridge_token_hash",
            new_callable=AsyncMock,
            return_value=(agent_id, project_id),
        ),
        patch("src.agent.acp_bridge.register", new_callable=AsyncMock, return_value="session-1"),
        patch("src.agent.acp_bridge.unregister", new_callable=AsyncMock),
        patch("src.agent.acp_bridge.heartbeat", new_callable=AsyncMock),
    )
    for p in patches:
        p.start()
    ws_cm = TestClient(_app).websocket_connect("/agent-bridge/ws")
    ws = ws_cm.__enter__()
    ws.send_json({"type": "hello", "agent_id": agent_id, "token": "tok"})
    assert ws.receive_json() == {"type": "hello_ack"}
    return ws, ws_cm, patches


def test_event_for_owned_conversation_is_persisted():
    ws, ws_cm, patches = _connected_ws()
    try:
        with (
            patch(
                "src.repositories.conversation_repository.get_conversation_agent_type",
                new_callable=AsyncMock,
                return_value=("agent-1", "acp"),
            ),
            patch(
                "src.repositories.conversation_repository.get_next_event_index",
                new_callable=AsyncMock,
                return_value=0,
            ),
            patch(
                "src.repositories.conversation_repository.get_conversation_realtime_context",
                new_callable=AsyncMock,
                return_value=("proj-1", None),
            ),
            patch(
                "src.routes.bridge.persist_conversation_event", new_callable=AsyncMock
            ) as mock_persist,
        ):
            ws.send_json(
                {
                    "type": "event",
                    "conversation_id": "conv-1",
                    "event_type": "MessageEvent",
                    "payload": "{}",
                }
            )
            ws.send_json({"type": "ping"})
            assert ws.receive_json() == {"type": "pong"}
        mock_persist.assert_awaited_once()
        assert mock_persist.await_args.kwargs["conversation_id"] == "conv-1"
        # project_id/actor_user_id must come from a per-conversation DB lookup
        # (get_conversation_realtime_context), not a client-supplied field on
        # the message, nor the connection-level agent resolution — a single
        # bridge session can relay conversations from different projects (or
        # a global-chat conversation with no project at all).
        assert mock_persist.await_args.kwargs["project_id"] == "proj-1"
        assert mock_persist.await_args.kwargs["actor_user_id"] is None
    finally:
        ws_cm.__exit__(None, None, None)
        for p in patches:
            p.stop()


def test_event_for_unowned_conversation_is_dropped():
    ws, ws_cm, patches = _connected_ws(agent_id="agent-1")
    try:
        with (
            patch(
                "src.repositories.conversation_repository.get_conversation_agent_type",
                new_callable=AsyncMock,
                return_value=("agent-other", "acp"),
            ),
            patch(
                "src.routes.bridge.persist_conversation_event", new_callable=AsyncMock
            ) as mock_persist,
        ):
            ws.send_json(
                {
                    "type": "event",
                    "conversation_id": "someone-elses-conversation",
                    "event_type": "MessageEvent",
                    "payload": "{}",
                }
            )
            ws.send_json({"type": "ping"})
            assert ws.receive_json() == {"type": "pong"}
        mock_persist.assert_not_awaited()
    finally:
        ws_cm.__exit__(None, None, None)
        for p in patches:
            p.stop()


def test_event_for_nonexistent_conversation_is_dropped():
    ws, ws_cm, patches = _connected_ws()
    try:
        with (
            patch(
                "src.repositories.conversation_repository.get_conversation_agent_type",
                new_callable=AsyncMock,
                return_value=None,
            ),
            patch(
                "src.routes.bridge.persist_conversation_event", new_callable=AsyncMock
            ) as mock_persist,
        ):
            ws.send_json(
                {"type": "event", "conversation_id": "no-such-conversation", "payload": "{}"}
            )
            ws.send_json({"type": "ping"})
            assert ws.receive_json() == {"type": "pong"}
        mock_persist.assert_not_awaited()
    finally:
        ws_cm.__exit__(None, None, None)
        for p in patches:
            p.stop()


def test_turn_status_for_owned_conversation_updates_status():
    ws, ws_cm, patches = _connected_ws()
    try:
        with (
            patch(
                "src.repositories.conversation_repository.get_conversation_agent_type",
                new_callable=AsyncMock,
                return_value=("agent-1", "acp"),
            ),
            patch(
                "src.repositories.conversation_repository.get_conversation_realtime_context",
                new_callable=AsyncMock,
                return_value=("proj-1", None),
            ),
            patch(
                "src.repositories.conversation_repository.update_conversation_status",
                new_callable=AsyncMock,
            ) as mock_update,
            patch("src.core.streams.publish_realtime", new_callable=AsyncMock) as mock_publish,
        ):
            ws.send_json({"type": "turn_status", "conversation_id": "conv-1", "status": "finished"})
            ws.send_json({"type": "ping"})
            assert ws.receive_json() == {"type": "pong"}
        mock_update.assert_awaited_once()
        assert mock_update.await_args.args[0] == "conv-1"
        mock_publish.assert_awaited_once()
        assert mock_publish.await_args.kwargs["project_id"] == "proj-1"
    finally:
        ws_cm.__exit__(None, None, None)
        for p in patches:
            p.stop()


def test_turn_status_for_unowned_conversation_is_dropped():
    ws, ws_cm, patches = _connected_ws(agent_id="agent-1")
    try:
        with (
            patch(
                "src.repositories.conversation_repository.get_conversation_agent_type",
                new_callable=AsyncMock,
                return_value=("agent-other", "acp"),
            ),
            patch(
                "src.repositories.conversation_repository.update_conversation_status",
                new_callable=AsyncMock,
            ) as mock_update,
        ):
            ws.send_json(
                {
                    "type": "turn_status",
                    "conversation_id": "someone-elses-conversation",
                    "status": "failed",
                }
            )
            ws.send_json({"type": "ping"})
            assert ws.receive_json() == {"type": "pong"}
        mock_update.assert_not_awaited()
    finally:
        ws_cm.__exit__(None, None, None)
        for p in patches:
            p.stop()


def test_disconnect_unregisters_with_authenticated_project_id_not_client_supplied():
    """Regression test: the loop used to reassign the outer `project_id`
    variable from each message's client-supplied "project_id" field, so a
    connection's final disconnect cleanup could run against whatever project
    the *last processed message* happened to name instead of the agent's
    real project — see routes/bridge.py's handling of "event"/"turn_status".
    """
    with (
        patch(
            "src.routes.bridge.find_agent_by_bridge_token_hash",
            new_callable=AsyncMock,
            return_value=("agent-1", "real-project"),
        ),
        patch(
            "src.agent.acp_bridge.register",
            new_callable=AsyncMock,
            return_value="session-1",
        ),
        patch("src.agent.acp_bridge.unregister", new_callable=AsyncMock) as mock_unregister,
        patch("src.agent.acp_bridge.heartbeat", new_callable=AsyncMock),
        patch(
            "src.repositories.conversation_repository.get_conversation_agent_type",
            new_callable=AsyncMock,
            return_value=("agent-1", "acp"),
        ),
        patch(
            "src.repositories.conversation_repository.get_next_event_index",
            new_callable=AsyncMock,
            return_value=0,
        ),
        patch(
            "src.repositories.conversation_repository.get_conversation_realtime_context",
            new_callable=AsyncMock,
            return_value=("real-project", None),
        ),
        patch("src.routes.bridge.persist_conversation_event", new_callable=AsyncMock),
    ):
        with TestClient(_app).websocket_connect("/agent-bridge/ws") as ws:
            ws.send_json({"type": "hello", "agent_id": "agent-1", "token": "tok"})
            assert ws.receive_json() == {"type": "hello_ack"}
            # A message carrying a spoofed/stale "project_id" must not be
            # able to influence what unregister() is eventually called with.
            ws.send_json(
                {
                    "type": "event",
                    "conversation_id": "conv-1",
                    "project_id": "spoofed-project",
                    "payload": "{}",
                }
            )
            ws.send_json({"type": "ping"})
            assert ws.receive_json() == {"type": "pong"}

        mock_unregister.assert_awaited_once_with("agent-1", "real-project", "session-1")


# ─── internal-only HTTP endpoints ──────────────────────────────────────────


async def test_bridge_status_missing_internal_token_returns_401(client):
    resp = await client.get("/agent-bridge/status/agent-1")
    assert resp.status_code == 401


async def test_bridge_status_wrong_internal_token_returns_401(client):
    resp = await client.get(
        "/agent-bridge/status/agent-1", headers={"X-Internal-Token": "wrong-key"}
    )
    assert resp.status_code == 401


async def test_bridge_status_returns_connected_state(client):
    with patch(
        "src.agent.acp_bridge.is_online", new_callable=AsyncMock, return_value=True
    ) as mock_is_online:
        resp = await client.get(
            "/agent-bridge/status/agent-1", headers={"X-Internal-Token": INTERNAL_TOKEN}
        )
    assert resp.status_code == 200
    assert resp.json() == {"connected": True}
    mock_is_online.assert_awaited_once_with("agent-1")


async def test_bridge_disconnect_requires_internal_token(client):
    resp = await client.post("/agent-bridge/disconnect/agent-1")
    assert resp.status_code == 401


async def test_bridge_disconnect_calls_evict(client):
    with patch("src.agent.acp_bridge.evict", new_callable=AsyncMock) as mock_evict:
        resp = await client.post(
            "/agent-bridge/disconnect/agent-1", headers={"X-Internal-Token": INTERNAL_TOKEN}
        )
    assert resp.status_code == 200
    assert resp.json() == {"ok": True}
    mock_evict.assert_awaited_once_with("agent-1")
