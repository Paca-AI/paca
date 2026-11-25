package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// Five day ends: Jan 1 .. Jan 5, 2026, 00:00 UTC.
var (
	trendFirst = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trendLast  = time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
)

func at(day, hour int) time.Time { return time.Date(2026, 1, day, hour, 0, 0, 0, time.UTC) }

func assertSeries(t *testing.T, name string, got []int64, want ...int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", name, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
	}
}

func TestWorkspaceTrendOpenTasks(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	p := fx.project()
	todo, done := uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO task_statuses (id, project_id, name, category) VALUES ($1,$3,'To Do','todo'), ($2,$3,'Done','done')`, todo, done, p)

	n := 0
	task := func(status any, created time.Time, deleted any) uuid.UUID {
		n++
		id := uuid.New()
		pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, status_id, title, created_at, deleted_at) VALUES ($1,$2,$3,$4,'t',$5,$6)`,
			id, p, n, status, created, deleted)
		return id
	}
	statusChange := func(taskID uuid.UUID, when time.Time, oldName, newName string) {
		pgExec(t, db, `INSERT INTO activities (project_id, entity_type, entity_id, activity_type, content, created_at)
			VALUES ($1, 'task', $2, 'task.updated', jsonb_build_object('changes', jsonb_build_array(
				jsonb_build_object('field', 'title', 'old', 'a', 'new', 'b'),
				jsonb_build_object('field', 'status', 'old', $3::text, 'new', $4::text))), $5)`,
			p, taskID, oldName, newName, when)
	}

	// A: finished on Jan 2 at noon.
	a := task(done, at(1, 0).Add(-48*time.Hour), nil)
	statusChange(a, at(2, 12), "To Do", "Done")
	// B: created Jan 2, never changed.
	task(todo, at(2, 6), nil)
	// C: open, deleted Jan 3 at noon.
	task(todo, at(1, 0).Add(-48*time.Hour), at(3, 12))
	// D: done Jan 1, reopened Jan 3; names matched case-insensitively.
	d := task(todo, at(1, 0).Add(-48*time.Hour), nil)
	statusChange(d, at(1, 12), "to do", "DONE")
	statusChange(d, at(3, 12), "done", "to do")
	// E: no status — never counted.
	task(nil, at(1, 0).Add(-48*time.Hour), nil)
	// F: changed from a status that no longer exists — counted as open then.
	f := task(done, at(1, 0).Add(-48*time.Hour), nil)
	statusChange(f, at(4, 12), "Archived", "Done")

	repo := NewWorkspaceTrendRepository(db)
	got, err := repo.OpenTaskTrend(context.Background(), []uuid.UUID{p}, trendFirst, trendLast)
	if err != nil {
		t.Fatal(err)
	}
	//                 A  B  C  D  F
	// Jan 1 00:00     1  0  1  1  1 = 4
	// Jan 2 00:00     1  0  1  0  1 = 3
	// Jan 3 00:00     0  1  1  0  1 = 3
	// Jan 4 00:00     0  1  0  1  1 = 3
	// Jan 5 00:00     0  1  0  1  0 = 2
	assertSeries(t, "open tasks", got, 4, 3, 3, 3, 2)

	// Another project's tasks are not included.
	other, err := repo.OpenTaskTrend(context.Background(), []uuid.UUID{fx.project()}, trendFirst, trendLast)
	if err != nil {
		t.Fatal(err)
	}
	assertSeries(t, "other project", other, 0, 0, 0, 0, 0)
}

func TestWorkspaceTrendProjectsAndUsers(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	p1, p2, gone := uuid.New(), uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO projects (id, name, created_at) VALUES ($1,'P1',$4), ($2,'P2',$5), ($3,'Gone',$4)`,
		p1, p2, gone, at(1, 0).Add(-time.Hour), at(3, 12))
	pgExec(t, db, `UPDATE projects SET deleted_at = $2 WHERE id = $1`, gone, at(2, 12))

	repo := NewWorkspaceTrendRepository(db)
	projects, err := repo.ProjectCountTrend(context.Background(), []uuid.UUID{p1, p2, gone}, trendFirst, trendLast)
	if err != nil {
		t.Fatal(err)
	}
	assertSeries(t, "projects", projects, 2, 2, 1, 2, 2)

	before, err := repo.UserCountTrend(context.Background(), trendFirst, trendLast)
	if err != nil {
		t.Fatal(err)
	}
	u := fx.user(false)
	pgExec(t, db, `UPDATE accounts SET created_at = $2 WHERE id = $1`, u, at(2, 12))
	after, err := repo.UserCountTrend(context.Background(), trendFirst, trendLast)
	if err != nil {
		t.Fatal(err)
	}
	diff := make([]int64, len(after))
	for i := range after {
		diff[i] = after[i] - before[i]
	}
	assertSeries(t, "new user", diff, 0, 0, 1, 1, 1)
}

func TestWorkspaceTrendEmptyInputs(t *testing.T) {
	db := newIAMPGTestDB(t)
	repo := NewWorkspaceTrendRepository(db)
	got, err := repo.OpenTaskTrend(context.Background(), nil, trendFirst, trendLast)
	if err != nil {
		t.Fatal(err)
	}
	assertSeries(t, "no projects", got, 0, 0, 0, 0, 0)
}

func TestWorkspaceTrendOpenTasksScoped(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	p1, p2 := fx.project(), fx.project()
	s1, s2 := uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO task_statuses (id, project_id, name, category) VALUES ($1,$3,'To Do','todo'), ($2,$4,'To Do','todo')`, s1, s2, p1, p2)
	sprint := uuid.New()
	pgExec(t, db, `INSERT INTO sprints (id, project_id, name) VALUES ($1,$2,'S')`, sprint, p1)
	created := at(1, 0).Add(-time.Hour)
	pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, status_id, sprint_id, title, created_at) VALUES
		($1,$4,1,$6,$8,'in sprint',$9), ($2,$4,2,$6,NULL,'no sprint',$9), ($3,$5,1,$7,NULL,'p2',$9)`,
		uuid.New(), uuid.New(), uuid.New(), p1, p2, s1, s2, sprint, created)

	repo := NewWorkspaceTrendRepository(db)
	all := []uuid.UUID{p1, p2}
	trend := func(ctx context.Context) []int64 {
		got, err := repo.OpenTaskTrend(ctx, all, trendFirst, trendLast)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	scoped := func(m map[string]*iam.Node) context.Context {
		return iam.WithProjectScopes(context.Background(), "task", m)
	}
	onlySprint := &iam.Node{Kind: iam.NodeCond, Key: "task.sprint_id", Op: "In", Values: iam.ValueList{sprint.String()}}

	assertSeries(t, "unscoped", trend(context.Background()), 3, 3, 3, 3, 3)
	assertSeries(t, "restricted p1, open p2",
		trend(scoped(map[string]*iam.Node{p1.String(): onlySprint, p2.String(): iam.True()})), 2, 2, 2, 2, 2)
	assertSeries(t, "deny-all", trend(scoped(map[string]*iam.Node{p1.String(): iam.False()})), 0, 0, 0, 0, 0)
}
