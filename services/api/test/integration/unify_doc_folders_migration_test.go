package integration_test

import (
	"io/fs"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/database"
	"github.com/Paca-AI/api/migrations"
)

const unifyDocFoldersMigrationFile = "000096_documentation_in_files.sql"

// Upgrading a database that already has documentation folders: they keep
// their ids, nest under the project's Documentation folder, documents stay
// where they were, and the docs view still reports the legacy shape.
func TestUnifyDocFoldersMigrationMovesExistingFolders(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := database.RunMigrationsFS(db, migrationsBefore(t, unifyDocFoldersMigrationFile)); err != nil {
		t.Fatalf("migrate to 000095: %v", err)
	}
	sqlText, err := fs.ReadFile(migrations.FS, unifyDocFoldersMigrationFile)
	if err != nil {
		t.Fatal(err)
	}

	project := uuid.New()
	account := uuid.New()
	member := uuid.New()
	mustExec(t, db, `INSERT INTO projects (id, name) VALUES ($1, 'P')`, project)
	mustExec(t, db, `INSERT INTO accounts (id, kind, name) VALUES ($1, 'user', 'Ann')`, account)
	mustExec(t, db, `INSERT INTO project_members (id, project_id, account_id) VALUES ($1, $2, $3)`, member, project, account)

	top, nested, odd, slash := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	mustExec(t, db, `INSERT INTO doc_folders (id, project_id, name, position, created_by) VALUES ($1, $2, 'Governance', 3, $3)`, top, project, member)
	mustExec(t, db, `INSERT INTO doc_folders (id, project_id, parent_id, name) VALUES ($1, $2, $3, 'Risks')`, nested, project, top)
	mustExec(t, db, `INSERT INTO doc_folders (id, project_id, name) VALUES ($1, $2, 'A\B')`, odd, project)
	mustExec(t, db, `INSERT INTO doc_folders (id, project_id, name) VALUES ($1, $2, 'Q1/Q2')`, slash, project)
	doc := uuid.New()
	mustExec(t, db, `INSERT INTO documents (id, project_id, folder_id, title) VALUES ($1, $2, $3, 'Plan')`, doc, project, nested)
	gone := uuid.New()
	mustExec(t, db, `INSERT INTO documents (id, project_id, title, deleted_at) VALUES ($1, $2, 'Gone', now())`, gone, project)

	if _, err := db.ExecContext(t.Context(), string(sqlText)); err != nil {
		t.Fatalf("apply %s: %v", unifyDocFoldersMigrationFile, err)
	}

	var root uuid.UUID
	if err := db.QueryRowContext(t.Context(), `SELECT id FROM file_folders WHERE project_id = $1 AND system_key = 'documentation'`, project).Scan(&root); err != nil {
		t.Fatalf("documentation folder: %v", err)
	}

	type row struct {
		parent  uuid.NullUUID
		name    string
		pos     int
		area    string
		account uuid.NullUUID
		member  uuid.NullUUID
	}
	get := func(id uuid.UUID) row {
		var r row
		if err := db.QueryRowContext(t.Context(),
			`SELECT parent_id, name, position, area, created_by, created_by_member FROM file_folders WHERE id = $1`, id).
			Scan(&r.parent, &r.name, &r.pos, &r.area, &r.account, &r.member); err != nil {
			t.Fatalf("folder %s: %v", id, err)
		}
		return r
	}
	if r := get(top); r.parent.UUID != root || r.name != "Governance" || r.pos != 3 || r.area != "docs" || r.account.UUID != account || r.member.UUID != member {
		t.Errorf("top = %+v, want under the Documentation folder with its author and position kept", r)
	}
	if r := get(nested); r.parent.UUID != top || r.area != "docs" {
		t.Errorf("nested = %+v, want its parent kept and area docs", r)
	}
	if r := get(odd); r.name != "A-B" {
		t.Errorf("name = %q, want the backslash replaced", r.name)
	}
	if r := get(slash); r.name != "Q1-Q2" {
		t.Errorf("name = %q, want the slash replaced", r.name)
	}

	var folderID uuid.UUID
	if err := db.QueryRowContext(t.Context(), `SELECT folder_id FROM documents WHERE id = $1`, doc).Scan(&folderID); err != nil || folderID != nested {
		t.Errorf("document folder = %v, %v; want it unchanged", folderID, err)
	}

	// The docs view keeps the legacy shape: top-level folders have no parent.
	var parent uuid.NullUUID
	var author uuid.NullUUID
	if err := db.QueryRowContext(t.Context(), `SELECT parent_id, created_by FROM doc_folders WHERE id = $1`, top).Scan(&parent, &author); err != nil || parent.Valid || author.UUID != member {
		t.Errorf("view row parent=%v created_by=%v err=%v; want NULL parent and the member id", parent, author, err)
	}
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM doc_folders WHERE project_id = $1`, project).Scan(&n); err != nil || n != 4 {
		t.Errorf("doc_folders rows = %d, %v; want 4 (the Documentation folder itself is not one)", n, err)
	}

	// The document is a Markdown file in the file tree, under its folder; a
	// document deleted before the migration is no trash entry.
	var name, area, contentType string
	var parentOf uuid.UUID
	if err := db.QueryRowContext(t.Context(), `SELECT name, area, content_type, parent_id FROM file_items WHERE id = $1`, doc).Scan(&name, &area, &contentType, &parentOf); err != nil {
		t.Fatalf("document in file_items: %v", err)
	}
	if name != "Plan.md" || area != "docs" || contentType != "text/markdown" || parentOf != nested {
		t.Errorf("document item = %q area %q type %q parent %v", name, area, contentType, parentOf)
	}
	var listed int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM file_items WHERE id = $1`, gone).Scan(&listed); err != nil || listed != 0 {
		t.Errorf("a document deleted before the migration is in file_items (%d, %v)", listed, err)
	}
}
