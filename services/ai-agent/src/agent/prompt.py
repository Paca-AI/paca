"""System-suffix and initial-prompt builders for agent conversations."""

from __future__ import annotations

from ..core.streams import TriggerMessage

_ACTION_TYPE_LABELS = {
    "task_assigned": "Task assignment",
    "chat_message": "Direct chat message",
    "description_write": "Write task description",
}


def _action_type_label(trigger: TriggerMessage) -> str:
    if trigger.trigger_type == "comment_mention":
        # task_id is set for task-comment mentions; absent for doc-comment mentions.
        return "Task comment mention" if trigger.task_id else "Document comment mention"
    return _ACTION_TYPE_LABELS.get(trigger.trigger_type, trigger.trigger_type)


# Fallback message for task-assignment triggers that arrive with no note.
# A human task-assignment fires with trigger.message == "" (the note is only
# populated by the automation-workflow engine — see Go's
# agentAssignmentNote()). Sending an empty message to the SDK errors out, so
# substitute this placeholder that tells the agent to get started.
_TASK_ASSIGNED_DEFAULT = (
    "You have been assigned a task. Load it via the Paca MCP tool and "
    "follow the default `paca` skill's routing to pick the right "
    "specialized skill for its status."
)


def build_initial_prompt(trigger: TriggerMessage) -> str:
    """Construct the first message sent to the agent.

    Uses ``trigger.message`` directly, but falls back to a sensible default
    for task-assignment triggers that arrive with an empty message (e.g. a
    human assigning a task — the note is only populated by the automation-
    workflow engine).
    """
    if trigger.message.strip():
        return trigger.message
    if trigger.trigger_type == "task_assigned":
        return _TASK_ASSIGNED_DEFAULT
    return trigger.message


def build_project_context_suffix(project_id: str) -> str:
    return (
        f"\n\n## Current Project Context\n"
        f"You are working inside project `{project_id}`.\n"
        "**Always pass this value as `projectId` in every MCP tool call** "
        "that requires it — never ask the user for the project ID and "
        "never call `list_projects` to find it.\n"
    )


# ACP agents don't get Paca-managed skills injected via system context the
# way the LLM/sandbox path does (see the "paca" entry in
# services/api/internal/platform/bundledskills/bundledskills.go) — the only
# way to route a local ACP CLI (Claude Code, Codex, ...) through the Paca
# skill is the literal slash command, so prefix every dispatched message
# with it.
_ACP_SKILL_TRIGGER = "/paca"


def build_acp_message(trigger: TriggerMessage) -> str:
    """Build the message sent to trigger an ACP agent.

    The LLM/sandbox path (executor.py) carries project and trigger context in
    AgentContext.system_message_suffix, a channel ACP agents don't have —
    apps/acp-bridge's ConversationRunner only ever calls
    conversation.send_message(message) on the plain local Conversation, with
    no separate system-suffix argument. So instead of a suffix, that same
    context (minus the repo section, which points at sandbox-only tools like
    clone_repository — ACP agents already have local repo access) is folded
    directly into this message, prefixed with the `/paca` skill trigger.
    """
    message = f"{_ACP_SKILL_TRIGGER} {build_initial_prompt(trigger)}"
    message += build_project_context_suffix(trigger.project_id)
    message += build_trigger_suffix(trigger)
    return message


def build_trigger_suffix(trigger: TriggerMessage, all_repos: list[dict] | None = None) -> str:
    """Build the system-message suffix for trigger-specific metadata.

    This supplements the base system suffix with the action type, contextual
    IDs (task / comment / chat session), and repository setup instructions.
    """
    lines = [
        "\n\n## Trigger Context",
        f"Action type: {_action_type_label(trigger)}",
    ]
    if trigger.task_id:
        lines.append(f"Task ID: {trigger.task_id}")
    if trigger.comment_id:
        lines.append(f"Comment ID: {trigger.comment_id}")
    if trigger.chat_session_id:
        lines.append(f"Chat Session ID: {trigger.chat_session_id}")

    if all_repos:
        lines.append("\n### Repository Setup Required")
        lines.append(
            f"This project has {len(all_repos)} linked"
            f" repositor{'y' if len(all_repos) == 1 else 'ies'}."
            " You MUST clone it before working on any code."
        )
        if len(all_repos) == 1:
            repo = all_repos[0]
            lines.append(
                f"\nClone the repository now by calling clone_repository with:"
                f"\n  plugin_id='{repo['plugin_id']}'"
                f"\n  repo_id='{repo['repo_id']}'"
                f"\n  (target_dir defaults to /workspace/repo)"
            )
            lines.append(f"\nRepository: {repo['full_name']}")
        else:
            lines.append(
                "\nCall list_repositories to get the available"
                " repositories, then clone the one you need."
            )

    return "\n".join(lines)
