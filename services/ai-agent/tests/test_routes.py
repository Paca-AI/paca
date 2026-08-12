"""Tests for FastAPI routes (health, llm, conversations) — no live services."""

from __future__ import annotations

import uuid
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from fastapi import FastAPI
from httpx import ASGITransport, AsyncClient

from src.core.registry import active_conversations
from src.routes.conversations import router as conv_router
from src.routes.health import router as health_router
from src.routes.llm import router as llm_router

# ─── Test app (no lifespan — avoids Redis/DB startup) ─────────────────────────

_app = FastAPI()
_app.include_router(health_router)
_app.include_router(llm_router)
_app.include_router(conv_router)

# Internal token value from conftest.py env setup
INTERNAL_TOKEN = "test-internal-key"


@pytest.fixture
async def client():
    async with AsyncClient(transport=ASGITransport(app=_app), base_url="http://test") as c:
        yield c


@pytest.fixture(autouse=True)
def clear_active_conversations():
    """Ensure no leftover conversation state bleeds between tests."""
    active_conversations.clear()
    yield
    active_conversations.clear()


# ─── Health ───────────────────────────────────────────────────────────────────


async def test_health_returns_ok(client):
    resp = await client.get("/health")
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}


# ─── LLM models ───────────────────────────────────────────────────────────────


async def test_llm_models_returns_dict(client):
    resp = await client.get("/llm/models")
    assert resp.status_code == 200
    data = resp.json()
    assert isinstance(data, dict)
    # Each value is an object with base_url and a models list
    for provider, info in data.items():
        assert isinstance(provider, str)
        assert isinstance(info, dict)
        assert isinstance(info["models"], list)


async def test_llm_models_includes_minimax_m2_7(client):
    resp = await client.get("/llm/models")
    assert resp.status_code == 200
    assert "MiniMax-M2.7" in resp.json()["minimax"]["models"]


# ─── Conversations — auth ─────────────────────────────────────────────────────


async def test_missing_internal_token_returns_401(client):
    conv_id = uuid.uuid4()
    resp = await client.get(f"/conversations/{conv_id}")
    assert resp.status_code == 401


async def test_wrong_internal_token_returns_401(client):
    conv_id = uuid.uuid4()
    resp = await client.get(
        f"/conversations/{conv_id}", headers={"X-Internal-Token": "wrong-key"}
    )
    assert resp.status_code == 401


# ─── GET /conversations/{id} ──────────────────────────────────────────────────


async def test_get_conversation_found(client):
    conv_id = uuid.uuid4()
    row = {
        "id": str(conv_id),
        "agent_id": "agent-1",
        "project_id": "proj-1",
        "status": "running",
        "trigger_type": "task_assigned",
        "created_at": "2024-01-01T00:00:00",
        "updated_at": "2024-01-01T00:00:00",
    }
    mock_pool = AsyncMock()
    mock_pool.fetchrow.return_value = row

    with patch("src.routes.conversations.get_pool", return_value=mock_pool):
        resp = await client.get(
            f"/conversations/{conv_id}",
            headers={"X-Internal-Token": INTERNAL_TOKEN},
        )

    assert resp.status_code == 200
    body = resp.json()
    assert body["id"] == str(conv_id)
    assert body["status"] == "running"


async def test_get_conversation_not_found(client):
    conv_id = uuid.uuid4()
    mock_pool = AsyncMock()
    mock_pool.fetchrow.return_value = None

    with patch("src.routes.conversations.get_pool", return_value=mock_pool):
        resp = await client.get(
            f"/conversations/{conv_id}",
            headers={"X-Internal-Token": INTERNAL_TOKEN},
        )

    assert resp.status_code == 404


# ─── GET /conversations/{id}/events ───────────────────────────────────────────


async def test_get_events_returns_list(client):
    conv_id = uuid.uuid4()
    events = [
        {
            "id": str(uuid.uuid4()),
            "conversation_id": str(conv_id),
            "event_type": "MessageEvent",
            "event_source": "agent",
            "event_index": 0,
            "payload": "{}",
            "created_at": "2024-01-01T00:00:00",
        }
    ]
    mock_pool = AsyncMock()
    mock_pool.fetch.return_value = events

    with patch("src.routes.conversations.get_pool", return_value=mock_pool):
        resp = await client.get(
            f"/conversations/{conv_id}/events",
            headers={"X-Internal-Token": INTERNAL_TOKEN},
        )

    assert resp.status_code == 200
    body = resp.json()
    assert "events" in body
    assert len(body["events"]) == 1
    assert body["events"][0]["event_type"] == "MessageEvent"


async def test_get_events_empty(client):
    conv_id = uuid.uuid4()
    mock_pool = AsyncMock()
    mock_pool.fetch.return_value = []

    with patch("src.routes.conversations.get_pool", return_value=mock_pool):
        resp = await client.get(
            f"/conversations/{conv_id}/events",
            headers={"X-Internal-Token": INTERNAL_TOKEN},
        )

    assert resp.status_code == 200
    assert resp.json()["events"] == []


# ─── POST /conversations/{id}/stop ────────────────────────────────────────────


async def test_stop_no_active_conversation_still_succeeds(client):
    conv_id = uuid.uuid4()
    with patch(
        "src.routes.conversations.update_conversation_status", new_callable=AsyncMock
    ):
        resp = await client.post(
            f"/conversations/{conv_id}/stop",
            headers={"X-Internal-Token": INTERNAL_TOKEN},
        )
    assert resp.status_code == 200
    assert resp.json()["status"] == "stopped"


async def test_stop_closes_active_conversation(client):
    conv_id = uuid.uuid4()
    mock_conv = MagicMock()
    active_conversations[str(conv_id)] = mock_conv

    with patch(
        "src.routes.conversations.update_conversation_status", new_callable=AsyncMock
    ):
        resp = await client.post(
            f"/conversations/{conv_id}/stop",
            headers={"X-Internal-Token": INTERNAL_TOKEN},
        )

    assert resp.status_code == 200
    mock_conv.close.assert_called_once()
    assert str(conv_id) not in active_conversations


# ─── POST /conversations/{id}/message ─────────────────────────────────────────


async def test_send_message_no_active_conversation_returns_404(client):
    conv_id = uuid.uuid4()
    resp = await client.post(
        f"/conversations/{conv_id}/message",
        headers={"X-Internal-Token": INTERNAL_TOKEN},
        json={"message": "hello"},
    )
    assert resp.status_code == 404


async def test_send_message_calls_send_message_on_conv(client):
    conv_id = uuid.uuid4()
    mock_conv = MagicMock()
    active_conversations[str(conv_id)] = mock_conv

    resp = await client.post(
        f"/conversations/{conv_id}/message",
        headers={"X-Internal-Token": INTERNAL_TOKEN},
        json={"message": "do the thing"},
    )

    assert resp.status_code == 200
    mock_conv.send_message.assert_called_once_with("do the thing")
