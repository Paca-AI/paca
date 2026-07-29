package automationdom

import (
	"fmt"
	"strings"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/google/uuid"
)

// Operator is a leaf comparison operator.
type Operator string

const (
	OpEquals      Operator = "equals"
	OpNotEquals   Operator = "not_equals"
	OpContains    Operator = "contains"
	OpGreaterThan Operator = "greater_than"
	OpLessThan    Operator = "less_than"
	OpIsEmpty     Operator = "is_empty"
	OpIsNotEmpty  Operator = "is_not_empty"
)

// Field is a leaf's comparison target on the task.
type Field string

const (
	FieldStatus      Field = "status_id"
	FieldTaskType    Field = "task_type_id"
	FieldPriority    Field = "importance"
	FieldAssignee    Field = "assignee_ids"
	FieldTag         Field = "tags"
	FieldCustomField Field = "custom_field"
)

// ConditionLeaf is a single comparison against one field of the task — the
// whole of a Condition branch's tree (no AND/OR/NOT nesting).
type ConditionLeaf struct {
	Field Field `json:"field"`
	// FieldKey names the custom-field definition key when Field is
	// FieldCustomField; unused otherwise.
	FieldKey string   `json:"field_key,omitempty"`
	Operator Operator `json:"operator"`
	Value    any      `json:"value,omitempty"`
	// Target retargets this leaf onto a task other than the walk's own
	// bound task (nil/self = today's behavior, evaluated via Evaluate
	// directly). See TaskTarget.
	Target *TaskTarget `json:"target,omitempty"`
	// MatchMode controls how a MultiValued Target's resolved tasks combine
	// into one true/false: "all" requires every resolved task to satisfy
	// the leaf; anything else (including "" and "any") requires only one.
	// Ignored when Target isn't set or isn't MultiValued.
	MatchMode string `json:"match_mode,omitempty"`
}

// Evaluate reports whether the leaf is satisfied by task. A nil leaf
// evaluates to true (an empty condition matches everything), matching the
// old rule engine's WrapLegacyFilter behavior for "no filter".
func (l *ConditionLeaf) Evaluate(task *taskdom.Task) bool {
	if l == nil {
		return true
	}
	switch l.Field {
	case FieldStatus:
		return compareUUIDPtr(task.StatusID, l.Operator, l.Value)
	case FieldTaskType:
		return compareUUIDPtr(task.TaskTypeID, l.Operator, l.Value)
	case FieldPriority:
		return compareInt(task.Importance, l.Operator, l.Value)
	case FieldAssignee:
		ids := make([]string, len(task.AssigneeIDs))
		for i, id := range task.AssigneeIDs {
			ids[i] = id.String()
		}
		return compareStringSlice(ids, l.Operator, l.Value)
	case FieldTag:
		return compareStringSlice(task.Tags, l.Operator, l.Value)
	case FieldCustomField:
		v, ok := task.CustomFields[l.FieldKey]
		if !ok {
			return l.Operator == OpIsEmpty
		}
		return compareAny(v, l.Operator, l.Value)
	default:
		return false
	}
}

// EvaluateAgainstTasks combines Evaluate across every task in tasks
// according to matchMode: "all" requires every task to satisfy the leaf;
// anything else (including "" and "any") requires only one. An empty tasks
// slice is always false regardless of matchMode — there's nothing to
// check, so the condition can't be considered satisfied either way (a
// vacuous "all blocking tasks are Done" reading true when there ARE no
// blocking tasks would be surprising, not what an author configuring this
// expects).
func (l *ConditionLeaf) EvaluateAgainstTasks(tasks []*taskdom.Task, matchMode string) bool {
	if len(tasks) == 0 {
		return false
	}
	if matchMode == "all" {
		for _, t := range tasks {
			if !l.Evaluate(t) {
				return false
			}
		}
		return true
	}
	for _, t := range tasks {
		if l.Evaluate(t) {
			return true
		}
	}
	return false
}

// Validate reports whether the leaf is well-formed: it has a field, a
// field_key when the field is custom_field, and a recognized operator.
func (l *ConditionLeaf) Validate() error {
	if l == nil {
		return nil
	}
	if l.Field == "" {
		return fmt.Errorf("automation: condition leaf requires a field")
	}
	if l.Field == FieldCustomField && strings.TrimSpace(l.FieldKey) == "" {
		return fmt.Errorf("automation: custom_field leaf requires field_key")
	}
	switch l.Operator {
	case OpEquals, OpNotEquals, OpContains, OpGreaterThan, OpLessThan, OpIsEmpty, OpIsNotEmpty:
	default:
		return fmt.Errorf("automation: unknown condition operator %q", l.Operator)
	}
	return nil
}

func compareUUIDPtr(field *uuid.UUID, op Operator, value any) bool {
	s := ""
	if field != nil {
		s = field.String()
	}
	switch op {
	case OpIsEmpty:
		return s == ""
	case OpIsNotEmpty:
		return s != ""
	case OpEquals:
		return s != "" && s == fmt.Sprintf("%v", value)
	case OpNotEquals:
		return s != fmt.Sprintf("%v", value)
	default:
		return false
	}
}

func compareInt(field int, op Operator, value any) bool {
	var target float64
	switch v := value.(type) {
	case float64:
		target = v
	case int:
		target = float64(v)
	default:
		return false
	}
	f := float64(field)
	switch op {
	case OpEquals:
		return f == target
	case OpNotEquals:
		return f != target
	case OpGreaterThan:
		return f > target
	case OpLessThan:
		return f < target
	default:
		return false
	}
}

func compareStringSlice(field []string, op Operator, value any) bool {
	switch op {
	case OpIsEmpty:
		return len(field) == 0
	case OpIsNotEmpty:
		return len(field) != 0
	case OpContains:
		want := fmt.Sprintf("%v", value)
		for _, s := range field {
			if s == want {
				return true
			}
		}
		return false
	case OpNotEquals:
		want := fmt.Sprintf("%v", value)
		for _, s := range field {
			if s == want {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func compareAny(field any, op Operator, value any) bool {
	switch op {
	case OpIsEmpty:
		return field == nil || field == ""
	case OpIsNotEmpty:
		return field != nil && field != ""
	case OpEquals:
		return fmt.Sprintf("%v", field) == fmt.Sprintf("%v", value)
	case OpNotEquals:
		return fmt.Sprintf("%v", field) != fmt.Sprintf("%v", value)
	case OpGreaterThan, OpLessThan:
		ff, fok := toFloat(field)
		tf, tok := toFloat(value)
		if !fok || !tok {
			return false
		}
		if op == OpGreaterThan {
			return ff > tf
		}
		return ff < tf
	default:
		return false
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}
