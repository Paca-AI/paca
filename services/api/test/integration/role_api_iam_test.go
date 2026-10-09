package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/platform/database"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	rolesvc "github.com/Paca-AI/api/internal/service/role"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/router"
	"github.com/Paca-AI/api/migrations"
)

// roleAPIEnv is the real stack behind the roles API: Postgres (all migrations
// applied), the IAM store/authorizer (with its policy cache), the role
// repository, service and handler, and the real router with its gates and
// escalation guards.
type roleAPIEnv struct {
	t      *testing.T
	db     *sqlx.DB
	authz  *iam.Authorizer
	server http.Handler
	tokens *jwttoken.Manager
}

type publicProjects struct{}

func (publicProjects) IsProjectPublic(context.Context, uuid.UUID) (bool, error) { return false, nil }

func newRoleAPIEnv(t *testing.T) *roleAPIEnv {
	t.Helper()
	sqlDB := newMigrationTestDB(t) // skips without PACA_TEST_PG_DSN
	if err := database.RunMigrationsFS(sqlDB, migrations.FS); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	db := sqlx.NewDb(sqlDB, "pgx")
	authz := pgRepo.NewIAMAuthorizer(db)
	roleRepo := pgRepo.NewRoleRepository(db)
	svc := rolesvc.New(roleRepo, authz, authz, authz.Registry(), authz.Schema())
	tokens := jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour)
	srv := router.New(router.Deps{
		TokenManager:         tokens,
		IAM:                  authz,
		ProjectVisibilitySvc: publicProjects{},
		Health:               handler.NewHealthHandler(),
		Role:                 handler.NewRoleHandler(svc),
		RolePolicies:         roleRepo,
		RoleAttachments:      httpmw.NewRoleServiceAttachments(svc),
		Log:                  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	env := &roleAPIEnv{t: t, db: db, authz: authz, server: srv, tokens: tokens}
	return env
}

func (e *roleAPIEnv) exec(q string, args ...any) {
	e.t.Helper()
	if _, err := e.db.ExecContext(e.t.Context(), q, args...); err != nil {
		e.t.Fatalf("exec %q: %v", q, err)
	}
}

func (e *roleAPIEnv) project() uuid.UUID {
	id := uuid.New()
	e.exec(`INSERT INTO projects (id, name) VALUES ($1, $2)`, id, "P-"+id.String()[:8])
	return id
}

func (e *roleAPIEnv) user() uuid.UUID {
	id := uuid.New()
	e.exec(`INSERT INTO users (id, username, password_hash) VALUES ($1, $2, 'x')`, id, "u_"+id.String()[:8])
	return id
}

// member makes user a live member of project and returns the member row id.
func (e *roleAPIEnv) member(project, user uuid.UUID) uuid.UUID {
	id := uuid.New()
	e.exec(`INSERT INTO project_members (id, project_id, user_id, member_type) VALUES ($1, $2, $3, 'human')`, id, project, user)
	return id
}

func (e *roleAPIEnv) role(name, policy string, owner *uuid.UUID) uuid.UUID {
	id := uuid.New()
	e.exec(`INSERT INTO roles (id, name, policy, project_id) VALUES ($1, $2, $3::jsonb, $4)`, id, name, policy, owner)
	return id
}

func (e *roleAPIEnv) attach(role, user uuid.UUID, project *uuid.UUID) {
	e.exec(`INSERT INTO role_attachments (role_id, principal_type, principal_id, project_id) VALUES ($1, 'user', $2, $3)`, role, user, project)
}

type apiResult struct {
	code int
	body []byte
}

func (r apiResult) errorCode() string {
	var env struct {
		ErrorCode string `json:"error_code"`
	}
	_ = json.Unmarshal(r.body, &env)
	return env.ErrorCode
}

func (r apiResult) data(t *testing.T, into any) {
	t.Helper()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
	if err := json.Unmarshal(env.Data, into); err != nil {
		t.Fatalf("decode data %s: %v", env.Data, err)
	}
}

func (e *roleAPIEnv) call(user uuid.UUID, method, path string, body any) apiResult {
	e.t.Helper()
	var rd io.Reader = http.NoBody
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequestWithContext(e.t.Context(), method, "/api/v1"+path, rd)
	req.Header.Set("Content-Type", "application/json")
	tok, err := e.tokens.IssueAccess(user.String(), "u", "USER", "fam", false)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	e.server.ServeHTTP(rec, req)
	return apiResult{code: rec.Code, body: rec.Body.Bytes()}
}

func (e *roleAPIEnv) allowed(user uuid.UUID, action, resource string) bool {
	e.t.Helper()
	res, err := e.authz.Authorize(e.t.Context(), iam.User(user.String()), action, resource)
	if err != nil {
		e.t.Fatalf("authorize: %v", err)
	}
	return res.Allowed
}

func policyJSON(statements ...string) map[string]any {
	var sts []json.RawMessage
	for _, s := range statements {
		sts = append(sts, json.RawMessage(s))
	}
	return map[string]any{"version": "2026-10-01", "statements": sts}
}

func allowStmt(actions, resources string) string {
	return `{"effect":"Allow","actions":[` + actions + `],"resources":[` + resources + `]}`
}

func TestRolesAPI_EndToEnd(t *testing.T) {
	e := newRoleAPIEnv(t)
	p, q := e.project(), e.project()
	pj, qj := p.String(), q.String()

	// A platform administrator: holds "*" on "*" platform-wide.
	root := e.user()
	everything := e.role("Everything", `{"version":"2026-10-01","statements":[`+allowStmt(`"*"`, `"*"`)+`]}`, nil)
	e.attach(everything, root, nil)

	// A project administrator: "*" on project P only, through a project role.
	admin := e.user()
	e.member(p, admin)
	pAdmin := e.role("P Admin", `{"version":"2026-10-01","statements":[`+allowStmt(`"*"`, `"project/`+pj+`/*"`)+`]}`, &p)
	e.attach(pAdmin, admin, &p)

	// A member without any role.
	bob := e.user()
	bobMember := e.member(p, bob)

	projectRoles := "/projects/" + pj + "/roles"

	t.Run("project admin creates a role limited to project tasks", func(t *testing.T) {
		res := e.call(admin, http.MethodPost, projectRoles, map[string]any{
			"name": "Task editor", "description": "edits tasks",
			"policy": policyJSON(allowStmt(`"tasks:*"`, `"project/`+pj+`/task/*"`)),
		})
		if res.code != http.StatusCreated {
			t.Fatalf("create: %d %s", res.code, res.body)
		}
		var role struct {
			ID              string         `json:"id"`
			ProjectID       *string        `json:"project_id"`
			Policy          map[string]any `json:"policy"`
			AttachmentCount int            `json:"attachment_count"`
			IsSystem        bool           `json:"is_system"`
		}
		res.data(t, &role)
		if role.ProjectID == nil || *role.ProjectID != pj || role.Policy["version"] != "2026-10-01" || role.IsSystem {
			t.Fatalf("role = %+v", role)
		}
	})

	t.Run("project admin cannot escalate", func(t *testing.T) {
		for name, policy := range map[string]map[string]any{
			"users:write":         policyJSON(allowStmt(`"users:write"`, `"user/*"`)),
			"another project":     policyJSON(allowStmt(`"tasks:read"`, `"project/`+qj+`/task/*"`)),
			"every project":       policyJSON(allowStmt(`"tasks:read"`, `"project/*/task/*"`)),
			"everything":          policyJSON(allowStmt(`"tasks:read"`, `"*"`)),
			"roles:write on role": policyJSON(allowStmt(`"roles:write"`, `"role/*"`)),
		} {
			res := e.call(admin, http.MethodPost, projectRoles, map[string]any{"name": "Evil " + name, "policy": policy})
			if res.code != http.StatusForbidden || res.errorCode() != "FORBIDDEN" {
				t.Errorf("%s: %d %s, want 403", name, res.code, res.body)
			}
		}
		var n int
		if err := e.db.Get(&n, `SELECT COUNT(*) FROM roles WHERE name LIKE 'Evil %'`); err != nil || n != 0 {
			t.Fatalf("a refused role must not be stored: %d err=%v", n, err)
		}
		// the project admin has no platform role access at all
		if res := e.call(admin, http.MethodGet, "/admin/roles", nil); res.code != http.StatusForbidden {
			t.Errorf("platform role list: %d", res.code)
		}
		if res := e.call(admin, http.MethodPost, "/admin/roles", map[string]any{"name": "X", "policy": policyJSON(allowStmt(`"tasks:read"`, `"project/`+pj+`/task/*"`))}); res.code != http.StatusForbidden {
			t.Errorf("platform role create: %d", res.code)
		}
		// nor can they edit a platform role through their project
		var platformID string
		if err := e.db.Get(&platformID, `SELECT id FROM roles WHERE name = 'Everything'`); err != nil {
			t.Fatal(err)
		}
		if res := e.call(admin, http.MethodPut, projectRoles+"/"+platformID, map[string]any{"name": "Everything", "policy": policyJSON(allowStmt(`"tasks:read"`, `"project/`+pj+`/task/*"`))}); res.code != http.StatusNotFound {
			t.Errorf("platform role through a project route: %d %s", res.code, res.body)
		}
	})

	t.Run("platform administrator can create anything", func(t *testing.T) {
		res := e.call(root, http.MethodPost, "/admin/roles", map[string]any{
			"name": "Users admin", "policy": policyJSON(allowStmt(`"users:write"`, `"user/*"`)),
		})
		if res.code != http.StatusCreated {
			t.Fatalf("create: %d %s", res.code, res.body)
		}
		if res := e.call(root, http.MethodPost, "/admin/roles", map[string]any{
			"name": "Second everything", "policy": policyJSON(allowStmt(`"*"`, `"*"`)),
		}); res.code != http.StatusCreated {
			t.Fatalf("create *: %d %s", res.code, res.body)
		}
		// a role owned by a project may only name resources inside that project,
		// whoever creates it (a workspace role is the way to span projects)
		for name, resource := range map[string]string{
			"another project": `"project/` + qj + `/task/*"`,
			"every project":   `"project/*"`,
			"everything":      `"*"`,
		} {
			res := e.call(root, http.MethodPost, projectRoles, map[string]any{
				"name": "Cross project " + name, "policy": policyJSON(allowStmt(`"tasks:read"`, resource)),
			})
			if res.code != http.StatusUnprocessableEntity || res.errorCode() != "ROLE_POLICY_INVALID" {
				t.Fatalf("root creates a project role with %s: %d %s", name, res.code, res.body)
			}
		}
		if res := e.call(root, http.MethodPost, projectRoles, map[string]any{
			"name": "Own project", "policy": policyJSON(allowStmt(`"tasks:read"`, `"project/`+pj+`/task/*"`)),
		}); res.code != http.StatusCreated {
			t.Fatalf("root creates a role inside the project: %d %s", res.code, res.body)
		}
		// the same policy is fine as a workspace role, which can reach any project
		if res := e.call(root, http.MethodPost, "/admin/roles", map[string]any{
			"name": "Cross project", "policy": policyJSON(allowStmt(`"tasks:read"`, `"project/`+qj+`/task/*"`)),
		}); res.code != http.StatusCreated {
			t.Fatalf("workspace role naming another project: %d %s", res.code, res.body)
		}
		// duplicate name in the same scope is a conflict
		if res := e.call(root, http.MethodPost, "/admin/roles", map[string]any{
			"name": "Users admin", "policy": policyJSON(allowStmt(`"users:read"`, `"user/*"`)),
		}); res.code != http.StatusConflict || res.errorCode() != "ROLE_NAME_TAKEN" {
			t.Fatalf("duplicate: %d %s", res.code, res.body)
		}
		// an invalid policy lists its issues
		res = e.call(root, http.MethodPost, "/admin/roles", map[string]any{
			"name": "Broken", "policy": policyJSON(allowStmt(`"nope:x"`, `"*"`)),
		})
		var env struct {
			Issues []struct{ Path, Message string } `json:"issues"`
		}
		_ = json.Unmarshal(res.body, &env)
		if res.code != http.StatusUnprocessableEntity || res.errorCode() != "ROLE_POLICY_INVALID" || len(env.Issues) != 1 || env.Issues[0].Path != "statements[0].actions[0]" {
			t.Fatalf("invalid policy: %d %s", res.code, res.body)
		}
		// only IAM policy JSON is accepted: a permission map is refused
		res = e.call(root, http.MethodPost, "/admin/roles", map[string]any{"name": "Map", "permissions": map[string]any{"tasks.read": true}})
		if res.code != http.StatusUnprocessableEntity {
			t.Fatalf("permission-map payload: %d %s", res.code, res.body)
		}
	})

	t.Run("replace-set assignment is effective immediately", func(t *testing.T) {
		task := "project/" + pj + "/task/" + uuid.NewString()
		var editor struct{ ID string }
		var list []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		e.call(admin, http.MethodGet, projectRoles+"/", nil).data(t, &list)
		for _, r := range list {
			if r.Name == "Task editor" {
				editor.ID = r.ID
			}
		}
		if editor.ID == "" {
			t.Fatalf("project listing is missing the role: %+v", list)
		}
		// the project listing shows only the project's own roles (and platform
		// roles already attached inside it), not every workspace role
		for _, r := range list {
			if r.Name == "Everything" {
				t.Fatal("a workspace role that is not attached in the project must not be listed there")
			}
		}

		if e.allowed(bob, "tasks:write", task) {
			t.Fatal("bob must not hold tasks:write yet")
		}
		res := e.call(admin, http.MethodPut, "/projects/"+pj+"/members/"+bobMember.String()+"/roles", map[string]any{"role_ids": []string{editor.ID}})
		if res.code != http.StatusOK {
			t.Fatalf("assign: %d %s", res.code, res.body)
		}
		if !e.allowed(bob, "tasks:write", task) {
			t.Fatal("the new attachment must be effective immediately")
		}
		var attached []struct {
			ID              string `json:"id"`
			AttachmentCount int    `json:"attachment_count"`
		}
		res.data(t, &attached)
		if len(attached) != 1 || attached[0].ID != editor.ID || attached[0].AttachmentCount != 1 {
			t.Fatalf("attached = %+v", attached)
		}
		var by *string
		if err := e.db.Get(&by, `SELECT created_by::text FROM role_attachments WHERE principal_id = $1 AND role_id = $2`, bob, editor.ID); err != nil || by == nil || *by != admin.String() {
			t.Fatalf("created_by = %v err=%v, want the caller", by, err)
		}

		// Editing the role changes what bob may do on the very next request,
		// even though the old policy was cached by the check above.
		res = e.call(admin, http.MethodPut, projectRoles+"/"+editor.ID, map[string]any{
			"name": "Task editor", "policy": policyJSON(allowStmt(`"tasks:read"`, `"project/`+pj+`/task/*"`)),
		})
		if res.code != http.StatusOK {
			t.Fatalf("update: %d %s", res.code, res.body)
		}
		if e.allowed(bob, "tasks:write", task) || !e.allowed(bob, "tasks:read", task) {
			t.Fatal("a role edit must invalidate the cached policy")
		}

		// Replacing with the empty set revokes.
		if res := e.call(admin, http.MethodPut, "/projects/"+pj+"/members/"+bobMember.String()+"/roles", map[string]any{"role_ids": []string{}}); res.code != http.StatusOK {
			t.Fatalf("revoke: %d %s", res.code, res.body)
		}
		if e.allowed(bob, "tasks:read", task) {
			t.Fatal("revoked attachment must not apply")
		}
		// Deleting an attached role removes its attachments.
		if res := e.call(admin, http.MethodPut, "/projects/"+pj+"/members/"+bobMember.String()+"/roles", map[string]any{"role_ids": []string{editor.ID}}); res.code != http.StatusOK {
			t.Fatalf("re-assign: %d", res.code)
		}
		if res := e.call(admin, http.MethodDelete, projectRoles+"/"+editor.ID, nil); res.code != http.StatusNoContent {
			t.Fatalf("delete: %d %s", res.code, res.body)
		}
		if e.allowed(bob, "tasks:read", task) {
			t.Fatal("deleting a role must drop its grants at once")
		}
	})

	t.Run("assignment guard", func(t *testing.T) {
		var usersAdmin string
		if err := e.db.Get(&usersAdmin, `SELECT id FROM roles WHERE name = 'Users admin'`); err != nil {
			t.Fatal(err)
		}
		// Assigning is gated like iam:PassRole: the project admin holds
		// roles:assign on every role inside its project, so a platform role
		// about users is assignable there (whatever it grants) -- and, since a
		// project-scoped attachment only ever acts inside the project, it
		// gives bob nothing outside it.
		res := e.call(admin, http.MethodPut, "/projects/"+pj+"/members/"+bobMember.String()+"/roles", map[string]any{"role_ids": []string{usersAdmin}})
		if res.code != http.StatusOK {
			t.Fatalf("assigning a role that is inert inside the project: %d %s", res.code, res.body)
		}
		if e.allowed(bob, "users:write", "user/"+uuid.NewString()) {
			t.Fatal("a project-scoped attachment must not reach platform resources")
		}
		if res := e.call(admin, http.MethodPut, "/projects/"+pj+"/members/"+bobMember.String()+"/roles", map[string]any{"role_ids": []string{}}); res.code != http.StatusOK {
			t.Fatalf("clear: %d", res.code)
		}
		// A project admin's roles:assign is limited to its project: it has no
		// way to attach the role platform-wide (role/<id> is not theirs).
		if res := e.call(admin, http.MethodPut, "/admin/users/"+bob.String()+"/roles", map[string]any{"role_ids": []string{usersAdmin}}); res.code != http.StatusForbidden {
			t.Fatalf("platform-wide assignment by a project admin: %d", res.code)
		}
		// ... unknown role ids are reported by the service (422), not the guard
		res = e.call(admin, http.MethodPut, "/projects/"+pj+"/members/"+bobMember.String()+"/roles", map[string]any{"role_ids": []string{uuid.NewString()}})
		if res.code != http.StatusUnprocessableEntity || res.errorCode() != "ROLE_NOT_ATTACHABLE" {
			t.Fatalf("unknown role: %d %s", res.code, res.body)
		}
		// the platform administrator may assign it, platform-wide
		res = e.call(root, http.MethodPut, "/admin/users/"+bob.String()+"/roles", map[string]any{"role_ids": []string{usersAdmin}})
		if res.code != http.StatusOK {
			t.Fatalf("root assigns: %d %s", res.code, res.body)
		}
		if !e.allowed(bob, "users:write", "user/"+uuid.NewString()) {
			t.Fatal("platform attachment must be effective immediately")
		}
		// a project role cannot be attached platform-wide
		var cross string
		_ = e.db.Get(&cross, `SELECT id FROM roles WHERE name = 'P Admin'`)
		res = e.call(root, http.MethodPut, "/admin/users/"+bob.String()+"/roles", map[string]any{"role_ids": []string{cross}})
		if res.code != http.StatusUnprocessableEntity {
			t.Fatalf("project role platform-wide: %d %s", res.code, res.body)
		}
		// unknown / deleted principals
		if res := e.call(root, http.MethodPut, "/admin/users/"+uuid.NewString()+"/roles", map[string]any{"role_ids": []string{}}); res.code != http.StatusNotFound {
			t.Fatalf("unknown user: %d", res.code)
		}
		if res := e.call(root, http.MethodGet, "/admin/agents/"+uuid.NewString()+"/roles", nil); res.code != http.StatusNotFound {
			t.Fatalf("unknown agent: %d", res.code)
		}
		if res := e.call(admin, http.MethodGet, "/projects/"+pj+"/members/"+uuid.NewString()+"/roles", nil); res.code != http.StatusNotFound {
			t.Fatalf("unknown member: %d", res.code)
		}
	})

	t.Run("project admin assigns a richer role than it could grant by hand", func(t *testing.T) {
		// "Everything" (* on *) is far more than anything the admin could put
		// in a role, yet roles:assign on the project's roles is all that is
		// asked to attach it inside the project (PassRole semantics).
		path := "/projects/" + pj + "/members/" + bobMember.String() + "/roles"
		if res := e.call(admin, http.MethodPut, path, map[string]any{"role_ids": []string{everything.String()}}); res.code != http.StatusOK {
			t.Fatalf("project admin assigns a richer role: %d %s", res.code, res.body)
		}
		if res := e.call(admin, http.MethodPut, path, map[string]any{"role_ids": []string{}}); res.code != http.StatusOK {
			t.Fatalf("clear: %d %s", res.code, res.body)
		}
	})

	t.Run("a scoped roles:assign assigns exactly those roles", func(t *testing.T) {
		task := "project/" + pj + "/task/" + uuid.NewString()
		roleA := e.role("Assignable A", `{"version":"2026-10-01","statements":[`+allowStmt(`"tasks:read"`, `"project/*/task/*"`)+`]}`, nil)
		roleB := e.role("Assignable B", `{"version":"2026-10-01","statements":[`+allowStmt(`"tasks:write"`, `"project/*/task/*"`)+`]}`, nil)
		a, b := roleA.String(), roleB.String()
		bobRoles := "/projects/" + pj + "/members/" + bobMember.String() + "/roles"

		// A project lead: manages members and may assign role A inside P only.
		lead := e.user()
		e.member(p, lead)
		leadRole := e.role("Lead "+uuid.NewString()[:8], `{"version":"2026-10-01","statements":[`+
			allowStmt(`"project.members:write"`, `"project/`+pj+`"`)+`,`+
			allowStmt(`"roles:assign"`, `"project/`+pj+`/role/`+a+`"`)+`]}`, &p)
		e.attach(leadRole, lead, &p)

		if res := e.call(lead, http.MethodPut, bobRoles, map[string]any{"role_ids": []string{a}}); res.code != http.StatusOK {
			t.Fatalf("lead assigns A: %d %s", res.code, res.body)
		}
		if !e.allowed(bob, "tasks:read", task) {
			t.Fatal("A must be effective at once")
		}
		for name, ids := range map[string][]string{"B alone": {b}, "A and B": {a, b}} {
			if res := e.call(lead, http.MethodPut, bobRoles, map[string]any{"role_ids": ids}); res.code != http.StatusForbidden || res.errorCode() != "FORBIDDEN" {
				t.Errorf("lead assigns %s: %d %s, want 403", name, res.code, res.body)
			}
		}
		if e.allowed(bob, "tasks:write", task) {
			t.Fatal("a refused assignment must not apply")
		}
		// keeping A unchanged is free, adding B still is not
		if res := e.call(lead, http.MethodPut, bobRoles, map[string]any{"role_ids": []string{a}}); res.code != http.StatusOK {
			t.Fatalf("unchanged A: %d %s", res.code, res.body)
		}
		// root attaches B; the lead cannot remove it (nor wipe the set)
		if res := e.call(admin, http.MethodPut, bobRoles, map[string]any{"role_ids": []string{a, b}}); res.code != http.StatusOK {
			t.Fatalf("admin attaches A and B: %d %s", res.code, res.body)
		}
		for name, ids := range map[string][]string{"drop B": {a}, "drop all": {}} {
			if res := e.call(lead, http.MethodPut, bobRoles, map[string]any{"role_ids": ids}); res.code != http.StatusForbidden {
				t.Errorf("lead %s: %d %s, want 403", name, res.code, res.body)
			}
		}
		// the lead may take A away when B stays
		if res := e.call(lead, http.MethodPut, bobRoles, map[string]any{"role_ids": []string{b}}); res.code != http.StatusOK {
			t.Fatalf("lead drops A, keeps B: %d %s", res.code, res.body)
		}
		if res := e.call(admin, http.MethodPut, bobRoles, map[string]any{"role_ids": []string{}}); res.code != http.StatusOK {
			t.Fatalf("clear: %d", res.code)
		}

		// roles:assign alone is enough to change a member's roles
		// (project.members:write is not required for this route).
		assigner := e.user()
		e.member(p, assigner)
		onlyAssign := e.role("Only assign "+uuid.NewString()[:8], `{"version":"2026-10-01","statements":[`+allowStmt(`"roles:assign"`, `"project/`+pj+`/role/*"`)+`]}`, &p)
		e.attach(onlyAssign, assigner, &p)
		if res := e.call(assigner, http.MethodPut, bobRoles, map[string]any{"role_ids": []string{a}}); res.code != http.StatusOK {
			t.Fatalf("roles:assign without members:write: %d %s", res.code, res.body)
		}
		if res := e.call(admin, http.MethodPut, bobRoles, map[string]any{"role_ids": []string{}}); res.code != http.StatusOK {
			t.Fatalf("clear after assigner: %d", res.code)
		}
		// ... and members:write alone assigns nothing
		manager := e.user()
		e.member(p, manager)
		onlyMembers := e.role("Only members "+uuid.NewString()[:8], `{"version":"2026-10-01","statements":[`+allowStmt(`"project.members:write"`, `"project/`+pj+`"`)+`]}`, &p)
		e.attach(onlyMembers, manager, &p)
		if res := e.call(manager, http.MethodPut, bobRoles, map[string]any{"role_ids": []string{a}}); res.code != http.StatusForbidden {
			t.Fatalf("members:write without roles:assign: %d %s", res.code, res.body)
		}
		// a platform-form grant (role/*) does not reach inside the project
		support := e.user()
		e.member(p, support)
		supportRole := e.role("Support", `{"version":"2026-10-01","statements":[`+allowStmt(`"roles:assign"`, `"role/*"`)+`]}`, nil)
		e.attach(supportRole, support, nil)
		if res := e.call(support, http.MethodPut, bobRoles, map[string]any{"role_ids": []string{a}}); res.code != http.StatusForbidden {
			t.Fatalf("platform roles:assign inside a project: %d %s", res.code, res.body)
		}
	})

	t.Run("platform assignment is scoped by the role resource", func(t *testing.T) {
		roleA := e.role("Platform A", `{"version":"2026-10-01","statements":[`+allowStmt(`"users:read"`, `"user/*"`)+`]}`, nil)
		roleB := e.role("Platform B", `{"version":"2026-10-01","statements":[`+allowStmt(`"users:write"`, `"user/*"`)+`]}`, nil)
		a, b := roleA.String(), roleB.String()
		target := e.user()
		path := "/admin/users/" + target.String() + "/roles"

		onlyA := e.user()
		onlyARole := e.role("Assign only A", `{"version":"2026-10-01","statements":[`+allowStmt(`"roles:assign"`, `"role/`+a+`"`)+`]}`, nil)
		e.attach(onlyARole, onlyA, nil)
		if res := e.call(onlyA, http.MethodPut, path, map[string]any{"role_ids": []string{a}}); res.code != http.StatusOK {
			t.Fatalf("assign A: %d %s", res.code, res.body)
		}
		if res := e.call(onlyA, http.MethodPut, path, map[string]any{"role_ids": []string{a, b}}); res.code != http.StatusForbidden {
			t.Fatalf("assign B: %d %s", res.code, res.body)
		}
		// agents: roles:assign and agents:write on the agent are both needed
		if res := e.call(onlyA, http.MethodPut, "/admin/agents/"+uuid.NewString()+"/roles", map[string]any{"role_ids": []string{a}}); res.code != http.StatusForbidden {
			t.Fatalf("roles:assign without agents:write on the agent: %d %s", res.code, res.body)
		}
		// removal is judged too: only root may take B away
		if res := e.call(root, http.MethodPut, path, map[string]any{"role_ids": []string{a, b}}); res.code != http.StatusOK {
			t.Fatalf("root assigns A and B: %d %s", res.code, res.body)
		}
		if res := e.call(onlyA, http.MethodPut, path, map[string]any{"role_ids": []string{a}}); res.code != http.StatusForbidden {
			t.Fatalf("removing B: %d %s", res.code, res.body)
		}
		// taking A away while B stays only changes A, which onlyA may do
		if res := e.call(onlyA, http.MethodPut, path, map[string]any{"role_ids": []string{b}}); res.code != http.StatusOK {
			t.Fatalf("removing A while B stays: %d %s", res.code, res.body)
		}
	})

	t.Run("the last platform-wide full access attachment stays", func(t *testing.T) {
		// root and "Second everything" is unattached; root is the only holder.
		res := e.call(root, http.MethodPut, "/admin/users/"+root.String()+"/roles", map[string]any{"role_ids": []string{}})
		if res.code != http.StatusConflict || res.errorCode() != "ROLE_LAST_FULL_ACCESS" {
			t.Fatalf("clear last holder: %d %s", res.code, res.body)
		}
		if !e.allowed(root, "users:write", "user/"+uuid.NewString()) {
			t.Fatal("root must still hold full access")
		}
		// a system role can be edited like any other, but not deleted; the
		// default cannot be deleted either
		var sys struct {
			ID     string `db:"id"`
			Name   string `db:"name"`
			Policy string `db:"policy"`
		}
		if err := e.db.Get(&sys, `SELECT id, name, policy::text AS policy FROM roles WHERE is_system AND project_id IS NULL LIMIT 1`); err == nil {
			res = e.call(root, http.MethodPut, "/admin/roles/"+sys.ID, map[string]any{"name": sys.Name, "description": "edited", "policy": json.RawMessage(sys.Policy)})
			if res.code != http.StatusOK {
				t.Fatalf("system role edit: %d %s", res.code, res.body)
			}
			res = e.call(root, http.MethodDelete, "/admin/roles/"+sys.ID, nil)
			if res.code != http.StatusConflict || res.errorCode() != "ROLE_IS_SYSTEM" {
				t.Fatalf("system role delete: %d %s", res.code, res.body)
			}
		}
		res = e.call(root, http.MethodPut, "/admin/roles/"+everything.String()+"/default", nil)
		if res.code != http.StatusOK {
			t.Fatalf("set default: %d %s", res.code, res.body)
		}
		if res := e.call(root, http.MethodDelete, "/admin/roles/"+everything.String(), nil); res.code != http.StatusConflict || res.errorCode() != "ROLE_IS_DEFAULT" {
			t.Fatalf("delete default: %d %s", res.code, res.body)
		}
		// a caller who could not grant the role cannot make it the default
		if res := e.call(admin, http.MethodPut, "/admin/roles/"+everything.String()+"/default", nil); res.code != http.StatusForbidden {
			t.Fatalf("project admin sets default: %d", res.code)
		}
	})

	t.Run("catalogue endpoints", func(t *testing.T) {
		var actions []string
		e.call(bob, http.MethodGet, "/roles/actions", nil).data(t, &actions)
		if len(actions) < 10 {
			t.Fatalf("actions = %v", actions)
		}
		var v struct {
			Valid  bool `json:"valid"`
			Issues []struct{ Path, Message string }
		}
		e.call(bob, http.MethodPost, "/roles/validate", map[string]any{"policy": policyJSON(allowStmt(`"nope:x"`, `"*"`))}).data(t, &v)
		if v.Valid || len(v.Issues) != 1 {
			t.Fatalf("validate = %+v", v)
		}
		var sim struct {
			Allowed bool `json:"allowed"`
		}
		res := e.call(bob, http.MethodPost, "/roles/simulate", map[string]any{
			"policy": policyJSON(allowStmt(`"tasks:read"`, `"project/*"`)), "action": "tasks:read", "resource": "project/" + pj,
		})
		res.data(t, &sim)
		if res.code != http.StatusOK || !sim.Allowed {
			t.Fatalf("simulate: %d %s", res.code, res.body)
		}
		// naming a principal needs roles:read on role/*
		named := map[string]any{
			"policy": policyJSON(allowStmt(`"tasks:read"`, `"project/*"`)), "action": "tasks:read", "resource": "project/" + pj,
			"principal": map[string]any{"type": "user", "id": root.String()},
		}
		if res := e.call(bob, http.MethodPost, "/roles/simulate", named); res.code != http.StatusForbidden {
			t.Fatalf("simulate naming a principal as bob: %d %s", res.code, res.body)
		}
		if res := e.call(root, http.MethodPost, "/roles/simulate", named); res.code != http.StatusOK {
			t.Fatalf("simulate naming a principal as root: %d %s", res.code, res.body)
		}
		// project mirror needs roles:read on the project
		if res := e.call(bob, http.MethodGet, projectRoles+"/actions", nil); res.code != http.StatusForbidden {
			t.Fatalf("project mirror as a roleless member: %d", res.code)
		}
		if res := e.call(admin, http.MethodGet, projectRoles+"/actions", nil); res.code != http.StatusOK {
			t.Fatalf("project mirror as project admin: %d", res.code)
		}
	})

	t.Run("me/permissions reports roles:assign on specific role IDs", func(t *testing.T) {
		// Create two project roles (editor and viewer)
		editor := e.role("Editor", `{"version":"2026-10-01","statements":[`+allowStmt(`"tasks:write"`, `"project/`+pj+`/task/*"`)+`]}`, &p)
		viewer := e.role("Viewer", `{"version":"2026-10-01","statements":[`+allowStmt(`"tasks:read"`, `"project/`+pj+`/task/*"`)+`]}`, &p)

		// Create a lead user with roles:assign only on the two specific roles
		lead := e.user()
		e.member(p, lead)
		leadRole := e.role("Lead", `{"version":"2026-10-01","statements":[`+
			allowStmt(`"roles:assign"`, `"project/`+pj+`/role/`+editor.String()+`","project/`+pj+`/role/`+viewer.String()+`"`)+`,`+
			allowStmt(`"roles:read","members:read","members:write"`, `"project/`+pj+`/*"`)+
			`]}`, &p)
		e.attach(leadRole, lead, &p)

		// GET /projects/:id/members/me/permissions should include roles:assign
		var projectPerms struct {
			Permissions []string `json:"permissions"`
		}
		res := e.call(lead, http.MethodGet, "/projects/"+pj+"/members/me/permissions", nil)
		if res.code != http.StatusOK {
			t.Fatalf("GET project permissions: %d %s", res.code, res.body)
		}
		res.data(t, &projectPerms)
		found := false
		for _, perm := range projectPerms.Permissions {
			if perm == "roles:assign" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("roles:assign not in project permissions (got %v), but lead can assign those specific roles", projectPerms.Permissions)
		}

		// For platform-level roles:assign on specific role IDs
		platformLead := e.user()
		platformEditor := e.role("Platform Editor", `{"version":"2026-10-01","statements":[`+allowStmt(`"users:read"`, `"user/*"`)+`]}`, nil)
		platformLeadRole := e.role("Platform Lead", `{"version":"2026-10-01","statements":[`+
			allowStmt(`"roles:assign"`, `"role/`+platformEditor.String()+`"`)+`,`+
			allowStmt(`"roles:read","users:read"`, `"*"`)+
			`]}`, nil)
		e.attach(platformLeadRole, platformLead, nil)

		var globalPerms struct {
			Permissions []string `json:"permissions"`
		}
		res = e.call(platformLead, http.MethodGet, "/users/me/global-permissions", nil)
		if res.code != http.StatusOK {
			t.Fatalf("GET global permissions: %d %s", res.code, res.body)
		}
		res.data(t, &globalPerms)
		found = false
		for _, perm := range globalPerms.Permissions {
			if perm == "roles:assign" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("roles:assign not in global permissions (got %v), but platform lead can assign that specific role", globalPerms.Permissions)
		}
	})
}
