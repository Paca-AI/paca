package integration_test

import (
	"encoding/json"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/database"
	"github.com/Paca-AI/api/migrations"
)

const rolesAssignMigrationFile = "000064_iam_roles.sql"
const rolesAssignMarker = "roles-assign-compatibility"

// Migration 000064 adds roles:assign next to project.members:write (or
// project.members:*) in eligible Allow statements, after normalizing project
// role resources, so roles that could change a member's roles under the old
// gate still can.
func TestRolesAssignMigration(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := database.RunMigrationsFS(db, migrationsBefore(t, rolesAssignMigrationFile)); err != nil {
		t.Fatalf("migrate to 000063: %v", err)
	}
	migrationSQL, err := fs.ReadFile(migrations.FS, rolesAssignMigrationFile)
	if err != nil {
		t.Fatal(err)
	}
	_, assignSQL, _ := markedBlock(t, string(migrationSQL), rolesAssignMarker)
	assignSQL = strings.TrimSpace(strings.TrimSuffix(assignSQL, "-- <<< roles-assign-compatibility"))
	assignSQL = strings.TrimSpace(strings.TrimPrefix(assignSQL, "-- >>> roles-assign-compatibility"))
	assignSQL = strings.TrimSpace(strings.TrimSuffix(assignSQL, "COMMIT;"))
	if err := database.RunMigrationsFS(db, migrationsThrough(t, "000063_add_project_exports.sql")); err != nil {
		t.Fatalf("prepare schema through 000063: %v", err)
	}
	p := uuid.New()
	mustExec(t, db, `INSERT INTO projects (id, name) VALUES ($1, 'One')`, p)
	pid := p.String()

	stmt := func(effect string, actions []string, resources ...string) map[string]any {
		return map[string]any{"effect": effect, "actions": actions, "resources": resources}
	}
	policy := func(stmts ...map[string]any) string {
		b, _ := json.Marshal(map[string]any{"version": "2026-10-01", "statements": stmts})
		return string(b)
	}
	insert := func(name string, project *uuid.UUID, pol string) {
		mustExec(t, db, `INSERT INTO roles (name, policy, project_id) VALUES ($1, $2::jsonb, $3)`, name, pol, project)
	}
	w := []string{"tasks:read", "project.members:write"}
	insert("owned", &p, policy(stmt("Allow", w, "project/"+pid+"/*")))
	insert("template", nil, policy(stmt("Allow", w, "project/*")))
	insert("list", &p, policy(stmt("Allow", []string{"project.members:write"}, "project/"+pid, "project/"+pid+"/*")))
	insert("wildcardAction", &p, policy(stmt("Allow", []string{"project.members:*"}, "project/"+pid+"/*")))
	insert("alreadyAssign", &p, policy(stmt("Allow", []string{"project.members:write", "roles:assign"}, "project/"+pid+"/*")))
	insert("star", &p, policy(stmt("Allow", []string{"*"}, "project/"+pid+"/*")))
	insert("rolesStar", &p, policy(stmt("Allow", []string{"roles:*", "project.members:write"}, "project/"+pid+"/*")))
	insert("deny", &p, policy(
		stmt("Allow", []string{"tasks:read"}, "project/"+pid+"/*"),
		stmt("Deny", []string{"project.members:write"}, "project/"+pid+"/*")))
	insert("two", &p, policy(
		stmt("Allow", []string{"project.members:write"}, "project/"+pid+"/*"),
		stmt("Allow", []string{"tasks:write"}, "project/"+pid+"/*"),
		stmt("Allow", []string{"project.members:write"}, "project/"+pid+"/role/*")))
	insert("projectOnly", &p, policy(stmt("Allow", []string{"project.members:write"}, "project/"+pid)))
	insert("noMembers", &p, policy(stmt("Allow", []string{"tasks:read", "roles:read"}, "project/"+pid+"/*")))
	insert("workspace", nil, policy(stmt("Allow", []string{"users:read", "projects:read"}, "user/*", "project/*")))

	stamp := func(name string) time.Time {
		var ts time.Time
		if err := db.QueryRowContext(t.Context(), `SELECT updated_at FROM roles WHERE name = $1`, name).Scan(&ts); err != nil {
			t.Fatal(err)
		}
		return ts
	}
	before := map[string]time.Time{}
	for _, n := range []string{"owned", "star", "noMembers", "workspace"} {
		before[n] = stamp(n)
	}
	time.Sleep(20 * time.Millisecond)

	if _, err := db.ExecContext(t.Context(), assignSQL); err != nil {
		t.Fatalf("apply roles:assign compatibility: %v", err)
	}

	actionsOf := func(name string) [][]string {
		var raw string
		if err := db.QueryRowContext(t.Context(), `SELECT policy::text FROM roles WHERE name = $1`, name).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Statements []struct {
				Effect  string   `json:"effect"`
				Actions []string `json:"actions"`
			} `json:"statements"`
		}
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatal(err)
		}
		out := [][]string{}
		for _, s := range doc.Statements {
			out = append(out, s.Actions)
		}
		return out
	}
	want := map[string][][]string{
		"owned":          {{"tasks:read", "project.members:write", "roles:assign"}},
		"template":       {{"tasks:read", "project.members:write", "roles:assign"}},
		"list":           {{"project.members:write", "roles:assign"}},
		"wildcardAction": {{"project.members:*", "roles:assign"}},
		"alreadyAssign":  {{"project.members:write", "roles:assign"}},
		"star":           {{"*"}},
		"rolesStar":      {{"roles:*", "project.members:write"}},
		"deny":           {{"tasks:read"}, {"project.members:write"}},
		"two":            {{"project.members:write", "roles:assign"}, {"tasks:write"}, {"project.members:write", "roles:assign"}},
		"projectOnly":    {{"project.members:write"}},
		"noMembers":      {{"tasks:read", "roles:read"}},
		"workspace":      {{"users:read", "projects:read"}},
	}
	check := func(label string) {
		for name, w := range want {
			if got := actionsOf(name); !reflect.DeepEqual(got, w) {
				t.Errorf("%s %s: actions %v, want %v", label, name, got, w)
			}
		}
	}
	check("applied")

	if !stamp("owned").After(before["owned"]) {
		t.Error("updated_at of a changed role was not bumped")
	}
	for _, n := range []string{"star", "noMembers", "workspace"} {
		if !stamp(n).Equal(before[n]) {
			t.Errorf("updated_at of unchanged role %s moved", n)
		}
	}

	// Replay the extracted transformation to verify idempotence.
	if _, err := db.ExecContext(t.Context(), assignSQL); err != nil {
		t.Fatalf("replay roles:assign compatibility: %v", err)
	}
	check("replay")
}
