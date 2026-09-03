package agent

import (
	"fmt"
	"strings"
)

// ContextItemType discriminates which kind of resource a ContextItemRef
// points at. Byte-identical to services/api's
// internal/domain/agent/entity.go ContextItemType — see this package's own
// doc comment (in trigger.go) for why agent-runner keeps its own copy
// instead of importing services/api's internal package.
type ContextItemType string

// ContextItemType values — mirrors the constants in services/api.
const (
	ContextItemTask         ContextItemType = "task"
	ContextItemDoc          ContextItemType = "doc"
	ContextItemConversation ContextItemType = "conversation"
	ContextItemAutomation   ContextItemType = "automation"
	ContextItemAnnotation   ContextItemType = "annotation"
)

// ContextItemRef is a reference to a Task, Doc, Conversation, or Automation
// the user attached to a chat message from the frontend composer's
// context-item picker. Byte-identical (field names/JSON tags) to
// services/api's agentdom.ContextItemRef — decoded here from the trigger
// stream's flat "context_items" JSON-string field (see
// messaging/decode.go's decodeTrigger) rather than shared via import, per
// this package's cross-service-duplication convention (see trigger.go).
type ContextItemRef struct {
	Type      ContextItemType `json:"type"`
	ID        string          `json:"id"`
	ProjectID *string         `json:"project_id,omitempty"`
	Title     string          `json:"title"`
}

// FormatAttachedContext renders the "## Attached Context" prompt section for
// items the user attached to a chat message (see ContextItemRef) — a hint
// block telling the agent which MCP tool to call to load full details on
// each one, since only a type+id+title stub rides the trigger stream, not
// the referenced content itself. Returns "" for an empty slice so callers
// can unconditionally insert/append the result without a separate len
// check of their own.
//
// Shared by both prompt-building call sites — executor/prompt.go's
// buildInitialMessage (LLM/sandbox path) and acpbridge/message.go's
// BuildACPMessage (ACP-bridge path) — specifically so the
// type-to-tool-name mapping can never drift between the two as separate
// hand-copies.
func FormatAttachedContext(items []ContextItemRef) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## Attached Context\n")
	b.WriteString("The user attached the following as context. Load full details before answering:\n")
	for _, item := range items {
		switch item.Type {
		case ContextItemTask:
			fmt.Fprintf(&b, "- Task (ID: %s) %q — call `get_task`.\n", item.ID, item.Title)
		case ContextItemDoc:
			fmt.Fprintf(&b, "- Documentation page (ID: %s) %q — call `read_doc` with `docId: %q`.\n", item.ID, item.Title, item.ID)
		case ContextItemAutomation:
			fmt.Fprintf(&b, "- Automation (ID: %s) %q — call `get_automation`.\n", item.ID, item.Title)
		case ContextItemConversation:
			fmt.Fprintf(&b, "- Conversation (ID: %s) %q — call `read_conversation`.\n", item.ID, item.Title)
		case ContextItemAnnotation:
			fmt.Fprintf(&b, "- Page annotation (ID: %s) %q — call `get_annotation`.\n", item.ID, item.Title)
		default:
			fmt.Fprintf(&b, "- %s (ID: %s) %q\n", item.Type, item.ID, item.Title)
		}
	}
	return b.String()
}
