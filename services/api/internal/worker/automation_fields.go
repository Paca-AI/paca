package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/pkg/vartemplate"
)

// materializeFields turns the stored, declarative field config of an
// update_task / create_task action into the concrete values for this run:
//
//   - Dynamic values are resolved against the walk's task (source "task"),
//     that task's parent ("parent") or now, overriding the fixed value of the
//     same field.
//   - {{variable}} templates are rendered in the description's text and in
//     string custom-field values (the title is rendered by the caller).
//   - When current is set (update_task), the list modes are folded in: "add"
//     and "remove" become the final tag / assignee list computed from the
//     task's current one, so the rest of the apply logic only ever has to
//     replace. A removal that empties tags is expressed through Clear.
//
// The input is never mutated: it is the automation's shared stored config.
func (c *AutomationConsumer) materializeFields(ctx context.Context, walkTask, current *taskdom.Task, in *automationdom.TaskFieldUpdate, vars map[string]string) (*automationdom.TaskFieldUpdate, error) {
	out := *in
	out.Clear = slices.Clone(in.Clear)
	out.Tags = slices.Clone(in.Tags)
	out.AssigneeIDs = slices.Clone(in.AssigneeIDs)
	if in.CustomFields != nil {
		out.CustomFields = make(map[string]any, len(in.CustomFields))
		for k, v := range in.CustomFields {
			if s, ok := v.(string); ok {
				v = vartemplate.Render(s, vars)
			}
			out.CustomFields[k] = v
		}
	}
	if len(in.Description) > 0 {
		out.Description = renderBlockNote(in.Description, vars)
	}

	keys := make([]string, 0, len(in.Dynamic))
	for k := range in.Dynamic {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parent *taskdom.Task
	parentLoaded := false
	now := time.Now()
	for _, key := range keys {
		dv := in.Dynamic[key]
		if !automationdom.DynamicFields[key] {
			return nil, fmt.Errorf("dynamic value: field %q cannot be set dynamically", key)
		}
		var src *taskdom.Task
		switch dv.Source {
		case automationdom.DynamicSourceTask:
			src = walkTask
		case automationdom.DynamicSourceParent:
			if !parentLoaded {
				parentLoaded = true
				if walkTask != nil && walkTask.ParentTaskID != nil {
					p, err := c.taskRepo.FindTaskByID(ctx, *walkTask.ParentTaskID)
					if err != nil {
						return nil, fmt.Errorf("dynamic value %s: find parent: %w", key, err)
					}
					parent = p
				}
			}
			src = parent
		case automationdom.DynamicSourceNow:
			// handled below: only a date has a "now" value.
		default:
			return nil, fmt.Errorf("dynamic value %s: unknown source %q", key, dv.Source)
		}
		if err := setDynamicField(&out, key, dv, src, now); err != nil {
			return nil, err
		}
	}

	if current != nil {
		switch in.TagsMode {
		case automationdom.ListModeAdd:
			out.Tags = unionStrings(current.Tags, out.Tags)
		case automationdom.ListModeRemove:
			out.Tags = subtractStrings(current.Tags, out.Tags)
			if len(out.Tags) == 0 && len(in.Tags) > 0 && len(current.Tags) > 0 {
				out.Clear = append(out.Clear, "tags")
			}
		}
		switch in.AssigneesMode {
		case automationdom.ListModeAdd:
			out.AssigneeIDs = unionUUIDs(current.AssigneeIDs, out.AssigneeIDs)
		case automationdom.ListModeRemove:
			out.AssigneeIDs = subtractUUIDs(current.AssigneeIDs, out.AssigneeIDs)
		}
	}
	return &out, nil
}

// setDynamicField resolves dv for key from src and stores the result on out.
// An unset source value is a no-op (the field is left as configured).
func setDynamicField(out *automationdom.TaskFieldUpdate, key string, dv automationdom.DynamicValue, src *taskdom.Task, now time.Time) error {
	srcField := dv.Field
	if srcField == "" {
		srcField = key
	}
	isDate := key == "start_date" || key == "due_date"

	if dv.Source == automationdom.DynamicSourceNow {
		if !isDate {
			return fmt.Errorf("dynamic value %s: source \"now\" only applies to dates", key)
		}
		t := shiftDays(now, dv.OffsetDays)
		setDate(out, key, &t)
		return nil
	}
	if src == nil {
		return nil // no parent / no task to read from
	}

	switch key {
	case "assignee_ids":
		switch srcField {
		case "assignee_ids":
			if len(src.AssigneeIDs) > 0 {
				out.AssigneeIDs = slices.Clone(src.AssigneeIDs)
			}
		case "reporter_id":
			if src.ReporterID != nil {
				out.AssigneeIDs = []uuid.UUID{*src.ReporterID}
			}
		default:
			return incompatible(key, srcField)
		}
	case "reporter_id":
		switch srcField {
		case "reporter_id":
			out.ReporterID = src.ReporterID
		case "assignee_ids":
			if len(src.AssigneeIDs) > 0 {
				id := src.AssigneeIDs[0]
				out.ReporterID = &id
			}
		default:
			return incompatible(key, srcField)
		}
	case "sprint_id", "task_type_id", "status_id":
		if srcField != key {
			return incompatible(key, srcField)
		}
		var v *uuid.UUID
		switch key {
		case "sprint_id":
			v = src.SprintID
		case "task_type_id":
			v = src.TaskTypeID
		case "status_id":
			v = src.StatusID
		}
		if v == nil {
			return nil
		}
		switch key {
		case "sprint_id":
			out.SprintID = v
		case "task_type_id":
			out.TaskTypeID = v
		case "status_id":
			out.StatusID = v
		}
	case "importance":
		if srcField != key {
			return incompatible(key, srcField)
		}
		v := src.Importance
		out.Importance = &v
	case "story_points":
		if srcField != key {
			return incompatible(key, srcField)
		}
		if src.StoryPoints != nil {
			v := *src.StoryPoints
			out.StoryPoints = &v
		}
	case "tags":
		if srcField != key {
			return incompatible(key, srcField)
		}
		if len(src.Tags) > 0 {
			out.Tags = slices.Clone(src.Tags)
		}
	case "start_date", "due_date":
		var v *time.Time
		switch srcField {
		case "start_date":
			v = src.StartDate
		case "due_date":
			v = src.DueDate
		default:
			return incompatible(key, srcField)
		}
		if v == nil {
			return nil
		}
		t := shiftDays(*v, dv.OffsetDays)
		setDate(out, key, &t)
	}
	return nil
}

func incompatible(key, srcField string) error {
	return fmt.Errorf("dynamic value: cannot set %s from %s", key, srcField)
}

func shiftDays(t time.Time, days *int) time.Time {
	if days == nil {
		return t
	}
	return t.AddDate(0, 0, *days)
}

func setDate(out *automationdom.TaskFieldUpdate, key string, t *time.Time) {
	if key == "start_date" {
		out.StartDate = t
	} else {
		out.DueDate = t
	}
}

// renderBlockNote renders {{variables}} in every "text" string of a BlockNote
// document. Rendering the decoded strings (not the raw JSON) keeps a value
// containing quotes or backslashes from breaking the document. A document
// that does not decode is returned unchanged.
func renderBlockNote(doc json.RawMessage, vars map[string]string) json.RawMessage {
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		return doc
	}
	var walk func(any) any
	walk = func(n any) any {
		switch t := n.(type) {
		case []any:
			for i := range t {
				t[i] = walk(t[i])
			}
		case map[string]any:
			for k, val := range t {
				if s, ok := val.(string); ok && k == "text" {
					t[k] = vartemplate.Render(s, vars)
				} else {
					t[k] = walk(val)
				}
			}
		}
		return n
	}
	out, err := json.Marshal(walk(v))
	if err != nil {
		return doc
	}
	return out
}

// plainTextBlocks builds a BlockNote document from plain text, one paragraph
// per line.
func plainTextBlocks(text string) json.RawMessage {
	var blocks []map[string]any
	start := 0
	for i := 0; i <= len(text); i++ {
		if i == len(text) || text[i] == '\n' {
			line := text[start:i]
			content := []map[string]any{}
			if line != "" {
				content = append(content, map[string]any{"type": "text", "text": line, "styles": map[string]any{}})
			}
			blocks = append(blocks, map[string]any{"type": "paragraph", "content": content})
			start = i + 1
		}
	}
	b, _ := json.Marshal(blocks)
	return b
}

// applyClear sets the "explicitly empty" form of every field named in clear
// that is not already empty on task, recording each in changed.
func applyClear(in *taskdom.UpdateTaskInput, changed map[string]any, task *taskdom.Task, clear []string) {
	for _, f := range clear {
		switch f {
		case "assignee_ids":
			if len(task.AssigneeIDs) > 0 {
				ids := []uuid.UUID{}
				in.AssigneeIDs = &ids
				changed[f] = ids
			}
		case "tags":
			if len(task.Tags) > 0 {
				tags := []string{}
				in.Tags = &tags
				changed[f] = tags
			}
		case "sprint_id":
			if task.SprintID != nil {
				var p *uuid.UUID
				in.SprintID = &p
				changed[f] = nil
			}
		case "story_points":
			if task.StoryPoints != nil {
				var p *int
				in.StoryPoints = &p
				changed[f] = nil
			}
		case "reporter_id":
			if task.ReporterID != nil {
				var p *uuid.UUID
				in.ReporterID = &p
				changed[f] = nil
			}
		case "start_date":
			if task.StartDate != nil {
				var p *time.Time
				in.StartDate = &p
				changed[f] = nil
			}
		case "due_date":
			if task.DueDate != nil {
				var p *time.Time
				in.DueDate = &p
				changed[f] = nil
			}
		case "parent_task_id":
			if task.ParentTaskID != nil {
				var p *uuid.UUID
				in.ParentTaskID = &p
				changed[f] = nil
			}
		}
	}
}

func unionStrings(a, b []string) []string {
	out := slices.Clone(a)
	for _, s := range b {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func subtractStrings(a, b []string) []string {
	out := []string{}
	for _, s := range a {
		if !slices.Contains(b, s) {
			out = append(out, s)
		}
	}
	return out
}

func unionUUIDs(a, b []uuid.UUID) []uuid.UUID {
	out := slices.Clone(a)
	for _, id := range b {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	if out == nil {
		out = []uuid.UUID{}
	}
	return out
}

func subtractUUIDs(a, b []uuid.UUID) []uuid.UUID {
	out := []uuid.UUID{}
	for _, id := range a {
		if !slices.Contains(b, id) {
			out = append(out, id)
		}
	}
	return out
}
