"""Database access layer for agent conversations and events."""

from __future__ import annotations

from ..core.db import get_pool
from ..models.conversation_status import ConversationStatus


async def update_conversation_status(
    conversation_id: str,
    status: ConversationStatus,
    error_message: str | None = None,
) -> None:
    pool = await get_pool()
    if error_message is not None:
        await pool.execute(
            (
                "UPDATE agent_conversations"
                " SET status = $1, error_message = $2,"
                " updated_at = now() WHERE id = $3"
            ),
            status,
            error_message,
            conversation_id,
        )
    else:
        await pool.execute(
            "UPDATE agent_conversations SET status = $1, updated_at = now() WHERE id = $2",
            status,
            conversation_id,
        )


async def fail_if_not_terminal(conversation_id: str, error_message: str) -> bool:
    """Atomically mark a conversation FAILED unless it has already reached a
    terminal status. Returns True iff this call performed the transition.

    Used by acp_dispatch.py's watchdog to fail a turn that never got a
    turn_status back from the bridge — the WHERE clause makes this race-safe
    against a legitimate terminal status arriving concurrently (the watchdog
    then simply loses the race and does nothing).
    """
    pool = await get_pool()
    result = await pool.execute(
        """
        UPDATE agent_conversations
        SET status = $1, error_message = $2, updated_at = now()
        WHERE id = $3 AND status NOT IN ('finished', 'failed', 'stopped')
        """,
        ConversationStatus.FAILED,
        error_message,
        conversation_id,
    )
    return result == "UPDATE 1"


async def get_conversation_agent_type(conversation_id: str) -> tuple[str, str] | None:
    """Return (agent_id, agent_type) for a conversation's owning agent.

    Used by worker._handle_control to decide whether a stop/pause control
    message should signal the in-process polling loop (LLM agents) or forward
    through the ACP bridge dispatch channel (see agent/acp_bridge.py) instead.
    """
    pool = await get_pool()
    row = await pool.fetchrow(
        """
        SELECT a.id AS agent_id, a.agent_type
        FROM agent_conversations c
        JOIN agents a ON a.id = c.agent_id
        WHERE c.id = $1
        """,
        conversation_id,
    )
    if row is None:
        return None
    return str(row["agent_id"]), row["agent_type"]


async def get_conversation_realtime_context(conversation_id: str) -> tuple[str | None, str | None]:
    """Return (project_id, actor_user_id) for routing a realtime event about
    this conversation — mirrors the two-actor-shape TriggerMessage/
    ChatSandboxState already carry, exactly one of which is set.

    Used by routes/bridge.py: unlike executor.run_conversation (which reads
    project_id/actor_user_id straight off the in-memory TriggerMessage for
    the turn it's currently running), the bridge WebSocket relay handles
    events/turn_status for whichever conversation_id the local daemon
    reports, so it needs a fresh per-conversation lookup rather than reusing
    the connect-time agent (a global ACP agent's bridge session can relay
    conversations with different project contexts, or none).
    """
    pool = await get_pool()
    row = await pool.fetchrow(
        "SELECT project_id, actor_user_id FROM agent_conversations WHERE id = $1",
        conversation_id,
    )
    if row is None:
        return None, None
    project_id = str(row["project_id"]) if row["project_id"] is not None else None
    actor_user_id = str(row["actor_user_id"]) if row["actor_user_id"] is not None else None
    return project_id, actor_user_id


async def get_next_event_index(conversation_id: str) -> int:
    """Return the next unused event_index for a conversation.

    event_index is unique per conversation_id (see the
    uq_agent_conversation_events_index constraint) and spans the
    conversation's entire lifetime, not just the current turn — a resumed
    chat conversation's next turn must continue numbering from where the
    previous turn left off. Starting back at 0 would collide with indices
    already used by earlier turns, and insert_conversation_event's
    `ON CONFLICT DO NOTHING` would then silently drop those new events.
    """
    pool = await get_pool()
    row = await pool.fetchrow(
        "SELECT COALESCE(MAX(event_index), -1) + 1 AS next_index"
        " FROM agent_conversation_events WHERE conversation_id = $1",
        conversation_id,
    )
    return row["next_index"] if row else 0


async def get_seen_event_ids(conversation_id: str) -> set[str]:
    """Return the SDK event ids already persisted for a conversation.

    Used to seed a fresh turn's in-memory dedup set (see executor._SeenEvents)
    so that reconcile() — which re-walks the *entire* remote SDK event
    history, including earlier turns, not just the current one — does not
    re-persist already-stored events from previous turns under new
    event_index values. Without this, every resumed chat turn duplicates the
    whole prior conversation history.
    """
    pool = await get_pool()
    rows = await pool.fetch(
        "SELECT payload->>'id' AS sdk_id FROM agent_conversation_events WHERE conversation_id = $1",
        conversation_id,
    )
    return {row["sdk_id"] for row in rows if row["sdk_id"] is not None}


async def insert_conversation_event(
    conversation_id: str,
    event_type: str,
    event_source: str,
    event_index: int,
    payload: str,
) -> None:
    pool = await get_pool()
    await pool.execute(
        """
        INSERT INTO agent_conversation_events
            (id, conversation_id, event_type, event_source, event_index, payload, created_at)
        VALUES
            (gen_random_uuid(), $1, $2, $3, $4, $5::jsonb, now())
        ON CONFLICT DO NOTHING
        """,
        conversation_id,
        event_type,
        event_source,
        event_index,
        payload,
    )
