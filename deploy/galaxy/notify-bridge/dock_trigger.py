#!/usr/bin/env python3
"""galaxy-dock-trigger — Paca events → ChatDock agent runs (ADR-038).

Sibling of the notify-bridge (same image, own compose service + consumer
group ``galaxy-dock-agent``): when a task is ASSIGNED to a designated agent
principal (a plain Paca service user, e.g. ``galaxy-tasks-agent``) or that
principal is @MENTIONED in a comment, run the platform ChatDock agent
(AgentOps assistant ``app_id=paca``) on behalf of the ASSIGNER/MENTIONER
and let it work the task via the ``paca_*`` MCP tools — replacing the
retired in-app OpenHands agent (ADR-037 doctrine: agent surface = ChatDock).

Identity model (galaxy-agent-identity): the run is attributed to the HUMAN
who triggered it. We mint a short-lived RS256 user token via identity
``/internal/mint-service-token`` with ``sub = <actor's users.oidc_sub>``
(their Vortex UUID) and call AgentOps ``/api/chat/stream`` with it — the
runtime then lifts that sub as ``act_as`` for every paca-mcp tool call, so
Paca sees the actor, never a service account. Actors without ``oidc_sub``
(never logged in via Vortex) are skipped with a warning: there is no lawful
identity to act as.

Loop guards: events actored by a trigger principal itself are ignored
(assignment by the agent, comments the agent authored — including its own
result comment, which is attributed to the human actor but never mentions
the agent, see the prompt contract below).

Delivery semantics: AT-MOST-ONCE per stream entry. The entry is acked
BEFORE the agent run is dispatched — a crash mid-run loses that trigger
(the human can re-assign/re-mention), which is strictly better than a retry
storm double-commenting on the task.

Environment (on top of the notify-bridge ones — PACA_VALKEY_URL,
PACA_DATABASE_URL, PACA_PUBLIC_URL, LOG_LEVEL):
  DOCK_TRIGGER_ENABLED           default "false" — anything else idles.
  DOCK_AGENT_TRIGGER_USERNAMES   default "galaxy-tasks-agent" (comma-sep,
                                 matched case-insensitively on users.username).
  AGENTOPS_URL                   default http://agentops-backend:8090
                                 (reachable via agentops-redis-net).
  GALAXY_IDENTITY_URL            default http://nexus-identity:8086
                                 (needs galaxy_network).
  GALAXY_INTERNAL_SERVICE_SECRET REQUIRED — authenticates the token mint.
  DOCK_TRIGGER_TIMEOUT_S         default 300 — wall clock per agent run.

Never logs secrets or tokens; identity comes from Paca's signed stream/DB
rows and the identity-service mint, not from anything client-supplied.
"""

from __future__ import annotations

import json
import logging
import os
import signal
import socket
import time
import uuid as uuidlib
from dataclasses import dataclass
from typing import Any, Optional

import main as bridge_lib
from main import (
    STREAM_ASSIGNMENTS,
    STREAM_PLUGIN_EVENTS,
    TYPE_COMMENT_ADDED,
    TYPE_TASK_ASSIGNED,
    TaskInfo,
    extract_team_mention_ids,
    extract_text_from_blocks,
    extract_username_mentions,
    is_uuid,
    parse_stream_entry,
)

log = logging.getLogger("dock-trigger")

CONSUMER_GROUP = "galaxy-dock-agent"

SQL_USER = """
SELECT u.id::text, COALESCE(u.username, ''), COALESCE(u.full_name, ''),
       u.email, u.oidc_sub
FROM users u
WHERE u.id = %s::uuid AND u.deleted_at IS NULL
"""

COMMENT_SNIPPET_MAX = 600


@dataclass(frozen=True)
class UserRow:
    user_id: str
    username: str
    full_name: str
    email: Optional[str]
    oidc_sub: Optional[str]


@dataclass(frozen=True)
class TriggerJob:
    """One agent run to dispatch: who acts, on which task, why."""

    kind: str                 # "assigned" | "mentioned"
    actor: UserRow            # the human the run acts as
    agent_username: str       # which trigger principal was targeted
    task: TaskInfo
    comment_text: str = ""    # mention flows carry the triggering comment


def build_prompt(job: TriggerJob, base_url: str) -> str:
    """The instruction the ChatDock agent receives. VN-first, explicit ids
    (labels must be self-contained — the run has no other context), and a
    hard contract: work via paca_* tools, ALWAYS end with a result comment,
    never mention the agent account (loop guard)."""
    t = job.task
    link = bridge_lib.task_deeplink(base_url, t.project_id, t.task_id)
    head = (
        f"[Kích hoạt tự động từ Galaxy Tasks] Task {t.human_ref} "
        f"“{t.title}” trong project “{t.project_name}” "
        f"(project_id={t.project_id}, task_id={t.task_id}, {link})"
    )
    if job.kind == "assigned":
        why = (
            f"{head} vừa được {job.actor.full_name or job.actor.username} giao "
            f"cho tài khoản agent “{job.agent_username}”."
        )
        ask = (
            "Hãy xử lý task này thay người giao: đọc chi tiết task bằng "
            "paca_task_get (kèm description và tiêu chí trong đó), làm phần "
            "việc phân tích/lập kế hoạch/triển khai mà task yêu cầu ở mức "
            "làm được bằng tool paca_* (đọc dữ liệu project, sprint, task "
            "liên quan khi cần)."
        )
    else:
        snippet = (job.comment_text or "").strip()
        if len(snippet) > COMMENT_SNIPPET_MAX:
            snippet = snippet[:COMMENT_SNIPPET_MAX] + "…"
        why = (
            f"{head}: {job.actor.full_name or job.actor.username} vừa nhắc đến "
            f"tài khoản agent “{job.agent_username}” trong một bình "
            f"luận: “{snippet}”"
        )
        ask = (
            "Hãy trả lời đúng yêu cầu trong bình luận đó thay người nhắc: đọc "
            "chi tiết task bằng paca_task_get và các bình luận trước bằng "
            "paca_comments_list để đủ ngữ cảnh, rồi thực hiện phần việc được "
            "nhờ bằng tool paca_*."
        )
    tail = (
        "BẮT BUỘC kết thúc bằng MỘT bình luận kết quả lên chính task này qua "
        f"paca_comment_add (project_id={t.project_id}, task_id={t.task_id}): "
        "tóm tắt việc đã làm/kết luận, ngắn gọn, tiếng Việt. Trong bình luận "
        f"KHÔNG nhắc “@{job.agent_username}” (tránh kích hoạt lặp). "
        "Đây là luồng tự động không có người chờ chat — không hỏi lại; nếu "
        "thiếu thông tin thì ghi rõ trong bình luận là cần bổ sung gì."
    )
    return f"{why}\n\n{ask}\n\n{tail}"


class DockTrigger(bridge_lib.Bridge):
    """Reuses the Bridge's lazy connections/resolvers; own group + handling."""

    def __init__(self) -> None:
        super().__init__()
        self.consumer = f"{CONSUMER_GROUP}.{socket.gethostname() or uuidlib.uuid4()}"
        self.trigger_usernames = {
            u.strip().lower()
            for u in os.getenv("DOCK_AGENT_TRIGGER_USERNAMES", "galaxy-tasks-agent").split(",")
            if u.strip()
        }
        self.agentops_url = os.getenv(
            "AGENTOPS_URL", "http://agentops-backend:8090"
        ).rstrip("/")
        self.identity_url = os.getenv(
            "GALAXY_IDENTITY_URL", "http://nexus-identity:8086"
        ).rstrip("/")
        self.service_secret = os.getenv("GALAXY_INTERNAL_SERVICE_SECRET", "")
        self.run_timeout_s = float(os.getenv("DOCK_TRIGGER_TIMEOUT_S", "300"))

    # -- lookups --------------------------------------------------------------

    def resolve_user(self, user_id: str) -> Optional[UserRow]:
        row = self._query_one(SQL_USER, user_id)
        if row is None:
            return None
        uid, username, full_name, email, oidc_sub = row
        return UserRow(uid, username or "", full_name or "", email, oidc_sub)

    def _is_trigger_username(self, username: str) -> bool:
        return (username or "").strip().lower() in self.trigger_usernames

    # -- event → job mapping --------------------------------------------------

    def job_from_assignment(self, payload: dict[str, Any]) -> Optional[TriggerJob]:
        member_id = payload.get("new_assignee_member_id")
        task_id = payload.get("task_id")
        actor_user_id = payload.get("actor_user_id")
        if not (is_uuid(member_id) and is_uuid(task_id) and is_uuid(actor_user_id)):
            return None
        member = self.resolve_member(str(member_id))
        if member is None or member.is_agent or not member.user_id:
            return None
        if not self._is_trigger_username(member.username):
            return None
        actor = self.resolve_user(str(actor_user_id))
        if actor is None:
            log.debug("assignment skipped: actor %s not found", actor_user_id)
            return None
        if self._is_trigger_username(actor.username):
            log.debug("assignment skipped: actor IS a trigger principal (loop guard)")
            return None
        if not actor.oidc_sub:
            log.warning(
                "assignment to %s skipped: actor %s has no oidc_sub — "
                "cannot act on their behalf",
                member.username, actor.username,
            )
            return None
        task = self.resolve_task(str(task_id))
        if task is None:
            return None
        return TriggerJob("assigned", actor, member.username, task)

    def job_from_comment(self, payload: dict[str, Any]) -> Optional[TriggerJob]:
        task_id = payload.get("task_id")
        project_id = payload.get("project_id")
        content = payload.get("content")
        if not (is_uuid(task_id) and is_uuid(project_id)) or not isinstance(content, str):
            return None
        if payload.get("actor_agent_id"):
            return None  # in-app agent comment — never a human trigger
        actor_member_id = payload.get("actor_id")
        if not is_uuid(actor_member_id):
            return None
        actor_member = self.resolve_member(str(actor_member_id))
        if actor_member is None or not actor_member.user_id:
            return None
        if self._is_trigger_username(actor_member.username):
            log.debug("comment skipped: authored by a trigger principal (loop guard)")
            return None

        users = {u.user_id: u for u in self.list_project_users(str(project_id))}
        mentioned_username: Optional[str] = None
        structured_ids = extract_team_mention_ids(content)
        if structured_ids:
            for mid in structured_ids:
                u = users.get(mid)
                if u is not None and self._is_trigger_username(u.username):
                    mentioned_username = u.username
                    break
        if mentioned_username is None:
            text = extract_text_from_blocks(content)
            handles = {h.lower() for h in extract_username_mentions(text)}
            hit = handles & self.trigger_usernames
            if hit:
                wanted = sorted(hit)[0]
                match = next(
                    (u for u in users.values() if u.username.lower() == wanted), None
                )
                # The principal must actually be a member of the project —
                # otherwise the agent could not read the task it is asked about.
                mentioned_username = match.username if match else None
        if mentioned_username is None:
            return None

        actor = self.resolve_user(str(actor_member.user_id))
        if actor is None:
            return None
        if not actor.oidc_sub:
            log.warning(
                "mention of %s skipped: actor %s has no oidc_sub — "
                "cannot act on their behalf",
                mentioned_username, actor.username,
            )
            return None
        task = self.resolve_task(str(task_id))
        if task is None:
            return None
        return TriggerJob(
            "mentioned", actor, mentioned_username, task,
            comment_text=extract_text_from_blocks(content),
        )

    # -- AgentOps dispatch ----------------------------------------------------

    def mint_actor_token(self, actor: UserRow) -> Optional[str]:
        """RS256 user token for the actor via identity mint (TTL capped 900s).
        sub = the actor's Vortex UUID so the AgentOps runtime lifts it as
        act_as for every paca-mcp call. No `type: service` claim — the token
        represents the human, and the runtime refuses to act-as service subs.
        """
        if not self.service_secret:
            log.error("GALAXY_INTERNAL_SERVICE_SECRET unset — cannot mint")
            return None
        import httpx

        extra: dict[str, Any] = {}
        if actor.email:
            extra["email"] = actor.email
        if actor.full_name or actor.username:
            extra["name"] = actor.full_name or actor.username
        try:
            r = httpx.post(
                f"{self.identity_url}/internal/mint-service-token",
                headers={"X-Service-Secret": self.service_secret,
                         "Content-Type": "application/json"},
                json={"sub": actor.oidc_sub, "aud": "agentops",
                      "ttl_seconds": 900, "extra": extra},
                timeout=10.0,
            )
        except Exception as exc:  # noqa: BLE001 — mint failure = skip trigger
            log.error("token mint unreachable: %s", exc)
            return None
        if r.status_code != 200:
            log.error("token mint failed: HTTP %d", r.status_code)
            return None
        return (r.json() or {}).get("access_token") or None

    def run_agent(self, job: TriggerJob) -> None:
        """Resolve the paca assistant, stream one chat turn, log the outcome.
        Mirrors the Teams-bot glue (agentops routers/teams_bot.py)."""
        import httpx

        token = self.mint_actor_token(job.actor)
        if not token:
            log.error(
                "trigger dropped (%s %s by %s): no actor token",
                job.kind, job.task.human_ref, job.actor.username,
            )
            return
        headers = {"Authorization": f"Bearer {token}",
                   "Content-Type": "application/json"}
        # Stable thread per task+actor: re-assignments/mentions of the same
        # task by the same person continue one conversation (context carries).
        thread_id = str(uuidlib.uuid5(
            uuidlib.NAMESPACE_URL,
            f"paca-dock-trigger:{job.task.task_id}:{job.actor.oidc_sub}",
        ))
        started = time.monotonic()
        outcome, reply_len = "error", 0
        try:
            with httpx.Client(timeout=httpx.Timeout(30.0, read=self.run_timeout_s)) as client:
                assistant_id = None
                r = client.get(
                    f"{self.agentops_url}/api/assistants/by-app/paca", headers=headers
                )
                if r.status_code == 200:
                    assistant_id = (r.json() or {}).get("id")
                if not assistant_id:
                    log.error("paca assistant lookup failed: HTTP %d", r.status_code)
                    return
                payload = {
                    "messages": [{"role": "user", "content": build_prompt(job, self.public_url)}],
                    "thread_id": thread_id,
                    "assistant_id": assistant_id,
                    "app_context": {"app_id": "paca", "source": "dock-trigger",
                                    "task_id": job.task.task_id},
                }
                parts: list[str] = []
                deadline = time.monotonic() + self.run_timeout_s
                with client.stream(
                    "POST", f"{self.agentops_url}/api/chat/stream",
                    headers=headers, json=payload,
                ) as resp:
                    ev = None
                    for line in resp.iter_lines():
                        if time.monotonic() > deadline:
                            outcome = "timeout"
                            break
                        if line.startswith("event:"):
                            ev = line[6:].strip()
                        elif line.startswith("data:") and ev in ("message_chunk", "message"):
                            raw = line[5:].strip()
                            if not raw:
                                continue
                            try:
                                j = json.loads(raw)
                            except ValueError:
                                continue
                            if j.get("agent") == "reporter" and isinstance(j.get("content"), str):
                                parts.append(j["content"])
                    else:
                        outcome = "ok"
                reply_len = len("".join(parts).strip())
        except Exception as exc:  # noqa: BLE001 — log, never crash the loop
            log.error(
                "agent run failed (%s %s by %s): %s",
                job.kind, job.task.human_ref, job.actor.username, exc,
            )
            return
        log.info(
            "agent run %s: kind=%s task=%s actor=%s agent_user=%s "
            "reply_chars=%d elapsed=%.0fs",
            outcome, job.kind, job.task.human_ref, job.actor.username,
            job.agent_username, reply_len, time.monotonic() - started,
        )

    # -- stream plumbing (own consumer group) ---------------------------------

    def ensure_groups(self) -> None:
        import redis

        for stream in (STREAM_ASSIGNMENTS, STREAM_PLUGIN_EVENTS):
            try:
                self.paca().xgroup_create(stream, CONSUMER_GROUP, id="$", mkstream=True)
                log.info("created consumer group %s on %s", CONSUMER_GROUP, stream)
            except redis.ResponseError as exc:
                if "BUSYGROUP" not in str(exc):
                    raise

    def handle_entry(self, stream: str, entry_id: str, fields: dict[str, Any]) -> None:
        try:
            etype, payload = parse_stream_entry(fields)
        except ValueError as exc:
            log.warning("%s %s: unparseable entry (%s) — acked", stream, entry_id, exc)
            self.paca().xack(stream, CONSUMER_GROUP, entry_id)
            return

        job: Optional[TriggerJob] = None
        if stream == STREAM_ASSIGNMENTS and etype == TYPE_TASK_ASSIGNED:
            job = self.job_from_assignment(payload)
        elif stream == STREAM_PLUGIN_EVENTS and etype == TYPE_COMMENT_ADDED:
            job = self.job_from_comment(payload)

        # At-most-once: ack BEFORE dispatch (see module docstring).
        self.paca().xack(stream, CONSUMER_GROUP, entry_id)
        if job is None:
            return
        log.info(
            "trigger: kind=%s task=%s (%s) actor=%s agent_user=%s",
            job.kind, job.task.human_ref, job.task.title[:60],
            job.actor.username, job.agent_username,
        )
        self.run_agent(job)

    def drain(self, start_id: str) -> int:
        resp = self.paca().xreadgroup(
            CONSUMER_GROUP,
            self.consumer,
            {STREAM_ASSIGNMENTS: start_id, STREAM_PLUGIN_EVENTS: start_id},
            count=bridge_lib.READ_COUNT,
            block=bridge_lib.READ_BLOCK_MS if start_id == ">" else None,
        )
        handled = 0
        for stream, entries in resp or []:
            for entry_id, fields in entries:
                if self.stopping:
                    return handled
                self.handle_entry(stream, entry_id, fields)
                handled += 1
        return handled


def main() -> int:
    logging.basicConfig(
        level=os.getenv("LOG_LEVEL", "INFO").upper(),
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    trigger = DockTrigger()

    def _stop(signum: int, _frame: Any) -> None:
        log.info("signal %d — stopping", signum)
        trigger.stopping = True

    signal.signal(signal.SIGTERM, _stop)
    signal.signal(signal.SIGINT, _stop)

    if os.getenv("DOCK_TRIGGER_ENABLED", "false").strip().lower() != "true":
        log.info("DOCK_TRIGGER_ENABLED != true — dock-trigger idle")
        while not trigger.stopping:
            time.sleep(5)
        return 0

    log.info(
        "starting: streams=%s,%s group=%s agentops=%s trigger_usernames=%s",
        STREAM_ASSIGNMENTS, STREAM_PLUGIN_EVENTS, CONSUMER_GROUP,
        trigger.agentops_url, sorted(trigger.trigger_usernames),
    )
    trigger.run()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
