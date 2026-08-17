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

// buildInitialMessage mirrors prompt.py's build_trigger_suffix +
// build_project_context_suffix (build_initial_prompt's job — the agent's
// own persona/instructions — moved to a .goosehints file in coldStart; see
// hints.go's buildGooseHints doc comment for why, including why an earlier
// version of this comment's claim that GOOSE_MOIM_MESSAGE_TEXT was the
// right replacement turned out to be wrong). Goose's ACP session/new itself
// has no system-message-suffix channel (unlike OpenHands SDK's
// AgentContext), confirmed empirically in the spike — session/new's params
// are only cwd and mcpServers — but that's not the same as Goose having no
// system-prompt channel at all: .goosehints is a real one, delivered as a
// file in the sandbox's cwd rather than an ACP protocol field.
//
// No skill content is folded in here, or anywhere else any more. An earlier
// version of this function kept a fallback that inlined any skill Goose's
// own file-based discovery couldn't take (malformed frontmatter) directly
// into this message — skills.go's prepareFileSkills now synthesizes
// frontmatter instead of ever needing that fallback, so every enabled
// skill is discoverable via `load_skill`, never dumped as raw text. What's
// left here is genuinely per-turn, request-scoped content only: project
// context, trigger context, and the user's own message.
func buildInitialMessage(trigger agent.Trigger) string {
	var b strings.Builder

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

	// TrimLeft, not the whole message: b's first Write is always one of the
	// "\n\n## ..." section separators above (there's no longer a
	// leading system prompt to separate from), which would otherwise leave
	// the turn's actual text starting with a couple of blank lines.
	return strings.TrimLeft(b.String(), "\n")
}
