package postgres

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

func TestParseTaskRef(t *testing.T) {
	for _, tc := range []struct {
		in     string
		prefix string
		number int
		ok     bool
	}{
		{"PRJ-12", "PRJ", 12, true},
		{" prj-12 ", "PRJ", 12, true},
		{"#7", "", 7, true},
		{"42", "", 42, true},
		{"PRJ-0", "", 0, false},
		{"PRJ-", "", 0, false},
		{"login bug", "", 0, false},
		{"TOOLONGPREFIX-1", "", 0, false},
	} {
		prefix, number, ok := parseTaskRef(tc.in)
		if prefix != tc.prefix || number != tc.number || ok != tc.ok {
			t.Errorf("parseTaskRef(%q) = %q, %d, %v; want %q, %d, %v", tc.in, prefix, number, ok, tc.prefix, tc.number, tc.ok)
		}
	}
}

func TestSearchProjects(t *testing.T) {
	db := newIAMPGTestDB(t)
	tag := uuid.NewString()[:8]
	mk := func(name, desc, prefix string) uuid.UUID {
		id := uuid.New()
		pgExec(t, db, `INSERT INTO projects (id, name, description, task_id_prefix) VALUES ($1,$2,$3,$4)`, id, name, desc, prefix)
		return id
	}
	contains := mk("The "+tag+" board", "", "")
	exact := mk(tag, "", "")
	byDesc := mk("Other", "about "+tag, "")
	byPrefix := mk("Keyed", "", "Q"+tag[:5])
	hidden := mk(tag+" hidden", "", "")
	gone := mk(tag+" gone", "", "")
	pgExec(t, db, `UPDATE projects SET deleted_at = NOW() WHERE id = $1`, gone)

	repo := NewSearchRepository(db)
	ids := []uuid.UUID{contains, exact, byDesc, byPrefix, gone}
	hits, err := repo.SearchProjects(context.Background(), ids, tag, 10)
	if err != nil {
		t.Fatal(err)
	}
	var got []uuid.UUID
	for _, h := range hits {
		got = append(got, h.ID)
	}
	// The exact name first; a project outside ids or deleted never appears.
	if len(got) != 3 || got[0] != exact || !slices.Contains(got, contains) || !slices.Contains(got, byDesc) || slices.Contains(got, hidden) {
		t.Fatalf("hits = %v, want %v first, then %v and %v", got, exact, contains, byDesc)
	}

	keyed, err := repo.SearchProjects(context.Background(), ids, "q"+tag[:5], 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(keyed) != 1 || keyed[0].ID != byPrefix {
		t.Fatalf("prefix search = %+v, want %v", keyed, byPrefix)
	}
}

func TestSearchTasks(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	p1, p2 := fx.project(), fx.project()
	pgExec(t, db, `UPDATE projects SET task_id_prefix = 'ABC' WHERE id = $1`, p1)
	todo := uuid.New()
	pgExec(t, db, `INSERT INTO task_statuses (id, project_id, name, category) VALUES ($1,$2,'To Do','todo')`, todo, p1)
	sprint := uuid.New()
	pgExec(t, db, `INSERT INTO sprints (id, project_id, name) VALUES ($1,$2,'S')`, sprint, p1)

	task := func(p uuid.UUID, n int, title string, sprintID any, deleted bool) uuid.UUID {
		id := uuid.New()
		var del any
		if deleted {
			del = "2026-01-01T00:00:00Z"
		}
		pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, title, status_id, sprint_id, deleted_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7, NOW() - make_interval(mins => $8::int))`, id, p, n, title, todo, sprintID, del, n)
		return id
	}
	login := task(p1, 1, "Fix login page", sprint, false)
	epic := uuid.New()
	pgExec(t, db, `INSERT INTO task_types (id, project_id, name, icon, color) VALUES ($1,$2,'Epic','Layers','#8b5cf6')`, epic, p1)
	pgExec(t, db, `UPDATE tasks SET task_type_id = $2 WHERE id = $1`, login, epic)
	logout := task(p1, 2, "Logout button", nil, false)
	task(p1, 3, "Login deleted", nil, true)
	twelve := task(p1, 12, "Unrelated", nil, false)
	other := task(p2, 12, "Login on p2", nil, false)

	repo := NewSearchRepository(db)
	all := []uuid.UUID{p1, p2}
	search := func(ctx context.Context, q string) []uuid.UUID {
		t.Helper()
		hits, err := repo.SearchTasks(ctx, all, q, 10)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]uuid.UUID, len(hits))
		for i, h := range hits {
			ids[i] = h.ID
		}
		return ids
	}

	// Title match, case-insensitive, most recently updated first; deleted
	// tasks are skipped.
	if got := search(context.Background(), "LOG"); !slices.Equal(got, []uuid.UUID{login, logout, other}) {
		t.Fatalf("LOG = %v, want %v", got, []uuid.UUID{login, logout, other})
	}
	// A full key finds that task only; a bare number finds it in every
	// project, ahead of title matches.
	if got := search(context.Background(), "abc-12"); !slices.Equal(got, []uuid.UUID{twelve}) {
		t.Fatalf("abc-12 = %v, want %v", got, twelve)
	}
	if got := search(context.Background(), "#12"); len(got) != 2 || !slices.Contains(got, twelve) || !slices.Contains(got, other) {
		t.Fatalf("#12 = %v, want %v and %v", got, twelve, other)
	}

	hits, err := repo.SearchTasks(context.Background(), all, "abc-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ProjectName == "" || hits[0].TaskIDPrefix != "ABC" ||
		hits[0].StatusName == nil || *hits[0].StatusName != "To Do" ||
		hits[0].TypeName == nil || *hits[0].TypeName != "Epic" ||
		hits[0].TypeIcon == nil || *hits[0].TypeIcon != "Layers" ||
		hits[0].TypeColor == nil || *hits[0].TypeColor != "#8b5cf6" {
		t.Fatalf("abc-1 hit = %+v, want the task with its project, status and type", hits)
	}
	if hits, _ := repo.SearchTasks(context.Background(), all, "abc-2", 10); len(hits) != 1 || hits[0].TypeName != nil {
		t.Fatalf("abc-2 hit = %+v, want a task with no type", hits)
	}

	// The caller's per-project task scope applies: limited to one sprint in
	// p1 and nothing in p2.
	onlySprint := &iam.Node{Kind: iam.NodeCond, Key: "task.sprint_id", Op: "In", Values: iam.ValueList{sprint.String()}}
	scoped := iam.WithProjectScopes(context.Background(), "task", map[string]*iam.Node{p1.String(): onlySprint, p2.String(): iam.False()})
	if got := search(scoped, "log"); !slices.Equal(got, []uuid.UUID{login}) {
		t.Fatalf("scoped = %v, want only %v", got, login)
	}
	denied := iam.WithProjectScopes(context.Background(), "task", map[string]*iam.Node{p1.String(): iam.False()})
	if got := search(denied, "log"); len(got) != 0 {
		t.Fatalf("deny-all = %v, want nothing", got)
	}
}

func TestSearchPeople(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	tag := uuid.NewString()[:8]
	person := func(name string, deleted bool) uuid.UUID {
		id := fx.user(deleted)
		pgExec(t, db, `UPDATE accounts SET name = $2 WHERE id = $1`, id, name)
		return id
	}
	teamA, teamB, elsewhere := fx.project(), fx.project(), fx.project()
	pgExec(t, db, `UPDATE projects SET name = 'A team' WHERE id = $1`, teamA)
	pgExec(t, db, `UPDATE projects SET name = 'B team' WHERE id = $1`, teamB)

	ann := person("Ann "+tag, false)
	fx.member(teamB, ann, false)
	fx.member(teamA, ann, false)
	bob := person("Bob "+tag, false)
	fx.member(elsewhere, bob, false)
	left := person("Cid "+tag, false)
	fx.member(teamA, left, true) // left the team
	gone := person("Dee "+tag, true)
	fx.member(teamA, gone, false)
	agent := fx.agent(&teamA, false)
	pgExec(t, db, `UPDATE accounts SET name = $2 WHERE id = $1`, agent, "Eve "+tag)
	fx.member(teamA, agent, false)

	repo := NewSearchRepository(db)
	teams := []uuid.UUID{teamA, teamB}

	// A regular caller finds only live human members of their teams, linked
	// to the first of those teams by name.
	hits, err := repo.SearchPeople(context.Background(), teams, false, tag, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != ann || hits[0].ProjectID == nil || *hits[0].ProjectID != teamA {
		t.Fatalf("team search = %+v, want Ann linked to team A", hits)
	}
	// Matching by username works too.
	var username string
	if err := db.Get(&username, `SELECT username FROM users WHERE id = $1`, ann); err != nil {
		t.Fatal(err)
	}
	if hits, err := repo.SearchPeople(context.Background(), teams, false, username, 10); err != nil || len(hits) != 1 || hits[0].ID != ann {
		t.Fatalf("username search = %+v, %v; want Ann", hits, err)
	}
	// No readable team: nobody.
	if hits, err := repo.SearchPeople(context.Background(), nil, false, tag, 10); err != nil || len(hits) != 0 {
		t.Fatalf("no teams = %+v, %v; want nothing", hits, err)
	}

	// users:read finds every live user, people outside the caller's teams
	// unlinked.
	hits, err = repo.SearchPeople(context.Background(), teams, true, tag, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID]*uuid.UUID{}
	for _, h := range hits {
		got[h.ID] = h.ProjectID
	}
	if len(got) != 3 || got[ann] == nil || got[bob] != nil || got[left] != nil {
		t.Fatalf("all-users search = %+v, want Ann (linked), Bob and Cid (unlinked)", hits)
	}
}
