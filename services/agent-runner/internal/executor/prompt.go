package executor

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

// isAutomationTrigger mirrors trigger_skills.py's automation detection: a
// task-assignment trigger with no human actor, or an explicit
// automation_message trigger, was fired by the automation-workflow engine
// rather than a person.
func isAutomationTrigger(t agent.Trigger) bool {
	if t.TriggerType == agent.TriggerTaskAssigned && t.ActorMemberID == nil {
		return true
	}
	return t.TriggerType == agent.TriggerAutomationMessage
}

// automationInvocationNote mirrors trigger_skills.py's
// _AUTOMATION_INVOCATION_NOTE — told to an agent invoked by the automation
// engine rather than a human, so it doesn't idle waiting for a live reply.
const automationInvocationNote = "There is no human watching this conversation live — don't wait for a " +
	"reply before acting, and don't assume someone will immediately see anything you post. If you get " +
	"stuck without enough information and a human needs to weigh in, add a task or document comment with " +
	"an `@username` mention instead of waiting in this conversation.\n"

// globalProjectContext mirrors prompt.py's _GLOBAL_CONTEXT_SUFFIX — shown
// instead of a project-scoped context block for a global-chat conversation
// (trigger.ProjectID == uuid.Nil).
const globalProjectContext = "\n\n## Current Context\n" +
	"You are a global Paca agent, not scoped to any single project. You may have been invited into one " +
	"or more projects and can act in any of them, plus admin-level tools (managing users, global roles, " +
	"projects) if your own role grants them.\n" +
	"**Always pass an explicit `projectId` in any MCP tool call that requires it** — call `list_projects` " +
	"first if you don't already know which project the user means; never assume a single current " +
	"project.\n"

// taskAssignedDefault mirrors prompt.py's _TASK_ASSIGNED_DEFAULT — a human
// task-assignment trigger arrives with an empty message (the note is only
// populated by the automation-workflow engine), and Goose (like the
// OpenHands SDK before it) errors on an empty first prompt.
const taskAssignedDefault = "You have been assigned a task. Load it via the Paca MCP tool and " +
	"follow the default `paca` skill's routing to pick the right specialized skill for its status."

// buildInitialMessage mirrors prompt.py's build_initial_prompt +
// build_trigger_suffix + build_project_context_suffix, combined into one
// function: Goose's ACP session/new has no system-message-suffix channel
// (unlike OpenHands SDK's AgentContext), confirmed empirically in the spike
// — session/new's params are only cwd and mcpServers — so everything that
// used to ride in a separate system suffix now has to be folded into the
// turn's own message instead, the same way apps/acp-bridge's
// build_acp_message already does for the acp execution path.
func buildInitialMessage(cfg agent.Config, trigger agent.Trigger) string {
	var b strings.Builder

	b.WriteString(cfg.SystemPrompt)

	for _, s := range cfg.Skills {
		if !s.IsEnabled {
			continue
		}
		// Trigger-based (keyword-activated) skills have no Goose analog
		// yet — see agent.Skill's doc comment — so every enabled skill is
		// included unconditionally for now.
		b.WriteString("\n\n## Skill: ")
		b.WriteString(s.SkillName)
		b.WriteString("\n")
		b.WriteString(s.SkillContent)
	}

	if trigger.ProjectID == uuid.Nil {
		b.WriteString(globalProjectContext)
	} else {
		b.WriteString("\n\n## Current Project Context\n")
		fmt.Fprintf(&b, "You are working inside project `%s`.\n", trigger.ProjectID)
		b.WriteString("**Always pass this value as `projectId` in every MCP tool call** " +
			"that requires it — never ask the user for the project ID and never call " +
			"`list_projects` to find it.\n")
	}

	b.WriteString("\n\n## Trigger Context\n")
	fmt.Fprintf(&b, "Action type: %s\n", actionTypeLabel(trigger))
	if isAutomationTrigger(trigger) {
		b.WriteString(automationInvocationNote)
	}
	if trigger.TaskID != nil {
		fmt.Fprintf(&b, "Task ID: %s\n", trigger.TaskID)
	}
	if trigger.CommentID != nil {
		fmt.Fprintf(&b, "Comment ID: %s\n", trigger.CommentID)
	}
	if trigger.ChatSessionID != nil {
		fmt.Fprintf(&b, "Chat Session ID: %s\n", trigger.ChatSessionID)
	}

	b.WriteString("\n\n## User Message\n")
	message := strings.TrimSpace(trigger.Message)
	if message == "" && trigger.TriggerType == agent.TriggerTaskAssigned {
		message = taskAssignedDefault
	}
	b.WriteString(message)

	return b.String()
}
