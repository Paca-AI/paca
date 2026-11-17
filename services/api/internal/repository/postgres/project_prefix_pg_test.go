package postgres

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/database"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
	"github.com/Paca-AI/api/migrations"
	"github.com/google/uuid"
)

func TestProjectPrefixMigrationPG(t *testing.T) {
	db := newIAMPGTestDB(t)
	pgExec(t, db, `DROP INDEX idx_projects_task_id_prefix; DROP TABLE project_prefix_changes_000093`)
	// Deliberately insert tied timestamps in reverse id order. Existing suffixes
	// and long prefixes must remain reserved while duplicates get new keys.
	prefixes := []string{"PAY", "pay", "Pay", "PAY2", "ABCDEFGHIJ", "abcdefghij", "ABCDEFGHI2", "", "", "pay", "PAY3", "ABCDEFGHI3", "ABCDEFGHIJ", "ABCDEFGHI4", "ABCDEFGHI5", "ABCDEFGHI6", "ABCDEFGHI7", "ABCDEFGHI8", "ABCDEFGHI9", "TIME", "time"}
	ids := make([]uuid.UUID, len(prefixes))
	for i := len(prefixes) - 1; i >= 0; i-- {
		ids[i] = uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1))
		var deleted any
		if i == 9 || i == 10 {
			deleted = time.Now()
		}
		pgExec(t, db, `INSERT INTO projects (id,name,task_id_prefix,created_at,deleted_at) VALUES ($1,$2,$3,'2026-01-01',$4)`, ids[i], fmt.Sprintf("fixture %d", i), prefixes[i], deleted)
	}
	pgExec(t, db, `UPDATE projects SET created_at='2025-01-01' WHERE id=$1`, ids[20])
	taskID := uuid.New()
	pgExec(t, db, `INSERT INTO tasks (id,project_id,title,task_number) VALUES ($1,$2,'kept task',1)`, taskID, ids[1])
	sql, err := migrations.FS.ReadFile("000093_unique_project_task_id_prefix.sql")
	if err != nil {
		t.Fatal(err)
	}
	// A surrounding transaction must be able to roll back all data, audit and DDL.
	tx, err := db.BeginTxx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(t.Context(), string(sql)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var before string
	if err = db.Get(&before, `SELECT task_id_prefix FROM projects WHERE id=$1`, ids[1]); err != nil || before != "pay" {
		t.Fatalf("rollback prefix=%q err=%v", before, err)
	}
	var auditExists bool
	if err = db.Get(&auditExists, `SELECT to_regclass('project_prefix_changes_000093') IS NOT NULL`); err != nil || auditExists {
		t.Fatalf("rollback audit exists=%v err=%v", auditExists, err)
	}
	// Reapply through the production runner (all earlier migrations are recorded).
	pgExec(t, db, `DELETE FROM schema_migrations WHERE filename='000093_unique_project_task_id_prefix.sql'`)
	if err = database.RunMigrationsFS(db.DB, migrations.FS); err != nil {
		t.Fatal(err)
	}
	want := append([]string(nil), prefixes...)
	want[1], want[2], want[5], want[12], want[19] = "PAY3", "PAY4", "ABCDEFGH10", "ABCDEFGH11", "TIME2"
	for i, id := range ids {
		var got string
		if err = db.Get(&got, `SELECT task_id_prefix FROM projects WHERE id=$1`, id); err != nil || got != want[i] {
			t.Errorf("project %d got %q want %q err=%v", i, got, want[i], err)
		}
	}
	var changed int
	if err = db.Get(&changed, `SELECT count(*) FROM project_prefix_changes_000093`); err != nil || changed != 5 {
		t.Fatalf("audit count=%d err=%v", changed, err)
	}
	var oldPrefix, newPrefix string
	if err = db.QueryRow(`SELECT old_prefix,new_prefix FROM project_prefix_changes_000093 WHERE project_id=$1`, ids[1]).Scan(&oldPrefix, &newPrefix); err != nil || oldPrefix != "pay" || newPrefix != "PAY3" {
		t.Fatalf("audit=%q -> %q err=%v", oldPrefix, newPrefix, err)
	}
	tasks := NewTaskRepository(db)
	gotTask, err := tasks.FindTaskByKey(t.Context(), "pay3", 1)
	if err != nil || gotTask.ID != taskID {
		t.Fatalf("renamed task lookup=%v err=%v", gotTask, err)
	}
	if _, err = tasks.FindTaskByKey(t.Context(), "PAY", 1); !errors.Is(err, taskdom.ErrTaskNotFound) {
		t.Fatalf("old key must not fall through to renamed project: %v", err)
	}
	if gotTask, err = tasks.FindTaskByID(t.Context(), taskID); err != nil || gotTask.ProjectID != ids[1] {
		t.Fatalf("UUID lookup=%v err=%v", gotTask, err)
	}
	// Crash-window replay must neither rename again nor lose the audit trail.
	pgExec(t, db, `DELETE FROM schema_migrations WHERE filename='000093_unique_project_task_id_prefix.sql'`)
	if err = database.RunMigrationsFS(db.DB, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if err = db.Get(&changed, `SELECT count(*) FROM project_prefix_changes_000093`); err != nil || changed != 5 {
		t.Fatalf("replay audit count=%d err=%v", changed, err)
	}
}

func TestProjectPrefixesPG(t *testing.T) {
	db := newIAMPGTestDB(t)
	repo := NewProjectRepository(db)
	ctx := t.Context()
	create := func(prefix string) *projectdom.Project {
		t.Helper()
		p := testProject(uuid.New(), prefix)
		if err := repo.Create(ctx, p, projectdom.ProjectSetup{}); err != nil {
			t.Fatal(err)
		}
		return p
	}
	first := create("PAY")
	duplicate := testProject(uuid.New(), "pay")
	if err := repo.Create(ctx, duplicate, projectdom.ProjectSetup{}); !errors.Is(err, projectdom.ErrPrefixTaken) {
		t.Fatalf("case-insensitive create: %v", err)
	}
	second := create("OTHER")
	second.TaskIDPrefix = "pAy"
	if err := repo.Update(ctx, second); !errors.Is(err, projectdom.ErrPrefixTaken) {
		t.Fatalf("case-insensitive update: %v", err)
	}
	persisted, err := repo.FindByID(ctx, second.ID)
	if err != nil || persisted.TaskIDPrefix != "OTHER" {
		t.Fatalf("failed update changed row: %v %v", persisted, err)
	}
	first.TaskIDPrefix = "pay"
	if err := repo.Update(ctx, first); err != nil {
		t.Fatalf("same project's case-only update: %v", err)
	}
	found, err := repo.FindByTaskIDPrefix(ctx, "PaY")
	if err != nil || found.ID != first.ID {
		t.Fatalf("case-insensitive lookup: %v %v", found, err)
	}
	if err := repo.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	reused := create("PAY")
	if found, err = repo.FindByTaskIDPrefix(ctx, "pay"); err != nil || found.ID != reused.ID {
		t.Fatalf("reuse: %v %v", found, err)
	}
	create("")
	create("")
	second.TaskIDPrefix = ""
	if err := repo.Update(ctx, second); err != nil {
		t.Fatalf("clear prefix: %v", err)
	}
	if _, err = repo.FindByTaskIDPrefix(ctx, ""); !errors.Is(err, projectdom.ErrNotFound) {
		t.Fatalf("empty lookup: %v", err)
	}
	// PostgreSQL reports the index name for both insert and update. Preserve
	// the existing name error behavior for a distinct name constraint.
	pgExec(t, db, `CREATE UNIQUE INDEX projects_name_key ON projects(name) WHERE name LIKE 'unique-%'`)
	first.Name = "unique-name"
	if err = repo.Update(ctx, first); err != nil {
		t.Fatal(err)
	}
	reused.Name = first.Name
	if err = repo.Update(ctx, reused); !errors.Is(err, projectdom.ErrNameTaken) {
		t.Fatalf("name conflict misclassified: %v", err)
	}
	duplicate.ID = uuid.New()
	duplicate.Name = first.Name
	duplicate.TaskIDPrefix = "FREE"
	if err = repo.Create(ctx, duplicate, projectdom.ProjectSetup{}); !errors.Is(err, projectdom.ErrNameTaken) {
		t.Fatalf("name create misclassified: %v", err)
	}
}

func TestProjectPrefixConcurrentCreatePG(t *testing.T) {
	db := newIAMPGTestDB(t)
	repo := NewProjectRepository(db)
	svc := projectsvc.New(repo, nil, nil)
	ctx := t.Context()
	// Occupied generated candidates must be skipped, including case differences.
	for _, prefix := range []string{"pay", "PAY2", "PAY4"} {
		if err := repo.Create(ctx, testProject(uuid.New(), prefix), projectdom.ProjectSetup{}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := svc.Create(ctx, projectdom.CreateProjectInput{Name: "Pay"})
	if err != nil || first.TaskIDPrefix != "PAY3" {
		t.Fatalf("first free: %v %v", first, err)
	}
	const n = 8
	results := make(chan *projectdom.Project, n)
	failures := make(chan error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range n {
		wg.Go(func() {
			<-start
			p, err := svc.Create(ctx, projectdom.CreateProjectInput{Name: "Pay"})
			if err != nil {
				failures <- err
			} else {
				results <- p
			}
		})
	}
	close(start)
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	seen := map[string]bool{}
	for p := range results {
		key := strings.ToUpper(p.TaskIDPrefix)
		if seen[key] || len(key) > 10 {
			t.Errorf("invalid allocated prefix %q", key)
		}
		seen[key] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d projects, want %d", len(seen), n)
	}
	var roleCount int
	if err = db.Get(&roleCount, `SELECT count(*) FROM roles WHERE project_id IS NOT NULL`); err != nil || roleCount != 4*(n+1) {
		t.Fatalf("roles from retries leaked: count=%d err=%v", roleCount, err)
	}
	// Two explicit requests race; one succeeds and the other gets the domain
	// conflict. The failed transaction leaves no project or setup behind.
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := svc.Create(ctx, projectdom.CreateProjectInput{Name: "Explicit", TaskIDPrefix: "EXPLICIT"})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, projectdom.ErrPrefixTaken) {
			conflict++
		} else {
			t.Error(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("explicit race success=%d conflict=%d", success, conflict)
	}
}

func TestProjectPrefixConcurrentUpdatePG(t *testing.T) {
	db := newIAMPGTestDB(t)
	repo := NewProjectRepository(db)
	projects := []*projectdom.Project{testProject(uuid.New(), "ONE"), testProject(uuid.New(), "TWO")}
	for _, p := range projects {
		if err := repo.Create(t.Context(), p, projectdom.ProjectSetup{}); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	errs := make(chan error, len(projects))
	var wg sync.WaitGroup
	for _, p := range projects {
		wg.Go(func() { <-start; p.TaskIDPrefix = "RACE"; errs <- repo.Update(t.Context(), p) })
	}
	close(start)
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, projectdom.ErrPrefixTaken) {
			conflict++
		} else {
			t.Error(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("update race success=%d conflict=%d", success, conflict)
	}
}
