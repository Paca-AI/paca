"""Shared in-memory registry for active conversations and stop signals."""

from __future__ import annotations

import dataclasses
import threading
import time
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from ..agent.docker_workspace import SandboxHandle

# Active Conversation objects keyed by conversation_id string.
# Multi-replica note: in a multi-replica deployment pair this with a Valkey key
# (paca:agent:active:{conversation_id}) to route control requests to the owning replica.
active_conversations: dict[str, object] = {}

# threading.Event per conversation_id; set() to signal the polling loop to
# stop — interrupt the in-flight turn (if any) and tear down the sandbox
# afterward. Unchanged from before this dict existed alongside pause_events.
# Set by worker._handle_control on "agent.stop".
stop_events: dict[str, threading.Event] = {}

# threading.Event per conversation_id; set() to signal the polling loop to
# interrupt the in-flight turn *without* tearing down the sandbox — the
# conversation goes back to "paused", ready for the next reply. This is the
# assistant-UI "stop" button. Set by worker._handle_control on "agent.pause".
pause_events: dict[str, threading.Event] = {}


@dataclasses.dataclass
class ChatSandboxState:
    """A chat conversation's sandbox, kept alive between user turns.

    Populated when a chat_message conversation reaches a natural per-turn
    finish or an interrupt-only pause (status "paused") instead of being torn
    down; consumed on the next reply to reattach without re-cloning/
    re-starting. Removed (and the sandbox stopped) on an explicit stop or by
    the idle reaper. `last_active_at` is refreshed by turn completion,
    interrupt-only pauses, and periodic frontend heartbeats — not just by
    sending a message — so the reaper only reclaims genuinely disconnected
    sessions.

    Multi-replica note: same caveat as `active_conversations` above — this is
    per-process only.
    """

    handle: SandboxHandle
    sdk_conversation_id: object
    # None for a global-chat conversation (no project) — actor_user_id is
    # set instead in that case. See core.streams.TriggerMessage.
    project_id: str | None
    actor_user_id: str | None = None
    last_active_at: float = dataclasses.field(default_factory=time.monotonic)


# conversation_id (str) -> ChatSandboxState for chat sessions paused between turns.
chat_sandboxes: dict[str, ChatSandboxState] = {}
