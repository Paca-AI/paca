package integration_test

import (
	"encoding/json"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/bootstrap/defaultroles"
	"github.com/Paca-AI/api/internal/platform/database"
	"github.com/Paca-AI/api/migrations"
)

const refinementAnalystMigrationFile = "000082_add_refinement_status_and_analyst_role.sql"

// Migration 000082 gives every existing project a Refinement status (unless it
// already has a refinement-category one) and the Analyst role, and is safe to
// run twice.
func TestRefinementAnalystMigration(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := database.RunMigrationsFS(db, migrationsBefore(t, refinementAnalystMigrationFile)); err != nil {
		t.Fatalf("migrate to 000081: %v", err)
	}
	migrationSQL, err := fs.ReadFile(migrations.FS, refinementAnalystMigrationFile)
	if err != nil {
		t.Fatal(err)
	}

	a, b, c := uuid.New(), uuid.New(), uuid.New()
	mustExec(t, db, `INSERT INTO projects (id, name) VALUES ($1, 'A'), ($2, 'B'), ($3, 'C')`, a, b, c)
	// A: the four statuses projects were created with before this change.
	aBacklog := uuid.New()
	mustExec(t, db, `INSERT INTO task_statuses (id, project_id, name, position, category, is_default) VALUES
		($1, $2, 'Backlog', 1, 'backlog', true), (gen_random_uuid(), $2, 'Todo', 2, 'todo', false),
		(gen_random_uuid(), $2, 'In Progress', 3, 'inprogress', false), (gen_random_uuid(), $2, 'Done', 4, 'done', false)`, aBacklog, a)
	task := uuid.New()
	mustExec(t, db, `INSERT INTO tasks (id, project_id, task_number, status_id, title) VALUES ($1, $2, 1, $3, 't')`, task, a, aBacklog)
	// B: already has its own refinement-category status.
	mustExec(t, db, `INSERT INTO task_statuses (project_id, name, position, category, is_default) VALUES
		($1, 'Grooming', 3, 'refinement', false), ($1, 'Backlog', 5, 'backlog', true)`, b)

	// C: already uses the name "Refinement" for a status of another category.
	mustExec(t, db, `INSERT INTO task_statuses (project_id, name, position, category, is_default) VALUES
		($1, 'Backlog', 1, 'backlog', true), ($1, 'refinement', 2, 'todo', false)`, c)

	type status struct {
		Name     string
		Position int
		Category string
		Default  bool
	}
	statuses := func(p uuid.UUID) []status {
		rows, err := db.QueryContext(t.Context(), `SELECT name, position, category, is_default FROM task_statuses WHERE project_id = $1 ORDER BY position, name`, p)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []status
		for rows.Next() {
			var s status
			if err := rows.Scan(&s.Name, &s.Position, &s.Category, &s.Default); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return out
	}
	analystPolicies := func(p uuid.UUID) []string {
		rows, err := db.QueryContext(t.Context(), `SELECT policy::text FROM roles WHERE project_id = $1 AND name = 'Analyst'`, p)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return out
	}

	wantA := []status{
		{"Refinement", 1, "refinement", false}, {"Backlog", 2, "backlog", true},
		{"Todo", 3, "todo", false}, {"In Progress", 4, "inprogress", false}, {"Done", 5, "done", false},
	}
	wantB := []status{{"Grooming", 3, "refinement", false}, {"Backlog", 5, "backlog", true}}
	wantC := []status{{"Drafts", 1, "refinement", false}, {"Backlog", 2, "backlog", true}, {"refinement", 3, "todo", false}}

	for run := 1; run <= 2; run++ {
		if _, err := db.ExecContext(t.Context(), string(migrationSQL)); err != nil {
			t.Fatalf("run %d: apply %s: %v", run, refinementAnalystMigrationFile, err)
		}
		if got := statuses(a); !reflect.DeepEqual(got, wantA) {
			t.Errorf("run %d: project A statuses = %v, want %v", run, got, wantA)
		}
		if got := statuses(b); !reflect.DeepEqual(got, wantB) {
			t.Errorf("run %d: project B statuses = %v, want %v", run, got, wantB)
		}
		if got := statuses(c); !reflect.DeepEqual(got, wantC) {
			t.Errorf("run %d: project C statuses = %v, want %v", run, got, wantC)
		}
		for _, p := range []uuid.UUID{a, b, c} {
			got := analystPolicies(p)
			if len(got) != 1 {
				t.Fatalf("run %d: project %s has %d Analyst roles, want 1", run, p, len(got))
			}
			if strings.Contains(got[0], defaultroles.ProjectIDToken) {
				t.Errorf("run %d: Analyst policy keeps the %s token: %s", run, defaultroles.ProjectIDToken, got[0])
			}
			assertSamePolicyAsTemplate(t, p, got[0])
		}
	}

	var taskStatus uuid.UUID
	if err := db.QueryRowContext(t.Context(), `SELECT status_id FROM tasks WHERE id = $1`, task).Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != aBacklog {
		t.Errorf("task status changed to %s, want %s", taskStatus, aBacklog)
	}
}

// assertSamePolicyAsTemplate checks the migrated policy matches what a new
// project gets from the shipped Analyst template, so the two cannot drift.
func assertSamePolicyAsTemplate(t *testing.T, project uuid.UUID, got string) {
	t.Helper()
	var want []byte
	for _, r := range defaultroles.Instantiate(project) {
		if r.Name == "Analyst" {
			want = r.Policy
		}
	}
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("migrated Analyst policy differs from the template:\n got  %s\n want %s", got, want)
	}
}
