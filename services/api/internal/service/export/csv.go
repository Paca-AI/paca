package exportsvc

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	activitydom "github.com/Paca-AI/api/internal/domain/activity"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// csvBOM makes Excel open the file as UTF-8 instead of the system code page.
const csvBOM = "\xef\xbb\xbf"

const csvTimeLayout = time.RFC3339
const csvDateLayout = "2006-01-02"

// lookups resolves the IDs stored on a task to the names a reader of the CSV
// expects to see. Every map is keyed by the referenced row's own ID; a missing
// key simply yields an empty cell (e.g. a status deleted since the task last
// referenced it).
type lookups struct {
	taskIDPrefix string
	statuses     map[uuid.UUID]*taskdom.TaskStatus
	types        map[uuid.UUID]*taskdom.TaskType
	sprints      map[uuid.UUID]string
	members      map[uuid.UUID]string // project_members.id -> display name
	// taskNumbers maps task ID -> task_number for resolving parent tasks to
	// their human-readable key. Filled as pages are written; see Service.
	taskNumbers map[uuid.UUID]int64
	fields      []*taskdom.CustomFieldDefinition
}

// csvWriter streams one CSV file (UTF-8 BOM, header, rows) into out.
type csvWriter struct {
	w    *csv.Writer
	look *lookups
	rows int
}

func newCSVWriter(out io.Writer, look *lookups, header []string) (*csvWriter, error) {
	if _, err := io.WriteString(out, csvBOM); err != nil {
		return nil, fmt.Errorf("write csv bom: %w", err)
	}
	cw := &csvWriter{w: csv.NewWriter(out), look: look}
	if err := cw.w.Write(header); err != nil {
		return nil, fmt.Errorf("write csv header: %w", err)
	}
	return cw, nil
}

// writeRow neutralizes every cell (see neutralizeFormula) and writes the row.
func (cw *csvWriter) writeRow(row []string) error {
	for i := range row {
		row[i] = neutralizeFormula(row[i])
	}
	if err := cw.w.Write(row); err != nil {
		return fmt.Errorf("write csv row: %w", err)
	}
	cw.rows++
	return nil
}

// finish flushes buffered rows to the underlying writer.
func (cw *csvWriter) finish() error {
	cw.w.Flush()
	if err := cw.w.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}
	return nil
}

func tasksHeader(fields []*taskdom.CustomFieldDefinition) []string {
	header := []string{
		"ID", "Title", "Description", "Type", "Status", "Status Category", "Sprint",
		"Parent", "Assignees", "Reporter", "Priority", "Story Points",
		"Start Date", "Due Date", "Tags", "Created At", "Updated At",
	}
	for _, f := range fields {
		header = append(header, f.DisplayName)
	}
	return header
}

func (cw *csvWriter) writeTask(t *taskdom.Task) error {
	l := cw.look
	l.taskNumbers[t.ID] = t.TaskNumber

	row := []string{
		l.taskKey(t.TaskNumber),
		t.Title,
		descriptionText(t.Description),
		l.typeName(t.TaskTypeID),
		l.statusName(t.StatusID),
		l.statusCategory(t.StatusID),
		l.sprintName(t.SprintID),
		l.parentKey(t.ParentTaskID),
		l.memberNames(t.AssigneeIDs),
		l.memberName(t.ReporterID),
		strconv.Itoa(t.Importance),
		intPtrString(t.StoryPoints),
		datePtrString(t.StartDate),
		datePtrString(t.DueDate),
		strings.Join(t.Tags, "; "),
		t.CreatedAt.UTC().Format(csvTimeLayout),
		t.UpdatedAt.UTC().Format(csvTimeLayout),
	}
	for _, f := range l.fields {
		row = append(row, customFieldString(t.CustomFields[f.FieldKey]))
	}
	return cw.writeRow(row)
}

var commentsHeader = []string{"Task ID", "Task Title", "Author", "Origin", "Comment", "Created At", "Edited At"}

// writeComment writes one task comment. Comments carry the task they are on
// by ID only; the key resolves through the tasks already written (an entry for
// a since-deleted task keeps its title and leaves the key empty).
func (cw *csvWriter) writeComment(a *activitydom.Activity) error {
	edited := ""
	if a.UpdatedAt.Sub(a.CreatedAt) > time.Second {
		edited = a.UpdatedAt.UTC().Format(csvTimeLayout)
	}
	return cw.writeRow([]string{
		cw.look.entityTaskKey(a), a.EntityTitle, actorLabel(a), a.Origin,
		commentText(a.Content),
		a.CreatedAt.UTC().Format(csvTimeLayout), edited,
	})
}

var activitiesHeader = []string{"Task ID", "Task Title", "Event", "Actor", "Origin", "Details", "Created At"}

// writeActivity writes one non-comment task activity. Details is the entry's
// own JSON content as stored: its shape depends on the event type.
func (cw *csvWriter) writeActivity(a *activitydom.Activity) error {
	return cw.writeRow([]string{
		cw.look.entityTaskKey(a), a.EntityTitle, a.ActivityType, actorLabel(a), a.Origin,
		compactJSON(a.Content),
		a.CreatedAt.UTC().Format(csvTimeLayout),
	})
}

func (l *lookups) entityTaskKey(a *activitydom.Activity) string {
	if a.EntityID == nil {
		return ""
	}
	if n, ok := l.taskNumbers[*a.EntityID]; ok {
		return l.taskKey(n)
	}
	return ""
}

func actorLabel(a *activitydom.Activity) string {
	if a.ActorName != "" {
		return a.ActorName
	}
	return a.ActorUsername
}

// commentText flattens a comment body: a BlockNote block array, the legacy
// {"text": "..."} shape, or a bare JSON string.
func commentText(raw json.RawMessage) string {
	if t := descriptionText(raw); t != "" {
		return t
	}
	var legacy struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &legacy); err == nil && legacy.Text != "" {
		return legacy.Text
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

// compactJSON re-encodes raw without insignificant whitespace; empty objects,
// arrays and null become an empty cell.
func compactJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return ""
	}
	switch buf.String() {
	case "", "null", "{}", "[]":
		return ""
	}
	return buf.String()
}

// neutralizeFormula stops spreadsheet apps from evaluating a cell as a
// formula (CSV injection): task titles, tags and custom fields are
// user-controlled, and a cell starting with = + - @ (or a tab/CR that some
// apps strip before checking) would otherwise run when the file is opened. A
// leading apostrophe makes Excel/Sheets/LibreOffice treat it as plain text.
func neutralizeFormula(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

func (l *lookups) taskKey(n int64) string {
	if l.taskIDPrefix == "" {
		return strconv.FormatInt(n, 10)
	}
	return l.taskIDPrefix + "-" + strconv.FormatInt(n, 10)
}

func (l *lookups) typeName(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	if t, ok := l.types[*id]; ok {
		return t.Name
	}
	return ""
}

func (l *lookups) statusName(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	if s, ok := l.statuses[*id]; ok {
		return s.Name
	}
	return ""
}

func (l *lookups) statusCategory(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	if s, ok := l.statuses[*id]; ok {
		return string(s.Category)
	}
	return ""
}

func (l *lookups) sprintName(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return l.sprints[*id]
}

func (l *lookups) parentKey(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	if n, ok := l.taskNumbers[*id]; ok {
		return l.taskKey(n)
	}
	return ""
}

func (l *lookups) memberName(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return l.members[*id]
}

func (l *lookups) memberNames(ids []uuid.UUID) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if n := l.members[id]; n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, "; ")
}

func intPtrString(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func datePtrString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(csvDateLayout)
}

// customFieldString renders a stored custom-field value: scalars as-is,
// multi-select (a JSON array) joined with "; ", booleans as true/false.
func customFieldString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			if s := customFieldString(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "; ")
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(b)
	}
}

// descriptionText flattens a BlockNote description (a JSON array of blocks
// whose inline content carries the text) into plain text, one block per line.
func descriptionText(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return ""
	}
	var blocks []map[string]any
	if err := json.Unmarshal(trimmed, &blocks); err != nil {
		return ""
	}
	lines := make([]string, 0, len(blocks))
	for _, b := range blocks {
		collectBlockText(b, &lines)
	}
	return strings.Join(lines, "\n")
}

func collectBlockText(block map[string]any, lines *[]string) {
	var sb strings.Builder
	switch content := block["content"].(type) {
	case []any:
		for _, item := range content {
			sb.WriteString(inlineText(item))
		}
	case string:
		sb.WriteString(content)
	}
	if s := strings.TrimSpace(sb.String()); s != "" {
		*lines = append(*lines, s)
	}
	if children, ok := block["children"].([]any); ok {
		for _, c := range children {
			if cm, ok := c.(map[string]any); ok {
				collectBlockText(cm, lines)
			}
		}
	}
}

func inlineText(item any) string {
	m, ok := item.(map[string]any)
	if !ok {
		return ""
	}
	switch m["type"] {
	case "text":
		s, _ := m["text"].(string)
		return s
	case "link":
		var sb strings.Builder
		if inner, ok := m["content"].([]any); ok {
			for _, i := range inner {
				sb.WriteString(inlineText(i))
			}
		}
		return sb.String()
	default:
		// Mentions and other custom inline nodes: show their display name.
		if props, ok := m["props"].(map[string]any); ok {
			for _, k := range []string{"name", "title", "label"} {
				if s, ok := props[k].(string); ok && s != "" {
					return s
				}
			}
		}
		return ""
	}
}
