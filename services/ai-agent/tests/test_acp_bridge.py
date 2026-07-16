"""Tests for the ACP local-bridge presence/dispatch registry (src/agent/acp_bridge.py).

Uses a minimal fake Valkey client rather than a real one — these tests only
need to verify the presence-key and publish-channel contract, not Redis
Pub/Sub delivery semantics itself.
"""

import asyncio
import json
from unittest.mock import AsyncMock

import pytest

import src.agent.acp_bridge as acp_bridge


class _FakePubSub:
    def __init__(self):
        self.subscribed: list[str] = []
        self.unsubscribed: list[str] = []
        self.closed = False

    async def subscribe(self, channel):
        self.subscribed.append(channel)

    async def listen(self):
        # Blocks until the forwarder task is cancelled by unregister() — no
        # messages are published through this fake in these tests.
        await asyncio.Event().wait()
        yield  # pragma: no cover — unreachable; keeps this an async generator

    async def unsubscribe(self, channel):
        self.unsubscribed.append(channel)

    async def aclose(self):
        self.closed = True


class _FakeRedis:
    def __init__(self):
        self.store: dict[str, str] = {}
        self.published: list[tuple[str, str]] = []

    async def set(self, key, value, ex=None):
        self.store[key] = value

    async def delete(self, key):
        self.store.pop(key, None)

    async def expire(self, key, ttl):
        if key in self.store:
            return True
        return False

    async def exists(self, key):
        return 1 if key in self.store else 0

    async def publish(self, channel, message):
        self.published.append((channel, message))

    def pubsub(self):
        return _FakePubSub()


@pytest.fixture(autouse=True)
def _clear_registry():
    acp_bridge._connections.clear()
    acp_bridge._forward_tasks.clear()
    yield
    acp_bridge._connections.clear()
    acp_bridge._forward_tasks.clear()


@pytest.fixture
def fake_redis(monkeypatch):
    fake = _FakeRedis()
    monkeypatch.setattr(acp_bridge, "get_client", lambda: fake)
    # register() starts a background forwarder task that calls
    # stream_store.get_pubsub_client() — fake that too so it doesn't try to
    # reach a real Valkey instance.
    monkeypatch.setattr(acp_bridge.stream_store, "get_pubsub_client", lambda: fake)
    return fake


@pytest.fixture(autouse=True)
def publish_realtime_mock(monkeypatch):
    """register()/unregister() publish a realtime status event on every call
    (see _publish_status) — mocked out here so tests don't hit real Valkey."""
    mock = AsyncMock()
    monkeypatch.setattr(acp_bridge.stream_store, "publish_realtime", mock)
    return mock


async def test_register_sets_presence_and_is_online(fake_redis):
    ws = AsyncMock()

    await acp_bridge.register("agent-1", "proj-1", ws)

    assert fake_redis.store["paca:acp-bridge:online:agent-1"] == "1"
    assert await acp_bridge.is_online("agent-1") is True

    await acp_bridge.unregister("agent-1", "proj-1")


async def test_register_publishes_connected_status(fake_redis, publish_realtime_mock):
    ws = AsyncMock()

    await acp_bridge.register("agent-1", "proj-1", ws)

    publish_realtime_mock.assert_awaited_once_with(
        project_id="proj-1",
        conversation_id="",
        event_type="agent.acp_bridge.status",
        extra_payload={"agent_id": "agent-1", "connected": True},
    )

    await acp_bridge.unregister("agent-1", "proj-1")


async def test_unregister_clears_presence(fake_redis):
    ws = AsyncMock()
    await acp_bridge.register("agent-1", "proj-1", ws)

    await acp_bridge.unregister("agent-1", "proj-1")

    assert "paca:acp-bridge:online:agent-1" not in fake_redis.store
    assert await acp_bridge.is_online("agent-1") is False


async def test_unregister_publishes_disconnected_status(fake_redis, publish_realtime_mock):
    ws = AsyncMock()
    await acp_bridge.register("agent-1", "proj-1", ws)
    publish_realtime_mock.reset_mock()

    await acp_bridge.unregister("agent-1", "proj-1")

    publish_realtime_mock.assert_awaited_once_with(
        project_id="proj-1",
        conversation_id="",
        event_type="agent.acp_bridge.status",
        extra_payload={"agent_id": "agent-1", "connected": False},
    )


async def test_dispatch_returns_false_when_offline(fake_redis):
    result = await acp_bridge.dispatch("agent-1", {"type": "start_turn"})

    assert result is False
    assert fake_redis.published == []


async def test_dispatch_publishes_when_online(fake_redis):
    ws = AsyncMock()
    await acp_bridge.register("agent-1", "proj-1", ws)

    result = await acp_bridge.dispatch(
        "agent-1", {"type": "start_turn", "conversation_id": "c1"}
    )

    assert result is True
    assert len(fake_redis.published) == 1
    channel, message = fake_redis.published[0]
    assert channel == "paca:acp-bridge:dispatch:agent-1"
    assert json.loads(message) == {"type": "start_turn", "conversation_id": "c1"}

    await acp_bridge.unregister("agent-1", "proj-1")


async def test_heartbeat_refreshes_presence_ttl(fake_redis):
    ws = AsyncMock()
    await acp_bridge.register("agent-1", "proj-1", ws)

    # Should not raise even though the fake's expire() is a no-op beyond
    # checking key presence.
    await acp_bridge.heartbeat("agent-1")

    await acp_bridge.unregister("agent-1", "proj-1")
