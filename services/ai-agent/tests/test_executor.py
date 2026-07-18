"""Tests for executor helpers."""

import asyncio
import json
import threading
from unittest.mock import AsyncMock, Mock

import pytest

from src.agent import executor
from src.agent.executor import (
    _AtomicCounter,
    _find_idle_chat_sandboxes,
    _keep_sandbox_alive,
    _make_event_callback,
    _make_reconciler,
    _post_turn_status,
    _SeenEvents,
    _wait_for_done_or_stop,
    teardown_paused_chat_sandbox,
)
from src.core import streams as stream_store
from src.core.registry import ChatSandboxState, chat_sandboxes, stop_events
from src.core.streams import TriggerMessage
from src.repositories import conversation_repository

# ─── _AtomicCounter ─────────────────────────────────────────────────────────


def test_atomic_counter_defaults_to_zero():
    counter = _AtomicCounter()
    assert counter.next() == 0
    assert counter.next() == 1


def test_atomic_counter_seeds_from_start():
    """A resumed chat turn must seed the counter from the conversation's
    existing next event_index (see run_conversation / get_next_event_index)
    rather than always restarting at 0 — event_index is unique per
    conversation for its entire lifetime, and insert_conversation_event's
    ON CONFLICT DO NOTHING silently drops anything that collides with an
    index an earlier turn already used."""
    counter = _AtomicCounter(start=7)
    assert counter.next() == 7
    assert counter.next() == 8


# ─── _make_event_callback ─────────────────────────────────────────────────────
#
# These run a real asyncio loop on a background thread because the callbacks
# call asyncio.run_coroutine_threadsafe(...).result(...) — exactly how they're
# driven in production (the SDK invokes them synchronously from the executor
# thread, while the main event loop runs separately).


@pytest.fixture
def bg_loop():
    loop = asyncio.new_event_loop()
    thread = threading.Thread(target=loop.run_forever, daemon=True)
    thread.start()
    yield loop
    loop.call_soon_threadsafe(loop.stop)
    thread.join(timeout=5)
    loop.close()


@pytest.fixture
def mock_persistence(monkeypatch):
    insert_mock = AsyncMock()
    monkeypatch.setattr(conversation_repository, "insert_conversation_event", insert_mock)
    monkeypatch.setattr(stream_store, "publish_event", AsyncMock())
    monkeypatch.setattr(stream_store, "publish_realtime", AsyncMock())
    return insert_mock


class MessageEvent:
    """Stand-in for openhands.sdk.event.MessageEvent — the callback only
    relies on the class name, `.id`, `.source`, and `.model_dump_json()`."""

    def __init__(self, source: str = "agent", id: str | None = None) -> None:
        self.source = source
        self.id = id

    def model_dump_json(self) -> str:
        return json.dumps({"source": self.source})


def _trigger(conversation_id: str = "conv-1") -> TriggerMessage:
    return TriggerMessage(
        stream_id="1-1",
        trigger_type="chat_message",
        conversation_id=conversation_id,
        agent_id="agent-1",
        project_id="proj-1",
        task_id=None,
        comment_id=None,
        chat_session_id=None,
        message="hi",
        actor_member_id="member-1",
        repo_plugin_ids=[],
    )


def test_event_callback_persists_agent_reply(bg_loop, mock_persistence):
    """Regression coverage for https://github.com/Paca-AI/paca/issues/211 and
    #214: openhands-sdk's RemoteConversation never wires up token_callbacks
    (confirmed by reading openhands.sdk.conversation.impl.remote_conversation
    — the real constructor has no such parameter, it's swallowed by a
    catch-all **kwargs), so the event callback is the only path that ever
    persists the agent's reply."""
    trigger = _trigger()
    event_callback = _make_event_callback(trigger, bg_loop, _AtomicCounter(), _SeenEvents())

    event_callback(MessageEvent(source="agent"))

    mock_persistence.assert_awaited_once()
    kwargs = mock_persistence.call_args.kwargs
    assert kwargs["event_type"] == "MessageEvent"
    assert kwargs["event_source"] == "agent"
    assert kwargs["conversation_id"] == "conv-1"


def test_event_callback_skips_non_message_noise_events(bg_loop, mock_persistence):
    """Internal bookkeeping events must be skipped."""
    trigger = _trigger()
    event_callback = _make_event_callback(trigger, bg_loop, _AtomicCounter(), _SeenEvents())

    noise = type("ConversationStateUpdateEvent", (), {"source": "agent"})()
    event_callback(noise)

    mock_persistence.assert_not_awaited()


# ─── _make_reconciler ─────────────────────────────────────────────────────────


class _FakeEventsList:
    """Stand-in for RemoteEventsList — reconcile() + iteration only."""

    def __init__(self, events: list) -> None:
        self._events = events
        self.reconcile_calls = 0

    def reconcile(self) -> int:
        self.reconcile_calls += 1
        return 0

    def __iter__(self):
        return iter(self._events)


class _FakeConversation:
    def __init__(self, events: list) -> None:
        self.state = type("State", (), {"events": _FakeEventsList(events)})()


def test_reconciler_persists_events_missed_by_dead_ws_thread(bg_loop, mock_persistence):
    """Simulates the openhands-sdk WS-thread-died scenario: events the agent
    produced never reached the live callback (nothing called it), but they
    are visible over REST via events.reconcile(). The reconciler must push
    those through the normal persistence path exactly once."""
    trigger = _trigger()
    seen = _SeenEvents()
    reconcile = _make_reconciler(trigger, bg_loop, _AtomicCounter(), seen)

    conversation = _FakeConversation(
        [MessageEvent(source="agent", id="evt-1"), MessageEvent(source="agent", id="evt-2")]
    )

    reconcile(conversation)

    assert mock_persistence.await_count == 2
    persisted_ids = {call.kwargs["event_index"] for call in mock_persistence.call_args_list}
    assert len(persisted_ids) == 2  # each event got its own monotonic index


def test_reconciler_does_not_reprocess_events_already_seen(bg_loop, mock_persistence):
    """An event already delivered via the live WebSocket callback (and marked
    seen there) must not be persisted again just because reconcile() also
    surfaces it from the full REST history."""
    trigger = _trigger()
    seen = _SeenEvents()
    event_callback = _make_event_callback(trigger, bg_loop, _AtomicCounter(), seen)
    reconcile = _make_reconciler(trigger, bg_loop, _AtomicCounter(), seen)

    live_event = MessageEvent(source="agent", id="evt-live")
    event_callback(live_event)
    mock_persistence.assert_awaited_once()
    mock_persistence.reset_mock()

    conversation = _FakeConversation([live_event, MessageEvent(source="agent", id="evt-new")])
    reconcile(conversation)

    mock_persistence.assert_awaited_once()
    assert mock_persistence.call_args.kwargs["conversation_id"] == "conv-1"


def test_seen_events_seeded_with_initial_ids_skips_them():
    """A resumed chat turn seeds _SeenEvents from
    conversation_repository.get_seen_event_ids (ids persisted in earlier
    turns) — an id already in that seed must be rejected as not-new."""
    seen = _SeenEvents(initial_ids={"evt-old"})
    assert seen.mark_new("evt-old") is False
    assert seen.mark_new("evt-new") is True


def test_reconciler_does_not_reprocess_events_from_earlier_turns(bg_loop, mock_persistence):
    """Reproduces the reported bug: a resumed chat turn's reconcile() sees the
    *entire* remote SDK event history, including events already persisted in
    earlier turns (a fresh run_conversation() call per turn). Without seeding
    _SeenEvents from get_seen_event_ids, those events get re-persisted under
    new event_index values — i.e. every prior message duplicates on reply."""
    trigger = _trigger()
    seen = _SeenEvents(initial_ids={"evt-prior-1", "evt-prior-2"})
    reconcile = _make_reconciler(trigger, bg_loop, _AtomicCounter(), seen)

    conversation = _FakeConversation(
        [
            MessageEvent(source="user", id="evt-prior-1"),
            MessageEvent(source="agent", id="evt-prior-2"),
            MessageEvent(source="user", id="evt-new"),
        ]
    )

    reconcile(conversation)

    mock_persistence.assert_awaited_once()
    assert mock_persistence.call_args.kwargs["event_index"] == 0


# ─── Chat sandbox pause/resume/teardown ───────────────────────────────────────


@pytest.fixture(autouse=True)
def _clear_chat_sandboxes():
    """chat_sandboxes/stop_events are module-level dicts shared across tests —
    reset them before and after each test so state doesn't leak between them."""
    chat_sandboxes.clear()
    stop_events.clear()
    yield
    chat_sandboxes.clear()
    stop_events.clear()


def _fake_sandbox_state(conversation_id: str, project_id: str = "proj-1") -> ChatSandboxState:
    handle = object()  # only ever passed to a mocked stop_sandbox in these tests
    return ChatSandboxState(
        handle=handle,  # type: ignore[arg-type]
        sdk_conversation_id="sdk-conv-xyz",
        project_id=project_id,
    )


@pytest.fixture
def mock_teardown(monkeypatch):
    # stop_sandbox is synchronous in production — a plain Mock, not AsyncMock.
    # executor.py did `from .docker_workspace import stop_sandbox` — patch the
    # name bound in executor's own namespace, not the origin module's attribute.
    stop_mock = Mock()
    monkeypatch.setattr(executor, "stop_sandbox", stop_mock)
    monkeypatch.setattr(conversation_repository, "update_conversation_status", AsyncMock())
    monkeypatch.setattr(stream_store, "publish_realtime", AsyncMock())
    return stop_mock


async def test_teardown_paused_chat_sandbox_stops_and_removes_entry(mock_teardown):
    chat_sandboxes["conv-1"] = _fake_sandbox_state("conv-1")

    result = await teardown_paused_chat_sandbox("conv-1")

    assert result is True
    assert "conv-1" not in chat_sandboxes
    mock_teardown.assert_called_once()


async def test_teardown_paused_chat_sandbox_marks_stopped(mock_teardown):
    chat_sandboxes["conv-1"] = _fake_sandbox_state("conv-1")

    await teardown_paused_chat_sandbox("conv-1")

    conversation_repository.update_conversation_status.assert_awaited_once_with(
        "conv-1", "stopped"
    )


async def test_teardown_paused_chat_sandbox_publishes_realtime_event(mock_teardown):
    chat_sandboxes["conv-1"] = _fake_sandbox_state("conv-1", project_id="proj-42")

    await teardown_paused_chat_sandbox("conv-1")

    stream_store.publish_realtime.assert_awaited_once_with(
        project_id="proj-42",
        conversation_id="conv-1",
        event_type="agent.conversation.stopped",
    )


async def test_teardown_paused_chat_sandbox_returns_false_when_absent(mock_teardown):
    result = await teardown_paused_chat_sandbox("no-such-conversation")

    assert result is False
    mock_teardown.assert_not_called()


async def test_teardown_paused_chat_sandbox_skips_in_flight_conversation(mock_teardown):
    """A turn actively running for this conversation must not have its
    sandbox yanked by the idle reaper or a stray "agent.stop" fallback — only
    the turn itself (run_conversation) may retire this chat_sandboxes entry."""
    chat_sandboxes["conv-1"] = _fake_sandbox_state("conv-1")
    stop_events["conv-1"] = threading.Event()

    result = await teardown_paused_chat_sandbox("conv-1")

    assert result is False
    assert "conv-1" in chat_sandboxes
    mock_teardown.assert_not_called()
    conversation_repository.update_conversation_status.assert_not_awaited()


def test_find_idle_chat_sandboxes_selects_only_entries_past_timeout():
    chat_sandboxes["fresh"] = ChatSandboxState(
        handle=object(),  # type: ignore[arg-type]
        sdk_conversation_id="sdk-1",
        project_id="proj-1",
        last_active_at=1000.0,
    )
    chat_sandboxes["stale"] = ChatSandboxState(
        handle=object(),  # type: ignore[arg-type]
        sdk_conversation_id="sdk-2",
        project_id="proj-1",
        last_active_at=0.0,
    )

    idle = _find_idle_chat_sandboxes(now=1000.0, timeout_seconds=500.0)

    assert idle == ["stale"]


def test_find_idle_chat_sandboxes_empty_when_none_idle():
    chat_sandboxes["fresh"] = ChatSandboxState(
        handle=object(),  # type: ignore[arg-type]
        sdk_conversation_id="sdk-1",
        project_id="proj-1",
        last_active_at=999.0,
    )

    idle = _find_idle_chat_sandboxes(now=1000.0, timeout_seconds=500.0)

    assert idle == []


def test_find_idle_chat_sandboxes_excludes_in_flight_conversation():
    """last_active_at isn't updated while a turn is running (only at turn-end
    and by heartbeats), so a long-running resumed turn can look stale by
    timestamp alone — it must still be excluded while stop_events shows it's
    actually in flight."""
    chat_sandboxes["stale-but-running"] = ChatSandboxState(
        handle=object(),  # type: ignore[arg-type]
        sdk_conversation_id="sdk-1",
        project_id="proj-1",
        last_active_at=0.0,
    )
    stop_events["stale-but-running"] = threading.Event()

    idle = _find_idle_chat_sandboxes(now=1000.0, timeout_seconds=500.0)

    assert idle == []


# ─── _keep_sandbox_alive / _post_turn_status ──────────────────────────────────


@pytest.mark.parametrize(
    "is_chat,errored,shutdown,expected",
    [
        (True, False, False, True),  # natural finish or interrupt-only stop
        (True, True, False, False),  # errored
        (True, False, True, False),  # real shutdown
        (False, False, False, False),  # non-chat trigger never pauses
    ],
)
def test_keep_sandbox_alive(is_chat, errored, shutdown, expected):
    assert _keep_sandbox_alive(is_chat, errored, shutdown) is expected


def test_post_turn_status_shutdown_returns_none():
    """A real shutdown's status is already written synchronously by Go — Python
    must not overwrite it."""
    assert _post_turn_status(is_chat=True, stopped=True, errored=False, shutdown=True) is None


def test_post_turn_status_errored():
    assert _post_turn_status(is_chat=True, stopped=False, errored=True, shutdown=False) == (
        "failed",
        "agent.conversation.failed",
    )


def test_post_turn_status_interrupt_only_stop_on_chat_pauses():
    """The assistant-UI stop button interrupts a chat turn — the sandbox stays
    alive, so the conversation goes back to "paused", not "stopped"."""
    assert _post_turn_status(is_chat=True, stopped=True, errored=False, shutdown=False) == (
        "paused",
        "agent.conversation.paused",
    )


def test_post_turn_status_interrupt_only_stop_on_non_chat_stops():
    """Non-chat triggers have no pause-between-turns concept — their sandbox
    was already torn down in _run_sync, so status reflects that."""
    assert _post_turn_status(is_chat=False, stopped=True, errored=False, shutdown=False) == (
        "stopped",
        "agent.conversation.stopped",
    )


def test_post_turn_status_natural_finish_chat_pauses():
    assert _post_turn_status(is_chat=True, stopped=False, errored=False, shutdown=False) == (
        "paused",
        "agent.conversation.paused",
    )


def test_post_turn_status_natural_finish_non_chat_finishes():
    assert _post_turn_status(is_chat=False, stopped=False, errored=False, shutdown=False) == (
        "finished",
        "agent.conversation.finished",
    )


# ─── _wait_for_done_or_stop ────────────────────────────────────────────────────


class _FakeConversationForWait:
    def __init__(self) -> None:
        self.interrupt = Mock()


def test_wait_for_done_or_stop_pause_event_interrupts_without_shutdown():
    """The assistant-UI stop button sets pause_event only — the wait loop must
    still call conversation.interrupt() but report shutdown=False."""
    conversation = _FakeConversationForWait()
    stop_event = threading.Event()
    pause_event = threading.Event()
    pause_event.set()
    reconcile = Mock()

    stopped, errored, shutdown = _wait_for_done_or_stop(
        conversation, stop_event, pause_event, reconcile
    )

    assert (stopped, errored, shutdown) == (True, False, False)
    conversation.interrupt.assert_called_once()
    reconcile.assert_called_once_with(conversation)


def test_wait_for_done_or_stop_stop_event_signals_shutdown():
    """A real shutdown sets stop_event — the wait loop reports shutdown=True."""
    conversation = _FakeConversationForWait()
    stop_event = threading.Event()
    pause_event = threading.Event()
    stop_event.set()
    reconcile = Mock()

    stopped, errored, shutdown = _wait_for_done_or_stop(
        conversation, stop_event, pause_event, reconcile
    )

    assert (stopped, errored, shutdown) == (True, False, True)
    conversation.interrupt.assert_called_once()
