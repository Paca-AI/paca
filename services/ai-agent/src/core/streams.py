"""Valkey / Redis stream client and message types."""

from __future__ import annotations

import json
import logging
import socket
from dataclasses import dataclass
from typing import Any

import redis.asyncio as aioredis

from ..config import settings

logger = logging.getLogger(__name__)

_client: aioredis.Redis | None = None
_pubsub_client: aioredis.Redis | None = None

TRIGGER_STREAM = "paca:agent:triggers"
EVENTS_STREAM = "paca:agent:events"
# Pub/Sub channel consumed by services/realtime for WebSocket fan-out.
REALTIME_CHANNEL = "paca.events"
CONSUMER_GROUP = "ai-agent-workers"
# Unique per replica so Valkey tracks each instance's PEL separately.
CONSUMER_NAME = f"worker-{socket.gethostname()}"


def get_client() -> aioredis.Redis:
    global _client
    if _client is None:
        _client = aioredis.from_url(settings.valkey_url, decode_responses=True)
    return _client


def get_pubsub_client() -> aioredis.Redis:
    """A dedicated client for long-lived Pub/Sub listeners (see
    agent/acp_bridge.py's dispatch forwarder).

    redis-py defaults socket_timeout to 5 seconds — fine for get_client()'s
    request/response commands, but wrong for a Pub/Sub .listen() loop that
    legitimately sits idle for long stretches between messages: the first
    time nothing arrives within 5s, the read raises redis.exceptions.
    TimeoutError and kills the listener for good (it never reconnects).
    socket_timeout=None blocks indefinitely instead; socket_keepalive stays
    on (the library default) so a genuinely dead TCP connection still
    surfaces as a ConnectionError rather than hanging forever.
    """
    global _pubsub_client
    if _pubsub_client is None:
        _pubsub_client = aioredis.from_url(
            settings.valkey_url, decode_responses=True, socket_timeout=None
        )
    return _pubsub_client


async def close_client() -> None:
    global _client, _pubsub_client
    if _client is not None:
        await _client.aclose()
        _client = None
    if _pubsub_client is not None:
        await _pubsub_client.aclose()
        _pubsub_client = None


async def ensure_consumer_group() -> None:
    """Create the consumer group on the trigger stream if it does not exist."""
    client = get_client()
    try:
        await client.xgroup_create(TRIGGER_STREAM, CONSUMER_GROUP, id="$", mkstream=True)
    except aioredis.ResponseError as e:
        if "BUSYGROUP" not in str(e):
            raise


async def publish_event(fields: dict[str, Any]) -> None:
    """Publish an agent event to the outbound Valkey stream."""
    client = get_client()
    serialized = {k: str(v) for k, v in fields.items()}
    await client.xadd(EVENTS_STREAM, serialized)


async def publish_realtime(
    project_id: str,
    conversation_id: str,
    event_type: str = "agent.conversation.event",
    extra_payload: dict[str, Any] | None = None,
) -> None:
    """Publish directly to the paca.events pub/sub channel so the realtime
    service immediately fans the event out to connected WebSocket clients.

    The realtime service routes any event whose type starts with "agent." to
    the project tasks room (see permissions.ts), so clients invalidate their
    conversation query caches and see new messages without waiting for the
    next poll cycle.
    """
    client = get_client()
    payload: dict[str, Any] = {
        "project_id": project_id,
        "conversation_id": conversation_id,
    }
    if extra_payload:
        payload.update(extra_payload)
    message = json.dumps({"type": event_type, "payload": payload})
    await client.publish(REALTIME_CHANNEL, message)


# Trigger types that carry a conversation run request. Mirrors the
# TopicAgent* constants in services/api/internal/events/topics.go.
_TRIGGER_TYPES = {
    "agent.task_assigned",
    "agent.comment_mention",
    "agent.chat_message",
    "agent.description_write",
}

# Control message types that direct an *existing* conversation.
_CONTROL_TYPES = {
    "agent.stop",  # interrupt (if any) + destroy the sandbox — unchanged from before
    "agent.pause",  # interrupt the in-flight turn only; sandbox untouched
    "agent.heartbeat",  # keep-alive ping; refreshes the chat sandbox idle timer
}


@dataclass
class TriggerMessage:
    stream_id: str
    trigger_type: str
    conversation_id: str
    agent_id: str
    project_id: str
    task_id: str | None
    comment_id: str | None
    chat_session_id: str | None
    message: str
    actor_member_id: str | None
    repo_plugin_ids: list[str]

    @classmethod
    def from_stream_entry(cls, stream_id: str, fields: dict[str, str]) -> TriggerMessage:
        repo_plugin_ids_str = fields.get("repo_plugin_ids", "")
        repo_plugin_ids = repo_plugin_ids_str.split(",") if repo_plugin_ids_str else []
        return cls(
            stream_id=stream_id,
            trigger_type=fields["trigger_type"],
            conversation_id=fields["conversation_id"],
            agent_id=fields["agent_id"],
            project_id=fields["project_id"],
            task_id=fields.get("task_id") or None,
            comment_id=fields.get("comment_id") or None,
            chat_session_id=fields.get("chat_session_id") or None,
            message=fields.get("message", ""),
            actor_member_id=fields.get("actor_member_id"),
            repo_plugin_ids=repo_plugin_ids,
        )


@dataclass
class ControlMessage:
    """A stop/pause/heartbeat directive for an already-running conversation."""

    stream_id: str
    control_type: str  # one of _CONTROL_TYPES
    conversation_id: str
    project_id: str

    @classmethod
    def from_stream_entry(cls, stream_id: str, fields: dict[str, str]) -> ControlMessage:
        return cls(
            stream_id=stream_id,
            control_type=fields["type"],
            conversation_id=fields["conversation_id"],
            project_id=fields["project_id"],
        )


async def read_triggers(
    count: int = 10, block_ms: int = 2000
) -> list[TriggerMessage | ControlMessage]:
    """Read new messages from the consumer group.

    Returns a mixed list: run-requests are ``TriggerMessage`` instances;
    stop directives are ``ControlMessage`` instances.
    """
    client = get_client()
    try:
        results = await client.xreadgroup(
            CONSUMER_GROUP,
            CONSUMER_NAME,
            {TRIGGER_STREAM: ">"},
            count=count,
            block=block_ms,
        )
    except Exception as exc:
        logger.error("Failed to read from stream: %s", exc)
        return []
    if not results:
        return []
    messages: list[TriggerMessage | ControlMessage] = []
    for _stream, entries in results:
        for stream_id, fields in entries:
            msg_type = fields.get("type", "")
            if msg_type in _CONTROL_TYPES:
                try:
                    messages.append(ControlMessage.from_stream_entry(stream_id, fields))
                except KeyError as e:
                    logger.warning(
                        "Dropping malformed control message %s: missing %s", stream_id, e
                    )
            elif msg_type in _TRIGGER_TYPES or "trigger_type" in fields:
                try:
                    messages.append(TriggerMessage.from_stream_entry(stream_id, fields))
                except KeyError as e:
                    logger.warning(
                        "Dropping malformed trigger message %s: missing %s", stream_id, e
                    )
            else:
                logger.warning(
                    "Dropping unrecognised stream message %s (type=%r)", stream_id, msg_type
                )
    return messages


async def ack_trigger(stream_id: str) -> None:
    client = get_client()
    await client.xack(TRIGGER_STREAM, CONSUMER_GROUP, stream_id)
