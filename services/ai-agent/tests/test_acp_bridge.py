"""Tests for the ACP local-bridge presence/dispatch registry (src/agent/acp_bridge.py).

Uses a minimal fake Valkey client rather than a real one — these tests only
need to verify the presence-key and publish-channel contract, not Redis
Pub/Sub delivery semantics itself. Eviction *delivery* (a published control
message actually reaching another connection's watcher task) is exercised
separately by driving _watch_for_eviction directly against a fake pubsub that
yields specific messages, since _FakePubSub below deliberately doesn't wire
publish() to listen().
"""

import asyncio
import json
from unittest.mock import AsyncMock, MagicMock

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
        # Blocks until the forwarder/watcher task is cancelled by
        # unregister() — no messages are published through this fake in
        # these tests.
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


class _FakeMessagePubSub:
    """A pubsub fake that yields a fixed sequence of messages then blocks —
    used to drive _watch_for_eviction directly with specific control-channel
    payloads, since _FakePubSub above never delivers anything.
    """

    def __init__(self, messages: list[str]):
        self._messages = messages

    async def subscribe(self, channel):
        pass

    async def listen(self):
        for m in self._messages:
            yield {"type": "message", "data": m}
        await asyncio.Event().wait()
        yield  # pragma: no cover — unreachable; keeps this an async generator

    async def unsubscribe(self, channel):
        pass

    async def aclose(self):
        pass


@pytest.fixture(autouse=True)
def _clear_registry():
    acp_bridge._connections.clear()
    acp_bridge._sessions.clear()
    acp_bridge._forward_tasks.clear()
    acp_bridge._eviction_tasks.clear()
    yield
    acp_bridge._connections.clear()
    acp_bridge._sessions.clear()
    acp_bridge._forward_tasks.clear()
    acp_bridge._eviction_tasks.clear()


@pytest.fixture
def fake_redis(monkeypatch):
    fake = _FakeRedis()
    monkeypatch.setattr(acp_bridge, "get_client", lambda: fake)
    # register() starts background forwarder/eviction-watcher tasks that call
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

    session_id = await acp_bridge.register("agent-1", "proj-1", ws)

    assert fake_redis.store["paca:acp-bridge:online:agent-1"] == "1"
    assert await acp_bridge.is_online("agent-1") is True

    await acp_bridge.unregister("agent-1", "proj-1", session_id)


async def test_register_publishes_connected_status(fake_redis, publish_realtime_mock):
    ws = AsyncMock()

    session_id = await acp_bridge.register("agent-1", "proj-1", ws)

    publish_realtime_mock.assert_awaited_once_with(
        project_id="proj-1",
        conversation_id="",
        event_type="agent.acp_bridge.status",
        extra_payload={"agent_id": "agent-1", "connected": True},
    )

    await acp_bridge.unregister("agent-1", "proj-1", session_id)


async def test_unregister_clears_presence(fake_redis):
    ws = AsyncMock()
    session_id = await acp_bridge.register("agent-1", "proj-1", ws)

    await acp_bridge.unregister("agent-1", "proj-1", session_id)

    assert "paca:acp-bridge:online:agent-1" not in fake_redis.store
    assert await acp_bridge.is_online("agent-1") is False


async def test_unregister_publishes_disconnected_status(fake_redis, publish_realtime_mock):
    ws = AsyncMock()
    session_id = await acp_bridge.register("agent-1", "proj-1", ws)
    publish_realtime_mock.reset_mock()

    await acp_bridge.unregister("agent-1", "proj-1", session_id)

    publish_realtime_mock.assert_awaited_once_with(
        project_id="proj-1",
        conversation_id="",
        event_type="agent.acp_bridge.status",
        extra_payload={"agent_id": "agent-1", "connected": False},
    )


async def test_unregister_noop_when_session_id_is_stale(fake_redis, publish_realtime_mock):
    """A disconnect from a connection that already lost an eviction race
    (its session_id no longer matches the currently registered one) must not
    tear down whatever session actually holds the agent now."""
    ws = AsyncMock()
    session_id = await acp_bridge.register("agent-1", "proj-1", ws)
    publish_realtime_mock.reset_mock()

    await acp_bridge.unregister("agent-1", "proj-1", "not-the-current-session")

    assert await acp_bridge.is_online("agent-1") is True
    publish_realtime_mock.assert_not_called()

    await acp_bridge.unregister("agent-1", "proj-1", session_id)


async def test_register_broadcasts_eviction_when_already_online(fake_redis):
    """A second register() for the same agent_id (e.g. a new daemon session,
    possibly on a different replica) must broadcast an eviction naming
    itself as the surviving session, so the first connection's watcher task
    closes it."""
    ws1 = AsyncMock()
    session1 = await acp_bridge.register("agent-1", "proj-1", ws1)

    ws2 = AsyncMock()
    session2 = await acp_bridge.register("agent-1", "proj-1", ws2)

    assert session1 != session2
    control_channel = acp_bridge._control_channel("agent-1")
    control_msgs = [msg for ch, msg in fake_redis.published if ch == control_channel]
    assert len(control_msgs) == 1
    assert json.loads(control_msgs[0]) == {"session_id": session2}

    await acp_bridge.unregister("agent-1", "proj-1", session2)


async def test_register_does_not_broadcast_eviction_for_first_connection(fake_redis):
    ws = AsyncMock()
    session_id = await acp_bridge.register("agent-1", "proj-1", ws)

    control_channel = acp_bridge._control_channel("agent-1")
    assert [msg for ch, msg in fake_redis.published if ch == control_channel] == []

    await acp_bridge.unregister("agent-1", "proj-1", session_id)


async def test_evict_publishes_force_evict_sentinel(fake_redis):
    await acp_bridge.evict("agent-1")

    control_channel = acp_bridge._control_channel("agent-1")
    control_msgs = [msg for ch, msg in fake_redis.published if ch == control_channel]
    assert len(control_msgs) == 1
    assert json.loads(control_msgs[0]) == {"session_id": acp_bridge._FORCE_EVICT_SESSION_ID}


async def test_watch_for_eviction_closes_ws_on_mismatched_session(monkeypatch):
    ws = AsyncMock()
    fake_client = MagicMock()
    fake_client.pubsub.return_value = _FakeMessagePubSub(
        [json.dumps({"session_id": "some-other-session"})]
    )
    monkeypatch.setattr(acp_bridge.stream_store, "get_pubsub_client", lambda: fake_client)

    await acp_bridge._watch_for_eviction("agent-1", "my-session", ws)

    ws.close.assert_awaited_once_with(code=4409)


async def test_watch_for_eviction_ignores_own_session_id(monkeypatch):
    ws = AsyncMock()
    fake_client = MagicMock()
    fake_client.pubsub.return_value = _FakeMessagePubSub([json.dumps({"session_id": "my-session"})])
    monkeypatch.setattr(acp_bridge.stream_store, "get_pubsub_client", lambda: fake_client)

    task = asyncio.create_task(acp_bridge._watch_for_eviction("agent-1", "my-session", ws))
    try:
        with pytest.raises(TimeoutError):
            await asyncio.wait_for(asyncio.shield(task), timeout=0.2)
    finally:
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task

    ws.close.assert_not_called()


async def test_dispatch_returns_false_when_offline(fake_redis):
    result = await acp_bridge.dispatch("agent-1", {"type": "start_turn"})

    assert result is False
    assert fake_redis.published == []


async def test_dispatch_publishes_when_online(fake_redis):
    ws = AsyncMock()
    session_id = await acp_bridge.register("agent-1", "proj-1", ws)

    result = await acp_bridge.dispatch("agent-1", {"type": "start_turn", "conversation_id": "c1"})

    assert result is True
    dispatch_channel = acp_bridge._dispatch_channel("agent-1")
    dispatch_msgs = [msg for ch, msg in fake_redis.published if ch == dispatch_channel]
    assert len(dispatch_msgs) == 1
    assert json.loads(dispatch_msgs[0]) == {"type": "start_turn", "conversation_id": "c1"}

    await acp_bridge.unregister("agent-1", "proj-1", session_id)


async def test_heartbeat_refreshes_presence_ttl(fake_redis):
    ws = AsyncMock()
    session_id = await acp_bridge.register("agent-1", "proj-1", ws)

    # Should not raise even though the fake's expire() is a no-op beyond
    # checking key presence.
    await acp_bridge.heartbeat("agent-1")

    await acp_bridge.unregister("agent-1", "proj-1", session_id)
