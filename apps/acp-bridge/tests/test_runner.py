"""Tests for command resolution and message-driven behavior of ConversationRunner.

Exercises the daemon's own logic without spawning a real ACP CLI subprocess —
`start_turn`/`interrupt` are tested against a fake conversation object rather
than a real `openhands.sdk.Conversation`.
"""

import asyncio
import json
import logging
import threading

import pytest
from pydantic import BaseModel, ConfigDict

from paca_acp_bridge.runner import (
    ConversationRunner,
    _AssistantTextRelay,
    _chunk_text,
    resolve_acp_command,
)


def test_resolve_builtin_provider_uses_sdk_default_command():
    command = resolve_acp_command("claude-code", [])
    assert command[:2] == ["npx", "-y"]
    assert "claude-agent-acp" in command[2]


def test_resolve_goose_provider_uses_local_default_command():
    """goose isn't in the OpenHands SDK's own acp_providers registry (only
    claude-code/codex/gemini-cli are — see docs/ai-agent/goose-migration.md),
    so this is resolved via a local override instead of get_acp_provider."""
    assert resolve_acp_command("goose", []) == ["goose", "acp"]


def test_resolve_goose_provider_ignores_any_explicit_command():
    """Matches the built-in-provider precedent (claude-code, etc. above):
    a fixed default always wins over acp_command for a named provider —
    only "custom" ever consults it."""
    assert resolve_acp_command("goose", ["should-be-ignored"]) == ["goose", "acp"]


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


async def test_run_conversation_survives_status_report_failure_after_success():
    """A failure in the outbound _send/report path (e.g. the bridge's outbox
    raising during shutdown) must not be conflated with the conversation
    itself failing — see _report_status_safely in runner.py. Locks in that
    a successful arun() only ever attempts one status report, even if that
    report itself blows up."""
    calls = []

    async def send(message):
        calls.append(message)
        raise RuntimeError("outbox closed")

    runner = ConversationRunner(workspace="/tmp", send=send)

    class _FakeConversation:
        def send_message(self, message):
            pass

        async def arun(self):
            pass

    # Must not raise, even though `send` always raises.
    await runner._run_conversation(_FakeConversation(), "conv-1", "proj-1", "hi")

    assert len(calls) == 1
    assert calls[0]["status"] == "finished"


async def test_event_callback_logs_when_send_fails_on_loop(caplog):
    """The same-loop dispatch branch schedules _send as a fire-and-forget
    task; a failure there must still surface as a logged warning instead of
    silently becoming an unretrieved-task-exception."""

    async def send(message):
        raise RuntimeError("boom")

    runner = ConversationRunner(workspace="/tmp", send=send)
    runner._loop = asyncio.get_running_loop()
    callback = runner._make_event_callback("conv-1", "proj-1")

    with caplog.at_level(logging.WARNING, logger="paca_acp_bridge.runner"):
        callback(_FakeEvent())
        # Two yields: one for the scheduled _send task to run and raise, a
        # second for its add_done_callback (scheduled once the task becomes
        # done) to actually fire.
        await asyncio.sleep(0)
        await asyncio.sleep(0)

    assert any("Failed to report event" in record.getMessage() for record in caplog.records)


class _FakeFinishAction(BaseModel):
    """Frozen like the SDK's FinishAction, so clearing the message in place is
    impossible and the copy path is the one under test."""

    model_config = ConfigDict(frozen=True)

    message: str


class _FakeFinishEvent(BaseModel):
    """Stands in for the ActionEvent that closes an ACP turn: a FinishAction
    whose message is the whole turn's assistant text, joined by the SDK.

    A real (frozen) model rather than a hand-rolled stub, so the payload is
    produced by Pydantic's serializer exactly as the SDK's own events are.
    """

    model_config = ConfigDict(frozen=True)

    action: _FakeFinishAction
    tool_name: str = "finish"
    reasoning_content: str | None = None
    source: str = "agent"

    def __init__(self, message: str, **kwargs) -> None:
        super().__init__(action=_FakeFinishAction(message=message), **kwargs)


class _FakeFinishObservation(BaseModel):
    """Stands in for the SDK's FinishObservation: content is a list of
    TextContent-like blocks, and `.text` joins them — same shape `Observation`
    itself uses, so `_blank_duplicate_finish_text` can be exercised as written
    (it reads `.text`, not `.content`, and clears via `.content` on a copy)."""

    model_config = ConfigDict(frozen=True)

    content: list[dict[str, str]]

    def __init__(self, text: str, **kwargs) -> None:
        super().__init__(content=[{"type": "text", "text": text}], **kwargs)

    @property
    def text(self) -> str:
        return "".join(item["text"] for item in self.content)


class _FakeFinishObservationEvent(BaseModel):
    """Stands in for the ObservationEvent that always follows a finish
    ActionEvent — the SDK pairs every action with an observation, and for
    "finish" both carry the identical joined text (FinishObservation.from_text
    is built from the same `response_text` as the FinishAction message)."""

    model_config = ConfigDict(frozen=True)

    observation: _FakeFinishObservation
    tool_name: str = "finish"
    source: str = "environment"

    def __init__(self, text: str, **kwargs) -> None:
        super().__init__(observation=_FakeFinishObservation(text), **kwargs)


async def _drain():
    # The on-loop dispatch path schedules tasks rather than awaiting them.
    for _ in range(5):
        await asyncio.sleep(0)


def _runner_with_relay(send):
    runner = ConversationRunner(workspace="/tmp", send=send)
    runner._loop = asyncio.get_running_loop()
    relay = _AssistantTextRelay()
    runner._text_relays["conv-1"] = relay
    return runner, relay


def test_chunk_text_accepts_a_plain_string_and_a_stream_chunk():
    """ACPAgent calls token callbacks with a str; the SDK's TokenCallbackType
    alias says litellm ModelResponseStream. Both have to work."""

    class _Delta:
        content = "from a stream chunk"

    class _Choice:
        delta = _Delta()

    class _Chunk:
        choices = [_Choice()]

    assert _chunk_text("from a string") == "from a string"
    assert _chunk_text(_Chunk()) == "from a stream chunk"
    assert _chunk_text(object()) == ""


async def test_streamed_text_is_emitted_before_the_event_that_follows_it():
    """The whole point of the relay: narration reaches Paca positioned against
    the tool call it describes, rather than in one lump at the end of the turn."""
    sent = []

    async def send(message):
        sent.append(message)

    runner, relay = _runner_with_relay(send)
    callback = runner._make_event_callback("conv-1", "proj-1")

    relay.on_token("Checking the failing test")
    relay.on_token(" first.")
    callback(_FakeEvent())
    await _drain()

    assert [m["event_type"] for m in sent] == ["MessageEvent", "_FakeEvent"]
    assert sent[0]["event_source"] == "agent"
    assert json.loads(sent[0]["payload"]) == {"content": "Checking the failing test first."}


async def test_nothing_extra_is_emitted_when_no_text_was_streamed():
    sent = []

    async def send(message):
        sent.append(message)

    runner, _relay = _runner_with_relay(send)
    callback = runner._make_event_callback("conv-1", "proj-1")

    callback(_FakeEvent())
    await _drain()

    assert [m["event_type"] for m in sent] == ["_FakeEvent"]


async def test_finish_message_is_blanked_once_its_text_has_been_streamed():
    """The SDK builds the FinishAction message by joining the same chunks, so
    forwarding it verbatim would repeat the whole turn's narration."""
    sent = []

    async def send(message):
        sent.append(message)

    runner, relay = _runner_with_relay(send)
    callback = runner._make_event_callback("conv-1", "proj-1")

    relay.on_token("First I read the file.")
    callback(_FakeEvent())
    relay.on_token(" Then I fixed it.")
    callback(_FakeFinishEvent("First I read the file. Then I fixed it."))
    await _drain()

    assert [m["event_type"] for m in sent] == [
        "MessageEvent",
        "_FakeEvent",
        "MessageEvent",
        "_FakeFinishEvent",
    ]
    assert json.loads(sent[3]["payload"])["action"]["message"] == ""
    # The text itself survives — split across the two MessageEvents.
    streamed = "".join(json.loads(sent[i]["payload"])["content"] for i in (0, 2))
    assert streamed == "First I read the file. Then I fixed it."


async def test_finish_message_is_kept_when_it_differs_from_the_streamed_text():
    """Only an exact duplicate is dropped. The SDK masks the joined text a
    second time, which can catch a secret split across two chunks; if that
    makes the two differ, the message must still be shown."""
    sent = []

    async def send(message):
        sent.append(message)

    runner, relay = _runner_with_relay(send)
    callback = runner._make_event_callback("conv-1", "proj-1")

    relay.on_token("streamed text")
    callback(_FakeFinishEvent("a differently masked version"))
    await _drain()

    assert json.loads(sent[1]["payload"])["action"]["message"] == ("a differently masked version")


async def test_segments_are_re_masked_before_they_are_emitted():
    """Chunks arrive individually masked, so a secret spanning two of them is
    only matchable once joined — the same reason the SDK re-masks at its own
    persistence boundary."""
    sent = []

    async def send(message):
        sent.append(message)

    class _Registry:
        @staticmethod
        def mask_secrets_in_output(text):
            return text.replace("sk-secret", "<masked>")

    class _State:
        secret_registry = _Registry()

    class _Conversation:
        state = _State()

    runner, relay = _runner_with_relay(send)
    relay.bind_masker(_Conversation())
    callback = runner._make_event_callback("conv-1", "proj-1")

    relay.on_token("token is sk-")
    relay.on_token("secret now")
    callback(_FakeEvent())
    await _drain()

    assert json.loads(sent[0]["payload"]) == {"content": "token is <masked> now"}


def test_bind_masker_tolerates_a_conversation_without_a_registry():
    relay = _AssistantTextRelay()
    relay.bind_masker(object())
    relay.on_token("unmasked but still forwarded")
    assert relay.take_pending() == "unmasked but still forwarded"


def test_start_turn_drops_state_from_the_previous_turn():
    relay = _AssistantTextRelay()
    relay.on_token("text from the previous turn")
    assert relay.take_pending() is not None

    relay.start_turn()
    assert relay.take_pending() is None
    # The previous turn's text must no longer suppress this turn's finish message.
    assert not relay.already_emitted("text from the previous turn")


async def test_blanking_the_finish_message_leaves_the_rest_of_the_payload_byte_intact():
    """The payload is serialized once, by Pydantic. Parsing it and re-encoding
    with json.dumps would escape non-ASCII elsewhere in the event and re-space
    the JSON; both models are frozen, so the message is cleared on a copy."""
    sent = []

    async def send(message):
        sent.append(message)

    runner, relay = _runner_with_relay(send)
    callback = runner._make_event_callback("conv-1", "proj-1")

    event = _FakeFinishEvent("done ✅", reasoning_content="café ✅ déjà vu")
    untouched = event.model_dump_json()

    relay.on_token("done ✅")
    callback(event)
    await _drain()

    payload = sent[-1]["payload"]
    # Non-ASCII stays as written, exactly as Pydantic emits it.
    assert "café ✅ déjà vu" in payload
    assert "\\u00e9" not in payload
    assert json.loads(payload)["action"]["message"] == ""
    # The SDK keeps this event in conversation history — it must not be mutated.
    assert event.action.message == "done ✅"
    assert event.model_dump_json() == untouched


async def test_a_masking_failure_drops_the_segment_and_keeps_the_finish_message():
    """Fail closed: unmaskable text is never emitted. Dropping the segment also
    leaves the FinishAction message un-blanked, so the text still reaches the
    conversation at the end of the turn, masked by the SDK."""
    sent = []

    async def send(message):
        sent.append(message)

    class _Registry:
        @staticmethod
        def mask_secrets_in_output(_text):
            raise RuntimeError("secret registry unavailable")

    class _State:
        secret_registry = _Registry()

    class _Conversation:
        state = _State()

    runner, relay = _runner_with_relay(send)
    relay.bind_masker(_Conversation())
    callback = runner._make_event_callback("conv-1", "proj-1")

    relay.on_token("text that could not be masked")
    callback(_FakeFinishEvent("text that could not be masked"))
    await _drain()

    assert [m["event_type"] for m in sent] == ["_FakeFinishEvent"]
    assert json.loads(sent[0]["payload"])["action"]["message"] == ("text that could not be masked")


async def test_non_finish_events_are_never_rewritten():
    """The guard is `tool_name == "finish"`; SDK ActionEvents always carry a
    tool_name, so anything else must pass through untouched."""
    sent = []

    async def send(message):
        sent.append(message)

    runner, relay = _runner_with_relay(send)
    callback = runner._make_event_callback("conv-1", "proj-1")

    event = _FakeFinishEvent("identical text", tool_name="str_replace_editor")
    relay.on_token("identical text")
    callback(event)
    await _drain()

    assert json.loads(sent[-1]["payload"])["action"]["message"] == "identical text"


async def test_finish_observation_is_blanked_alongside_the_finish_action():
    """The SDK closes every turn with *two* events built from the same text:
    the ActionEvent (FinishAction.message) and, immediately after, an
    ObservationEvent (FinishObservation.text) — `_finalize_successful_turn`
    emits both, always. Blanking only the action half would still leave the
    observation half persisting the same text a second time server-side
    (every event is persisted unconditionally), even though nothing renders
    it — the earlier version of this fix missed this second event entirely."""
    sent = []

    async def send(message):
        sent.append(message)

    runner, relay = _runner_with_relay(send)
    callback = runner._make_event_callback("conv-1", "proj-1")

    relay.on_token("First I read the file.")
    callback(_FakeEvent())
    relay.on_token(" Then I fixed it.")
    callback(_FakeFinishEvent("First I read the file. Then I fixed it."))
    callback(_FakeFinishObservationEvent("First I read the file. Then I fixed it."))
    await _drain()

    assert [m["event_type"] for m in sent] == [
        "MessageEvent",
        "_FakeEvent",
        "MessageEvent",
        "_FakeFinishEvent",
        "_FakeFinishObservationEvent",
    ]
    assert json.loads(sent[3]["payload"])["action"]["message"] == ""
    assert json.loads(sent[4]["payload"])["observation"]["content"] == []


async def test_finish_observation_is_kept_when_it_differs_from_the_streamed_text():
    sent = []

    async def send(message):
        sent.append(message)

    runner, relay = _runner_with_relay(send)
    callback = runner._make_event_callback("conv-1", "proj-1")

    relay.on_token("streamed text")
    callback(_FakeFinishObservationEvent("a differently masked version"))
    await _drain()

    observation = json.loads(sent[-1]["payload"])["observation"]
    assert observation["content"] == [{"type": "text", "text": "a differently masked version"}]


async def test_finish_message_is_blanked_when_it_is_a_trailing_duplicate_after_a_retry():
    """ACPAgent retries a turn in place on a transient connection error:
    `_reset_client_for_turn` clears the SDK's own accumulated text for the
    new attempt but re-wires the *same* on_token callback, so this relay's
    `_emitted` keeps growing across every attempt while the SDK's eventual
    FinishAction message reflects only the attempt that actually succeeded.
    Without a trailing-duplicate check, that survivor message would fail the
    (exact-match) dedup and be shown again in full — on top of whatever
    already streamed from the failed attempt."""
    sent = []

    async def send(message):
        sent.append(message)

    runner, relay = _runner_with_relay(send)
    callback = runner._make_event_callback("conv-1", "proj-1")

    # Attempt 1 streams some narration, then the connection drops.
    relay.on_token("Connecting to the server...")
    callback(_FakeEvent())
    # Attempt 2 (the retry) streams its own narration and succeeds; the SDK's
    # FinishAction message is built only from this attempt's accumulated text.
    relay.on_token("Retrying now. Done.")
    callback(_FakeFinishEvent("Retrying now. Done."))
    await _drain()

    assert json.loads(sent[-1]["payload"])["action"]["message"] == ""


async def test_already_emitted_does_not_match_an_unrelated_prefix():
    """A trailing-duplicate check must still reject text that only shares a
    prefix (or is unrelated) with what streamed — not every substring match
    is a real duplicate."""
    relay = _AssistantTextRelay()
    relay.on_token("Checking the failing test first.")
    relay.take_pending()

    assert not relay.already_emitted("Checking the failing")
    assert not relay.already_emitted("something else entirely")
    assert relay.already_emitted("Checking the failing test first.")
    assert relay.already_emitted("test first.")
