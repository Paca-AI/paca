package exportsvc

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

func TestNeutralizeFormula(t *testing.T) {
	for in, want := range map[string]string{
		"":              "",
		"plain":         "plain",
		"=SUM(A1)":      "'=SUM(A1)",
		"+1":            "'+1",
		"-1":            "'-1",
		"@cmd":          "'@cmd",
		"\tpad":         "'\tpad",
		"a=b":           "a=b",
		"2026-01-02":    "2026-01-02",
		"nested =1":     "nested =1",
		"\rcarriage":    "'\rcarriage",
		"hello, world":  "hello, world",
		"multi\nline =": "multi\nline =",
	} {
		if got := neutralizeFormula(in); got != want {
			t.Errorf("neutralizeFormula(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDescriptionText(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"paragraph","content":[{"type":"text","text":"Hello "},{"type":"text","text":"world"}]},
		{"type":"paragraph","content":[{"type":"link","href":"x","content":[{"type":"text","text":"a link"}]}]},
		{"type":"paragraph","content":[{"type":"teamMention","props":{"id":"1","name":"Ada"}}]},
		{"type":"bulletListItem","content":[{"type":"text","text":"parent"}],"children":[
			{"type":"bulletListItem","content":[{"type":"text","text":"child"}]}]},
		{"type":"paragraph","content":[]}
	]`)
	want := "Hello world\na link\nAda\nparent\nchild"
	if got := descriptionText(raw); got != want {
		t.Errorf("descriptionText = %q, want %q", got, want)
	}
	for _, empty := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage("not json")} {
		if got := descriptionText(empty); got != "" {
			t.Errorf("descriptionText(%q) = %q, want empty", empty, got)
		}
	}
}

func TestCustomFieldString(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"x", "x"},
		{true, "true"},
		{float64(3), "3"},
		{2.5, "2.5"},
		{[]any{"a", "b"}, "a; b"},
		{map[string]any{"k": "v"}, `{"k":"v"}`},
	} {
		if got := customFieldString(tc.in); got != tc.want {
			t.Errorf("customFieldString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExportFileName(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]string{
		"Paca":                   "Paca-export-2026-10-03.zip",
		"My Project / v2":        "My-Project-v2-export-2026-10-03.zip",
		`evil"; filename="x.exe`: "evil-filename-x.exe-export-2026-10-03.zip",
		"!!!":                    "project-export-2026-10-03.zip",
		"":                       "project-export-2026-10-03.zip",
		"../../etc/passwd":       "etc-passwd-export-2026-10-03.zip",
	} {
		if got := exportFileName(in, now); got != want {
			t.Errorf("exportFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCSVWriter_ResolvesNamesAndEscapes(t *testing.T) {
	statusID, typeID, sprintID, memberA, memberB, parentID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	look := &lookups{
		taskIDPrefix: "PAC",
		statuses:     map[uuid.UUID]*taskdom.TaskStatus{statusID: {Name: "In Review", Category: taskdom.StatusCategoryInProgress}},
		types:        map[uuid.UUID]*taskdom.TaskType{typeID: {Name: "Bug"}},
		sprints:      map[uuid.UUID]string{sprintID: "Sprint 1"},
		members:      map[uuid.UUID]string{memberA: "Ada", memberB: "Grace"},
		taskNumbers:  map[uuid.UUID]int64{parentID: 7},
		fields: []*taskdom.CustomFieldDefinition{
			{FieldKey: "env", DisplayName: "Environment"},
			{FieldKey: "tags2", DisplayName: "Labels"},
		},
	}
	var out bytes.Buffer
	cw, err := newCSVWriter(&out, look, tasksHeader(look.fields))
	if err != nil {
		t.Fatal(err)
	}
	sp := 5
	start := time.Date(2026, 1, 2, 23, 0, 0, 0, time.UTC)
	created := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	task := &taskdom.Task{
		ID: uuid.New(), TaskNumber: 12, Title: `=HYPERLINK("http://x")`,
		Description: json.RawMessage(`[{"type":"paragraph","content":[{"type":"text","text":"line, with comma"}]}]`),
		TaskTypeID:  &typeID, StatusID: &statusID, SprintID: &sprintID, ParentTaskID: &parentID,
		AssigneeIDs: []uuid.UUID{memberA, memberB}, ReporterID: &memberA,
		Importance: 3, StoryPoints: &sp, StartDate: &start, Tags: []string{"api", "ui"},
		CustomFields: map[string]any{"env": "prod", "tags2": []any{"x", "y"}},
		CreatedAt:    created, UpdatedAt: created,
	}
	if err := cw.writeTask(task); err != nil {
		t.Fatal(err)
	}
	if err := cw.finish(); err != nil {
		t.Fatal(err)
	}
	data := out.Bytes()
	if !strings.HasPrefix(string(data), csvBOM) {
		t.Fatal("missing UTF-8 BOM")
	}
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), csvBOM))).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("want header + 1 row, got %d", len(recs))
	}
	got := map[string]string{}
	for i, h := range recs[0] {
		got[h] = recs[1][i]
	}
	want := map[string]string{
		"ID": "PAC-12", "Title": `'=HYPERLINK("http://x")`, "Description": "line, with comma",
		"Type": "Bug", "Status": "In Review", "Status Category": "inprogress", "Sprint": "Sprint 1",
		"Parent": "PAC-7", "Assignees": "Ada; Grace", "Reporter": "Ada", "Priority": "3",
		"Story Points": "5", "Start Date": "2026-01-02", "Due Date": "", "Tags": "api; ui",
		"Created At": "2026-01-01T10:00:00Z", "Environment": "prod", "Labels": "x; y",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("column %q = %q, want %q", k, got[k], v)
		}
	}
	if cw.rows != 1 {
		t.Errorf("rows = %d, want 1", cw.rows)
	}
}

func TestCSVWriter_MissingReferencesYieldEmptyCells(t *testing.T) {
	gone := uuid.New()
	look := &lookups{
		statuses: map[uuid.UUID]*taskdom.TaskStatus{}, types: map[uuid.UUID]*taskdom.TaskType{},
		sprints: map[uuid.UUID]string{}, members: map[uuid.UUID]string{}, taskNumbers: map[uuid.UUID]int64{},
	}
	var out bytes.Buffer
	cw, _ := newCSVWriter(&out, look, tasksHeader(nil))
	task := &taskdom.Task{ID: uuid.New(), TaskNumber: 1, Title: "t", StatusID: &gone, TaskTypeID: &gone,
		SprintID: &gone, ParentTaskID: &gone, AssigneeIDs: []uuid.UUID{gone}, ReporterID: &gone}
	if err := cw.writeTask(task); err != nil {
		t.Fatal(err)
	}
	_ = cw.finish()
	data := out.Bytes()
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), csvBOM))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if recs[1][0] != "1" {
		t.Errorf("ID without prefix = %q, want 1", recs[1][0])
	}
	for _, i := range []int{3, 4, 5, 6, 7, 8, 9} {
		if recs[1][i] != "" {
			t.Errorf("%s = %q, want empty", recs[0][i], recs[1][i])
		}
	}
}

func TestCSVWriter_NeutralizesHeaderCells(t *testing.T) {
	var out bytes.Buffer
	look := &lookups{taskNumbers: map[uuid.UUID]int64{}}
	// A custom field's display name is user-controlled and lands in the header.
	fields := []*taskdom.CustomFieldDefinition{
		{FieldKey: "a", DisplayName: `=HYPERLINK("http://evil","x")`},
		{FieldKey: "b", DisplayName: "+1"},
		{FieldKey: "c", DisplayName: "Fine"},
	}
	cw, err := newCSVWriter(&out, look, tasksHeader(fields))
	if err != nil {
		t.Fatal(err)
	}
	if err := cw.finish(); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(out.String(), csvBOM))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	header := recs[0]
	n := len(header)
	if got := header[n-3:]; got[0] != `'=HYPERLINK("http://evil","x")` || got[1] != "'+1" || got[2] != "Fine" {
		t.Errorf("custom-field header cells = %q", got)
	}
}
