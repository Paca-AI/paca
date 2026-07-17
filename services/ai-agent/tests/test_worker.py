"""Tests for worker._handle_control's stop/pause/heartbeat dispatch."""

import threading
from unittest.mock import AsyncMock

import pytest

import src.worker as worker
from src.core.registry import ChatSandboxState, chat_sandboxes, pause_events, stop_events
from src.core.streams import ControlMessage


@pytest.fixture(autouse=True)
def _clear_registry():
    """All three registries are module-level dicts shared across tests."""
    stop_events.clear()
    pause_events.clear()
    chat_sandboxes.clear()
    yield
    stop_events.clear()
    pause_events.clear()
    chat_sandboxes.clear()


def _control(control_type: str, conversation_id: str = "conv-1", project_id: str = "proj-1"):
    return ControlMessage(
        stream_id="1-1",
        control_type=control_type,
        conversation_id=conversation_id,
        project_id=project_id,
    )


def _fake_sandbox_state(project_id: str = "proj-1") -> ChatSandboxState:
    return ChatSandboxState(
        handle=object(),  # type: ignore[arg-type]
        sdk_conversation_id="sdk-conv-xyz",
        project_id=project_id,
        last_active_at=0.0,
    )


# ─── agent.stop (full teardown, unchanged from before) ─────────────────────────


async def test_stop_sets_stop_event_when_run_in_flight(monkeypatch):
    teardown_mock = AsyncMock(return_value=True)
    monkeypatch.setattr(worker, "teardown_paused_chat_sandbox", teardown_mock)
    stop_event = threading.Event()
    stop_events["conv-1"] = stop_event

    await worker._handle_control(_control("agent.stop"))

    assert stop_event.is_set()
    teardown_mock.assert_not_called()


async def test_stop_tears_down_paused_sandbox_when_not_in_flight(monkeypatch):
    teardown_mock = AsyncMock(return_value=True)
    monkeypatch.setattr(worker, "teardown_paused_chat_sandbox", teardown_mock)
    monkeypatch.setattr(
        worker.conversation_repository,
        "get_conversation_agent_type",
        AsyncMock(return_value=None),
    )

    await worker._handle_control(_control("agent.stop"))

    teardown_mock.assert_awaited_once_with("conv-1")


# ─── agent.pause (interrupt-only) ──────────────────────────────────────────────


async def test_pause_sets_pause_event_when_run_in_flight():
    pause_event = threading.Event()
    pause_events["conv-1"] = pause_event

    await worker._handle_control(_control("agent.pause"))

    assert pause_event.is_set()


async def test_pause_does_not_fall_through_to_teardown_when_not_in_flight(monkeypatch):
    """The lightweight pause must be a no-op when nothing is running — unlike
    the old overloaded stop behavior, it must never tear down a paused
    sandbox."""
    # worker.py does `from .agent.executor import ... teardown_paused_chat_sandbox`,
    # so the name to patch is the one bound in worker's own namespace.
    teardown_mock = AsyncMock(return_value=True)
    monkeypatch.setattr(worker, "teardown_paused_chat_sandbox", teardown_mock)
    monkeypatch.setattr(
        worker.conversation_repository,
        "get_conversation_agent_type",
        AsyncMock(return_value=None),
    )
    chat_sandboxes["conv-1"] = _fake_sandbox_state()

    await worker._handle_control(_control("agent.pause"))

    teardown_mock.assert_not_called()
    assert "conv-1" in chat_sandboxes


# ─── ACP-agent stop/pause forwarding ───────────────────────────────────────────


async def test_stop_forwards_to_acp_bridge_when_agent_is_acp(monkeypatch):
    teardown_mock = AsyncMock(return_value=True)
    monkeypatch.setattr(worker, "teardown_paused_chat_sandbox", teardown_mock)
    monkeypatch.setattr(
        worker.conversation_repository,
        "get_conversation_agent_type",
        AsyncMock(return_value=("agent-1", "acp")),
    )
    dispatch_mock = AsyncMock(return_value=True)
    monkeypatch.setattr(worker.acp_bridge, "dispatch", dispatch_mock)

    await worker._handle_control(_control("agent.stop"))

    dispatch_mock.assert_awaited_once_with(
        "agent-1", {"type": "stop_turn", "conversation_id": "conv-1"}
    )
    teardown_mock.assert_not_called()


async def test_pause_forwards_to_acp_bridge_when_agent_is_acp(monkeypatch):
    monkeypatch.setattr(
        worker.conversation_repository,
        "get_conversation_agent_type",
        AsyncMock(return_value=("agent-1", "acp")),
    )
    dispatch_mock = AsyncMock(return_value=True)
    monkeypatch.setattr(worker.acp_bridge, "dispatch", dispatch_mock)

    await worker._handle_control(_control("agent.pause"))

    dispatch_mock.assert_awaited_once_with(
        "agent-1", {"type": "pause_turn", "conversation_id": "conv-1"}
    )


# ─── agent.heartbeat ────────────────────────────────────────────────────────────


async def test_heartbeat_refreshes_last_active_at():
    chat_sandboxes["conv-1"] = _fake_sandbox_state(project_id="proj-1")

    await worker._handle_control(_control("agent.heartbeat", project_id="proj-1"))

    assert chat_sandboxes["conv-1"].last_active_at > 0.0


async def test_heartbeat_ignored_when_project_id_mismatches():
    chat_sandboxes["conv-1"] = _fake_sandbox_state(project_id="proj-1")

    await worker._handle_control(_control("agent.heartbeat", project_id="proj-OTHER"))

    assert chat_sandboxes["conv-1"].last_active_at == 0.0


async def test_heartbeat_no_op_when_sandbox_absent():
    # Should not raise even though no sandbox is registered for this replica.
    await worker._handle_control(_control("agent.heartbeat"))


# ─── _process_trigger agent_type branching ─────────────────────────────────────


async def test_process_trigger_dispatches_acp_agents_to_bridge(monkeypatch):
    from src.models.agent import AgentConfig

    acp_config = AgentConfig(
        agent_id="agent-1",
        project_id="proj-1",
        system_prompt=None,
        llm_provider="",
        llm_model="",
        llm_api_key_secret_ref="",
        llm_base_url="",
        max_iterations=500,
        agent_type="acp",
    )
    monkeypatch.setattr(worker, "load_agent_config", AsyncMock(return_value=acp_config))
    dispatch_acp_mock = AsyncMock()
    monkeypatch.setattr(worker, "dispatch_acp_trigger", dispatch_acp_mock)
    run_conversation_mock = AsyncMock()
    monkeypatch.setattr(worker, "run_conversation", run_conversation_mock)

    trigger = _trigger_message()
    await worker._process_trigger(trigger)

    dispatch_acp_mock.assert_awaited_once_with(trigger, acp_config)
    run_conversation_mock.assert_not_called()


async def test_process_trigger_runs_llm_agents_as_before(monkeypatch):
    from src.models.agent import AgentConfig

    llm_config = AgentConfig(
        agent_id="agent-1",
        project_id="proj-1",
        system_prompt=None,
        llm_provider="anthropic",
        llm_model="claude-sonnet-4-6",
        llm_api_key_secret_ref="key",
        llm_base_url="",
        max_iterations=500,
    )
    monkeypatch.setattr(worker, "load_agent_config", AsyncMock(return_value=llm_config))
    dispatch_acp_mock = AsyncMock()
    monkeypatch.setattr(worker, "dispatch_acp_trigger", dispatch_acp_mock)
    run_conversation_mock = AsyncMock()
    monkeypatch.setattr(worker, "run_conversation", run_conversation_mock)

    trigger = _trigger_message()
    await worker._process_trigger(trigger)

    run_conversation_mock.assert_awaited_once_with(trigger, llm_config)
    dispatch_acp_mock.assert_not_called()


def _trigger_message():
    from src.core.streams import TriggerMessage

    return TriggerMessage(
        stream_id="1-1",
        trigger_type="chat_message",
        conversation_id="conv-1",
        agent_id="agent-1",
        project_id="proj-1",
        task_id=None,
        comment_id=None,
        chat_session_id="sess-1",
        message="hello",
        actor_member_id="member-1",
        repo_plugin_ids=[],
    )


# ─── unknown control type ──────────────────────────────────────────────────────


async def test_unknown_control_type_logs_and_does_not_raise(caplog):
    await worker._handle_control(_control("agent.something_else"))
    assert "Unknown control type" in caplog.text
