"""Tests for command resolution and message-driven behavior of ConversationRunner.

Exercises the daemon's own logic without spawning a real ACP CLI subprocess —
`start_turn`/`interrupt` are tested against a fake conversation object rather
than a real `openhands.sdk.Conversation`.
"""

import pytest

from paca_acp_bridge.runner import ConversationRunner, resolve_acp_command


def test_resolve_builtin_provider_uses_sdk_default_command():
    command = resolve_acp_command("claude-code", [])
    assert command[:2] == ["npx", "-y"]
    assert "claude-agent-acp" in command[2]


def test_resolve_custom_provider_uses_explicit_command():
    command = resolve_acp_command("custom", ["my-server", "--flag"])
    assert command == ["my-server", "--flag"]


def test_resolve_custom_provider_without_command_raises():
    with pytest.raises(ValueError):
        resolve_acp_command("custom", [])


def test_resolve_unknown_provider_falls_back_to_explicit_command():
    command = resolve_acp_command("not-a-real-provider", ["fallback-cmd"])
    assert command == ["fallback-cmd"]


async def test_interrupt_calls_conversation_interrupt_for_known_conversation():
    sent = []

    async def send(message):
        sent.append(message)

    runner = ConversationRunner(workspace="/tmp", send=send)

    class _FakeConversation:
        def __init__(self):
            self.interrupted = False

        def interrupt(self):
            self.interrupted = True

    fake_conv = _FakeConversation()
    from paca_acp_bridge.runner import _ConversationHandle

    runner._conversations["conv-1"] = _ConversationHandle(
        conversation=fake_conv,
        thread=None,  # type: ignore[arg-type]
    )

    runner.interrupt("conv-1")

    assert fake_conv.interrupted is True


async def test_interrupt_is_a_no_op_for_unknown_conversation():
    async def send(message):
        pass

    runner = ConversationRunner(workspace="/tmp", send=send)

    # Should not raise even though "conv-missing" was never started.
    runner.interrupt("conv-missing")
    runner.interrupt(None)


async def test_start_turn_reports_failure_for_unresolvable_custom_provider():
    sent = []

    async def send(message):
        sent.append(message)

    runner = ConversationRunner(workspace="/tmp", send=send)

    await runner.start_turn(
        {
            "conversation_id": "conv-1",
            "project_id": "proj-1",
            "message": "hi",
            "acp_provider": "custom",
            "acp_command": [],
        }
    )

    assert len(sent) == 1
    assert sent[0]["type"] == "turn_status"
    assert sent[0]["status"] == "failed"
    assert "conv-1" not in runner._conversations


async def test_start_turn_rejects_resume_while_previous_turn_still_running():
    sent = []

    async def send(message):
        sent.append(message)

    runner = ConversationRunner(workspace="/tmp", send=send)

    class _FakeConversation:
        pass

    class _FakeAliveThread:
        def is_alive(self):
            return True

    from paca_acp_bridge.runner import _ConversationHandle

    fake_conv = _FakeConversation()
    runner._conversations["conv-1"] = _ConversationHandle(
        conversation=fake_conv,
        thread=_FakeAliveThread(),  # type: ignore[arg-type]
    )

    await runner.start_turn(
        {
            "conversation_id": "conv-1",
            "project_id": "proj-1",
            "message": "a follow-up message",
        }
    )

    assert len(sent) == 1
    assert sent[0]["type"] == "turn_status"
    assert sent[0]["status"] == "failed"
    # The still-running conversation's handle must be left untouched — no
    # second thread should have been started against it.
    assert runner._conversations["conv-1"].conversation is fake_conv
