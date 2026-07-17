"""Unit tests for the notify-bridge pure mapping/filter functions.

Run from the repo root with:

    uv run --with pytest,redis,"psycopg[binary]" \
        pytest deploy/galaxy/notify-bridge/tests -q

(redis/psycopg are only needed because main.py's I/O layer imports them
lazily inside Bridge — the functions under test here are pure.)
"""

from __future__ import annotations

import json
import pathlib
import sys

import pytest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

import main  # noqa: E402
from main import (  # noqa: E402
    Member,
    ProjectUser,
    TaskInfo,
    build_fanout_event,
    extract_team_mention_ids,
    extract_text_from_blocks,
    extract_username_mentions,
    map_assignment,
    map_comment_mentions,
    parse_stream_entry,
    publish_events,
    task_deeplink,
)

# ── fixtures ────────────────────────────────────────────────────────────────

UUID_TASK = "11111111-1111-1111-1111-111111111111"
UUID_PROJECT = "22222222-2222-2222-2222-222222222222"
UUID_MEMBER = "33333333-3333-3333-3333-333333333333"
UUID_USER = "44444444-4444-4444-4444-444444444444"
UUID_ACTOR_USER = "55555555-5555-5555-5555-555555555555"
UUID_ACTOR_MEMBER = "66666666-6666-6666-6666-666666666666"
SUB = "99999999-9999-9999-9999-999999999999"

TASK = TaskInfo(
    task_id=UUID_TASK,
    project_id=UUID_PROJECT,
    title="Fix login flow",
    number=12,
    project_name="Paca",
    prefix="PACA",
)

HUMAN = Member(
    member_id=UUID_MEMBER,
    user_id=UUID_USER,
    is_agent=False,
    oidc_sub=SUB,
    username="alice",
)


def assignment_payload(**overrides):
    payload = {
        "task_id": UUID_TASK,
        "project_id": UUID_PROJECT,
        "new_assignee_member_id": UUID_MEMBER,
        "actor_user_id": UUID_ACTOR_USER,
    }
    payload.update(overrides)
    return payload


def blocks_with_mention(user_id: str) -> str:
    return json.dumps(
        [
            {
                "content": [
                    {"type": "text", "text": "please look at this "},
                    {"type": "teamMention", "props": {"id": user_id, "name": "Alice"}},
                    {"type": "taskReference", "props": {"id": "not-a-mention"}},
                ]
            }
        ]
    )


def comment_payload(content: str, **overrides):
    payload = {
        "id": "77777777-7777-7777-7777-777777777777",
        "task_id": UUID_TASK,
        "project_id": UUID_PROJECT,
        "activity_type": "comment",
        "content": content,
        "actor_id": UUID_ACTOR_MEMBER,
    }
    payload.update(overrides)
    return payload


# ── parse_stream_entry ──────────────────────────────────────────────────────


def test_parse_stream_entry_decodes_type_and_payload():
    etype, payload = parse_stream_entry(
        {"type": "task.assigned", "payload": json.dumps({"task_id": UUID_TASK})}
    )
    assert etype == "task.assigned"
    assert payload == {"task_id": UUID_TASK}


def test_parse_stream_entry_accepts_bytes_fields():
    etype, payload = parse_stream_entry(
        {b"type": b"x", "type": b"task.assigned", "payload": b'{"a": 1}'}
    )
    assert etype == "task.assigned"
    assert payload == {"a": 1}


@pytest.mark.parametrize(
    "fields",
    [
        {},
        {"type": "task.assigned"},
        {"type": "task.assigned", "payload": "not-json"},
        {"type": "task.assigned", "payload": '["not", "an", "object"]'},
    ],
)
def test_parse_stream_entry_rejects_malformed(fields):
    with pytest.raises(ValueError):
        parse_stream_entry(fields)


# ── BlockNote extraction ────────────────────────────────────────────────────


def test_extract_text_from_blocks_joins_inline_text():
    raw = json.dumps(
        [
            {"content": [{"type": "text", "text": "hello"}, {"type": "text", "text": "world"}]},
            {"content": [{"type": "teamMention", "props": {"id": "x"}}]},
        ]
    )
    assert extract_text_from_blocks(raw) == "hello world"


def test_extract_text_from_blocks_legacy_object():
    assert extract_text_from_blocks('{"text": "legacy body"}') == "legacy body"


def test_extract_text_from_blocks_garbage_is_empty():
    assert extract_text_from_blocks("not json") == ""


def test_extract_team_mention_ids_only_team_mentions():
    assert extract_team_mention_ids(blocks_with_mention(UUID_USER)) == [UUID_USER]


def test_extract_username_mentions():
    assert extract_username_mentions("hey @alice and @bob.dev!") == {"alice", "bob.dev"}


# ── event shape ─────────────────────────────────────────────────────────────


def test_build_fanout_event_matches_house_schema():
    event = build_fanout_event(SUB, "task_assigned", "t", "b", "https://x/y", {"k": 1})
    assert event == {
        "user_ids": [SUB],
        "type": "task_assigned",
        "title": "t",
        "body": "b",
        "deeplink": "https://x/y",
        "payload": {"k": 1},
        "source_app": "paca",
    }


def test_task_deeplink_shape():
    assert (
        task_deeplink("https://tasks.skyplatform.net/", UUID_PROJECT, UUID_TASK)
        == f"https://tasks.skyplatform.net/projects/{UUID_PROJECT}/tasks/{UUID_TASK}"
    )


# ── map_assignment ──────────────────────────────────────────────────────────


def test_map_assignment_happy_path():
    events = map_assignment(
        assignment_payload(),
        resolve_member=lambda mid: HUMAN,
        resolve_task=lambda tid: TASK,
        base_url="https://tasks.skyplatform.net",
    )
    assert len(events) == 1
    event = events[0]
    assert event["user_ids"] == [SUB]
    assert event["type"] == "task_assigned"
    assert "PACA-12" in event["title"]
    assert event["body"] == "Fix login flow"
    assert event["deeplink"].endswith(f"/projects/{UUID_PROJECT}/tasks/{UUID_TASK}")
    assert event["payload"]["paca_type"] == "assigned"
    assert event["source_app"] == "paca"


def test_map_assignment_skips_self_assignment():
    payload = assignment_payload(actor_user_id=UUID_USER)
    assert (
        map_assignment(payload, lambda m: HUMAN, lambda t: TASK, "https://x") == []
    )


def test_map_assignment_skips_agent_assignee():
    agent = Member(member_id=UUID_MEMBER, user_id=None, is_agent=True, oidc_sub=None)
    assert (
        map_assignment(assignment_payload(), lambda m: agent, lambda t: TASK, "https://x")
        == []
    )


def test_map_assignment_skips_user_without_oidc_sub():
    unlinked = Member(
        member_id=UUID_MEMBER, user_id=UUID_USER, is_agent=False, oidc_sub=None
    )
    assert (
        map_assignment(assignment_payload(), lambda m: unlinked, lambda t: TASK, "https://x")
        == []
    )


def test_map_assignment_skips_missing_member_or_task():
    assert (
        map_assignment(assignment_payload(), lambda m: None, lambda t: TASK, "https://x")
        == []
    )
    assert (
        map_assignment(assignment_payload(), lambda m: HUMAN, lambda t: None, "https://x")
        == []
    )


def test_map_assignment_skips_malformed_ids():
    payload = assignment_payload(new_assignee_member_id="not-a-uuid")
    assert map_assignment(payload, lambda m: HUMAN, lambda t: TASK, "https://x") == []


def test_map_assignment_carries_workflow_attribution():
    payload = assignment_payload(workflow_name="Auto-triage")
    events = map_assignment(payload, lambda m: HUMAN, lambda t: TASK, "https://x")
    assert events[0]["payload"]["workflow_name"] == "Auto-triage"


# ── map_comment_mentions ────────────────────────────────────────────────────

ALICE = ProjectUser(user_id=UUID_USER, username="alice", oidc_sub=SUB)
ACTOR = ProjectUser(user_id=UUID_ACTOR_USER, username="carol", oidc_sub="88888888-8888-8888-8888-888888888888")
ACTOR_MEMBER = Member(
    member_id=UUID_ACTOR_MEMBER,
    user_id=UUID_ACTOR_USER,
    is_agent=False,
    oidc_sub=ACTOR.oidc_sub,
    username="carol",
)


def resolve_actor(member_id):
    return ACTOR_MEMBER if member_id == UUID_ACTOR_MEMBER else None


def test_map_comment_mentions_structured():
    events = map_comment_mentions(
        comment_payload(blocks_with_mention(UUID_USER)),
        resolve_member=resolve_actor,
        list_project_users=lambda pid: [ALICE, ACTOR],
        resolve_task=lambda tid: TASK,
        base_url="https://tasks.skyplatform.net",
    )
    assert len(events) == 1
    event = events[0]
    assert event["user_ids"] == [SUB]
    assert event["type"] == "task_mentioned"
    assert "PACA-12" in event["title"]
    assert "please look at this" in event["body"]
    assert event["payload"]["paca_type"] == "mentioned"


def test_map_comment_mentions_skips_self_mention():
    events = map_comment_mentions(
        comment_payload(blocks_with_mention(UUID_ACTOR_USER)),
        resolve_member=resolve_actor,
        list_project_users=lambda pid: [ALICE, ACTOR],
        resolve_task=lambda tid: TASK,
        base_url="https://x",
    )
    assert events == []


def test_map_comment_mentions_agent_mention_id_drops_out():
    # Agent mentions embed agents.id, which is not a project user id.
    events = map_comment_mentions(
        comment_payload(blocks_with_mention("abcdefab-0000-0000-0000-000000000000")),
        resolve_member=resolve_actor,
        list_project_users=lambda pid: [ALICE, ACTOR],
        resolve_task=lambda tid: TASK,
        base_url="https://x",
    )
    assert events == []


def test_map_comment_mentions_username_fallback():
    content = json.dumps([{"content": [{"type": "text", "text": "ping @ALICE please"}]}])
    events = map_comment_mentions(
        comment_payload(content),
        resolve_member=resolve_actor,
        list_project_users=lambda pid: [ALICE, ACTOR],
        resolve_task=lambda tid: TASK,
        base_url="https://x",
    )
    assert len(events) == 1
    assert events[0]["user_ids"] == [SUB]


def test_map_comment_mentions_dedups_repeated_mentions():
    content = json.dumps(
        [
            {
                "content": [
                    {"type": "teamMention", "props": {"id": UUID_USER, "name": "Alice"}},
                    {"type": "teamMention", "props": {"id": UUID_USER, "name": "Alice"}},
                ]
            }
        ]
    )
    events = map_comment_mentions(
        comment_payload(content),
        resolve_member=resolve_actor,
        list_project_users=lambda pid: [ALICE],
        resolve_task=lambda tid: TASK,
        base_url="https://x",
    )
    assert len(events) == 1


def test_map_comment_mentions_skips_unlinked_user():
    unlinked = ProjectUser(user_id=UUID_USER, username="alice", oidc_sub=None)
    events = map_comment_mentions(
        comment_payload(blocks_with_mention(UUID_USER)),
        resolve_member=resolve_actor,
        list_project_users=lambda pid: [unlinked],
        resolve_task=lambda tid: TASK,
        base_url="https://x",
    )
    assert events == []


def test_map_comment_mentions_no_mentions_no_events():
    content = json.dumps([{"content": [{"type": "text", "text": "no mentions here"}]}])
    events = map_comment_mentions(
        comment_payload(content),
        resolve_member=resolve_actor,
        list_project_users=lambda pid: [ALICE],
        resolve_task=lambda tid: TASK,
        base_url="https://x",
    )
    assert events == []


# ── publish path (mock redis) ───────────────────────────────────────────────


class FakeRedis:
    """Minimal redis stand-in recording publishes."""

    def __init__(self):
        self.published = []

    def publish(self, channel, message):
        self.published.append((channel, message))
        return 1


def test_publish_events_publishes_one_message_per_event():
    fake = FakeRedis()
    events = [
        build_fanout_event(SUB, "task_assigned", "t1", None, "https://x/1", {}),
        build_fanout_event(SUB, "task_mentioned", "t2", "b", "https://x/2", {}),
    ]
    count = publish_events(fake, "notify.fan-out", events)
    assert count == 2
    assert [c for c, _ in fake.published] == ["notify.fan-out", "notify.fan-out"]
    decoded = [json.loads(m) for _, m in fake.published]
    assert decoded[0]["type"] == "task_assigned"
    assert decoded[1]["deeplink"] == "https://x/2"
    # The fan-out consumer requires UUID user ids — assert we kept them intact.
    assert decoded[0]["user_ids"] == [SUB]


def test_publish_events_propagates_redis_errors():
    class BrokenRedis:
        def publish(self, channel, message):
            raise ConnectionError("redis down")

    with pytest.raises(ConnectionError):
        publish_events(BrokenRedis(), "notify.fan-out", [build_fanout_event(SUB, "t", "t", None, "d", {})])
