package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// openTaskRepoTestDB sets up an in-memory SQLite DB with a minimal tasks
// table for exercising applyTaskFilter via CountTasks.
func openTaskRepoTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	schema := `
		CREATE TABLE tasks (
			id          TEXT PRIMARY KEY,
			project_id  TEXT NOT NULL,
			deleted_at  DATETIME
		);
		CREATE TABLE task_assignees (
			task_id     TEXT NOT NULL,
			member_id   TEXT NOT NULL,
			assigned_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (task_id, member_id)
		);`
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

// TestTaskRepository_CountTasks_AssigneeNullWithAssigneeIDs is a regression
// test for https://github.com/Paca-AI/paca/issues/272: combining the
// "unassigned" filter with an "assigned to specific users" filter used to
// produce a WHERE clause with unbalanced parentheses, which crashed with a
// SQL syntax error instead of returning matching tasks.
func TestTaskRepository_CountTasks_AssigneeNullWithAssigneeIDs(t *testing.T) {
	db := openTaskRepoTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	projectID := uuid.New()
	assigneeIn := uuid.New()
	assigneeOut := uuid.New()

	taskUnassigned := uuid.New().String()
	taskIn := uuid.New().String()
	taskOut := uuid.New().String()
	db.MustExec(`INSERT INTO tasks (id, project_id) VALUES ($1, $2)`, taskUnassigned, projectID.String())
	db.MustExec(`INSERT INTO tasks (id, project_id) VALUES ($1, $2)`, taskIn, projectID.String())
	db.MustExec(`INSERT INTO tasks (id, project_id) VALUES ($1, $2)`, taskOut, projectID.String())
	db.MustExec(`INSERT INTO task_assignees (task_id, member_id) VALUES ($1, $2)`, taskIn, assigneeIn.String())
	db.MustExec(`INSERT INTO task_assignees (task_id, member_id) VALUES ($1, $2)`, taskOut, assigneeOut.String())

	filter := taskdom.TaskFilter{
		AssigneeNull: true,
		AssigneeIDs:  []uuid.UUID{assigneeIn},
	}

	count, err := repo.CountTasks(ctx, projectID, filter)
	if err != nil {
		t.Fatalf("expected no error combining AssigneeNull with AssigneeIDs, got: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 tasks (unassigned + matching assignee), got %d", count)
	}
}

// TestSyncTaskAssignees_PreservesUnchangedRows is a regression test: UpdateTask
// used to unconditionally DELETE-then-reinsert every task_assignees row on
// every call, which reset assigned_at for every assignee even when an update
// didn't touch assignees at all (e.g. renaming a task), corrupting
// assignment history and making the assigned_at-ordered assignee list
// (used for "first assignee" swimlane grouping and avatar-stack order)
// reorder nondeterministically after unrelated edits. syncTaskAssignees must
// leave rows for members present in both the old and new set untouched, and
// only remove/insert the actual diff.
func TestSyncTaskAssignees_PreservesUnchangedRows(t *testing.T) {
	db := openTaskRepoTestDB(t)
	ctx := context.Background()

	taskID := uuid.New()
	memberA := uuid.New()
	memberB := uuid.New()
	memberC := uuid.New()

	db.MustExec(`INSERT INTO tasks (id, project_id) VALUES ($1, $2)`, taskID.String(), uuid.New().String())
	db.MustExec(`INSERT INTO task_assignees (task_id, member_id, assigned_at) VALUES ($1, $2, $3)`,
		taskID.String(), memberA.String(), "2020-01-01T00:00:00Z")
	db.MustExec(`INSERT INTO task_assignees (task_id, member_id, assigned_at) VALUES ($1, $2, $3)`,
		taskID.String(), memberB.String(), "2020-01-02T00:00:00Z")

	type row struct {
		MemberID   string `db:"member_id"`
		AssignedAt string `db:"assigned_at"`
	}
	readRows := func() map[string]string {
		t.Helper()
		var rows []row
		if err := db.SelectContext(ctx, &rows, `SELECT member_id, assigned_at FROM task_assignees WHERE task_id=$1`, taskID.String()); err != nil {
			t.Fatalf("query rows: %v", err)
		}
		out := make(map[string]string, len(rows))
		for _, r := range rows {
			out[r.MemberID] = r.AssignedAt
		}
		return out
	}

	// A no-op sync (wantIDs identical to the current set, simulating an
	// update that doesn't touch assignee_ids at all) must not touch
	// assigned_at for either existing member.
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := syncTaskAssignees(ctx, tx, taskID, []uuid.UUID{memberA, memberB}); err != nil {
		t.Fatalf("syncTaskAssignees (no-op): %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	got := readRows()
	if len(got) != 2 {
		t.Fatalf("expected 2 assignee rows to survive a no-op sync, got %+v", got)
	}
	if got[memberA.String()] != "2020-01-01T00:00:00Z" || got[memberB.String()] != "2020-01-02T00:00:00Z" {
		t.Fatalf("expected assigned_at to be preserved by a no-op sync, got %+v", got)
	}

	// A real change (drop B, add C) must preserve A's original assigned_at,
	// remove B, and add C.
	tx2, err := db.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := syncTaskAssignees(ctx, tx2, taskID, []uuid.UUID{memberA, memberC}); err != nil {
		t.Fatalf("syncTaskAssignees (change): %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	got = readRows()
	if _, stillPresent := got[memberB.String()]; stillPresent {
		t.Fatalf("expected memberB to be removed after dropping it from wantIDs, got %+v", got)
	}
	if got[memberA.String()] != "2020-01-01T00:00:00Z" {
		t.Fatalf("expected memberA's original assigned_at to survive an unrelated diff, got %q", got[memberA.String()])
	}
	if _, added := got[memberC.String()]; !added {
		t.Fatalf("expected memberC to be newly inserted, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// applyCustomFieldFilters
// ---------------------------------------------------------------------------

func f64Ptr(v float64) *float64 { return &v }
func strPtr(v string) *string   { return &v }

func TestApplyCustomFieldFilters_Select(t *testing.T) {
	b := newQueryBuilder()
	applyCustomFieldFilters(b, map[string]taskdom.CustomFieldFilterQuery{
		"priority": {FieldType: "select", Values: []string{"High", "Urgent"}},
	})
	wantClause := "(custom_fields->>$1) IN ($2,$3)"
	if len(b.whereClauses) != 1 || b.whereClauses[0] != wantClause {
		t.Fatalf("whereClauses = %+v, want [%q]", b.whereClauses, wantClause)
	}
	wantArgs := []interface{}{"priority", "High", "Urgent"}
	if !reflect.DeepEqual(b.args, wantArgs) {
		t.Fatalf("args = %+v, want %+v", b.args, wantArgs)
	}
}

func TestApplyCustomFieldFilters_Boolean(t *testing.T) {
	b := newQueryBuilder()
	applyCustomFieldFilters(b, map[string]taskdom.CustomFieldFilterQuery{
		"is_blocked": {FieldType: "boolean", Values: []string{"true"}},
	})
	wantClause := "(custom_fields->>$1) IN ($2)"
	if len(b.whereClauses) != 1 || b.whereClauses[0] != wantClause {
		t.Fatalf("whereClauses = %+v, want [%q]", b.whereClauses, wantClause)
	}
	wantArgs := []interface{}{"is_blocked", "true"}
	if !reflect.DeepEqual(b.args, wantArgs) {
		t.Fatalf("args = %+v, want %+v", b.args, wantArgs)
	}
}

func TestApplyCustomFieldFilters_MultiSelect(t *testing.T) {
	b := newQueryBuilder()
	applyCustomFieldFilters(b, map[string]taskdom.CustomFieldFilterQuery{
		"labels": {FieldType: "multi_select", Values: []string{"backend", "urgent"}},
	})
	wantClause := "(custom_fields->$1 @> to_jsonb($2::text) OR custom_fields->$1 @> to_jsonb($3::text))"
	if len(b.whereClauses) != 1 || b.whereClauses[0] != wantClause {
		t.Fatalf("whereClauses = %+v, want [%q]", b.whereClauses, wantClause)
	}
	wantArgs := []interface{}{"labels", "backend", "urgent"}
	if !reflect.DeepEqual(b.args, wantArgs) {
		t.Fatalf("args = %+v, want %+v", b.args, wantArgs)
	}
}

func TestApplyCustomFieldFilters_NumberRange(t *testing.T) {
	b := newQueryBuilder()
	applyCustomFieldFilters(b, map[string]taskdom.CustomFieldFilterQuery{
		"effort": {FieldType: "number", Min: f64Ptr(2), Max: f64Ptr(8)},
	})
	numExpr := customFieldNumericExpr("$1")
	wantClauses := []string{
		numExpr + " >= $2",
		numExpr + " <= $3",
	}
	if !reflect.DeepEqual(b.whereClauses, wantClauses) {
		t.Fatalf("whereClauses = %+v, want %+v", b.whereClauses, wantClauses)
	}
	wantArgs := []interface{}{"effort", 2.0, 8.0}
	if !reflect.DeepEqual(b.args, wantArgs) {
		t.Fatalf("args = %+v, want %+v", b.args, wantArgs)
	}
}

func TestApplyCustomFieldFilters_NumberMinOnly(t *testing.T) {
	b := newQueryBuilder()
	applyCustomFieldFilters(b, map[string]taskdom.CustomFieldFilterQuery{
		"effort": {FieldType: "number", Min: f64Ptr(5)},
	})
	wantClauses := []string{customFieldNumericExpr("$1") + " >= $2"}
	if !reflect.DeepEqual(b.whereClauses, wantClauses) {
		t.Fatalf("whereClauses = %+v, want %+v", b.whereClauses, wantClauses)
	}
}

func TestApplyCustomFieldFilters_DateRange(t *testing.T) {
	b := newQueryBuilder()
	applyCustomFieldFilters(b, map[string]taskdom.CustomFieldFilterQuery{
		"due": {FieldType: "date", After: strPtr("2024-01-01"), Before: strPtr("2024-06-01")},
	})
	dateExpr := customFieldDateExpr("$1")
	wantClauses := []string{
		dateExpr + " >= $2",
		dateExpr + " <= $3",
	}
	if !reflect.DeepEqual(b.whereClauses, wantClauses) {
		t.Fatalf("whereClauses = %+v, want %+v", b.whereClauses, wantClauses)
	}
	wantArgs := []interface{}{"due", "2024-01-01", "2024-06-01"}
	if !reflect.DeepEqual(b.args, wantArgs) {
		t.Fatalf("args = %+v, want %+v", b.args, wantArgs)
	}
}

// TestApplyCustomFieldFilters_InvalidStoredValueDoesNotCrashQuery is a
// regression test: a task can end up with a stray non-numeric/non-date
// string under a field key (e.g. a select-type field was deleted and a new
// date-type field was created reusing the same field_key before the old
// value was purged). A bare ::numeric/::date cast on such a row aborts the
// entire query with a Postgres error instead of just excluding that row, so
// the generated expression must guard the cast with a regex check. This
// asserts the guard is present in the generated SQL text; the regex/cast
// semantics themselves are Postgres-only and can't run against the SQLite
// harness used elsewhere in this file.
func TestApplyCustomFieldFilters_InvalidStoredValueDoesNotCrashQuery(t *testing.T) {
	b := newQueryBuilder()
	applyCustomFieldFilters(b, map[string]taskdom.CustomFieldFilterQuery{
		"due": {FieldType: "date", After: strPtr("2024-01-01")},
	})
	if len(b.whereClauses) != 1 {
		t.Fatalf("expected 1 where clause, got %+v", b.whereClauses)
	}
	clause := b.whereClauses[0]
	if !strings.Contains(clause, "CASE WHEN") || !strings.Contains(clause, "~") {
		t.Fatalf("expected a regex-guarded CASE expression, got %q", clause)
	}
}

func TestApplyCustomFieldFilters_TextContains(t *testing.T) {
	b := newQueryBuilder()
	applyCustomFieldFilters(b, map[string]taskdom.CustomFieldFilterQuery{
		"notes": {FieldType: "text", Contains: strPtr("100% done_now")},
	})
	wantClause := "(custom_fields->>$1) ILIKE $2"
	if len(b.whereClauses) != 1 || b.whereClauses[0] != wantClause {
		t.Fatalf("whereClauses = %+v, want [%q]", b.whereClauses, wantClause)
	}
	wantArgs := []interface{}{"notes", "%100\\% done\\_now%"}
	if !reflect.DeepEqual(b.args, wantArgs) {
		t.Fatalf("args = %+v, want %+v", b.args, wantArgs)
	}
}

func TestApplyCustomFieldFilters_UrlContains(t *testing.T) {
	b := newQueryBuilder()
	applyCustomFieldFilters(b, map[string]taskdom.CustomFieldFilterQuery{
		"link": {FieldType: "url", Contains: strPtr("example.com")},
	})
	if len(b.whereClauses) != 1 {
		t.Fatalf("expected 1 where clause, got %+v", b.whereClauses)
	}
}

func TestApplyCustomFieldFilters_EmptyOrBlankValuesSkipped(t *testing.T) {
	blank := "   "
	b := newQueryBuilder()
	applyCustomFieldFilters(b, map[string]taskdom.CustomFieldFilterQuery{
		"empty_select": {FieldType: "select"},
		"empty_multi":  {FieldType: "multi_select"},
		"empty_number": {FieldType: "number"},
		"empty_date":   {FieldType: "date"},
		"blank_text":   {FieldType: "text", Contains: &blank},
	})
	if len(b.whereClauses) != 0 || len(b.args) != 0 {
		t.Fatalf("expected no-op for empty filter criteria, got clauses=%+v args=%+v", b.whereClauses, b.args)
	}
}

func TestApplyCustomFieldFilters_MultipleFieldsAreSortedForDeterminism(t *testing.T) {
	b := newQueryBuilder()
	applyCustomFieldFilters(b, map[string]taskdom.CustomFieldFilterQuery{
		"zeta":  {FieldType: "text", Contains: strPtr("z")},
		"alpha": {FieldType: "text", Contains: strPtr("a")},
	})
	wantArgs := []interface{}{"alpha", "%a%", "zeta", "%z%"}
	if !reflect.DeepEqual(b.args, wantArgs) {
		t.Fatalf("args = %+v, want %+v (alpha before zeta)", b.args, wantArgs)
	}
}

func TestApplyCustomFieldFilters_ComposesWithBaseFilterClauses(t *testing.T) {
	b := newQueryBuilder()
	pidP := b.placeholder()
	b.args = append(b.args, "proj-1")
	b.whereClauses = append(b.whereClauses, "project_id = "+pidP)
	applyTaskFilter(b, taskdom.TaskFilter{
		CustomFieldFilters: map[string]taskdom.CustomFieldFilterQuery{
			"priority": {FieldType: "select", Values: []string{"High"}},
		},
	})
	wantClauses := []string{
		"project_id = $1",
		"(custom_fields->>$2) IN ($3)",
	}
	if !reflect.DeepEqual(b.whereClauses, wantClauses) {
		t.Fatalf("whereClauses = %+v, want %+v", b.whereClauses, wantClauses)
	}
}

// ---------------------------------------------------------------------------
// Built-in field filters: start_date, due_date, story_points, importance, tags
// ---------------------------------------------------------------------------

func intPtr(v int) *int { return &v }

func TestApplyTaskFilter_DateRanges(t *testing.T) {
	b := newQueryBuilder()
	applyTaskFilter(b, taskdom.TaskFilter{
		StartDateAfter:  strPtr("2024-01-01"),
		StartDateBefore: strPtr("2024-06-01"),
		DueDateAfter:    strPtr("2024-02-01"),
		DueDateBefore:   strPtr("2024-07-01"),
	})
	wantClauses := []string{
		"start_date >= $1",
		"start_date <= $2",
		"due_date >= $3",
		"due_date <= $4",
	}
	if !reflect.DeepEqual(b.whereClauses, wantClauses) {
		t.Fatalf("whereClauses = %+v, want %+v", b.whereClauses, wantClauses)
	}
	wantArgs := []interface{}{"2024-01-01", "2024-06-01", "2024-02-01", "2024-07-01"}
	if !reflect.DeepEqual(b.args, wantArgs) {
		t.Fatalf("args = %+v, want %+v", b.args, wantArgs)
	}
}

func TestApplyTaskFilter_Schedule(t *testing.T) {
	cases := []struct {
		name        string
		schedule    taskdom.ScheduleFilter
		wantClauses []string
		wantArgs    []interface{}
	}{
		{
			name:        "start_and_due_window",
			schedule:    taskdom.ScheduleFilter{From: strPtr("2026-03-01"), To: strPtr("2026-03-31")},
			wantClauses: []string{"GREATEST(start_date, due_date) >= $1", "LEAST(start_date, due_date) <= $2"},
			wantArgs:    []interface{}{"2026-03-01", "2026-03-31"},
		},
		{
			name:        "explicit_start_and_due_from_only",
			schedule:    taskdom.ScheduleFilter{By: taskdom.ScheduleByStartAndDue, From: strPtr("2026-03-01")},
			wantClauses: []string{"GREATEST(start_date, due_date) >= $1"},
			wantArgs:    []interface{}{"2026-03-01"},
		},
		{
			name:        "start_date_window",
			schedule:    taskdom.ScheduleFilter{By: taskdom.ScheduleByStartDate, From: strPtr("2026-03-01"), To: strPtr("2026-03-31")},
			wantClauses: []string{"start_date >= $1", "start_date <= $2"},
			wantArgs:    []interface{}{"2026-03-01", "2026-03-31"},
		},
		{
			name:        "due_date_to_only",
			schedule:    taskdom.ScheduleFilter{By: taskdom.ScheduleByDueDate, To: strPtr("2026-03-31")},
			wantClauses: []string{"due_date <= $1"},
			wantArgs:    []interface{}{"2026-03-31"},
		},
		{
			name:        "unscheduled_start_and_due",
			schedule:    taskdom.ScheduleFilter{Unscheduled: true},
			wantClauses: []string{"(start_date IS NULL AND due_date IS NULL)"},
		},
		{
			name:        "unscheduled_start_date",
			schedule:    taskdom.ScheduleFilter{By: taskdom.ScheduleByStartDate, Unscheduled: true},
			wantClauses: []string{"start_date IS NULL"},
		},
		{
			name:        "unscheduled_due_date",
			schedule:    taskdom.ScheduleFilter{By: taskdom.ScheduleByDueDate, Unscheduled: true},
			wantClauses: []string{"due_date IS NULL"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newQueryBuilder()
			schedule := tc.schedule
			applyTaskFilter(b, taskdom.TaskFilter{Schedule: &schedule})
			if !reflect.DeepEqual(b.whereClauses, tc.wantClauses) {
				t.Fatalf("whereClauses = %+v, want %+v", b.whereClauses, tc.wantClauses)
			}
			if len(b.args) != len(tc.wantArgs) || (len(tc.wantArgs) > 0 && !reflect.DeepEqual(b.args, tc.wantArgs)) {
				t.Fatalf("args = %+v, want %+v", b.args, tc.wantArgs)
			}
		})
	}
}

func TestApplyTaskFilter_DateRanges_OnlyOneBoundSet(t *testing.T) {
	b := newQueryBuilder()
	applyTaskFilter(b, taskdom.TaskFilter{DueDateBefore: strPtr("2024-12-31")})
	wantClauses := []string{"due_date <= $1"}
	if !reflect.DeepEqual(b.whereClauses, wantClauses) {
		t.Fatalf("whereClauses = %+v, want %+v", b.whereClauses, wantClauses)
	}
}

func TestApplyTaskFilter_StoryPointsRange(t *testing.T) {
	b := newQueryBuilder()
	applyTaskFilter(b, taskdom.TaskFilter{
		StoryPointsMin: intPtr(2),
		StoryPointsMax: intPtr(8),
	})
	wantClauses := []string{
		"story_points >= $1",
		"story_points <= $2",
	}
	if !reflect.DeepEqual(b.whereClauses, wantClauses) {
		t.Fatalf("whereClauses = %+v, want %+v", b.whereClauses, wantClauses)
	}
	wantArgs := []interface{}{2, 8}
	if !reflect.DeepEqual(b.args, wantArgs) {
		t.Fatalf("args = %+v, want %+v", b.args, wantArgs)
	}
}

func TestApplyTaskFilter_ImportanceRanges_ORsMultipleRanges(t *testing.T) {
	b := newQueryBuilder()
	applyTaskFilter(b, taskdom.TaskFilter{
		// Non-contiguous selection (e.g. "Low" and "Critical" but not
		// "Medium"/"High") must be representable as independent ranges.
		ImportanceRanges: []taskdom.IntRange{
			{Min: 1, Max: 19},
			{Min: 100, Max: 2147483647},
		},
	})
	wantClause := "((importance BETWEEN $1 AND $2) OR (importance BETWEEN $3 AND $4))"
	if len(b.whereClauses) != 1 || b.whereClauses[0] != wantClause {
		t.Fatalf("whereClauses = %+v, want [%q]", b.whereClauses, wantClause)
	}
	wantArgs := []interface{}{1, 19, 100, 2147483647}
	if !reflect.DeepEqual(b.args, wantArgs) {
		t.Fatalf("args = %+v, want %+v", b.args, wantArgs)
	}
}

func TestApplyTaskFilter_ImportanceRanges_EmptyIsNoop(t *testing.T) {
	b := newQueryBuilder()
	applyTaskFilter(b, taskdom.TaskFilter{})
	if len(b.whereClauses) != 0 {
		t.Fatalf("expected no clauses, got %+v", b.whereClauses)
	}
}

func TestApplyTaskFilter_Tags_ORsAnyMatch(t *testing.T) {
	b := newQueryBuilder()
	applyTaskFilter(b, taskdom.TaskFilter{Tags: []string{"urgent", "bug"}})
	wantClause := "(tags @> to_jsonb($1::text) OR tags @> to_jsonb($2::text))"
	if len(b.whereClauses) != 1 || b.whereClauses[0] != wantClause {
		t.Fatalf("whereClauses = %+v, want [%q]", b.whereClauses, wantClause)
	}
	wantArgs := []interface{}{"urgent", "bug"}
	if !reflect.DeepEqual(b.args, wantArgs) {
		t.Fatalf("args = %+v, want %+v", b.args, wantArgs)
	}
}

// TestUnmarshalCustomFieldOptions_LegacyStringElements is a regression test
// for a rolling-deploy window flagged in PR review: an old-code replica can
// still write the pre-000050 plain-string options shape
// (`["Low","High"]`) to a row after a new-code replica has already
// migrated the table to `{value,color}` objects. Reads must tolerate a mix
// of both shapes rather than erroring.
func TestUnmarshalCustomFieldOptions_LegacyStringElements(t *testing.T) {
	opts, err := unmarshalCustomFieldOptions([]byte(`["Low","High"]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []taskdom.CustomFieldOption{{Value: "Low"}, {Value: "High"}}
	if !reflect.DeepEqual(opts, want) {
		t.Fatalf("opts = %+v, want %+v", opts, want)
	}
}

func TestUnmarshalCustomFieldOptions_NewObjectElements(t *testing.T) {
	opts, err := unmarshalCustomFieldOptions([]byte(`[{"value":"Low","color":"#22c55e"},{"value":"High","color":null}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	green := "#22c55e"
	want := []taskdom.CustomFieldOption{{Value: "Low", Color: &green}, {Value: "High"}}
	if !reflect.DeepEqual(opts, want) {
		t.Fatalf("opts = %+v, want %+v", opts, want)
	}
}

func TestUnmarshalCustomFieldOptions_MixedLegacyAndNewElements(t *testing.T) {
	opts, err := unmarshalCustomFieldOptions([]byte(`["Low",{"value":"High","color":"#ef4444"}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	red := "#ef4444"
	want := []taskdom.CustomFieldOption{{Value: "Low"}, {Value: "High", Color: &red}}
	if !reflect.DeepEqual(opts, want) {
		t.Fatalf("opts = %+v, want %+v", opts, want)
	}
}

func TestUnmarshalCustomFieldOptions_Empty(t *testing.T) {
	opts, err := unmarshalCustomFieldOptions(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) != 0 {
		t.Fatalf("opts = %+v, want empty", opts)
	}
}

// openTaskDefaultsTestDB has just the two tables whose default row the delete
// queries protect.
func openTaskDefaultsTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	schema := `
		CREATE TABLE task_types (
			id         TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			name       TEXT NOT NULL,
			is_default BOOLEAN NOT NULL DEFAULT 0
		);
		CREATE TABLE task_statuses (
			id         TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			name       TEXT NOT NULL,
			is_default BOOLEAN NOT NULL DEFAULT 0
		);`
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

// New tasks land in the project's default type and status, so neither may be
// deleted. The service checks first, but the delete refuses too: a
// SetDefault* landing between that read and the delete would otherwise remove
// the new default and leave the project without one.
func TestTaskRepository_DeleteRefusesTheDefault(t *testing.T) {
	ctx := context.Background()
	project := uuid.New().String()

	type deleter func(repo *TaskRepository, id uuid.UUID) error
	cases := []struct {
		name     string
		table    string
		del      deleter
		errIsDef error
	}{
		{"task type", "task_types", func(r *TaskRepository, id uuid.UUID) error { return r.DeleteTaskType(ctx, id) }, taskdom.ErrTypeIsDefault},
		{"task status", "task_statuses", func(r *TaskRepository, id uuid.UUID) error { return r.DeleteTaskStatus(ctx, id) }, taskdom.ErrStatusIsDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openTaskDefaultsTestDB(t)
			repo := NewTaskRepository(db)
			ordinary, def := uuid.New(), uuid.New()
			for _, row := range []struct {
				id        uuid.UUID
				isDefault bool
			}{{ordinary, false}, {def, true}} {
				if _, err := db.ExecContext(ctx, `INSERT INTO `+tc.table+` (id, project_id, name, is_default) VALUES ($1, $2, $3, $4)`,
					row.id.String(), project, "n-"+row.id.String(), row.isDefault); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}
			count := func() int {
				var n int
				if err := db.GetContext(ctx, &n, `SELECT COUNT(*) FROM `+tc.table); err != nil {
					t.Fatalf("count: %v", err)
				}
				return n
			}

			if err := tc.del(repo, def); !errors.Is(err, tc.errIsDef) {
				t.Fatalf("deleting the default = %v, want %v", err, tc.errIsDef)
			}
			if got := count(); got != 2 {
				t.Fatalf("rows after refused delete = %d, want 2 (the default must survive)", got)
			}

			if err := tc.del(repo, ordinary); err != nil {
				t.Fatalf("deleting an ordinary row = %v, want nil", err)
			}
			if got := count(); got != 1 {
				t.Fatalf("rows after delete = %d, want 1", got)
			}

			// Deleting something already gone stays a no-op, as before.
			if err := tc.del(repo, uuid.New()); err != nil {
				t.Fatalf("deleting a missing row = %v, want nil", err)
			}
		})
	}
}

func TestAttachSubtaskCounts(t *testing.T) {
	db := openTaskRepoTestDB(t)
	ctx := context.Background()
	for _, stmt := range []string{
		`ALTER TABLE tasks ADD COLUMN parent_task_id TEXT`,
		`ALTER TABLE tasks ADD COLUMN status_id TEXT`,
		`ALTER TABLE tasks ADD COLUMN task_type_id TEXT`,
		`CREATE TABLE task_types (id TEXT PRIMARY KEY, name TEXT NOT NULL, is_system BOOLEAN NOT NULL)`,
		`INSERT INTO task_types VALUES ('epic', 'Epic', 1)`,
		`CREATE TABLE task_statuses (id TEXT PRIMARY KEY, category TEXT NOT NULL)`,
		`INSERT INTO task_statuses VALUES ('s-todo', 'todo'), ('s-done', 'done')`,
		`INSERT INTO tasks (id, project_id, parent_task_id, status_id, deleted_at) VALUES
			('parent', 'p', NULL, NULL, NULL),
			('lone', 'p', NULL, NULL, NULL),
			('c1', 'p', 'parent', 's-done', NULL),
			('c2', 'p', 'parent', 's-todo', NULL),
			('c3', 'p', 'parent', NULL, NULL),
			('gone', 'p', 'parent', 's-done', CURRENT_TIMESTAMP),
			('foreign', 'other', 'parent', 's-done', NULL)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}

	parent := &taskdom.Task{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001")}
	lone := &taskdom.Task{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	// Re-key the fixtures to the UUIDs the repo will query.
	for old, id := range map[string]uuid.UUID{"parent": parent.ID, "lone": lone.ID} {
		if _, err := db.ExecContext(ctx, `UPDATE tasks SET id = ? WHERE id = ?`, id.String(), old); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE tasks SET parent_task_id = ? WHERE parent_task_id = ?`, id.String(), old); err != nil {
			t.Fatal(err)
		}
	}

	// An Epic reports its children too: the Timeline nests them under it.
	db.MustExec(`INSERT INTO tasks (id, project_id, task_type_id) VALUES ('00000000-0000-0000-0000-000000000003', 'p', 'epic')`)
	db.MustExec(`INSERT INTO tasks (id, project_id, parent_task_id) VALUES ('epic-child', 'p', '00000000-0000-0000-0000-000000000003')`)
	epic := &taskdom.Task{ID: uuid.MustParse("00000000-0000-0000-0000-000000000003")}

	repo := &TaskRepository{db: db}
	if err := repo.attachSubtaskCounts(ctx, []*taskdom.Task{parent, lone, epic}); err != nil {
		t.Fatalf("attachSubtaskCounts: %v", err)
	}
	if parent.SubtaskCount != 3 || parent.SubtaskDoneCount != 1 {
		t.Errorf("parent counts = %d/%d, want 1/3 (deleted and other-project children excluded)", parent.SubtaskDoneCount, parent.SubtaskCount)
	}
	if epic.SubtaskCount != 1 {
		t.Errorf("epic subtask count = %d, want 1", epic.SubtaskCount)
	}
	if lone.SubtaskCount != 0 || lone.SubtaskDoneCount != 0 {
		t.Errorf("lone counts = %d/%d, want 0/0", lone.SubtaskDoneCount, lone.SubtaskCount)
	}
}

func TestTaskRepository_CountTasks_ExcludeSubtasks(t *testing.T) {
	db := openTaskRepoTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()
	projectID := uuid.New().String()
	for _, stmt := range []string{
		`ALTER TABLE tasks ADD COLUMN parent_task_id TEXT`,
		`ALTER TABLE tasks ADD COLUMN task_type_id TEXT`,
		`ALTER TABLE tasks ADD COLUMN sprint_id TEXT`,
		`CREATE TABLE task_types (id TEXT PRIMARY KEY, name TEXT NOT NULL, is_system BOOLEAN NOT NULL)`,
		`INSERT INTO task_types VALUES ('epic', 'Epic', 1), ('story', 'Story', 0)`,
	} {
		db.MustExec(stmt)
	}
	db.MustExec(`INSERT INTO tasks (id, project_id, task_type_id) VALUES ('foreign-parent', 'other-project', 'story')`)
	for _, row := range []struct {
		id, typ, parent string
		deleted         bool
	}{
		{"epic-1", "epic", "", false},
		{"parent", "story", "", false},
		{"in-epic", "story", "epic-1", false}, // belongs to an Epic: stays
		{"child", "story", "parent", false},   // subtask of a story: hidden
		{"untyped-parent", "", "", false},
		{"child-untyped", "story", "untyped-parent", false}, // parent without a type is not an Epic: hidden
		{"gone-parent", "story", "", true},
		{"orphan", "story", "gone-parent", false},    // parent deleted: stays
		{"cross", "story", "foreign-parent", false},  // parent is in another project: stays
		{"sprinted-child", "story", "parent", false}, // parent is in another sprint: stays
	} {
		var typ, parent, deleted any
		if row.typ != "" {
			typ = row.typ
		}
		if row.parent != "" {
			parent = row.parent
		}
		if row.deleted {
			deleted = "2026-01-01 00:00:00"
		}
		var sprint any
		if row.id == "sprinted-child" {
			sprint = "sprint-1"
		}
		db.MustExec(`INSERT INTO tasks (id, project_id, task_type_id, parent_task_id, deleted_at, sprint_id) VALUES ($1, $2, $3, $4, $5, $6)`,
			row.id, projectID, typ, parent, deleted, sprint)
	}

	pid := uuid.MustParse(projectID)
	all, err := repo.CountTasks(ctx, pid, taskdom.TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if all != 9 {
		t.Fatalf("without the filter want 9 live tasks, got %d", all)
	}
	got, err := repo.CountTasks(ctx, pid, taskdom.TaskFilter{ExcludeSubtasks: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != 7 {
		t.Fatalf("ExcludeSubtasks want 7 (epic, parent, in-epic, untyped-parent, orphan, cross, sprinted-child), got %d", got)
	}
}
