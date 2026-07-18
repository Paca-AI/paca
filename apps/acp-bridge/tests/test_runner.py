"""Tests for command resolution and message-driven behavior of ConversationRunner.

Exercises the daemon's own logic without spawning a real ACP CLI subprocess —
`start_turn`/`interrupt` are tested against a fake conversation object rather
than a real `openhands.sdk.Conversation`.
"""

import asyncio
import threading

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

    completed_task = asyncio.get_running_loop().create_future()
    completed_task.set_result(None)
    runner._conversations["conv-1"] = _ConversationHandle(
        conversation=fake_conv,
        task=completed_task,  # type: ignore[arg-type]
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

    class _FakeRunningTask:
        def done(self):
            return False

    from paca_acp_bridge.runner import _ConversationHandle

    fake_conv = _FakeConversation()
    runner._conversations["conv-1"] = _ConversationHandle(
        conversation=fake_conv,
        task=_FakeRunningTask(),  # type: ignore[arg-type]
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
    # second task should have been started against it.
    assert runner._conversations["conv-1"].conversation is fake_conv


async def test_start_turn_resume_drives_conversation_via_arun_and_reports_finished():
    """Locks in the arun()-based (not run()-on-a-thread) execution path:
    conversation.interrupt() can only cancel a turn immediately if the turn
    is actually tracked as an asyncio Task via arun() — see runner.py's
    module docstring for why the old thread+run() model couldn't do this."""
    sent = []

    async def send(message):
        sent.append(message)

    runner = ConversationRunner(workspace="/tmp", send=send)

    class _FakeConversation:
        def __init__(self):
            self.sent_messages: list[str] = []
            self.ran = False

        def send_message(self, message):
            self.sent_messages.append(message)

        async def arun(self):
            self.ran = True

    fake_conv = _FakeConversation()
    from paca_acp_bridge.runner import _ConversationHandle

    done_task = asyncio.get_running_loop().create_future()
    done_task.set_result(None)
    runner._conversations["conv-1"] = _ConversationHandle(
        conversation=fake_conv,
        task=done_task,  # type: ignore[arg-type]
    )

    await runner.start_turn(
        {"conversation_id": "conv-1", "project_id": "proj-1", "message": "a follow-up message"}
    )
    await runner._conversations["conv-1"].task

    assert fake_conv.sent_messages == ["a follow-up message"]
    assert fake_conv.ran is True
    assert sent == [
        {
            "type": "turn_status",
            "conversation_id": "conv-1",
            "project_id": "proj-1",
            "status": "finished",
        }
    ]


class _FakeEvent:
    def model_dump_json(self):
        return "{}"


async def test_event_callback_schedules_without_blocking_when_called_on_loop():
    """arun()'s own on_event calls (finalizing a turn, emitting InterruptEvent
    on cancellation) happen synchronously on this daemon's own event-loop
    thread. Blocking there with run_coroutine_threadsafe(...).result() would
    make the loop wait on a coroutine it can only run once this call
    returns — a guaranteed 10s self-deadlock. Calling the callback directly
    (as arun() does) must return immediately instead."""
    sent = []

    async def send(message):
        sent.append(message)

    runner = ConversationRunner(workspace="/tmp", send=send)
    runner._loop = asyncio.get_running_loop()
    callback = runner._make_event_callback("conv-1", "proj-1")

    callback(_FakeEvent())  # must not block waiting for _send to run

    await asyncio.sleep(0)  # let the scheduled task actually run

    assert len(sent) == 1
    assert sent[0]["conversation_id"] == "conv-1"
    assert sent[0]["event_type"] == "_FakeEvent"


async def test_event_callback_uses_threadsafe_dispatch_when_called_off_loop():
    """ACPAgent streams some mid-turn updates from its own background
    ("portal") thread — a genuinely different thread than this daemon's
    loop — where the blocking run_coroutine_threadsafe(...).result() dispatch
    is still correct and necessary."""
    sent = []

    async def send(message):
        sent.append(message)

    runner = ConversationRunner(workspace="/tmp", send=send)
    runner._loop = asyncio.get_running_loop()
    callback = runner._make_event_callback("conv-1", "proj-1")

    thread = threading.Thread(target=callback, args=(_FakeEvent(),))
    thread.start()
    for _ in range(200):
        if not thread.is_alive():
            break
        await asyncio.sleep(0.01)

    assert not thread.is_alive()
    assert len(sent) == 1
    assert sent[0]["conversation_id"] == "conv-1"
