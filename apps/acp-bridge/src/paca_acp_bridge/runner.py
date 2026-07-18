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
        conversation = Conversation(
            agent=agent,
            workspace=self.workspace,
            callbacks=[self._make_event_callback(conversation_id, project_id)],
        )
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
        `conversation.interrupt()` cancels the tracked `arun()` task via a
        non-blocking `call_soon_threadsafe` — it never waits on the
        conversation's state lock, so it can't stall this loop the way the
        sync `run()` path's `pause()` fallback could (see module docstring).
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
            await self._report_status(conversation_id, project_id, "finished")
        except Exception as exc:
            logger.exception("Conversation %s failed", conversation_id)
            await self._report_status(conversation_id, project_id, "failed", str(exc))

    def _make_event_callback(self, conversation_id: str, project_id: str) -> Callable[[Any], None]:
        def callback(event: Any) -> None:
            try:
                payload = event.model_dump_json() if hasattr(event, "model_dump_json") else "{}"
                message = {
                    "type": "event",
                    "conversation_id": conversation_id,
                    "project_id": project_id,
                    "event_type": type(event).__name__,
                    "event_source": str(getattr(event, "source", "agent")),
                    "payload": payload,
                }
                # ACPAgent streams mid-turn events from its own background
                # ("portal") thread, not necessarily this callback's caller,
                # so this still has to hop back onto the event loop
                # threadsafe rather than assuming it's already there.
                future = asyncio.run_coroutine_threadsafe(self._send(message), self._loop)
                future.result(timeout=10)
            except Exception:
                logger.warning(
                    "Failed to report event %s for conversation %s",
                    type(event).__name__,
                    conversation_id,
                    exc_info=True,
                )

        return callback

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
