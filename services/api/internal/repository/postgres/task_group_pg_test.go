package postgres

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/google/uuid"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// groupFx seeds a project whose tasks vary along every group-by dimension.
type groupFx struct {
	*scopeFx
	repo *TaskRepository
	// task numbers, by the property the test asks about
	ids map[int]uuid.UUID
	m1  uuid.UUID
}

func newGroupFx(t *testing.T) *groupFx {
	f := &groupFx{scopeFx: newScopeFx(t), ids: map[int]uuid.UUID{}}
	f.repo = NewTaskRepository(f.db)
	u := f.fx.user(false)
	f.m1 = f.fx.member(f.p, u, false)
	st := uuid.New()
	pgExec(t, f.db, `INSERT INTO task_statuses (id, project_id, name, position, category) VALUES ($1,$2,'Todo',0,'todo')`, st, f.p)
	rows := []struct {
		n        int
		tags     string
		imp      int
		cf       string
		assignee bool
		status   bool
	}{
		{1, `[]`, 0, `{}`, false, false},
		{2, `["a","b"]`, 10, `{"size":"S","labels":["x","y"],"done":true,"pts":3}`, true, true},
		{3, `["b"]`, 60, `{"size":"L","labels":["y"],"done":false,"pts":10}`, false, true},
		{4, `[]`, 150, `{"size":"","labels":[],"pts":null}`, false, false},
	}
	for _, r := range rows {
		id := uuid.New()
		f.ids[r.n] = id
		var status any
		if r.status {
			status = st
		}
		pgExec(t, f.db, `INSERT INTO tasks (id, project_id, task_number, title, tags, importance, custom_fields, status_id, reporter_id)
			VALUES ($1,$2,$3,'t',$4::jsonb,$5,$6::jsonb,$7,$8)`, id, f.p, r.n, r.tags, r.imp, r.cf, status, f.reporterFor(r.n))
		if r.assignee {
			pgExec(t, f.db, `INSERT INTO task_assignees (task_id, member_id) VALUES ($1,$2)`, id, f.m1)
		}
	}
	return f
}

func (f *groupFx) reporterFor(n int) any {
	if n == 2 {
		return f.m1
	}
	return nil
}

// numbers lists the task numbers a group holds, sorted.
func (f *groupFx) numbers(g *taskdom.TaskGroup, extra func(*taskdom.TaskFilter)) []int {
	f.t.Helper()
	filter := taskdom.TaskFilter{Group: g}
	if extra != nil {
		extra(&filter)
	}
	tasks, _, err := f.repo.ListTasks(context.Background(), f.p, filter, 50, taskdom.TaskSort{})
	if err != nil {
		f.t.Fatal(err)
	}
	out := []int{}
	for _, tk := range tasks {
		out = append(out, int(tk.TaskNumber))
	}
	sort.Ints(out)
	// The count agrees with the page.
	n, err := f.repo.CountTasks(context.Background(), f.p, filter)
	if err != nil || int(n) != len(out) {
		f.t.Fatalf("count = %d, %v; page has %d", n, err, len(out))
	}
	return out
}

func TestTaskGroupFilter(t *testing.T) {
	f := newGroupFx(t)
	cases := []struct {
		name string
		g    taskdom.TaskGroup
		want []int
	}{
		{"tag a", taskdom.TaskGroup{By: "tags", Key: "a"}, []int{2}},
		{"tag b", taskdom.TaskGroup{By: "tags", Key: "b"}, []int{2, 3}},
		{"untagged", taskdom.TaskGroup{By: "tags", Key: "__none"}, []int{1, 4}},
		{"assignee", taskdom.TaskGroup{By: "assignee", Key: ""}, nil},
		{"unassigned", taskdom.TaskGroup{By: "assignee", Key: "__unassigned"}, []int{1, 3, 4}},
		{"reporter none", taskdom.TaskGroup{By: "reporter", Key: "__none"}, []int{1, 3, 4}},
		{"no status", taskdom.TaskGroup{By: "status", Key: "__none"}, []int{1, 4}},
		{"importance none", taskdom.TaskGroup{By: "importance", Key: "0"}, []int{1}},
		{"importance low", taskdom.TaskGroup{By: "importance", Key: "1"}, []int{2}},
		{"importance high", taskdom.TaskGroup{By: "importance", Key: "3"}, []int{3}},
		{"importance critical", taskdom.TaskGroup{By: "importance", Key: "4"}, []int{4}},
		{"importance has no sentinel", taskdom.TaskGroup{By: "importance", Key: "__none"}, []int{}},
		{"a sentinel of another dimension is an ordinary key", taskdom.TaskGroup{By: "tags", Key: "__unassigned"}, []int{}},
		{"sprint sentinel", taskdom.TaskGroup{By: "sprint", Key: "__backlog"}, []int{1, 2, 3, 4}},
		{"importance unknown bucket", taskdom.TaskGroup{By: "importance", Key: "9"}, []int{}},
		{"select", taskdom.TaskGroup{By: "size", Key: "S", FieldType: "select"}, []int{2}},
		{"select unset", taskdom.TaskGroup{By: "size", Key: "__none", FieldType: "select"}, []int{1, 4}},
		{"multi_select", taskdom.TaskGroup{By: "labels", Key: "y", FieldType: "multi_select"}, []int{2, 3}},
		{"multi_select unset", taskdom.TaskGroup{By: "labels", Key: "__none", FieldType: "multi_select"}, []int{1, 4}},
		{"boolean yes", taskdom.TaskGroup{By: "done", Key: "true", FieldType: "boolean"}, []int{2}},
		{"boolean no includes unset", taskdom.TaskGroup{By: "done", Key: "false", FieldType: "boolean"}, []int{1, 3, 4}},
		{"number", taskdom.TaskGroup{By: "pts", Key: "10", FieldType: "number"}, []int{3}},
		{"number written with a scale", taskdom.TaskGroup{By: "pts", Key: "10.0", FieldType: "number"}, []int{3}},
		{"number malformed", taskdom.TaskGroup{By: "pts", Key: "ten", FieldType: "number"}, []int{}},
		{"number hex float", taskdom.TaskGroup{By: "pts", Key: "0x1p-2", FieldType: "number"}, []int{}},
		{"number unset", taskdom.TaskGroup{By: "pts", Key: "__none", FieldType: "number"}, []int{1, 4}},
	}
	for _, c := range cases {
		if c.want == nil {
			continue
		}
		g := c.g
		if got := f.numbers(&g, nil); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}

	if got := f.numbers(&taskdom.TaskGroup{By: "assignee", Key: f.m1.String()}, nil); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("assignee: got %v", got)
	}
	if got := f.numbers(&taskdom.TaskGroup{By: "reporter", Key: f.m1.String()}, nil); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("reporter: got %v", got)
	}
	// A malformed id selects nothing instead of failing.
	if got := f.numbers(&taskdom.TaskGroup{By: "status", Key: "nope"}, nil); len(got) != 0 {
		t.Errorf("malformed id: got %v", got)
	}
	// A group composes with the view's other filters.
	got := f.numbers(&taskdom.TaskGroup{By: "tags", Key: "b"}, func(fl *taskdom.TaskFilter) {
		fl.ImportanceRanges = []taskdom.IntRange{{Min: 50, Max: 99}}
	})
	if !reflect.DeepEqual(got, []int{3}) {
		t.Errorf("group + filter: got %v", got)
	}
}

func TestListGroupColumns(t *testing.T) {
	f := newGroupFx(t)
	ctx := context.Background()
	keys := func(by, ft string, filter taskdom.TaskFilter) []string {
		cols, err := f.repo.ListGroupColumns(ctx, f.p, filter, by, ft)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, c := range cols {
			out = append(out, c.Key)
		}
		return out
	}
	if got := keys("tags", "", taskdom.TaskFilter{}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("tags: %v", got)
	}
	// The view's filters narrow the columns...
	narrow := taskdom.TaskFilter{ImportanceRanges: []taskdom.IntRange{{Min: 50, Max: 99}}}
	if got := keys("tags", "", narrow); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("tags, filtered: %v", got)
	}
	// ...but the listing's own group does not.
	both := taskdom.TaskFilter{Group: &taskdom.TaskGroup{By: "tags", Key: "a"}}
	if got := keys("tags", "", both); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("tags, grouped: %v", got)
	}
	cols, err := f.repo.ListGroupColumns(ctx, f.p, taskdom.TaskFilter{}, "assignee", "")
	if err != nil || len(cols) != 1 || cols[0].Key != f.m1.String() || cols[0].Count != 1 || cols[0].Label == "" {
		t.Errorf("assignee columns: %+v, %v", cols, err)
	}
	if got := keys("reporter", "", taskdom.TaskFilter{}); !reflect.DeepEqual(got, []string{f.m1.String()}) {
		t.Errorf("reporter: %v", got)
	}
	if got := keys("pts", "number", taskdom.TaskFilter{}); !reflect.DeepEqual(got, []string{"3", "10"}) {
		t.Errorf("numbers sort numerically: %v", got)
	}
	if got := keys("size", "select", taskdom.TaskFilter{}); !reflect.DeepEqual(got, []string{"L", "S"}) {
		t.Errorf("select values: %v", got)
	}
	if _, err := f.repo.ListGroupColumns(ctx, f.p, taskdom.TaskFilter{}, "status", ""); err == nil {
		t.Error("status has fixed columns; want an error")
	}
}
