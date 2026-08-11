package acpbridge

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

var actionTypeLabels = map[agent.TriggerType]string{
	agent.TriggerTaskAssigned:      "Task assignment",
	agent.TriggerChatMessage:       "Direct chat message",
	agent.TriggerDescriptionWrite:  "Write task description",
	agent.TriggerAutomationMessage: "Automation-triggered message",
}

func actionTypeLabel(t agent.Trigger) string {
	if t.TriggerType == agent.TriggerCommentMention {
		if t.TaskID != nil {
			return "Task comment mention"
		}
		return "Document comment mention"
	}
	if t.TriggerType == agent.TriggerTaskAssigned && t.ActorMemberID == nil {
		return "Automation-triggered task assignment"
	}
	if label, ok := actionTypeLabels[t.TriggerType]; ok {
		return label
	}
	return string(t.TriggerType)
}

// taskAssignedDefault mirrors prompt.py's _TASK_ASSIGNED_DEFAULT — see
// internal/executor/prompt.go's identical constant for why this exists;
// duplicated rather than imported since executor is llm-sandbox-specific
// and acpbridge has no other reason to depend on it.
const taskAssignedDefault = "You have been assigned a task. Load it via the Paca MCP tool and " +
	"follow the default `paca` skill's routing to pick the right specialized skill for its status."

// BuildACPMessage builds the message sent to trigger an ACP agent — mirrors
// prompt.py's build_acp_message exactly: apps/acp-bridge's ConversationRunner
// only ever calls conversation.send_message(message) on a plain local
// Conversation, with no separate system-suffix channel (unlike the
// LLM/sandbox path's AgentContext.system_message_suffix), so project and
// trigger context are folded directly into this message instead, prefixed
// with the /paca skill trigger. Unlike executor/prompt.go's
// buildInitialMessage, this has no repository-setup section: it points at
// sandbox-only tools like clone_repository, and an ACP agent already has
// local repo access on the user's own machine.
func BuildACPMessage(trigger agent.Trigger) string {
	var b strings.Builder

	b.WriteString("/paca ")
	// Uses trigger.Message itself (not a trimmed copy) once it's confirmed
	// non-blank — mirrors build_initial_prompt's own
	// `if trigger.message.strip(): return trigger.message` exactly (checks
	// trimmed, returns untrimmed).
	message := trigger.Message
	if strings.TrimSpace(message) == "" && trigger.TriggerType == agent.TriggerTaskAssigned {
		message = taskAssignedDefault
	}
	b.WriteString(message)

	if trigger.ProjectID != uuid.Nil {
		b.WriteString("\n\n## Current Project Context\n")
		fmt.Fprintf(&b, "You are working inside project `%s`.\n", trigger.ProjectID)
		b.WriteString("**Always pass this value as `projectId` in every MCP tool call** " +
			"that requires it — never ask the user for the project ID and never call " +
			"`list_projects` to find it.\n")
	} else {
		// Mirrors prompt.py's _GLOBAL_CONTEXT_SUFFIX verbatim.
		b.WriteString("\n\n## Current Context\n" +
			"You are a global Paca agent, not scoped to any single project. You may " +
			"have been invited into one or more projects and can act in any of " +
			"them, plus admin-level tools (managing users, global roles, projects) " +
			"if your own role grants them.\n" +
			"**Always pass an explicit `projectId` in any MCP tool call that " +
			"requires it** — call `list_projects` first if you don't already know " +
			"which project the user means; never assume a single current project.\n")
	}

	b.WriteString("\n\n## Trigger Context\n")
	fmt.Fprintf(&b, "Action type: %s\n", actionTypeLabel(trigger))
	if trigger.TaskID != nil {
		fmt.Fprintf(&b, "Task ID: %s\n", trigger.TaskID)
	}
	if trigger.CommentID != nil {
		fmt.Fprintf(&b, "Comment ID: %s\n", trigger.CommentID)
	}
	if trigger.ChatSessionID != nil {
		fmt.Fprintf(&b, "Chat Session ID: %s\n", trigger.ChatSessionID)
	}

	return b.String()
}
