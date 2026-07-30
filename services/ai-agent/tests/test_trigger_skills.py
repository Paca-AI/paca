"""Tests for deterministic per-trigger-type context (src/agent/trigger_skills.py)."""

from openhands.sdk import AgentContext
from openhands.sdk.context import Skill

from src.agent.trigger_skills import (
    _SKILL_ROUTING_PROCEDURE,
    append_trigger_skill,
    get_trigger_skill,
)

# ─── get_trigger_skill ────────────────────────────────────────────────────────


def test_task_assigned_returns_trigger_task_assigned():
    skill = get_trigger_skill("task_assigned", task_id=None)
    assert skill is not None
    assert skill.name == "paca-trigger-task-assigned"
    assert skill.trigger is None


def test_task_comment_mention_returns_trigger_task_assigned():
    skill = get_trigger_skill("comment_mention", task_id="task-1")
    assert skill is not None
    assert skill.name == "paca-trigger-task-assigned"


def test_doc_comment_mention_returns_trigger_doc_comment():
    skill = get_trigger_skill("comment_mention", task_id=None)
    assert skill is not None
    assert skill.name == "paca-trigger-doc-comment"


def test_chat_message_returns_trigger_chat():
    skill = get_trigger_skill("chat_message", task_id=None)
    assert skill is not None
    assert skill.name == "paca-trigger-chat"


def test_description_write_returns_trigger_description_write():
    skill = get_trigger_skill("description_write", task_id=None)
    assert skill is not None
    assert skill.name == "paca-trigger-description-write"


def test_unknown_trigger_type_returns_none():
    assert get_trigger_skill("something_else", task_id=None) is None


def test_task_assigned_human_ignores_is_automation_default():
    """Default is_automation=False keeps existing human-assignment behavior."""
    skill = get_trigger_skill("task_assigned", task_id=None)
    assert skill.name == "paca-trigger-task-assigned"


def test_task_assigned_automation_returns_trigger_automation():
    skill = get_trigger_skill("task_assigned", task_id=None, is_automation=True)
    assert skill is not None
    assert skill.name == "paca-trigger-automation"
    assert skill.trigger is None


def test_task_assigned_automation_reuses_normal_skill_routing_procedure():
    """The automation-triggered prompt only changes invocation framing (no
    human present) — the actual task-assignment workflow (skill routing) is
    identical to the human-triggered path, not a separate procedure."""
    human_skill = get_trigger_skill("task_assigned", task_id=None)
    automation_skill = get_trigger_skill("task_assigned", task_id=None, is_automation=True)
    assert _SKILL_ROUTING_PROCEDURE in human_skill.content
    assert _SKILL_ROUTING_PROCEDURE in automation_skill.content


def test_automation_message_returns_trigger_automation():
    skill = get_trigger_skill("automation_message", task_id=None)
    assert skill is not None
    assert skill.name == "paca-trigger-automation"
    assert skill.trigger is None


def test_task_assigned_automation_and_automation_message_share_reserved_name():
    """Both automation-triggered variants use the same reserved skill name —
    a conversation only ever gets one of the two, so there's no collision
    risk, but the name must match what's registered in Go's
    ReservedSkillNames (entity.go) either way."""
    task_variant = get_trigger_skill("task_assigned", task_id=None, is_automation=True)
    message_variant = get_trigger_skill("automation_message", task_id=None)
    assert task_variant.name == message_variant.name == "paca-trigger-automation"


def test_all_trigger_skills_are_always_active():
    """`trigger=None` is load-bearing: these are chosen deterministically by
    trigger type, not something the model should discover via <available_skills>."""
    for trigger_type, task_id in [
        ("task_assigned", None),
        ("comment_mention", "task-1"),
        ("comment_mention", None),
        ("chat_message", None),
        ("description_write", None),
        ("automation_message", None),
    ]:
        skill = get_trigger_skill(trigger_type, task_id)
        assert skill.trigger is None


def test_all_trigger_skills_are_paca_prefixed():
    """The paca- prefix groups these with the rest of the default skill set
    (paca, paca-do, paca-doc, ...) so the reserved-name check on the API side
    (agent_service.go) and a human skimming an agent's skill list can both
    recognize them as internal at a glance."""
    for trigger_type, task_id in [
        ("task_assigned", None),
        ("comment_mention", "task-1"),
        ("comment_mention", None),
        ("chat_message", None),
        ("description_write", None),
        ("automation_message", None),
    ]:
        skill = get_trigger_skill(trigger_type, task_id)
        assert skill.name.startswith("paca-")


# ─── append_trigger_skill ─────────────────────────────────────────────────────


def test_append_trigger_skill_appends_when_no_collision():
    skills: list[Skill] = [Skill(name="paca", content="...", trigger=None)]
    append_trigger_skill(skills, "chat_message", None, "conv-1")
    assert [s.name for s in skills] == ["paca", "paca-trigger-chat"]


def test_append_trigger_skill_is_noop_for_unknown_trigger_type():
    skills: list[Skill] = [Skill(name="paca", content="...", trigger=None)]
    append_trigger_skill(skills, "something_else", None, "conv-1")
    assert [s.name for s in skills] == ["paca"]


def test_append_trigger_skill_passes_through_is_automation():
    skills: list[Skill] = [Skill(name="paca", content="...", trigger=None)]
    append_trigger_skill(skills, "task_assigned", None, "conv-1", is_automation=True)
    assert [s.name for s in skills] == ["paca", "paca-trigger-automation"]


def test_append_trigger_skill_defaults_to_human_task_assignment():
    skills: list[Skill] = [Skill(name="paca", content="...", trigger=None)]
    append_trigger_skill(skills, "task_assigned", None, "conv-1")
    assert [s.name for s in skills] == ["paca", "paca-trigger-task-assigned"]


def test_append_trigger_skill_skips_on_name_collision():
    """Regression test: a user-configured skill can legally be named
    'paca-trigger-chat' (nothing stops it client- or server-side beyond the
    API's reserved-name check, which is a separate, defense-in-depth layer —
    see agent_service.go). If it collides with this conversation's trigger
    skill, the user's skill must win, not crash the conversation."""
    user_skill = Skill(name="paca-trigger-chat", content="user content", trigger=None)
    skills: list[Skill] = [user_skill]

    append_trigger_skill(skills, "chat_message", None, "conv-1")

    assert len(skills) == 1
    assert skills[0] is user_skill
    assert skills[0].content == "user content"


def test_append_trigger_skill_collision_does_not_crash_agent_context():
    """End-to-end confirmation of the fix: constructing AgentContext with the
    result of append_trigger_skill must never raise, even on a collision.
    (AgentContext raises ValueError on any duplicate skill name — this is the
    exact crash this function exists to prevent.)"""
    skills: list[Skill] = [Skill(name="paca-trigger-chat", content="user content", trigger=None)]
    append_trigger_skill(skills, "chat_message", None, "conv-1")

    AgentContext(skills=skills, system_message_suffix="")  # must not raise
