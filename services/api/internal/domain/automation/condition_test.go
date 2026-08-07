package automationdom

import (
	"testing"
	"time"

	"github.com/google/uuid"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

func leaf(field Field, op Operator, value any) *ConditionLeaf {
	return &ConditionLeaf{Field: field, Operator: op, Value: value}
}

func TestConditionLeaf_Evaluate_NilMatchesEverything(t *testing.T) {
	var l *ConditionLeaf
	if !l.Evaluate(&taskdom.Task{}, nil) {
		t.Fatal("expected nil condition leaf to evaluate true")
	}
}

func TestConditionLeaf_Evaluate_StatusEquals(t *testing.T) {
	statusID := uuid.New()
	task := &taskdom.Task{StatusID: &statusID}

	n := leaf(FieldStatus, OpEquals, statusID.String())
	if !n.Evaluate(task, nil) {
		t.Fatal("expected status_id equals match to be true")
	}

	other := uuid.New()
	n2 := leaf(FieldStatus, OpEquals, other.String())
	if n2.Evaluate(task, nil) {
		t.Fatal("expected status_id equals mismatch to be false")
	}
}

func TestConditionLeaf_Evaluate_StatusIsEmpty(t *testing.T) {
	task := &taskdom.Task{StatusID: nil}
	if !leaf(FieldStatus, OpIsEmpty, nil).Evaluate(task, nil) {
		t.Fatal("expected is_empty true for nil status")
	}
	if leaf(FieldStatus, OpIsNotEmpty, nil).Evaluate(task, nil) {
		t.Fatal("expected is_not_empty false for nil status")
	}
}

func TestConditionLeaf_Evaluate_PriorityComparisons(t *testing.T) {
	task := &taskdom.Task{Importance: 5}

	cases := []struct {
		op    Operator
		value float64
		want  bool
	}{
		{OpEquals, 5, true},
		{OpEquals, 3, false},
		{OpNotEquals, 3, true},
		{OpGreaterThan, 3, true},
		{OpGreaterThan, 5, false},
		{OpLessThan, 10, true},
		{OpLessThan, 5, false},
	}
	for _, c := range cases {
		got := leaf(FieldPriority, c.op, c.value).Evaluate(task, nil)
		if got != c.want {
			t.Errorf("importance=5 %s %v: got %v, want %v", c.op, c.value, got, c.want)
		}
	}
}

func TestConditionLeaf_Evaluate_TagsContains(t *testing.T) {
	task := &taskdom.Task{Tags: []string{"urgent", "bug"}}
	if !leaf(FieldTag, OpContains, "urgent").Evaluate(task, nil) {
		t.Fatal("expected contains match for present tag")
	}
	if leaf(FieldTag, OpContains, "feature").Evaluate(task, nil) {
		t.Fatal("expected contains mismatch for absent tag")
	}
	if leaf(FieldTag, OpIsEmpty, nil).Evaluate(task, nil) {
		t.Fatal("expected is_empty false when tags present")
	}
}

func TestConditionLeaf_Evaluate_AssigneeContains(t *testing.T) {
	memberID := uuid.New()
	task := &taskdom.Task{AssigneeIDs: []uuid.UUID{memberID}}
	if !leaf(FieldAssignee, OpContains, memberID.String()).Evaluate(task, nil) {
		t.Fatal("expected contains match for assigned member")
	}
	other := uuid.New()
	if leaf(FieldAssignee, OpContains, other.String()).Evaluate(task, nil) {
		t.Fatal("expected contains mismatch for unassigned member")
	}
}

func TestConditionLeaf_Evaluate_CustomField(t *testing.T) {
	task := &taskdom.Task{CustomFields: map[string]any{"release_tag": "v2"}}
	l := &ConditionLeaf{Field: FieldCustomField, FieldKey: "release_tag", Operator: OpEquals, Value: "v2"}
	if !l.Evaluate(task, nil) {
		t.Fatal("expected custom_field equals match")
	}
	missing := &ConditionLeaf{Field: FieldCustomField, FieldKey: "not_set", Operator: OpIsEmpty}
	if !missing.Evaluate(task, nil) {
		t.Fatal("expected is_empty true for a custom field key that isn't set")
	}
}

// Dates travel over the wire as RFC 3339 strings everywhere else in this
// API (see compareTimePtr's doc comment) — the automation config panel's
// start_date/due_date pickers must send that shape, not a bare
// "YYYY-MM-DD" (what a plain <input type="date"> produces), or every
// comparison here silently evaluates false.
func TestConditionLeaf_Evaluate_DueDateComparisons(t *testing.T) {
	due := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	task := &taskdom.Task{DueDate: &due}

	cases := []struct {
		op    Operator
		value string
		want  bool
	}{
		{OpEquals, "2026-08-03T00:00:00Z", true},
		{OpEquals, "2026-08-04T00:00:00Z", false},
		{OpNotEquals, "2026-08-04T00:00:00Z", true},
		{OpGreaterThan, "2026-08-02T00:00:00Z", true},
		{OpGreaterThan, "2026-08-04T00:00:00Z", false},
		{OpLessThan, "2026-08-04T00:00:00Z", true},
		{OpLessThan, "2026-08-02T00:00:00Z", false},
	}
	for _, c := range cases {
		got := leaf(FieldDueDate, c.op, c.value).Evaluate(task, nil)
		if got != c.want {
			t.Errorf("due_date=2026-08-03 %s %v: got %v, want %v", c.op, c.value, got, c.want)
		}
	}
}

func TestConditionLeaf_Evaluate_DueDateIsEmpty(t *testing.T) {
	task := &taskdom.Task{DueDate: nil}
	if !leaf(FieldDueDate, OpIsEmpty, nil).Evaluate(task, nil) {
		t.Fatal("expected is_empty true for nil due_date")
	}
	if leaf(FieldDueDate, OpIsNotEmpty, nil).Evaluate(task, nil) {
		t.Fatal("expected is_not_empty false for nil due_date")
	}

	due := time.Now()
	set := &taskdom.Task{DueDate: &due}
	if leaf(FieldDueDate, OpIsEmpty, nil).Evaluate(set, nil) {
		t.Fatal("expected is_empty false when due_date is set")
	}
}

// TestConditionLeaf_Evaluate_DateFieldRejectsNonRFC3339Value guards against
// the exact regression this once had: a bare "YYYY-MM-DD" value (what a
// native <input type="date"> produces) must not silently match — it isn't
// a valid RFC 3339 string, so time.Parse fails and the comparator falls
// back to its documented not-found-not-panic false, same as a mistyped
// numeric value would for compareInt.
func TestConditionLeaf_Evaluate_DateFieldRejectsNonRFC3339Value(t *testing.T) {
	due := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	task := &taskdom.Task{DueDate: &due}
	if leaf(FieldDueDate, OpEquals, "2026-08-03").Evaluate(task, nil) {
		t.Fatal("expected a bare date-only value (not RFC 3339) to fail to match, not silently succeed")
	}
}

func TestConditionLeaf_Validate(t *testing.T) {
	if err := (&ConditionLeaf{}).Validate(); err == nil {
		t.Fatal("expected error for a leaf with no field")
	}
	badLeaf := &ConditionLeaf{Field: FieldCustomField, Operator: OpEquals}
	if err := badLeaf.Validate(); err == nil {
		t.Fatal("expected error for custom_field leaf missing field_key")
	}
	unknownOp := &ConditionLeaf{Field: FieldStatus, Operator: "bogus"}
	if err := unknownOp.Validate(); err == nil {
		t.Fatal("expected error for unknown operator")
	}
	if err := leaf(FieldStatus, OpEquals, "x").Validate(); err != nil {
		t.Fatalf("expected valid leaf to pass validation, got %v", err)
	}
}

func TestConditionLeaf_Validate_RejectsUnknownField(t *testing.T) {
	if err := leaf(Field("bogus"), OpEquals, "x").Validate(); err == nil {
		t.Fatal("expected error for unrecognized field")
	}
}

func TestConditionLeaf_Validate_RejectsOperatorNotImplementedForField(t *testing.T) {
	// compareStringSlice (tags/assignee_ids) has no OpEquals case — it would
	// silently and permanently evaluate false rather than erroring, so
	// Validate must reject the combination instead of letting it through.
	if err := leaf(FieldTag, OpEquals, "urgent").Validate(); err == nil {
		t.Fatal("expected error for tags + equals (not implemented by compareStringSlice)")
	}
	if err := leaf(FieldAssignee, OpEquals, "x").Validate(); err == nil {
		t.Fatal("expected error for assignee_ids + equals (not implemented by compareStringSlice)")
	}
	// compareInt (importance) has no is_empty/is_not_empty/contains case.
	if err := leaf(FieldPriority, OpIsEmpty, nil).Validate(); err == nil {
		t.Fatal("expected error for importance + is_empty (not implemented by compareInt)")
	}
	// Sanity check: the combinations each compare function DOES implement
	// still pass.
	if err := leaf(FieldTag, OpContains, "urgent").Validate(); err != nil {
		t.Fatalf("expected tags + contains to be valid, got %v", err)
	}
	if err := leaf(FieldPriority, OpGreaterThan, 1).Validate(); err != nil {
		t.Fatalf("expected importance + greater_than to be valid, got %v", err)
	}
}

func TestConditionLeaf_EvaluateAgainstTasks_EmptyIsAlwaysFalse(t *testing.T) {
	n := leaf(FieldTag, OpIsEmpty, nil)
	if n.EvaluateAgainstTasks(nil, "any") {
		t.Fatal("expected an empty task list to be false for match_mode any")
	}
	if n.EvaluateAgainstTasks(nil, "all") {
		t.Fatal("expected an empty task list to be false for match_mode all (no vacuous truth)")
	}
}

func TestConditionLeaf_EvaluateAgainstTasks_AnyRequiresOneMatch(t *testing.T) {
	statusID := uuid.New()
	other := uuid.New()
	n := leaf(FieldStatus, OpEquals, statusID.String())
	tasks := []*taskdom.Task{{StatusID: &other}, {StatusID: &statusID}, {StatusID: &other}}
	if !n.EvaluateAgainstTasks(tasks, "any") {
		t.Fatal("expected any-mode to match when at least one task satisfies the leaf")
	}
	if !n.EvaluateAgainstTasks(tasks, "") {
		t.Fatal("expected empty match_mode to default to any semantics")
	}
	noMatch := []*taskdom.Task{{StatusID: &other}, {StatusID: &other}}
	if n.EvaluateAgainstTasks(noMatch, "any") {
		t.Fatal("expected any-mode to be false when no task satisfies the leaf")
	}
}

func TestConditionLeaf_EvaluateAgainstTasks_AllRequiresEveryMatch(t *testing.T) {
	statusID := uuid.New()
	other := uuid.New()
	n := leaf(FieldStatus, OpEquals, statusID.String())
	allMatch := []*taskdom.Task{{StatusID: &statusID}, {StatusID: &statusID}}
	if !n.EvaluateAgainstTasks(allMatch, "all") {
		t.Fatal("expected all-mode to match when every task satisfies the leaf")
	}
	mixed := []*taskdom.Task{{StatusID: &statusID}, {StatusID: &other}}
	if n.EvaluateAgainstTasks(mixed, "all") {
		t.Fatal("expected all-mode to be false when any task fails to satisfy the leaf")
	}
}

// --- sprint-scoped fields -----------------------------------------------------

func TestConditionLeaf_Evaluate_SprintFields_NilSprintIsFalse(t *testing.T) {
	for _, f := range []Field{FieldSprintName, FieldSprintStatus, FieldSprintGoal, FieldSprintStartDate, FieldSprintEndDate} {
		if leaf(f, OpEquals, "x").Evaluate(&taskdom.Task{}, nil) {
			t.Fatalf("expected sprint field %q to evaluate false against a nil sprint, not panic or match", f)
		}
	}
}

func TestConditionLeaf_Evaluate_TaskFields_NilTaskIsFalse(t *testing.T) {
	if leaf(FieldStatus, OpIsEmpty, nil).Evaluate(nil, nil) {
		// is_empty on a nil task's nil status would otherwise be true —
		// confirm the nil-task guard short-circuits before the switch, not
		// just coincidentally matching.
		t.Fatal("expected a task field to evaluate false against a nil task, not fall through to a field-specific default")
	}
}

func TestConditionLeaf_Evaluate_SprintName(t *testing.T) {
	sprint := &sprintdom.Sprint{Name: "Sprint 4"}
	if !leaf(FieldSprintName, OpEquals, "Sprint 4").Evaluate(nil, sprint) {
		t.Fatal("expected sprint_name equals match")
	}
	if !leaf(FieldSprintName, OpContains, "Sprint").Evaluate(nil, sprint) {
		t.Fatal("expected sprint_name contains match")
	}
}

func TestConditionLeaf_Evaluate_SprintStatus(t *testing.T) {
	sprint := &sprintdom.Sprint{Status: sprintdom.SprintStatusActive}
	if !leaf(FieldSprintStatus, OpEquals, "active").Evaluate(nil, sprint) {
		t.Fatal("expected sprint_status equals match")
	}
	if leaf(FieldSprintStatus, OpEquals, "completed").Evaluate(nil, sprint) {
		t.Fatal("expected sprint_status equals mismatch to be false")
	}
}

func TestConditionLeaf_Evaluate_SprintGoal(t *testing.T) {
	if leaf(FieldSprintGoal, OpIsEmpty, nil).Evaluate(nil, &sprintdom.Sprint{}) != true {
		t.Fatal("expected sprint_goal is_empty true when Goal is nil")
	}
	goal := "Ship the automation epic"
	sprint := &sprintdom.Sprint{Goal: &goal}
	if !leaf(FieldSprintGoal, OpContains, "automation").Evaluate(nil, sprint) {
		t.Fatal("expected sprint_goal contains match")
	}
	if leaf(FieldSprintGoal, OpIsEmpty, nil).Evaluate(nil, sprint) {
		t.Fatal("expected sprint_goal is_empty false once Goal is set")
	}
}

func TestConditionLeaf_Evaluate_SprintDates(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	sprint := &sprintdom.Sprint{StartDate: &start, EndDate: &end}
	if !leaf(FieldSprintStartDate, OpEquals, "2026-08-01T00:00:00Z").Evaluate(nil, sprint) {
		t.Fatal("expected sprint_start_date equals match")
	}
	if !leaf(FieldSprintEndDate, OpGreaterThan, "2026-08-01T00:00:00Z").Evaluate(nil, sprint) {
		t.Fatal("expected sprint_end_date greater_than match")
	}
	if !leaf(FieldSprintStartDate, OpIsEmpty, nil).Evaluate(nil, &sprintdom.Sprint{}) {
		t.Fatal("expected sprint_start_date is_empty true for an unset date")
	}
}

func TestConditionLeaf_Validate_SprintFields(t *testing.T) {
	if err := leaf(FieldSprintStatus, OpEquals, "active").Validate(); err != nil {
		t.Fatalf("expected valid sprint_status leaf to pass, got %v", err)
	}
	if err := leaf(FieldSprintStatus, OpContains, "active").Validate(); err == nil {
		t.Fatal("expected sprint_status + contains to be rejected (not implemented for that field)")
	}
	if err := leaf(FieldSprintName, OpContains, "x").Validate(); err != nil {
		t.Fatalf("expected valid sprint_name + contains leaf to pass, got %v", err)
	}
}

func TestIsSprintField(t *testing.T) {
	for _, f := range []Field{FieldSprintName, FieldSprintStatus, FieldSprintGoal, FieldSprintStartDate, FieldSprintEndDate} {
		if !IsSprintField(f) {
			t.Fatalf("expected %q to be a sprint field", f)
		}
	}
	for _, f := range []Field{FieldStatus, FieldSprint, FieldTitle} {
		if IsSprintField(f) {
			t.Fatalf("expected %q to NOT be a sprint field", f)
		}
	}
}
