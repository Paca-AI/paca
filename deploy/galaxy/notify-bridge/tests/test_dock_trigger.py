"""Unit tests for the dock-trigger event→job mapping (no I/O).

Run from the repo root with:

    uv run --with pytest,redis,"psycopg[binary]",httpx \
        pytest deploy/galaxy/notify-bridge/tests -q

DockTrigger's resolver methods are stubbed; connections are lazy so
nothing dials Valkey/Postgres/identity here.
"""

from __future__ import annotations

import json
import pathlib
import sys

import pytest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

import dock_trigger as dt  # noqa: E402
from main import Member, ProjectUser, TaskInfo  # noqa: E402

UUID_TASK = "11111111-1111-1111-1111-111111111111"
UUID_PROJECT = "22222222-2222-2222-2222-222222222222"
UUID_AGENT_MEMBER = "33333333-3333-3333-3333-333333333333"
UUID_AGENT_USER = "44444444-4444-4444-4444-444444444444"
UUID_ACTOR_USER = "55555555-5555-5555-5555-555555555555"
UUID_ACTOR_MEMBER = "66666666-6666-6666-6666-666666666666"
ACTOR_SUB = "99999999-9999-9999-9999-999999999999"

TASK = TaskInfo(
    task_id=UUID_TASK, project_id=UUID_PROJECT, title="Phân tích yêu cầu",
    number=7, project_name="Galaxy Paca Pilot", prefix="GXP",
)
AGENT_MEMBER = Member(
    member_id=UUID_AGENT_MEMBER, user_id=UUID_AGENT_USER, is_agent=False,
    oidc_sub=None, username="galaxy-tasks-agent", full_name="Galaxy Tasks Agent",
)
ACTOR_MEMBER = Member(
    member_id=UUID_ACTOR_MEMBER, user_id=UUID_ACTOR_USER, is_agent=False,
    oidc_sub=ACTOR_SUB, username="cao.phan", full_name="Cao Phan",
)
ACTOR = dt.UserRow(
    user_id=UUID_ACTOR_USER, username="cao.phan", full_name="Cao Phan",
    email="cao.phan@galaxytechnology.vn", oidc_sub=ACTOR_SUB,
)


@pytest.fixture
def trigger(monkeypatch):
    monkeypatch.setenv("DOCK_AGENT_TRIGGER_USERNAMES", "galaxy-tasks-agent")
    t = dt.DockTrigger()
    members = {UUID_AGENT_MEMBER: AGENT_MEMBER, UUID_ACTOR_MEMBER: ACTOR_MEMBER}
    users = {UUID_ACTOR_USER: ACTOR}
    monkeypatch.setattr(t, "resolve_member", lambda mid: members.get(mid))
    monkeypatch.setattr(t, "resolve_user", lambda uid: users.get(uid))
    monkeypatch.setattr(t, "resolve_task", lambda tid: TASK if tid == UUID_TASK else None)
    monkeypatch.setattr(t, "list_project_users", lambda pid: [
        ProjectUser(user_id=UUID_AGENT_USER, username="galaxy-tasks-agent", oidc_sub=None),
        ProjectUser(user_id=UUID_ACTOR_USER, username="cao.phan", oidc_sub=ACTOR_SUB),
    ])
    return t


def assignment_payload(**over):
    p = {"task_id": UUID_TASK, "project_id": UUID_PROJECT,
         "new_assignee_member_id": UUID_AGENT_MEMBER,
         "actor_user_id": UUID_ACTOR_USER}
    p.update(over)
    return p


def mention_content(target_user_id):
    return json.dumps([{"content": [
        {"type": "text", "text": "nhờ phân tích giúp "},
        {"type": "teamMention", "props": {"id": target_user_id, "name": "Agent"}},
    ]}])


def comment_payload(content, **over):
    p = {"id": "7777", "task_id": UUID_TASK, "project_id": UUID_PROJECT,
         "activity_type": "comment", "content": content,
         "actor_id": UUID_ACTOR_MEMBER}
    p.update(over)
    return p


def test_assignment_to_agent_user_triggers(trigger):
    job = trigger.job_from_assignment(assignment_payload())
    assert job is not None and job.kind == "assigned"
    assert job.actor.oidc_sub == ACTOR_SUB
    assert job.agent_username == "galaxy-tasks-agent"
    assert job.task.human_ref == "GXP-7"


def test_assignment_to_normal_user_ignored(trigger):
    job = trigger.job_from_assignment(
        assignment_payload(new_assignee_member_id=UUID_ACTOR_MEMBER)
    )
    assert job is None


def test_assignment_by_agent_itself_is_loop_guarded(trigger, monkeypatch):
    agent_actor = dt.UserRow(UUID_AGENT_USER, "galaxy-tasks-agent",
                             "Galaxy Tasks Agent", None, "some-sub")
    monkeypatch.setattr(trigger, "resolve_user",
                        lambda uid: agent_actor if uid == UUID_AGENT_USER else None)
    job = trigger.job_from_assignment(
        assignment_payload(actor_user_id=UUID_AGENT_USER)
    )
    assert job is None


def test_assignment_actor_without_oidc_sub_skipped(trigger, monkeypatch):
    no_sub = dt.UserRow(UUID_ACTOR_USER, "cao.phan", "Cao Phan", None, None)
    monkeypatch.setattr(trigger, "resolve_user", lambda uid: no_sub)
    assert trigger.job_from_assignment(assignment_payload()) is None


def test_structured_mention_of_agent_triggers(trigger):
    job = trigger.job_from_comment(comment_payload(mention_content(UUID_AGENT_USER)))
    assert job is not None and job.kind == "mentioned"
    assert job.actor.username == "cao.phan"
    assert "nhờ phân tích giúp" in job.comment_text


def test_plaintext_mention_fallback_triggers(trigger):
    content = json.dumps([{"content": [
        {"type": "text", "text": "cc @galaxy-tasks-agent xử lý giúp"}]}])
    job = trigger.job_from_comment(comment_payload(content))
    assert job is not None and job.agent_username == "galaxy-tasks-agent"


def test_mention_of_other_user_ignored(trigger):
    job = trigger.job_from_comment(comment_payload(mention_content(UUID_ACTOR_USER)))
    assert job is None


def test_agent_authored_comment_is_loop_guarded(trigger):
    payload = comment_payload(mention_content(UUID_AGENT_USER),
                              actor_agent_id="some-agent")
    assert trigger.job_from_comment(payload) is None


def test_comment_by_trigger_principal_is_loop_guarded(trigger, monkeypatch):
    members = {UUID_ACTOR_MEMBER: AGENT_MEMBER}  # comment authored by the agent user
    monkeypatch.setattr(trigger, "resolve_member", lambda mid: members.get(mid))
    payload = comment_payload(mention_content(UUID_AGENT_USER))
    assert trigger.job_from_comment(payload) is None


def test_prompt_contains_ids_and_loop_guard(trigger):
    job = trigger.job_from_assignment(assignment_payload())
    prompt = dt.build_prompt(job, "https://tasks.skyplatform.net")
    assert UUID_TASK in prompt and UUID_PROJECT in prompt
    assert "GXP-7" in prompt
    assert "paca_comment_add" in prompt
    assert "@galaxy-tasks-agent" in prompt  # the do-NOT-mention instruction
