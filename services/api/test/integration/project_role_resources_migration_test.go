package integration_test

import (
	"encoding/json"
	"io/fs"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/database"
	"github.com/Paca-AI/api/migrations"
)

const projectRoleResourcesMigrationFile = "000064_iam_roles.sql"
const projectRoleResourcesMarker = "project-role-resources"

// Consolidated migration 000064 rewrites resources of project-owned roles to
// lie inside their project, and leaves workspace roles and roles that already do alone.
func TestProjectRoleResourcesMigration(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := database.RunMigrationsFS(db, migrationsBefore(t, projectRoleResourcesMigrationFile)); err != nil {
		t.Fatalf("migrate to 000063: %v", err)
	}
	migrationSQL, err := fs.ReadFile(migrations.FS, projectRoleResourcesMigrationFile)
	if err != nil {
		t.Fatal(err)
	}
	_, resourceSQL, _ := markedBlock(t, string(migrationSQL), projectRoleResourcesMarker)
	p, q := uuid.New(), uuid.New()
	mustExec(t, db, `INSERT INTO projects (id, name) VALUES ($1, 'One'), ($2, 'Two')`, p, q)

	stmt := func(effect string, resources ...string) map[string]any {
		return map[string]any{"effect": effect, "actions": []string{"tasks:read"}, "resources": resources}
	}
	policy := func(stmts ...map[string]any) string {
		b, _ := json.Marshal(map[string]any{"version": "2026-10-01", "statements": stmts})
		return string(b)
	}
	insert := func(name string, project *uuid.UUID, pol string) {
		mustExec(t, db, `INSERT INTO roles (name, policy, project_id) VALUES ($1, $2::jsonb, $3)`, name, pol, project)
	}
	pid := p.String()
	insert("star", &p, policy(stmt("Allow", "project/*")))
	insert("deep", &p, policy(stmt("Allow", "project/*/role/*", "project/*/task/*")))
	insert("everything", &p, policy(stmt("Allow", "*")))
	insert("already", &p, policy(stmt("Allow", "project/"+pid+"/*"), stmt("Deny", "project/"+pid+"/agent/a/*")))
	insert("foreign", &p, policy(stmt("Allow", "project/"+q.String()+"/*", "user/*"), stmt("Allow", "project/*")))
	insert("onlyForeign", &p, policy(stmt("Allow", "project/"+q.String()+"/*")))
	insert("workspace", nil, policy(stmt("Allow", "project/*")))

	if _, err := db.ExecContext(t.Context(), resourceSQL); err != nil {
		t.Fatalf("apply project-role resource normalization: %v", err)
	}
	resourcesOf := func(name string) [][]string {
		var raw string
		if err := db.QueryRowContext(t.Context(), `SELECT policy::text FROM roles WHERE name = $1`, name).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Statements []struct {
				Resources []string `json:"resources"`
			} `json:"statements"`
		}
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatal(err)
		}
		out := [][]string{}
		for _, s := range doc.Statements {
			out = append(out, s.Resources)
		}
		return out
	}
	want := map[string][][]string{
		"star":        {{"project/" + pid}},
		"deep":        {{"project/" + pid + "/role/*", "project/" + pid + "/task/*"}},
		"everything":  {{"project/" + pid + "/*"}},
		"already":     {{"project/" + pid + "/*"}, {"project/" + pid + "/agent/a/*"}},
		"foreign":     {{"project/" + pid}},
		"onlyForeign": {},
		"workspace":   {{"project/*"}},
	}
	// "foreign": its first statement loses both resources and is dropped, the
	// second becomes project/<id>.
	for name, w := range want {
		if got := resourcesOf(name); !reflect.DeepEqual(got, w) {
			t.Errorf("%s: resources %v, want %v", name, got, w)
		}
	}
}
