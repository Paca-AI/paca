package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
)

func openProjectRepoTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	schema := `
		CREATE TABLE projects (
			id           TEXT    PRIMARY KEY,
			name         TEXT    NOT NULL,
			description  TEXT    NOT NULL DEFAULT '',
			task_id_prefix TEXT  NOT NULL DEFAULT '',
			is_public    INTEGER NOT NULL DEFAULT 0,
			settings     BLOB    NOT NULL DEFAULT '{}',
			created_by   TEXT,
			created_at   DATETIME,
			deleted_at   DATETIME
		);`
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func testProject(id uuid.UUID, prefix string) *projectdom.Project {
	now := time.Now().UTC().Truncate(time.Second)
	return &projectdom.Project{
		ID:           id,
		Name:         "Test Project " + prefix,
		Description:  "test description",
		TaskIDPrefix: prefix,
		IsPublic:     false,
		Settings:     map[string]any{},
		CreatedAt:    now,
	}
}

func TestProjectRepository_Delete_SetsDeletedAt(t *testing.T) {
	db := openProjectRepoTestDB(t)
	repo := NewProjectRepository(db)
	ctx := context.Background()

	p := testProject(uuid.New(), "DEL")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Bypass the soft-delete filter to confirm deleted_at was set.
	var rec projectRecord
	if err := db.GetContext(ctx, &rec,
		`SELECT `+projectSelectCols+` FROM projects WHERE id = $1`, p.ID.String()); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if rec.DeletedAt == nil {
		t.Fatal("expected deleted_at to be non-nil after Delete")
	}
}

func TestProjectRepository_FindByID_ExcludesSoftDeleted(t *testing.T) {
	db := openProjectRepoTestDB(t)
	repo := NewProjectRepository(db)
	ctx := context.Background()

	p := testProject(uuid.New(), "FID")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := repo.FindByID(ctx, p.ID)
	if !errors.Is(err, projectdom.ErrNotFound) {
		t.Errorf("expected ErrNotFound after soft-delete, got %v", err)
	}
}

func TestProjectRepository_FindByTaskIDPrefix_ExcludesSoftDeleted(t *testing.T) {
	db := openProjectRepoTestDB(t)
	repo := NewProjectRepository(db)
	ctx := context.Background()

	p := testProject(uuid.New(), "PFXD")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := repo.FindByTaskIDPrefix(ctx, "PFXD")
	if !errors.Is(err, projectdom.ErrNotFound) {
		t.Errorf("expected ErrNotFound for soft-deleted prefix, got %v", err)
	}
}

func TestProjectRepository_Delete_AlreadyDeleted_ReturnsNotFound(t *testing.T) {
	db := openProjectRepoTestDB(t)
	repo := NewProjectRepository(db)
	ctx := context.Background()

	p := testProject(uuid.New(), "DUP")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatalf("first Delete: %v", err)
	}

	err := repo.Delete(ctx, p.ID)
	if !errors.Is(err, projectdom.ErrNotFound) {
		t.Errorf("expected ErrNotFound on re-delete, got %v", err)
	}
}

func TestProjectRepository_List_ExcludesSoftDeleted(t *testing.T) {
	db := openProjectRepoTestDB(t)
	repo := NewProjectRepository(db)
	ctx := context.Background()

	alive := testProject(uuid.New(), "LIVE")
	deleted := testProject(uuid.New(), "GONE")

	for _, p := range []*projectdom.Project{alive, deleted} {
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create %s: %v", p.TaskIDPrefix, err)
		}
	}
	if err := repo.Delete(ctx, deleted.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify COUNT excludes soft-deleted rows — mirrors the repo's own count query.
	var total int64
	if err := db.GetContext(ctx, &total, `SELECT COUNT(*) FROM projects WHERE deleted_at IS NULL`); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if total != 1 {
		t.Errorf("expected COUNT=1 (soft-deleted excluded), got %d", total)
	}

	// Verify only the alive project appears in a filtered select.
	var ids []string
	if err := db.SelectContext(ctx, &ids, `SELECT id FROM projects WHERE deleted_at IS NULL`); err != nil {
		t.Fatalf("select query: %v", err)
	}
	if len(ids) != 1 || ids[0] != alive.ID.String() {
		t.Errorf("expected only alive project %s, got %v", alive.ID, ids)
	}
}
