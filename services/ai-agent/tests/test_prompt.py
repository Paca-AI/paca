"""Tests for the prompt and system-suffix builders."""

from src.agent.prompt import (
    build_acp_message,
    build_initial_prompt,
    build_project_context_suffix,
    build_trigger_suffix,
)
from src.core.streams import TriggerMessage


def _trigger(**kwargs) -> TriggerMessage:
    defaults = dict(
        stream_id="1-1",
        trigger_type="task_assigned",
        conversation_id="conv-123",
        agent_id="agent-456",
        project_id="proj-789",
        task_id=None,
        comment_id=None,
        chat_session_id=None,
        message="Implement feature X",
        actor_member_id="member-001",
        repo_plugin_ids=[],
    )
    return TriggerMessage(**{**defaults, **kwargs})


# ── build_initial_prompt ──────────────────────────────────────────────────────


def test_initial_prompt_returns_message_when_present():
    result = build_initial_prompt(_trigger(message="Do something"))
    assert result == "Do something"


def test_initial_prompt_task_assigned_fallback():
    result = build_initial_prompt(
        _trigger(trigger_type="task_assigned", message="")
    )
    assert result  # not empty
    assert "assigned" in result.lower()


def test_initial_prompt_empty_stays_empty_for_other_triggers():
    """Non-task-assigned triggers keep their (possibly empty) message as-is."""
    result = build_initial_prompt(
        _trigger(trigger_type="chat_message", message="")
    )
    assert result == ""


# ── build_project_context_suffix ──────────────────────────────────────────────


def test_project_id_appears_in_suffix():
    result = build_project_context_suffix("proj-123")
    assert "proj-123" in result


def test_suffix_instructs_agent_to_pass_project_id():
    result = build_project_context_suffix("proj-abc")
    assert "projectId" in result


def test_suffix_discourages_list_projects_call():
    result = build_project_context_suffix("proj-abc")
    assert "list_projects" in result


def test_different_project_ids_produce_different_suffixes():
    assert build_project_context_suffix("proj-1") != build_project_context_suffix("proj-2")


# ── build_acp_message ─────────────────────────────────────────────────────────


def test_acp_message_prefixes_paca_skill():
    result = build_acp_message(_trigger(message="Do something"))
    assert result.startswith("/paca Do something")


def test_acp_message_uses_codex_skill_syntax():
    result = build_acp_message(_trigger(message="Do something"), "codex")
    assert result.startswith("$paca Do something")


def test_acp_message_keeps_slash_syntax_for_other_providers():
    for provider in ("claude-code", "gemini-cli", "custom", None):
        result = build_acp_message(_trigger(message="Do something"), provider)
        assert result.startswith("/paca Do something")


def test_acp_message_uses_task_assigned_fallback():
    result = build_acp_message(_trigger(trigger_type="task_assigned", message=""))
    assert result.startswith("/paca ")
    assert "assigned" in result.lower()


def test_acp_message_includes_project_context():
    result = build_acp_message(_trigger(project_id="proj-789"))
    assert "proj-789" in result
    assert "projectId" in result


def test_acp_message_includes_trigger_context():
    result = build_acp_message(_trigger(trigger_type="task_assigned", task_id="task-42"))
    assert "Action type: Task assignment" in result
    assert "task-42" in result


def test_acp_message_omits_repo_instructions():
    """ACP agents already have local repo access — clone_repository is a
    sandbox-only tool, so the repo section of build_trigger_suffix must not
    be folded into the ACP message even when the trigger has linked repos."""
    result = build_acp_message(_trigger(repo_plugin_ids=["plugin-1"]))
    assert "clone_repository" not in result
    assert "Repository" not in result


def test_action_type_task_assigned():
    result = build_trigger_suffix(_trigger(trigger_type="task_assigned"))
    assert "Action type: Task assignment" in result


def test_action_type_task_comment_mention():
    result = build_trigger_suffix(
        _trigger(trigger_type="comment_mention", task_id="task-1")
    )
    assert "Action type: Task comment mention" in result


def test_action_type_doc_comment_mention():
    result = build_trigger_suffix(
        _trigger(trigger_type="comment_mention", task_id=None)
    )
    assert "Action type: Document comment mention" in result


def test_action_type_chat_message():
    result = build_trigger_suffix(_trigger(trigger_type="chat_message"))
    assert "Action type: Direct chat message" in result


def test_action_type_description_write():
    result = build_trigger_suffix(_trigger(trigger_type="description_write"))
    assert "Action type: Write task description" in result


def test_task_id_included_when_set():
    result = build_trigger_suffix(_trigger(task_id="task-42"))
    assert "task-42" in result


def test_task_id_absent_when_none():
    result = build_trigger_suffix(_trigger(task_id=None))
    assert "Task ID" not in result


def test_comment_id_included_when_set():
    result = build_trigger_suffix(_trigger(comment_id="cmt-7"))
    assert "cmt-7" in result


def test_no_repo_section_without_repos():
    result = build_trigger_suffix(_trigger(), all_repos=None)
    assert "Repository" not in result


def test_single_repo_inline_clone_instructions():
    repo = {
        "plugin_id": "plugin-1",
        "repo_id": "repo-99",
        "full_name": "org/myrepo",
        "clone_url": "https://github.com/org/myrepo.git",
    }
    result = build_trigger_suffix(_trigger(), all_repos=[repo])
    assert "plugin-1" in result
    assert "repo-99" in result
    assert "org/myrepo" in result


def test_multiple_repos_list_repositories_hint():
    repos = [
        {"plugin_id": "p1", "repo_id": "r1", "full_name": "org/a", "clone_url": ""},
        {"plugin_id": "p2", "repo_id": "r2", "full_name": "org/b", "clone_url": ""},
    ]
    result = build_trigger_suffix(_trigger(), all_repos=repos)
    assert "list_repositories" in result
