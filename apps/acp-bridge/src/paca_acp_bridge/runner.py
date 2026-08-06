"""Runs local OpenHands ACP conversations and reports events back over the bridge.

Each conversation is a plain, fully local `openhands.sdk.Conversation` backed
by `ACPAgent` — the SDK's own default execution mode (same as its quickstart
example: `Conversation(agent=agent, workspace="./my-project")`). ACPAgent
spawns the chosen coding CLI (Claude Code / Codex / Gemini CLI / a custom ACP
server) as a genuine local subprocess against `workspace`, using whatever
auth, MCP servers, and skills are already configured in the user's own local
environment — this daemon does not manage or forward any of that. That also
means the CLI's own native tools (bash, git, gh, etc.) run with the user's own
real credentials, so it can clone/push/open PRs exactly as if the user were
driving it themselves — something a Paca-hosted sandbox can never do, since
ACPAgent doesn't accept custom tools.

Conversations are driven via `conversation.arun()` (the SDK's async run
loop), scheduled as a plain `asyncio.Task` on this daemon's own event loop,
rather than `conversation.run()` on a dedicated thread. `run()`/`pause()`
only take effect *between* whole agent steps and can't cancel one already in
flight — for `ACPAgent` a single step is the whole ACP turn (every tool call
the coding CLI makes before replying), so pressing "stop" mid-turn wouldn't
actually stop anything until that turn finished on its own. `arun()` tracks
the run as a cancellable `asyncio.Task`, so `conversation.interrupt()` can
cancel it immediately (mid-tool-call) and the SDK sends the coding CLI a
real ACP `session/cancel` in response — see `LocalConversation.interrupt()`
and `ACPAgent.astep()`'s `CancelledError` handling. It also sidesteps a
cross-thread state-lock deadlock the SDK documents for the sync `run()` path
(OpenHands SDK issues #3348/#3350): here, `interrupt()` never blocks waiting
on the conversation's state lock, so it's safe to call directly from this
event loop's own thread.
"""

from __future__ import annotations

import asyncio
import dataclasses
import json
import logging
from collections.abc import Awaitable, Callable
from typing import Any

from openhands.sdk import Conversation
from openhands.sdk.agent import ACPAgent
from openhands.sdk.settings.acp_providers import get_acp_provider

logger = logging.getLogger(__name__)

SendFn = Callable[[dict[str, Any]], Awaitable[None]]


@dataclasses.dataclass
class _ConversationHandle:
    conversation: Conversation
    task: asyncio.Task[None]


def _chunk_text(chunk: Any) -> str:
    """Pull the text out of whatever a token callback was handed.

    ``ACPAgent`` invokes the conversation's token callbacks with a plain ``str``
    — the already-masked ``AgentMessageChunk`` text — while the SDK's own
    ``TokenCallbackType`` alias describes a litellm ``ModelResponseStream``.
    Handle both, so this keeps working whichever way that inconsistency is
    eventually settled.
    """
    if isinstance(chunk, str):
        return chunk
    text = ""
    for choice in getattr(chunk, "choices", None) or []:
        content = getattr(getattr(choice, "delta", None), "content", None)
        if isinstance(content, str):
            text += content
    return text


class _AssistantTextRelay:
    """Collects streamed assistant text so it can be emitted in position.

    ``ACPAgent`` accumulates every ``AgentMessageChunk`` for the whole turn and
    persists the joined result only at the end, as the ``FinishAction`` message
    on the turn's closing ``ActionEvent``. Tool calls, by contrast, are emitted
    as they happen. The result is that everything the agent *said* arrives in
    one block after everything it *did*, with no way to tell which narration
    belonged to which step.

    This relay buffers the chunks and hands them back in segments. The runner
    emits each segment as a ``MessageEvent`` immediately before the next event
    it forwards, which puts the narration back between the tool calls it
    describes.
    """

    def __init__(self) -> None:
        self._pending: list[str] = []
        self._emitted: list[str] = []
        self._mask: Callable[[str], str] | None = None

    def bind_masker(self, conversation: Any) -> None:
        """Adopt the conversation's secret masker, if it exposes one.

        The chunks arrive individually masked, but a secret split across two of
        them only becomes matchable once they are joined — which is exactly why
        the SDK re-masks at its own persistence boundary. Segments are a
        persistence boundary too, so re-mask here for the same reason.
        """
        registry = getattr(getattr(conversation, "state", None), "secret_registry", None)
        mask = getattr(registry, "mask_secrets_in_output", None)
        if callable(mask):
            self._mask = mask

    def start_turn(self) -> None:
        self._pending.clear()
        self._emitted.clear()

    def on_token(self, chunk: Any) -> None:
        text = _chunk_text(chunk)
        if text:
            self._pending.append(text)

    def take_pending(self) -> str | None:
        """Return (and consume) the text streamed since the last call."""
        if not self._pending:
            return None
        text = "".join(self._pending)
        self._pending.clear()
        if self._mask is not None:
            try:
                text = self._mask(text)
            except Exception:
                logger.debug("Secret masking failed for streamed text", exc_info=True)
        if not text:
            return None
        self._emitted.append(text)
        return text

    def already_emitted(self, message: str) -> bool:
        """True if `message` is exactly the text already sent as MessageEvents."""
        return bool(self._emitted) and "".join(self._emitted) == message


def resolve_acp_command(acp_provider: str | None, acp_command: list[str]) -> list[str]:
    """Resolve the command to launch the ACP server.

    Built-in providers (claude-code/codex/gemini-cli) use the OpenHands SDK's
    own default command for that provider; "custom" (or an unrecognized
    provider) requires an explicit acp_command from the agent's config.
    """
    if acp_provider and acp_provider != "custom":
        provider = get_acp_provider(acp_provider)
        if provider is not None:
            return list(provider.default_command)
    if not acp_command:
        raise ValueError(
            f"No default command for acp_provider={acp_provider!r}; "
            "a custom acp_command is required"
        )
    return acp_command


class ConversationRunner:
    """Owns one local `Conversation` per active conversation_id.

    Mirrors services/ai-agent's executor.py chat_sandboxes pattern (reuse a
    live conversation across turns of the same chat), but entirely
    in-process — there's no sandbox to start/stop here.
    """

    def __init__(self, workspace: str, send: SendFn) -> None:
        self.workspace = workspace
        self._send = send
        # Captured lazily in start_turn() (always awaited from inside the
        # running loop) rather than here — __init__ runs before
        # asyncio.run() starts the loop that will actually be running when
        # ACPAgent's own background ("portal") thread needs to schedule
        # coroutines back onto it (e.g. mid-turn streaming events).
        self._loop: asyncio.AbstractEventLoop | None = None
        self._conversations: dict[str, _ConversationHandle] = {}
        self._text_relays: dict[str, _AssistantTextRelay] = {}

    async def start_turn(self, data: dict[str, Any]) -> None:
        self._loop = asyncio.get_running_loop()
        conversation_id = data["conversation_id"]
        project_id = data["project_id"]
        message = data.get("message", "")

        existing = self._conversations.get(conversation_id)
        if existing is not None:
            if not existing.task.done():
                # A previous turn's task is still driving this same
                # Conversation object via .send_message()/.arun() — starting
                # a second one on top of it would call into the SDK
                # concurrently, which Conversation isn't built to handle.
                # Reject explicitly rather than risking corrupted state, so
                # the caller gets an immediate failure instead of waiting
                # out acp_dispatch.py's watchdog timeout.
                logger.warning(
                    "Ignoring start_turn for conversation %s: a previous turn is still running",
                    conversation_id,
                )
                await self._report_status(
                    conversation_id,
                    project_id,
                    "failed",
                    "A previous turn for this conversation is still running; please retry.",
                )
                return
            # Resume — reply on the conversation object already running from
            # an earlier turn in this same chat session.
            relay = self._text_relays.get(conversation_id)
            if relay is not None:
                relay.start_turn()
            existing.task = asyncio.create_task(
                self._run_conversation(existing.conversation, conversation_id, project_id, message)
            )
            return

        try:
            command = resolve_acp_command(data.get("acp_provider"), data.get("acp_command") or [])
        except Exception as exc:
            logger.error("Cannot start conversation %s: %s", conversation_id, exc)
            await self._report_status(conversation_id, project_id, "failed", str(exc))
            return

        agent = ACPAgent(acp_command=command)
        relay = _AssistantTextRelay()
        self._text_relays[conversation_id] = relay
        conversation = Conversation(
            agent=agent,
            workspace=self.workspace,
            callbacks=[self._make_event_callback(conversation_id, project_id)],
            token_callbacks=[relay.on_token],
        )
        relay.bind_masker(conversation)
        task = asyncio.create_task(
            self._run_conversation(conversation, conversation_id, project_id, message)
        )
        self._conversations[conversation_id] = _ConversationHandle(
            conversation=conversation, task=task
        )

    def interrupt(self, conversation_id: str | None) -> None:
        """Handle a stop_turn/pause_turn message — both just interrupt the
        in-flight turn; there's no sandbox lifecycle to additionally tear
        down (unlike the cloud path's full stop vs. pause distinction).

        Safe to call directly here, synchronously, on the event-loop thread:
        while a turn is actually in flight, `conversation.interrupt()`
        cancels the tracked `arun()` task via a non-blocking
        `call_soon_threadsafe`, so it can't stall this loop the way the sync
        `run()` path's `pause()` fallback could (see module docstring). If
        the tracked task has already finished by the time this runs (a race
        with the turn wrapping up on its own), `interrupt()` falls back to a
        synchronous `pause()` that does briefly acquire the state lock — a
        much smaller window than the sync-`run()` deadlock this replaces,
        since nothing here holds that lock for the duration of a whole turn.
        """
        if not conversation_id:
            return
        handle = self._conversations.get(conversation_id)
        if handle is None:
            return
        try:
            handle.conversation.interrupt()
        except Exception:
            logger.exception("Failed to interrupt conversation %s", conversation_id)

    async def _run_conversation(
        self, conversation: Conversation, conversation_id: str, project_id: str, message: str
    ) -> None:
        try:
            conversation.send_message(message)
            await conversation.arun()
        except Exception as exc:
            logger.exception("Conversation %s failed", conversation_id)
            await self._report_status_safely(conversation_id, project_id, "failed", str(exc))
            return
        await self._report_status_safely(conversation_id, project_id, "finished")

    async def _report_status_safely(
        self, conversation_id: str, project_id: str, status: str, error_message: str | None = None
    ) -> None:
        # A failure to *report* status (e.g. the bridge's outbox raising
        # during shutdown) must not be conflated with the conversation
        # itself failing — isolated here so it can only ever produce a log
        # line, never a misleading "failed" status for a turn that actually
        # ran fine (or a second, unhandled exception out of the task).
        try:
            await self._report_status(conversation_id, project_id, status, error_message)
        except Exception:
            logger.warning(
                "Failed to report status for conversation %s", conversation_id, exc_info=True
            )

    def _strip_duplicated_finish_message(self, payload: str, relay: _AssistantTextRelay) -> str:
        """Blank a FinishAction message that was already streamed as text.

        The SDK builds that message by joining every chunk of the turn, so once
        those chunks have gone out as ``MessageEvent``s it is a verbatim second
        copy — the whole turn's narration repeated after the tool calls. Nothing
        server-side reads it (it is rendered, not consumed), and the same text
        is still persisted in the events it was split into, so dropping it loses
        nothing.

        Only an exact match is removed. If the two ever diverge — the SDK masks
        the join a second time, which can catch a secret split across two chunks
        — the message is left alone and shown as-is rather than discarded.
        """
        try:
            data = json.loads(payload)
        except (TypeError, ValueError):
            return payload
        if not isinstance(data, dict) or data.get("tool_name") not in (None, "finish"):
            return payload
        action = data.get("action")
        if not isinstance(action, dict):
            return payload
        message = action.get("message")
        if not isinstance(message, str) or not relay.already_emitted(message):
            return payload
        action["message"] = ""
        return json.dumps(data)

    def _make_event_callback(self, conversation_id: str, project_id: str) -> Callable[[Any], None]:
        def callback(event: Any) -> None:
            event_type = type(event).__name__
            try:
                payload = event.model_dump_json() if hasattr(event, "model_dump_json") else "{}"
                relay = self._text_relays.get(conversation_id)
                if relay is not None:
                    # Flush what the agent said since the previous event before
                    # forwarding this one, so its narration is ordered against
                    # the tool calls it describes.
                    streamed = relay.take_pending()
                    if streamed is not None:
                        self._dispatch_event(
                            {
                                "type": "event",
                                "conversation_id": conversation_id,
                                "project_id": project_id,
                                "event_type": "MessageEvent",
                                "event_source": "agent",
                                "payload": json.dumps({"content": streamed}),
                            },
                            "MessageEvent",
                            conversation_id,
                        )
                    payload = self._strip_duplicated_finish_message(payload, relay)
                message = {
                    "type": "event",
                    "conversation_id": conversation_id,
                    "project_id": project_id,
                    "event_type": event_type,
                    "event_source": str(getattr(event, "source", "agent")),
                    "payload": payload,
                }
                self._dispatch_event(message, event_type, conversation_id)
            except Exception:
                logger.warning(
                    "Failed to report event %s for conversation %s",
                    event_type,
                    conversation_id,
                    exc_info=True,
                )

        return callback

    def _dispatch_event(
        self, message: dict[str, Any], event_type: str, conversation_id: str
    ) -> None:
        try:
            called_from_our_loop = asyncio.get_running_loop() is self._loop
        except RuntimeError:
            called_from_our_loop = False
        if called_from_our_loop:
            # Most on_event calls now happen this way: arun() runs as
            # a task directly on this daemon's own loop, and things
            # like finalizing a turn or emitting InterruptEvent on
            # cancellation call back into this callback synchronously
            # from that same task. Blocking here with
            # run_coroutine_threadsafe(...).result() would deadlock —
            # the loop can't run the coroutine it just scheduled
            # while its own thread is stuck waiting on it (always
            # timing out after the full 10s). Just schedule it — but
            # still log if _send ends up failing, same as the
            # cross-thread branch below, rather than letting it
            # become a silent unretrieved-exception warning.
            def _log_send_failure(
                task: asyncio.Task[None],
                _event_type: str = event_type,
                _conversation_id: str = conversation_id,
            ) -> None:
                if task.cancelled():
                    return
                exc = task.exception()
                if exc is not None:
                    logger.warning(
                        "Failed to report event %s for conversation %s",
                        _event_type,
                        _conversation_id,
                        exc_info=exc,
                    )

            asyncio.get_running_loop().create_task(self._send(message)).add_done_callback(
                _log_send_failure
            )
        else:
            # Mid-turn ACP streaming updates fire from ACPAgent's own
            # background ("portal") thread, so this hop really is
            # cross-thread there, and safe to wait on.
            future = asyncio.run_coroutine_threadsafe(self._send(message), self._loop)
            future.result(timeout=10)

    async def _report_status(
        self, conversation_id: str, project_id: str, status: str, error_message: str | None = None
    ) -> None:
        message: dict[str, Any] = {
            "type": "turn_status",
            "conversation_id": conversation_id,
            "project_id": project_id,
            "status": status,
        }
        if error_message:
            message["error_message"] = error_message
        await self._send(message)
