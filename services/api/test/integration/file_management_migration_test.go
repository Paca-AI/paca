package integration_test

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/database"
	"github.com/Paca-AI/api/migrations"
)

const fileManagementMigrationFile = "000093_file_management.sql"

func policyActions(t *testing.T, raw []byte) [][]string {
	t.Helper()
	var p struct {
		Statements []struct {
			Actions []string `json:"actions"`
		} `json:"statements"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	out := make([][]string, len(p.Statements))
	for i, s := range p.Statements {
		out[i] = s.Actions
	}
	return out
}

func joined(actions []string) string { return strings.Join(actions, ",") }

// The file manager migration gives existing roles the files actions the way
// the design says: project-scoped docs grants are mirrored, a grant that could
// reach the general scope (resource "*") is not, and denies are left alone.
func TestFileManagementMigrationRoleGrants(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := database.RunMigrationsFS(db, migrationsBefore(t, fileManagementMigrationFile)); err != nil {
		t.Fatalf("migrate to 000092: %v", err)
	}
	sqlText, err := fs.ReadFile(migrations.FS, fileManagementMigrationFile)
	if err != nil {
		t.Fatal(err)
	}

	project := uuid.New()
	mustExec(t, db, `INSERT INTO projects (id, name) VALUES ($1, 'P')`, project)
	insertRole := func(name string, projectID any, policy string) {
		mustExec(t, db, `INSERT INTO roles (name, project_id, policy) VALUES ($1, $2, $3::jsonb)`, name, projectID, policy)
	}
	insertRole("FilesProjectEditor", project, `{"version":"2026-10-01","statements":[
		{"sid":"Work","effect":"Allow","actions":["tasks:read","docs:read","docs:write"],"resources":["project/p/*"]},
		{"sid":"Block","effect":"Deny","actions":["docs:write"],"resources":["project/p/doc/x"]}]}`)
	insertRole("FilesPlatformDocs", nil, `{"version":"2026-10-01","statements":[
		{"sid":"Docs","effect":"Allow","actions":["docs:read","docs:write"],"resources":["*"]}]}`)
	insertRole("FilesMixedResources", nil, `{"version":"2026-10-01","statements":[
		{"sid":"Mixed","effect":"Allow","actions":["docs:read"],"resources":["project/p/*","*"]}]}`)

	if _, err := db.ExecContext(t.Context(), string(sqlText)); err != nil {
		t.Fatalf("apply %s: %v", fileManagementMigrationFile, err)
	}

	policyOf := func(name string) [][]string {
		var raw []byte
		if err := db.QueryRowContext(t.Context(), `SELECT policy FROM roles WHERE name = $1`, name).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return policyActions(t, raw)
	}

	if got := policyOf("FilesProjectEditor"); joined(got[0]) != "tasks:read,docs:read,docs:write,files:read,files:write" || joined(got[1]) != "docs:write" {
		t.Errorf("project role = %v, want its allow statement to gain files:read/write and the deny left alone", got)
	}
	if got := policyOf("FilesPlatformDocs"); joined(got[0]) != "docs:read,docs:write" {
		t.Errorf("platform role with resource * = %v, must not gain files actions (they would reach file/*)", got)
	}
	if got := policyOf("FilesMixedResources"); joined(got[0]) != "docs:read" {
		t.Errorf("statement that includes resource * = %v, must not gain files actions", got)
	}
}

// Removing an attachment from a task trashes its file, but only when the file
// has a folder: one without is not in file_items, so the trash list and the
// purge could never reach it.
func TestFileManagementMigrationTaskAttachmentTrigger(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := database.RunMigrationsFS(db, migrationsBefore(t, "99999999")); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	project, task, other := uuid.New(), uuid.New(), uuid.New()
	mustExec(t, db, `INSERT INTO projects (id, name) VALUES ($1, 'P')`, project)
	mustExec(t, db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1, $2, 1, 't')`, task, project)

	addAttachment := func(taskKeyID uuid.UUID) uuid.UUID {
		file := uuid.New()
		mustExec(t, db, `INSERT INTO files (id, storage_key, bucket, file_name, content_type, file_size, upload_status)
			VALUES ($1, $2, 'b', 'a.txt', 'text/plain', 1, 'uploaded')`, file, "tasks/"+taskKeyID.String()+"/"+file.String()+"/a.txt")
		mustExec(t, db, `INSERT INTO task_attachments (task_id, file_id) VALUES ($1, $2)`, task, file)
		return file
	}
	trashed := func(file uuid.UUID) bool {
		var n int
		if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM files WHERE id = $1 AND deleted_at IS NOT NULL`, file).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}

	withFolder := addAttachment(task) // its key names a real task: classified into a folder
	noFolder := addAttachment(other)  // its key names a task that does not exist: no folder
	mustExec(t, db, `DELETE FROM task_attachments`)

	if !trashed(withFolder) {
		t.Error("an attachment with a folder must be trashed when it leaves its task")
	}
	if trashed(noFolder) {
		t.Error("a file with no folder is invisible to the manager and must not be stamped as trashed")
	}
}
