package automationdom

import (
	"testing"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/google/uuid"
)

func leaf(field Field, op Operator, value any) *ConditionLeaf {
	return &ConditionLeaf{Field: field, Operator: op, Value: value}
}

func TestConditionLeaf_Evaluate_NilMatchesEverything(t *testing.T) {
	var l *ConditionLeaf
	if !l.Evaluate(&taskdom.Task{}) {
		t.Fatal("expected nil condition leaf to evaluate true")
	}
}

func TestConditionLeaf_Evaluate_StatusEquals(t *testing.T) {
	statusID := uuid.New()
	task := &taskdom.Task{StatusID: &statusID}

	n := leaf(FieldStatus, OpEquals, statusID.String())
	if !n.Evaluate(task) {
		t.Fatal("expected status_id equals match to be true")
	}

	other := uuid.New()
	n2 := leaf(FieldStatus, OpEquals, other.String())
	if n2.Evaluate(task) {
		t.Fatal("expected status_id equals mismatch to be false")
	}
}

func TestConditionLeaf_Evaluate_StatusIsEmpty(t *testing.T) {
	task := &taskdom.Task{StatusID: nil}
	if !leaf(FieldStatus, OpIsEmpty, nil).Evaluate(task) {
		t.Fatal("expected is_empty true for nil status")
	}
	if leaf(FieldStatus, OpIsNotEmpty, nil).Evaluate(task) {
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
		got := leaf(FieldPriority, c.op, c.value).Evaluate(task)
		if got != c.want {
			t.Errorf("importance=5 %s %v: got %v, want %v", c.op, c.value, got, c.want)
		}
	}
}

func TestConditionLeaf_Evaluate_TagsContains(t *testing.T) {
	task := &taskdom.Task{Tags: []string{"urgent", "bug"}}
	if !leaf(FieldTag, OpContains, "urgent").Evaluate(task) {
		t.Fatal("expected contains match for present tag")
	}
	if leaf(FieldTag, OpContains, "feature").Evaluate(task) {
		t.Fatal("expected contains mismatch for absent tag")
	}
	if leaf(FieldTag, OpIsEmpty, nil).Evaluate(task) {
		t.Fatal("expected is_empty false when tags present")
	}
}

func TestConditionLeaf_Evaluate_AssigneeContains(t *testing.T) {
	memberID := uuid.New()
	task := &taskdom.Task{AssigneeIDs: []uuid.UUID{memberID}}
	if !leaf(FieldAssignee, OpContains, memberID.String()).Evaluate(task) {
		t.Fatal("expected contains match for assigned member")
	}
	other := uuid.New()
	if leaf(FieldAssignee, OpContains, other.String()).Evaluate(task) {
		t.Fatal("expected contains mismatch for unassigned member")
	}
}

func TestConditionLeaf_Evaluate_CustomField(t *testing.T) {
	task := &taskdom.Task{CustomFields: map[string]any{"release_tag": "v2"}}
	l := &ConditionLeaf{Field: FieldCustomField, FieldKey: "release_tag", Operator: OpEquals, Value: "v2"}
	if !l.Evaluate(task) {
		t.Fatal("expected custom_field equals match")
	}
	missing := &ConditionLeaf{Field: FieldCustomField, FieldKey: "not_set", Operator: OpIsEmpty}
	if !missing.Evaluate(task) {
		t.Fatal("expected is_empty true for a custom field key that isn't set")
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
