"""Tests for dispatch_acp_trigger's online/offline branches (src/agent/acp_dispatch.py)."""

from unittest.mock import AsyncMock, MagicMock

import src.agent.acp_dispatch as acp_dispatch
from src.agent.prompt import build_acp_message
from src.core.streams import TriggerMessage
from src.models.agent import AgentConfig
from src.models.conversation_status import ConversationStatus


def _trigger() -> TriggerMessage:
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


def _acp_config() -> AgentConfig:
    return AgentConfig(
        agent_id="agent-1",
        project_id="proj-1",
        system_prompt=None,
        llm_provider="",
        llm_model="",
        llm_api_key_secret_ref="",
        llm_base_url="",
        max_iterations=500,
        agent_type="acp",
        acp_provider="claude-code",
        acp_command=[],
    )


async def test_offline_bridge_fails_conversation(monkeypatch):
    monkeypatch.setattr(acp_dispatch.acp_bridge, "is_online", AsyncMock(return_value=False))
    dispatch_mock = AsyncMock()
    monkeypatch.setattr(acp_dispatch.acp_bridge, "dispatch", dispatch_mock)
    update_status = AsyncMock()
    monkeypatch.setattr(
        acp_dispatch.conversation_repository, "update_conversation_status", update_status
    )
    publish_realtime = AsyncMock()
    monkeypatch.setattr(acp_dispatch.stream_store, "publish_realtime", publish_realtime)

    await acp_dispatch.dispatch_acp_trigger(_trigger(), _acp_config())

    dispatch_mock.assert_not_called()
    update_status.assert_awaited_once_with(
        "conv-1", ConversationStatus.FAILED, error_message=acp_dispatch._OFFLINE_MESSAGE
    )
    publish_realtime.assert_awaited_once()
    assert publish_realtime.await_args.kwargs["event_type"] == "agent.conversation.failed"


async def test_online_bridge_dispatches_start_turn(monkeypatch):
    monkeypatch.setattr(acp_dispatch.acp_bridge, "is_online", AsyncMock(return_value=True))
    dispatch_mock = AsyncMock(return_value=True)
    monkeypatch.setattr(acp_dispatch.acp_bridge, "dispatch", dispatch_mock)
    update_status = AsyncMock()
    monkeypatch.setattr(
        acp_dispatch.conversation_repository, "update_conversation_status", update_status
    )
    schedule_watchdog = MagicMock()
    monkeypatch.setattr(acp_dispatch, "_schedule_watchdog", schedule_watchdog)

    trigger = _trigger()
    config = _acp_config()
    await acp_dispatch.dispatch_acp_trigger(trigger, config)

    update_status.assert_awaited_once_with("conv-1", ConversationStatus.RUNNING)
    dispatch_mock.assert_awaited_once_with(
        "agent-1",
        {
            "type": "start_turn",
            "conversation_id": "conv-1",
            "project_id": "proj-1",
            "message": build_acp_message(trigger),
            "trigger_type": "chat_message",
            "acp_provider": "claude-code",
            "acp_command": [],
        },
    )
    schedule_watchdog.assert_called_once_with("conv-1", "proj-1", 30, trigger.actor_user_id)


async def test_bridge_goes_offline_mid_dispatch_still_fails(monkeypatch):
    """is_online() passes but dispatch() itself returns False (race: daemon
    disconnected between the check and the publish) — must still fail the
    conversation rather than leaving it stuck at RUNNING forever."""
    monkeypatch.setattr(acp_dispatch.acp_bridge, "is_online", AsyncMock(return_value=True))
    monkeypatch.setattr(acp_dispatch.acp_bridge, "dispatch", AsyncMock(return_value=False))
    update_status = AsyncMock()
    monkeypatch.setattr(
        acp_dispatch.conversation_repository, "update_conversation_status", update_status
    )
    publish_realtime = AsyncMock()
    monkeypatch.setattr(acp_dispatch.stream_store, "publish_realtime", publish_realtime)

    await acp_dispatch.dispatch_acp_trigger(_trigger(), _acp_config())

    assert update_status.await_args_list[-1].args == (
        "conv-1",
        ConversationStatus.FAILED,
    )
    assert (
        update_status.await_args_list[-1].kwargs["error_message"] == acp_dispatch._OFFLINE_MESSAGE
    )
    publish_realtime.assert_awaited_once()


async def test_watchdog_fails_conversation_still_running_after_timeout(monkeypatch):
    sleep_mock = AsyncMock()
    monkeypatch.setattr(acp_dispatch.asyncio, "sleep", sleep_mock)
    fail_if_not_terminal = AsyncMock(return_value=True)
    monkeypatch.setattr(
        acp_dispatch.conversation_repository, "fail_if_not_terminal", fail_if_not_terminal
    )
    publish_realtime = AsyncMock()
    monkeypatch.setattr(acp_dispatch.stream_store, "publish_realtime", publish_realtime)

    await acp_dispatch._watchdog("conv-1", "proj-1", 30)

    sleep_mock.assert_awaited_once_with(30 * 60)
    fail_if_not_terminal.assert_awaited_once_with("conv-1", acp_dispatch._TIMEOUT_MESSAGE)
    publish_realtime.assert_awaited_once()
    assert publish_realtime.await_args.kwargs["event_type"] == "agent.conversation.failed"


async def test_watchdog_noop_when_conversation_already_terminal(monkeypatch):
    """fail_if_not_terminal returns False when a turn_status already landed
    (or the conversation was stopped) before the watchdog woke up — the
    watchdog must not publish a spurious failed event in that case."""
    monkeypatch.setattr(acp_dispatch.asyncio, "sleep", AsyncMock())
    fail_if_not_terminal = AsyncMock(return_value=False)
    monkeypatch.setattr(
        acp_dispatch.conversation_repository, "fail_if_not_terminal", fail_if_not_terminal
    )
    publish_realtime = AsyncMock()
    monkeypatch.setattr(acp_dispatch.stream_store, "publish_realtime", publish_realtime)

    await acp_dispatch._watchdog("conv-1", "proj-1", 30)

    publish_realtime.assert_not_called()
