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
"""

from __future__ import annotations

import asyncio
import dataclasses
import logging
import threading
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
    thread: threading.Thread


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
        # background threads need to schedule coroutines back onto it.
        self._loop: asyncio.AbstractEventLoop | None = None
        self._conversations: dict[str, _ConversationHandle] = {}

    async def start_turn(self, data: dict[str, Any]) -> None:
        self._loop = asyncio.get_running_loop()
        conversation_id = data["conversation_id"]
        project_id = data["project_id"]
        message = data.get("message", "")

        existing = self._conversations.get(conversation_id)
        if existing is not None:
            # Resume — reply on the conversation object already running from
            # an earlier turn in this same chat session. `.run()` is a
            # blocking, synchronous SDK call for a local (non-remote)
            # Conversation, so it must happen on its own thread here too —
            # calling it directly from this coroutine would freeze the
            # daemon's whole event loop (heartbeats, other conversations'
            # events) until the turn finishes.
            thread = threading.Thread(
                target=self._run_conversation,
                args=(existing.conversation, conversation_id, project_id, message),
                daemon=True,
            )
            existing.thread = thread
            thread.start()
            return

        try:
            command = resolve_acp_command(
                data.get("acp_provider"), data.get("acp_command") or []
            )
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
        thread = threading.Thread(
            target=self._run_conversation,
            args=(conversation, conversation_id, project_id, message),
            daemon=True,
        )
        self._conversations[conversation_id] = _ConversationHandle(
            conversation=conversation, thread=thread
        )
        thread.start()

    def interrupt(self, conversation_id: str | None) -> None:
        """Handle a stop_turn/pause_turn message — both just interrupt the
        in-flight turn; there's no sandbox lifecycle to additionally tear
        down (unlike the cloud path's full stop vs. pause distinction)."""
        if not conversation_id:
            return
        handle = self._conversations.get(conversation_id)
        if handle is None:
            return
        try:
            handle.conversation.interrupt()
        except Exception:
            logger.exception("Failed to interrupt conversation %s", conversation_id)

    def _run_conversation(
        self, conversation: Conversation, conversation_id: str, project_id: str, message: str
    ) -> None:
        try:
            conversation.send_message(message)
            conversation.run()
            self._report_status_threadsafe(conversation_id, project_id, "finished")
        except Exception as exc:
            logger.exception("Conversation %s failed", conversation_id)
            self._report_status_threadsafe(conversation_id, project_id, "failed", str(exc))

    def _make_event_callback(self, conversation_id: str, project_id: str) -> Callable[[Any], None]:
        def callback(event: Any) -> None:
            payload = event.model_dump_json() if hasattr(event, "model_dump_json") else "{}"
            message = {
                "type": "event",
                "conversation_id": conversation_id,
                "project_id": project_id,
                "event_type": type(event).__name__,
                "event_source": str(getattr(event, "source", "agent")),
                "payload": payload,
            }
            future = asyncio.run_coroutine_threadsafe(self._send(message), self._loop)
            try:
                future.result(timeout=10)
            except Exception:
                logger.warning(
                    "Failed to report event for conversation %s", conversation_id, exc_info=True
                )

        return callback

    def _report_status_threadsafe(
        self, conversation_id: str, project_id: str, status: str, error_message: str | None = None
    ) -> None:
        future = asyncio.run_coroutine_threadsafe(
            self._report_status(conversation_id, project_id, status, error_message), self._loop
        )
        try:
            future.result(timeout=10)
        except Exception:
            logger.warning(
                "Failed to report status for conversation %s", conversation_id, exc_info=True
            )

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
