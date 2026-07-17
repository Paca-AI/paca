#!/usr/bin/env python3
"""galaxy-notify-bridge — Paca task events → Galaxy notification inbox (ADR-038 P3.1).

Self-contained sidecar for the Galaxy deployment of Paca. It tails Paca's
own Valkey streams, mirrors the exact filtering Paca applies when it writes
its ``notifications`` table (types ``assigned`` + ``mentioned``), maps the
recipient Paca user to their Vortex identity (``users.oidc_sub``), and
republishes to the platform notification fan-out so the Galaxy inbox
(mobile + web bell) picks Paca events up like any other house app.

Sources (ground truth in Paca services/api — do not invent shapes):
  * ``paca.task_assignments``  — ``task.assigned`` entries appended by
    events.PublishAssignmentChanged. This is the same stream Paca's own
    NotificationConsumer (group ``api.notification_writer``) turns into
    ``assigned`` notification rows. Payload JSON: task_id, project_id,
    new_assignee_member_id, old_assignee_member_id?, actor_user_id,
    + optional workflow attribution (workflow_id/workflow_name/...).
  * ``paca.plugin_events``     — every activity event (fanout in
    tasksvc.ActivitySvc). Comment creations arrive as type
    ``task.comment.added`` with the full BlockNote content; Paca derives
    ``mentioned`` notifications synchronously in AddComment, so this stream
    is the only durable place mentions can be replayed from.
    NOTE: ``paca.task_activities`` (consumed by the workflow engine) does
    NOT carry comments (they persist to Postgres directly) nor dedicated
    assignment events — hence the two streams above.

Both streams are read with a SEPARATE consumer group ``galaxy-notify-bridge``
(never Paca's own groups), created at "$" so a fresh deploy does not replay
history as a notification flood.

Fan-out contract (mirrors Galaxy-AI-Project-Management
pm_service/utils/notify_emit.py and Galaxy-Nexus services/notification-service):
  PUBLISH <NOTIFY_FANOUT_CHANNEL default notify.fan-out> on the platform
  Redis with JSON: {user_ids: [vortex-sub…], type, title, body, deeplink,
  payload, source_app}. user_ids MUST be Vortex identity UUIDs — the
  fan-out worker does UUID(raw) and drops anything else, which is why Paca
  users without ``users.oidc_sub`` are skipped here (debug log).

Deeplinks are the SPA's real route (apps/web routes):
  {PACA_PUBLIC_URL}/projects/<project_id>/tasks/<task_id>
An https link is used instead of PM's ``galaxy://pm/task/…`` because the
mobile inbox opens deeplinks with Linking.openURL and no native Paca screen
exists; the payload carries the raw ids so a future handler can rewrite it.

Environment:
  BRIDGE_ENABLED           default "false" — anything else than "true" idles.
  PACA_VALKEY_URL          default redis://valkey:6379/0     (stack network)
  PACA_DATABASE_URL        default matches deploy/docker-compose.prod.yml;
                           opened read-only (default_transaction_read_only=on).
  GALAXY_NOTIFY_REDIS_URL  default redis://agentops-redis:6379/0 (galaxy_network,
                           same convention as NOTIFY_REDIS_URL in pm_service)
  NOTIFY_FANOUT_CHANNEL    default notify.fan-out
  PACA_PUBLIC_URL          default https://tasks.skyplatform.net

Never logs secrets or connection strings; identity comes from Paca's signed
writes (streams + DB rows), not from anything client-supplied.
"""

from __future__ import annotations

import json
import logging
import os
import re
import signal
import socket
import time
import uuid as uuidlib
from dataclasses import dataclass
from typing import Any, Callable, Optional

log = logging.getLogger("notify-bridge")

# ── Constants mirroring Paca source (services/api/internal/events) ──────────
STREAM_ASSIGNMENTS = "paca.task_assignments"
STREAM_PLUGIN_EVENTS = "paca.plugin_events"
CONSUMER_GROUP = "galaxy-notify-bridge"

TYPE_TASK_ASSIGNED = "task.assigned"  # entries on STREAM_ASSIGNMENTS
TYPE_COMMENT_ADDED = "task.comment.added"  # entries on STREAM_PLUGIN_EVENTS

# Fan-out event types (double as mute-preference channels in the
# notification-service; task_assigned matches what PM already emits).
FANOUT_TYPE_ASSIGNED = "task_assigned"
FANOUT_TYPE_MENTIONED = "task_mentioned"
SOURCE_APP = "paca"

# Mirrors notificationsvc mentionRegexp (legacy plain-text @mentions).
MENTION_RE = re.compile(r"@([a-zA-Z0-9._-]+)")
BODY_MAX = 140  # PM truncates notification bodies to 140 chars

READ_COUNT = 50
READ_BLOCK_MS = 5000


# ── Resolved row shapes (filled from read-only Postgres lookups) ────────────


@dataclass(frozen=True)
class Member:
    """An active project_members row joined to users."""

    member_id: str
    user_id: Optional[str]  # None for agent members
    is_agent: bool
    oidc_sub: Optional[str]
    username: str = ""
    full_name: str = ""


@dataclass(frozen=True)
class TaskInfo:
    """A tasks row joined to projects, for titles + deeplinks."""

    task_id: str
    project_id: str
    title: str
    number: int
    project_name: str
    prefix: str

    @property
    def human_ref(self) -> str:
        """PACA-12 style ref, mirroring projects.task_id_prefix + task_number."""
        if self.prefix and self.number:
            return f"{self.prefix}-{self.number}"
        if self.number:
            return f"#{self.number}"
        return self.title[:40]


@dataclass(frozen=True)
class ProjectUser:
    """A human member of a project (users row), for mention resolution."""

    user_id: str
    username: str
    oidc_sub: Optional[str]


# ── Pure parsing/mapping helpers (unit-tested; no I/O) ──────────────────────


def parse_stream_entry(fields: dict[str, Any]) -> tuple[str, dict[str, Any]]:
    """Decode a Paca stream entry ({type: <topic>, payload: <json>}) as
    written by messaging.Publisher.Append. Raises ValueError on malformed
    entries so the caller can ack-and-skip them (they never heal on retry).
    """
    etype = fields.get("type")
    raw = fields.get("payload")
    if isinstance(etype, bytes):
        etype = etype.decode("utf-8", "replace")
    if isinstance(raw, bytes):
        raw = raw.decode("utf-8", "replace")
    if not etype or raw is None:
        raise ValueError("stream entry missing type/payload fields")
    payload = json.loads(raw)
    if not isinstance(payload, dict):
        raise ValueError("stream entry payload is not a JSON object")
    return str(etype), payload


def is_uuid(value: Any) -> bool:
    try:
        uuidlib.UUID(str(value))
        return True
    except (ValueError, TypeError, AttributeError):
        return False


def extract_text_from_blocks(raw: str) -> str:
    """Mirror tasksvc.extractTextFromBlocks: BlockNote blocks[].content[].text
    joined with spaces, falling back to the legacy {"text": "..."} object."""
    try:
        doc = json.loads(raw)
    except (ValueError, TypeError):
        return ""
    if isinstance(doc, list):
        parts: list[str] = []
        for block in doc:
            if not isinstance(block, dict):
                continue
            for item in block.get("content") or []:
                if isinstance(item, dict):
                    text = item.get("text")
                    if isinstance(text, str) and text:
                        parts.append(text)
        return " ".join(parts)
    if isinstance(doc, dict) and isinstance(doc.get("text"), str):
        return doc["text"]
    return ""


def extract_team_mention_ids(raw: str) -> list[str]:
    """Mirror pkg/mention.ExtractTeamMentionsFromBlocks: inline content items
    of type "teamMention" carry props.id (users.id for humans, agents.id for
    agent members — the latter simply won't resolve to a user and drop out)."""
    try:
        doc = json.loads(raw)
    except (ValueError, TypeError):
        return []
    if not isinstance(doc, list):
        return []
    ids: list[str] = []
    for block in doc:
        if not isinstance(block, dict):
            continue
        for item in block.get("content") or []:
            if not isinstance(item, dict) or item.get("type") != "teamMention":
                continue
            props = item.get("props") or {}
            mention_id = props.get("id") if isinstance(props, dict) else None
            if isinstance(mention_id, str) and mention_id:
                ids.append(mention_id)
    return ids


def extract_username_mentions(text: str) -> set[str]:
    """Mirror notificationsvc.extractMentions (legacy @username parsing)."""
    return {m.group(1) for m in MENTION_RE.finditer(text or "")}


def task_deeplink(base_url: str, project_id: str, task_id: str) -> str:
    return f"{base_url.rstrip('/')}/projects/{project_id}/tasks/{task_id}"


def build_fanout_event(
    sub: str,
    type_: str,
    title: str,
    body: Optional[str],
    deeplink: str,
    payload: dict[str, Any],
) -> dict[str, Any]:
    """The exact event shape pm_service/utils/notify_emit.py publishes."""
    return {
        "user_ids": [str(sub)],
        "type": type_,
        "title": title,
        "body": body,
        "deeplink": deeplink,
        "payload": payload,
        "source_app": SOURCE_APP,
    }


def map_assignment(
    payload: dict[str, Any],
    resolve_member: Callable[[str], Optional[Member]],
    resolve_task: Callable[[str], Optional[TaskInfo]],
    base_url: str,
) -> list[dict[str, Any]]:
    """task.assigned → at most one ``task_assigned`` fan-out event.

    Mirrors notificationsvc.NotifyAssigned: resolve the assignee member to a
    user, skip agent assignees (Paca triggers agent conversations for those,
    not notifications), skip self-assignment, then map user → oidc_sub.
    """
    member_id = payload.get("new_assignee_member_id")
    task_id = payload.get("task_id")
    project_id = payload.get("project_id")
    actor_user_id = str(payload.get("actor_user_id") or "")
    if not (is_uuid(member_id) and is_uuid(task_id) and is_uuid(project_id)):
        log.debug("assignment skipped: malformed ids in payload")
        return []

    member = resolve_member(str(member_id))
    if member is None:
        log.debug("assignment skipped: member %s not found", member_id)
        return []
    if member.is_agent or not member.user_id:
        log.debug("assignment skipped: assignee member %s is an agent", member_id)
        return []
    if actor_user_id and member.user_id == actor_user_id:
        log.debug("assignment skipped: self-assignment by user %s", actor_user_id)
        return []
    if not member.oidc_sub:
        log.debug(
            "assignment skipped: user %s has no oidc_sub (not linked to Vortex)",
            member.user_id,
        )
        return []

    task = resolve_task(str(task_id))
    if task is None:
        log.debug("assignment skipped: task %s not found/deleted", task_id)
        return []

    extra_payload: dict[str, Any] = {
        "task_id": str(task_id),
        "project_id": str(project_id),
        "paca_type": "assigned",
        "task_ref": task.human_ref,
        "actor_user_id": actor_user_id or None,
    }
    if payload.get("workflow_name"):
        extra_payload["workflow_name"] = payload["workflow_name"]

    return [
        build_fanout_event(
            member.oidc_sub,
            FANOUT_TYPE_ASSIGNED,
            f"Bạn được giao task {task.human_ref}",
            (task.title or "")[:BODY_MAX] or None,
            task_deeplink(base_url, str(project_id), str(task_id)),
            extra_payload,
        )
    ]


def map_comment_mentions(
    payload: dict[str, Any],
    resolve_member: Callable[[str], Optional[Member]],
    list_project_users: Callable[[str], list[ProjectUser]],
    resolve_task: Callable[[str], Optional[TaskInfo]],
    base_url: str,
) -> list[dict[str, Any]]:
    """task.comment.added → one ``task_mentioned`` event per mentioned user.

    Mirrors tasksvc.AddComment + notificationsvc.NotifyMentioned: structured
    BlockNote teamMention ids take precedence (agent ids drop out because
    they aren't users), legacy plain-text @username parsing is the fallback;
    recipients must be active human project members; self-mentions and
    duplicates are skipped.
    """
    task_id = payload.get("task_id")
    project_id = payload.get("project_id")
    content = payload.get("content")
    if not (is_uuid(task_id) and is_uuid(project_id)) or not isinstance(content, str):
        log.debug("mention skipped: malformed comment payload")
        return []
    if payload.get("actor_agent_id"):
        actor_user_id = None  # agent comment: no human self to exclude
    else:
        actor_member_id = payload.get("actor_id")
        actor_user_id = None
        if is_uuid(actor_member_id):
            actor = resolve_member(str(actor_member_id))
            actor_user_id = actor.user_id if actor else None

    members = {u.user_id: u for u in list_project_users(str(project_id))}
    mention_ids = extract_team_mention_ids(content)
    targets: list[ProjectUser] = []
    if mention_ids:
        for mid in mention_ids:
            user = members.get(mid)
            if user is not None:
                targets.append(user)
    else:
        comment_text = extract_text_from_blocks(content)
        usernames = {u.lower() for u in extract_username_mentions(comment_text)}
        if usernames:
            targets = [u for u in members.values() if u.username.lower() in usernames]

    if not targets:
        return []

    task = resolve_task(str(task_id))
    if task is None:
        log.debug("mention skipped: task %s not found/deleted", task_id)
        return []

    comment_text = extract_text_from_blocks(content)
    deeplink = task_deeplink(base_url, str(project_id), str(task_id))
    events: list[dict[str, Any]] = []
    seen: set[str] = set()
    for user in targets:
        if user.user_id in seen:
            continue
        seen.add(user.user_id)
        if actor_user_id and user.user_id == actor_user_id:
            log.debug("mention skipped: self-mention by user %s", actor_user_id)
            continue
        if not user.oidc_sub:
            log.debug(
                "mention skipped: user %s has no oidc_sub (not linked to Vortex)",
                user.user_id,
            )
            continue
        events.append(
            build_fanout_event(
                user.oidc_sub,
                FANOUT_TYPE_MENTIONED,
                f"Bạn được nhắc đến trong task {task.human_ref}",
                comment_text[:BODY_MAX] or None,
                deeplink,
                {
                    "task_id": str(task_id),
                    "project_id": str(project_id),
                    "paca_type": "mentioned",
                    "task_ref": task.human_ref,
                    "comment_id": payload.get("id"),
                },
            )
        )
    return events


def publish_events(client: Any, channel: str, events: list[dict[str, Any]]) -> int:
    """Publish each fan-out event as its own message (PM publishes one event
    per notification too). Raises on Redis errors so the caller leaves the
    stream entry unacked and retries."""
    for event in events:
        client.publish(channel, json.dumps(event, ensure_ascii=False, default=str))
    return len(events)


# ── I/O layer ────────────────────────────────────────────────────────────────

SQL_MEMBER = """
SELECT pm.user_id::text,
       (pm.member_type = 'agent') AS is_agent,
       u.oidc_sub,
       COALESCE(u.username, ''),
       COALESCE(u.full_name, '')
FROM project_members pm
LEFT JOIN users u ON u.id = pm.user_id AND u.deleted_at IS NULL
WHERE pm.id = %s::uuid
"""

SQL_TASK = """
SELECT t.project_id::text,
       t.title,
       t.task_number,
       p.name,
       p.task_id_prefix
FROM tasks t
JOIN projects p ON p.id = t.project_id
WHERE t.id = %s::uuid AND t.deleted_at IS NULL
"""

SQL_PROJECT_USERS = """
SELECT u.id::text, u.username, u.oidc_sub
FROM project_members pm
JOIN users u ON u.id = pm.user_id
WHERE pm.project_id = %s::uuid
  AND pm.deleted_at IS NULL
  AND pm.member_type = 'human'
  AND u.deleted_at IS NULL
"""


class Bridge:
    """Wires the pure mapping onto real Valkey/Postgres/Redis connections."""

    def __init__(self) -> None:
        self.paca_valkey_url = os.getenv("PACA_VALKEY_URL", "redis://valkey:6379/0")
        self.galaxy_redis_url = os.getenv(
            "GALAXY_NOTIFY_REDIS_URL", "redis://agentops-redis:6379/0"
        )
        self.channel = os.getenv("NOTIFY_FANOUT_CHANNEL", "notify.fan-out")
        self.database_url = os.getenv(
            "PACA_DATABASE_URL",
            "postgres://paca:changeme@postgres:5432/paca?sslmode=disable",
        )
        self.public_url = os.getenv("PACA_PUBLIC_URL", "https://tasks.skyplatform.net")
        self.consumer = f"{CONSUMER_GROUP}.{socket.gethostname() or uuidlib.uuid4()}"
        self._paca: Any = None
        self._galaxy: Any = None
        self._db: Any = None
        self.stopping = False

    # -- connections (lazy; reconnect on failure) ---------------------------

    def paca(self) -> Any:
        if self._paca is None:
            import redis

            self._paca = redis.Redis.from_url(
                self.paca_valkey_url, decode_responses=True, socket_connect_timeout=5
            )
        return self._paca

    def galaxy(self) -> Any:
        if self._galaxy is None:
            import redis

            self._galaxy = redis.Redis.from_url(
                self.galaxy_redis_url, decode_responses=True, socket_connect_timeout=5
            )
        return self._galaxy

    def db(self) -> Any:
        if self._db is None or self._db.closed:
            import psycopg

            # Read-only session: the bridge must never write to Paca's DB.
            self._db = psycopg.connect(
                self.database_url,
                autocommit=True,
                options="-c default_transaction_read_only=on",
            )
        return self._db

    def _query_one(self, sql: str, arg: str) -> Optional[tuple]:
        with self.db().cursor() as cur:
            cur.execute(sql, (arg,))
            return cur.fetchone()

    def _query_all(self, sql: str, arg: str) -> list[tuple]:
        with self.db().cursor() as cur:
            cur.execute(sql, (arg,))
            return cur.fetchall()

    # -- resolvers (read-only lookups) ---------------------------------------

    def resolve_member(self, member_id: str) -> Optional[Member]:
        row = self._query_one(SQL_MEMBER, member_id)
        if row is None:
            return None
        user_id, is_agent, oidc_sub, username, full_name = row
        return Member(
            member_id=member_id,
            user_id=user_id,
            is_agent=bool(is_agent),
            oidc_sub=oidc_sub,
            username=username or "",
            full_name=full_name or "",
        )

    def resolve_task(self, task_id: str) -> Optional[TaskInfo]:
        row = self._query_one(SQL_TASK, task_id)
        if row is None:
            return None
        project_id, title, number, project_name, prefix = row
        return TaskInfo(
            task_id=task_id,
            project_id=project_id,
            title=title or "",
            number=int(number or 0),
            project_name=project_name or "",
            prefix=prefix or "",
        )

    def list_project_users(self, project_id: str) -> list[ProjectUser]:
        return [
            ProjectUser(user_id=r[0], username=r[1] or "", oidc_sub=r[2])
            for r in self._query_all(SQL_PROJECT_USERS, project_id)
        ]

    # -- stream plumbing ------------------------------------------------------

    def ensure_groups(self) -> None:
        """Create the bridge's consumer groups at "$" (new events only) so a
        fresh deploy never replays stream history as a notification flood."""
        import redis

        for stream in (STREAM_ASSIGNMENTS, STREAM_PLUGIN_EVENTS):
            try:
                self.paca().xgroup_create(stream, CONSUMER_GROUP, id="$", mkstream=True)
                log.info("created consumer group %s on %s", CONSUMER_GROUP, stream)
            except redis.ResponseError as exc:
                if "BUSYGROUP" not in str(exc):
                    raise

    def map_entry(self, stream: str, etype: str, payload: dict[str, Any]) -> list[dict[str, Any]]:
        if stream == STREAM_ASSIGNMENTS and etype == TYPE_TASK_ASSIGNED:
            return map_assignment(
                payload, self.resolve_member, self.resolve_task, self.public_url
            )
        if stream == STREAM_PLUGIN_EVENTS and etype == TYPE_COMMENT_ADDED:
            return map_comment_mentions(
                payload,
                self.resolve_member,
                self.list_project_users,
                self.resolve_task,
                self.public_url,
            )
        return []

    def handle_entry(self, stream: str, entry_id: str, fields: dict[str, Any]) -> None:
        """Process one stream entry; acks on success AND on permanently
        malformed entries (retrying those can never succeed). Infra errors
        (DB/Redis down) propagate so the entry stays pending and is retried."""
        try:
            etype, payload = parse_stream_entry(fields)
        except ValueError as exc:
            log.warning("%s %s: unparseable entry (%s) — acked", stream, entry_id, exc)
            self.paca().xack(stream, CONSUMER_GROUP, entry_id)
            return

        events = self.map_entry(stream, etype, payload)
        if events:
            published = publish_events(self.galaxy(), self.channel, events)
            log.info(
                "%s %s: published %d notification(s) type=%s task=%s",
                stream,
                entry_id,
                published,
                events[0]["type"],
                events[0]["payload"].get("task_id"),
            )
        self.paca().xack(stream, CONSUMER_GROUP, entry_id)

    def drain(self, start_id: str) -> int:
        """One xreadgroup pass over both streams from start_id (">" for new
        entries, "0" for this consumer's pending backlog). Returns the number
        of entries handled."""
        resp = self.paca().xreadgroup(
            CONSUMER_GROUP,
            self.consumer,
            {STREAM_ASSIGNMENTS: start_id, STREAM_PLUGIN_EVENTS: start_id},
            count=READ_COUNT,
            block=READ_BLOCK_MS if start_id == ">" else None,
        )
        handled = 0
        for stream, entries in resp or []:
            for entry_id, fields in entries:
                if self.stopping:
                    return handled
                self.handle_entry(stream, entry_id, fields)
                handled += 1
        return handled

    def run(self) -> None:
        self.ensure_groups()
        # Re-attempt anything this consumer read but never acked (crash mid-batch).
        try:
            pending = self.drain("0")
            if pending:
                log.info("recovered %d pending entrie(s) from a previous run", pending)
        except Exception as exc:  # noqa: BLE001 — startup backlog is best-effort
            log.warning("pending sweep failed (will retry live): %s", exc)

        backoff = 1.0
        while not self.stopping:
            try:
                self.drain(">")
                backoff = 1.0
            except Exception as exc:  # noqa: BLE001 — keep the bridge alive
                log.warning("bridge loop error: %s — retrying in %.0fs", exc, backoff)
                # Drop connections so the next pass reconnects cleanly.
                self._paca = None
                self._galaxy = None
                try:
                    if self._db is not None and not self._db.closed:
                        self._db.close()
                except Exception:  # noqa: BLE001
                    pass
                self._db = None
                time.sleep(backoff)
                backoff = min(backoff * 2, 30.0)


def main() -> int:
    logging.basicConfig(
        level=os.getenv("LOG_LEVEL", "INFO").upper(),
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    bridge = Bridge()

    def _stop(signum: int, _frame: Any) -> None:
        log.info("signal %d — stopping", signum)
        bridge.stopping = True

    signal.signal(signal.SIGTERM, _stop)
    signal.signal(signal.SIGINT, _stop)

    if os.getenv("BRIDGE_ENABLED", "false").strip().lower() != "true":
        log.info(
            "BRIDGE_ENABLED != true — bridge idle "
            "(set BRIDGE_ENABLED=true, or --scale notify-bridge=0 to remove)"
        )
        while not bridge.stopping:
            time.sleep(5)
        return 0

    log.info(
        "starting: streams=%s,%s group=%s channel=%s",
        STREAM_ASSIGNMENTS,
        STREAM_PLUGIN_EVENTS,
        CONSUMER_GROUP,
        bridge.channel,
    )
    bridge.run()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
