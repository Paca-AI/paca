package meetingdom

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Limits on proposal payloads. The card shows these fields as written, so they
// stay small enough to read.
const (
	maxTitleLen   = 300
	maxBodyLen    = 20000
	maxPriorityLn = 32
)

type taskCreatePayload struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	AssigneeID  string `json:"assignee_id"`
	SprintID    string `json:"sprint_id"`
}

type taskUpdatePayload struct {
	TaskID     string  `json:"task_id"`
	StatusID   *string `json:"status_id"`
	AssigneeID *string `json:"assignee_id"`
	Priority   *string `json:"priority"`
	SprintID   *string `json:"sprint_id"`
}

type taskCommentPayload struct {
	TaskID string `json:"task_id"`
	Body   string `json:"body"`
}

type docPayload struct {
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
}

// decodeStrict decodes JSON into v, rejecting unknown fields and trailing data.
func decodeStrict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("unexpected trailing data")
	}
	return nil
}

func validID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

func optionalID(s *string) bool { return s == nil || *s == "" || validID(*s) }

// ValidatePayload checks that payload fits kind and returns it normalised
// (the fields of the kind only, as JSON). The error wraps ErrProposalInvalid
// and says what is wrong, for the assistant's model to correct itself.
func ValidatePayload(kind string, payload []byte) ([]byte, error) {
	bad := func(format string, a ...any) ([]byte, error) {
		return nil, fmt.Errorf("%w: %s", ErrProposalInvalid, fmt.Sprintf(format, a...))
	}
	switch kind {
	case KindTaskCreate:
		var p taskCreatePayload
		if err := decodeStrict(payload, &p); err != nil {
			return bad("task.create payload: %v", err)
		}
		p.Title = strings.TrimSpace(p.Title)
		if p.Title == "" || len(p.Title) > maxTitleLen {
			return bad("title is required (at most %d characters)", maxTitleLen)
		}
		if len(p.Description) > maxBodyLen || len(p.Priority) > maxPriorityLn {
			return bad("description or priority is too long")
		}
		if p.AssigneeID != "" && !validID(p.AssigneeID) || p.SprintID != "" && !validID(p.SprintID) {
			return bad("assignee_id and sprint_id must be ids")
		}
		return json.Marshal(p)
	case KindTaskUpdate:
		var p taskUpdatePayload
		if err := decodeStrict(payload, &p); err != nil {
			return bad("task.update payload: %v", err)
		}
		if !validID(p.TaskID) {
			return bad("task_id must be a task id")
		}
		if p.StatusID == nil && p.AssigneeID == nil && p.Priority == nil && p.SprintID == nil {
			return bad("change at least one of status_id, assignee_id, priority, sprint_id")
		}
		if !optionalID(p.StatusID) || !optionalID(p.AssigneeID) || !optionalID(p.SprintID) {
			return bad("status_id, assignee_id and sprint_id must be ids")
		}
		if p.Priority != nil && len(*p.Priority) > maxPriorityLn {
			return bad("priority is too long")
		}
		return json.Marshal(p)
	case KindTaskComment:
		var p taskCommentPayload
		if err := decodeStrict(payload, &p); err != nil {
			return bad("task.comment payload: %v", err)
		}
		p.Body = strings.TrimSpace(p.Body)
		if !validID(p.TaskID) || p.Body == "" || len(p.Body) > maxBodyLen {
			return bad("task_id and a comment body (at most %d characters) are required", maxBodyLen)
		}
		return json.Marshal(p)
	case KindDocCreateFromNotes:
		var p docPayload
		if err := decodeStrict(payload, &p); err != nil {
			return bad("doc.create_from_notes payload: %v", err)
		}
		p.Title = strings.TrimSpace(p.Title)
		if p.Title == "" || len(p.Title) > maxTitleLen || strings.TrimSpace(p.Markdown) == "" || len(p.Markdown) > 5*maxBodyLen {
			return bad("title and markdown are required")
		}
		return json.Marshal(p)
	default:
		return bad("unknown kind %q", kind)
	}
}

// ValidKind reports whether kind is a known proposal kind.
func ValidKind(kind string) bool {
	for _, k := range ProposalKinds {
		if k == kind {
			return true
		}
	}
	return false
}
