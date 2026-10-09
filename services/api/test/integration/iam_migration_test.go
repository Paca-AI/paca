package integration_test

// Golden test for migrations/000064_iam_roles.sql: seeds a fixture covering
// every legacy role shape on a real Postgres, applies the consolidated IAM
// migration, and checks that the new IAM model (iam.Evaluate over the migrated roles and
// attachments) answers every (principal, action, resource) probe the same
// way the legacy resolver did. The legacy resolver below is a frozen copy of
// authz.Authorizer + postgres.AuthzPermissionStore + the
// RequireAgentAccess/RequireEnvironmentAccess gates, so this test keeps
// working after the legacy code is removed.
//
// Restricted agents/environments (access_mode = 'restricted' and their grant
// tables) are deliberately NOT converted: the fixture keeps some, the oracle
// still answers them with the legacy gate, and the test asserts that the
// migrated result is the OPEN answer (the plain permission check) for every
// probe, so a restricted resource opens up to everyone the roles allow until
// an admin adds Deny roles.
//
// Needs a disposable Postgres: set PACA_TEST_PG_DSN (e.g.
// postgres://postgres:test@127.0.0.1:5432/paca?sslmode=disable). Each test
// creates and drops its own database there. Skipped when unset.

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/platform/database"
	"github.com/Paca-AI/api/migrations"
)

const iamMigrationFile = "000064_iam_roles.sql"

// ---------------------------------------------------------------------------
// Database harness
// ---------------------------------------------------------------------------

// newMigrationTestDB creates a fresh database on PACA_TEST_PG_DSN's server
// and returns a handle to it; the database is dropped on cleanup.
func newMigrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PACA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PACA_TEST_PG_DSN not set; skipping Postgres migration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })

	name := "iam_mig_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("PACA_TEST_PG_DSN must be a URL: %v", err)
	}
	u.Path = "/" + name
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.ExecContext(t.Context(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return db
}

// migrationsBefore returns the embedded migrations strictly before name.
func migrationsBefore(t *testing.T, name string) fs.FS {
	t.Helper()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() >= name {
			continue
		}
		b, err := fs.ReadFile(migrations.FS, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = &fstest.MapFile{Data: b}
	}
	return out
}

// migrationsThrough returns the embedded migrations up to and including name.
func migrationsThrough(t *testing.T, name string) fs.FS {
	t.Helper()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() > name {
			continue
		}
		b, err := fs.ReadFile(migrations.FS, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = &fstest.MapFile{Data: b}
	}
	return out
}

func iamMigrationSQL(t *testing.T) string {
	t.Helper()
	b, err := fs.ReadFile(migrations.FS, iamMigrationFile)
	if err != nil {
		t.Fatalf("read %s: %v", iamMigrationFile, err)
	}
	return string(b)
}

// markedBlock returns the text between "-- >>> tag" and "-- <<< tag".
func markedBlock(t *testing.T, src, tag string) (before, block, after string) {
	t.Helper()
	open, closing := "-- >>> "+tag+"\n", "-- <<< "+tag+"\n"
	i, j := strings.Index(src, open), strings.Index(src, closing)
	if i < 0 || j < i {
		t.Fatalf("marker %q not found in %s", tag, iamMigrationFile)
	}
	return src[:i], src[i+len(open) : j], src[j+len(closing):]
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

type principal struct {
	label string
	typ   string // "user" | "agent"
	id    uuid.UUID
}

type iamFixture struct {
	P1, P2                  uuid.UUID
	SA, SA2, U1, M1, M2, M3 principal
	M4, M5, AD, MX, DU      principal
	A1, A2, GA, GA2         principal
	E1                      uuid.UUID
	all                     []principal
}

var editorPerms = `{"project.members.read":true,"project.roles.read":true,"tasks.read":true,"tasks.write":true,
 "sprints.read":true,"sprints.write":true,"views.read":true,"views.write":true,"docs.read":true,"docs.write":true,
 "agents.read":true,"agents.write":true,"conversations.read":true,"conversations.write":true,
 "workflows.read":true,"workflows.write":true,"environments.read":true,"environments.write":true,
 "environments.connect":true,"annotations.read":true,"annotations.write":true,"annotations.resolve":true}`

var viewerPerms = `{"project.members.read":true,"project.roles.read":true,"tasks.read":true,"sprints.read":true,
 "views.read":true,"docs.read":true,"agents.read":true,"conversations.read":true,"workflows.read":true,
 "environments.read":true,"annotations.read":true}`

// seedIAMFixture writes the legacy-model fixture:
//
//	users:   SA (SUPER_ADMIN, no membership), SA2 (SUPER_ADMIN, Viewer in P1),
//	         U1 (USER, no membership), MX (ADMIN, Viewer in P1),
//	         M1 Editor P1 + Viewer P2, M2 Viewer P1, M3 Editor P1 (membership
//	         soft-deleted), M4 template role in P1, M5 custom "Settings" role
//	         (wildcards) in P1, AD project Admin ("*") in P1, DU soft-deleted
//	         user with an Editor membership in P1
//	agents:  A1 restricted (Editor in P1, grant: M1 and M3's deleted row),
//	         A2 restricted (Viewer in P1, no grants), GA global agent with a
//	         mixed-format global role (Editor in P1), GA2 global agent with
//	         SUPER_ADMIN's "*" and no membership
//	envs:    E1 restricted in P1 (grants: M2, GA)
func seedIAMFixture(t *testing.T, db *sql.DB) *iamFixture {
	t.Helper()
	f := &iamFixture{P1: uuid.New(), P2: uuid.New(), E1: uuid.New()}
	roleID := func(name string) uuid.UUID {
		var id uuid.UUID
		if err := db.QueryRowContext(t.Context(), `SELECT id FROM global_roles WHERE name = $1`, name).Scan(&id); err != nil {
			t.Fatalf("global role %s: %v", name, err)
		}
		return id
	}
	superAdmin, admin, user := roleID("SUPER_ADMIN"), roleID("ADMIN"), roleID("USER")
	mixRole := uuid.New()
	mustExec(t, db, `INSERT INTO global_roles (id, name, permissions) VALUES ($1, 'FX_MIX',
		'{"tasks.*": true, "projects.read": "TRUE", "agents.write": 1, "docs.read": false, " users.read ": true}')`, mixRole)

	newUser := func(label string, role uuid.UUID, deleted bool) principal {
		p := principal{label: label, typ: "user", id: uuid.New()}
		var del any
		if deleted {
			del = "2026-01-01T00:00:00Z"
		}
		mustExec(t, db, `INSERT INTO users (id, username, password_hash, role_id, deleted_at) VALUES ($1, $2, 'x', $3, $4)`,
			p.id, "fx_"+strings.ToLower(label), role, del)
		f.all = append(f.all, p)
		return p
	}
	f.SA = newUser("SA", superAdmin, false)
	f.SA2 = newUser("SA2", superAdmin, false)
	f.U1 = newUser("U1", user, false)
	f.MX = newUser("MX", admin, false)
	f.M1 = newUser("M1", user, false)
	f.M2 = newUser("M2", user, false)
	f.M3 = newUser("M3", user, false)
	f.M4 = newUser("M4", user, false)
	f.M5 = newUser("M5", user, false)
	f.AD = newUser("AD", user, false)
	f.DU = newUser("DU", user, true)

	mustExec(t, db, `INSERT INTO projects (id, name) VALUES ($1, 'Fixture One'), ($2, 'Fixture Two')`, f.P1, f.P2)

	projRole := func(project *uuid.UUID, name, perms string) uuid.UUID {
		id := uuid.New()
		mustExec(t, db, `INSERT INTO project_roles (id, project_id, role_name, permissions) VALUES ($1, $2, $3, $4)`,
			id, project, name, perms)
		return id
	}
	p1Admin := projRole(&f.P1, "Admin", `{"*": true}`)
	p1Editor := projRole(&f.P1, "Editor", editorPerms)
	p1Viewer := projRole(&f.P1, "Viewer", viewerPerms)
	p1Settings := projRole(&f.P1, "Settings", `{"project.settings.*": true, "tasks.read": true, "project.roles.*": true}`)
	projRole(&f.P2, "Admin", `{"*": true}`)
	p2Viewer := projRole(&f.P2, "Viewer", viewerPerms)
	template := projRole(nil, "FX_TEMPLATE", `{"tasks.*": true, "conversations.read": true, "environments.*": true}`)

	newAgent := func(label string, project *uuid.UUID, globalRole *uuid.UUID, restricted bool) principal {
		p := principal{label: label, typ: "agent", id: uuid.New()}
		scope, mode := "project", "open"
		if project == nil {
			scope = "global"
		}
		if restricted {
			mode = "restricted"
		}
		mustExec(t, db, `INSERT INTO agents (id, project_id, name, handle, llm_provider, llm_model, llm_api_key_secret,
			agent_scope, global_role_id, access_mode) VALUES ($1, $2, $3, $4, 'openai', 'gpt', 'k', $5, $6, $7)`,
			p.id, project, "Agent "+label, "fx-"+strings.ToLower(label), scope, globalRole, mode)
		f.all = append(f.all, p)
		return p
	}
	f.A1 = newAgent("A1", &f.P1, nil, true)
	f.A2 = newAgent("A2", &f.P1, nil, true)
	f.GA = newAgent("GA", nil, &mixRole, false)
	f.GA2 = newAgent("GA2", nil, &superAdmin, false)

	member := func(project uuid.UUID, p principal, role uuid.UUID, deleted bool) uuid.UUID {
		id := uuid.New()
		var del any
		if deleted {
			del = "2026-01-01T00:00:00Z"
		}
		if p.typ == "agent" {
			mustExec(t, db, `INSERT INTO project_members (id, project_id, agent_id, member_type, project_role_id, deleted_at)
				VALUES ($1, $2, $3, 'agent', $4, $5)`, id, project, p.id, role, del)
		} else {
			mustExec(t, db, `INSERT INTO project_members (id, project_id, user_id, member_type, project_role_id, deleted_at)
				VALUES ($1, $2, $3, 'human', $4, $5)`, id, project, p.id, role, del)
		}
		return id
	}
	m1 := member(f.P1, f.M1, p1Editor, false)
	member(f.P2, f.M1, p2Viewer, false)
	m2 := member(f.P1, f.M2, p1Viewer, false)
	m3 := member(f.P1, f.M3, p1Editor, true)
	member(f.P1, f.M4, template, false)
	member(f.P1, f.M5, p1Settings, false)
	member(f.P1, f.AD, p1Admin, false)
	member(f.P1, f.SA2, p1Viewer, false)
	member(f.P1, f.MX, p1Viewer, false)
	member(f.P1, f.DU, p1Editor, false)
	member(f.P1, f.A1, p1Editor, false)
	member(f.P1, f.A2, p1Viewer, false)
	ga := member(f.P1, f.GA, p1Editor, false)

	mustExec(t, db, `INSERT INTO environments (id, project_id, name, slug, backend, secret_key_encrypted, access_mode)
		VALUES ($1, $2, 'Env One', 'env-one', 'docker', 'x', 'restricted')`, f.E1, f.P1)

	mustExec(t, db, `INSERT INTO agent_access_grants (agent_id, member_id) VALUES ($1, $2), ($1, $3)`, f.A1.id, m1, m3)
	mustExec(t, db, `INSERT INTO environment_access_grants (environment_id, member_id) VALUES ($1, $2), ($1, $3)`, f.E1, m2, ga)
	return f
}

// prepareLegacyDB returns a database at migration 000063 holding the fixture.
func prepareLegacyDB(t *testing.T) (*sql.DB, *iamFixture) {
	t.Helper()
	db := newMigrationTestDB(t)
	if err := database.RunMigrationsFS(db, migrationsBefore(t, iamMigrationFile)); err != nil {
		t.Fatalf("migrate to 000063: %v", err)
	}
	return db, seedIAMFixture(t, db)
}

// ---------------------------------------------------------------------------
// Frozen legacy resolver (authz.Authorizer + AuthzPermissionStore + access
// gates as of migration 000063).
// ---------------------------------------------------------------------------

type legacyResolver struct {
	t  *testing.T
	db *sql.DB
}

func (l legacyResolver) perms(q string, args ...any) []string {
	l.t.Helper()
	rows, err := l.db.QueryContext(l.t.Context(), q, args...)
	if err != nil {
		l.t.Fatalf("legacy query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			l.t.Fatal(err)
		}
		out = append(out, legacyPermissionsFromJSON(raw)...)
	}
	return out
}

// legacyPermissionsFromJSON is a copy of postgres.permissionsFromJSON.
func legacyPermissionsFromJSON(raw []byte) []string {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	var out []string
	add := func(k string) {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	switch v := payload.(type) {
	case map[string]any:
		for key, enabled := range v {
			switch e := enabled.(type) {
			case bool:
				if e {
					add(key)
				}
			case float64:
				if e != 0 {
					add(key)
				}
			case string:
				if strings.EqualFold(e, "true") {
					add(key)
				}
			}
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				add(s)
			}
		}
	}
	return out
}

// legacyHasPermission is a copy of authz.hasPermission.
func legacyHasPermission(granted []string, req string) bool {
	for _, p := range granted {
		if p == "*" || p == req {
			return true
		}
		if strings.HasSuffix(p, ".*") && strings.HasPrefix(req, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}

func (l legacyResolver) globalPerms(p principal) []string {
	if p.typ == "user" {
		return l.perms(`SELECT gr.permissions FROM global_roles gr JOIN users u ON u.role_id = gr.id
			WHERE u.id = $1 AND u.deleted_at IS NULL`, p.id)
	}
	return l.perms(`SELECT gr.permissions FROM global_roles gr JOIN agents a ON a.global_role_id = gr.id
		WHERE a.id = $1 AND a.agent_scope = 'global' AND a.deleted_at IS NULL`, p.id)
}

func (l legacyResolver) projectPerms(p principal, project string) []string {
	col := "user_id"
	if p.typ == "agent" {
		col = "agent_id"
	}
	q := `SELECT pr.permissions FROM project_roles pr JOIN project_members pm ON pm.project_role_id = pr.id
		WHERE pm.` + col + ` = $1 AND pm.project_id = $2 AND pm.deleted_at IS NULL`
	rows := l.perms(q, p.id, project)
	var n int
	if err := l.db.QueryRowContext(l.t.Context(), `SELECT count(*) FROM project_members pm WHERE pm.`+col+` = $1 AND pm.project_id = $2
		AND pm.deleted_at IS NULL`, p.id, project).Scan(&n); err != nil {
		l.t.Fatal(err)
	}
	if n > 0 {
		rows = append(rows, "projects.read")
	}
	return rows
}

func (l legacyResolver) exists(q string, args ...any) bool {
	l.t.Helper()
	var ok bool
	if err := l.db.QueryRowContext(l.t.Context(), `SELECT EXISTS (`+q+`)`, args...).Scan(&ok); err != nil {
		l.t.Fatalf("legacy exists: %v", err)
	}
	return ok
}

func (l legacyResolver) isDeleted(p principal) bool {
	table := "users"
	if p.typ == "agent" {
		table = "agents"
	}
	return l.exists(`SELECT 1 FROM `+table+` WHERE id = $1 AND deleted_at IS NOT NULL`, p.id)
}

func (l legacyResolver) activeMemberID(p principal, project string) (string, bool) {
	col := "user_id"
	if p.typ == "agent" {
		col = "agent_id"
	}
	var id string
	err := l.db.QueryRowContext(l.t.Context(), `SELECT id FROM project_members WHERE `+col+` = $1 AND project_id = $2 AND deleted_at IS NULL`,
		p.id, project).Scan(&id)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		l.t.Fatal(err)
	}
	return id, true
}

// legacyGate lists the permissions whose routes were behind
// RequireAgentAccess / RequireEnvironmentAccess (router.go).
var legacyGate = map[string][]string{
	"agent":       {"conversations.read", "conversations.write"},
	"environment": {"environments.read", "environments.write", "environments.connect"},
}

// allowed answers one probe the way the legacy stack did. A soft-deleted
// user or agent is treated as having no access (it can no longer
// authenticate; the migration gives it no attachments).
func (l legacyResolver) allowed(p principal, legacyKey, resource string) bool {
	return l.decide(p, legacyKey, resource, true)
}

// allowedUngated answers the probe as an ungated legacy route would (the
// plain RequirePermissions check, without RequireAgentAccess /
// RequireEnvironmentAccess) — e.g. PATCH /environments/{id}.
func (l legacyResolver) allowedUngated(p principal, legacyKey, resource string) bool {
	return l.decide(p, legacyKey, resource, false)
}

func (l legacyResolver) decide(p principal, legacyKey, resource string, gated bool) bool {
	if l.isDeleted(p) {
		return false
	}
	segs := strings.Split(resource, "/")
	if segs[0] != "project" || len(segs) < 2 {
		return legacyHasPermission(l.globalPerms(p), legacyKey)
	}
	project := segs[1]
	var granted []string
	if p.typ == "user" {
		// GHSA-hjcj: a global role counts in a project only through "*".
		for _, g := range l.globalPerms(p) {
			if g == "*" {
				granted = append(granted, g)
			}
		}
	}
	granted = append(granted, l.projectPerms(p, project)...)
	if !legacyHasPermission(granted, legacyKey) {
		return false
	}
	// A gated route's resource is the agent/environment or a sub-path of it
	// (ssh-keys, port-forwards, chat-sessions, ...).
	if !gated || len(segs) < 4 || !slices.Contains(legacyGate[segs[2]], legacyKey) {
		return true
	}
	// RequireAgentAccess / RequireEnvironmentAccess.
	kind, resID := segs[2], segs[3]
	memberID, ok := l.activeMemberID(p, project)
	if !ok {
		return false // resolveActorMemberID failed
	}
	if kind == "agent" {
		if !l.exists(`SELECT 1 FROM agents a JOIN project_members pm ON pm.agent_id = a.id AND pm.deleted_at IS NULL
			AND pm.project_id = $1 WHERE a.id = $2 AND a.deleted_at IS NULL`, project, resID) {
			return false // FindVisibleAgentInProject: not found
		}
		if !l.exists(`SELECT 1 FROM agents WHERE id = $1 AND access_mode = 'restricted'`, resID) {
			return true
		}
		return l.exists(`SELECT 1 FROM agent_access_grants WHERE agent_id = $1 AND member_id = $2`, resID, memberID)
	}
	if !l.exists(`SELECT 1 FROM environments WHERE id = $1 AND project_id = $2 AND deleted_at IS NULL`, resID, project) {
		return false
	}
	if !l.exists(`SELECT 1 FROM environments WHERE id = $1 AND access_mode = 'restricted'`, resID) {
		return true
	}
	return l.exists(`SELECT 1 FROM environment_access_grants WHERE environment_id = $1 AND member_id = $2`, resID, memberID)
}

// ---------------------------------------------------------------------------
// New model
// ---------------------------------------------------------------------------

func loadGrants(t *testing.T, db *sql.DB, p principal) []iam.Grant {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `SELECT r.id, COALESCE(ra.project_id::text, ''), r.policy
		FROM role_attachments ra JOIN roles r ON r.id = ra.role_id
		WHERE ra.principal_type = $1 AND ra.principal_id = $2`, p.typ, p.id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []iam.Grant
	for rows.Next() {
		var g iam.Grant
		var raw []byte
		if err := rows.Scan(&g.RoleID, &g.ProjectID, &raw); err != nil {
			t.Fatal(err)
		}
		pol, err := iam.ParsePolicy(raw)
		if err != nil {
			t.Fatalf("role %s: %v", g.RoleID, err)
		}
		g.Policy = pol
		out = append(out, g)
	}
	return out
}

func iamAllowed(grants []iam.Grant, p principal, action, resource string) bool {
	return iam.Evaluate(grants, iam.Request{
		Action:   action,
		Resource: resource,
		Attrs:    map[string][]string{"principal.id": {p.id.String()}, "principal.type": {p.typ}},
	}).Allowed
}

type rolePolicy struct {
	name, kind string
	project    sql.NullString
	policy     *iam.Policy
	raw        string
}

func loadRoles(t *testing.T, db *sql.DB) []rolePolicy {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `SELECT name, COALESCE(legacy_kind, ''), project_id::text, policy FROM roles ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []rolePolicy
	for rows.Next() {
		var r rolePolicy
		if err := rows.Scan(&r.name, &r.kind, &r.project, &r.raw); err != nil {
			t.Fatal(err)
		}
		pol, err := iam.ParsePolicy([]byte(r.raw))
		if err != nil {
			t.Fatalf("role %q policy does not parse: %v", r.name, err)
		}
		r.policy = pol
		out = append(out, r)
	}
	return out
}

func findRole(t *testing.T, roles []rolePolicy, name string, project *uuid.UUID) rolePolicy {
	t.Helper()
	for _, r := range roles {
		if r.name == name && (project == nil) == !r.project.Valid && (project == nil || r.project.String == project.String()) {
			return r
		}
	}
	t.Fatalf("role %q (project %v) not found", name, project)
	return rolePolicy{}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestIAMMigration(t *testing.T) {
	db, f := prepareLegacyDB(t)
	legacy := legacyResolver{t: t, db: db}

	// Legacy answers must be taken before the migration runs (it does not
	// drop the legacy tables, but this keeps the oracle honest).
	matrixActions := []string{
		"tasks:read", "tasks:write", "agents:write", "conversations:read", "conversations:write",
		"environments:read", "environments:connect", "users:read", "projects:read",
		"project.settings.task_types:write",
	}
	actionKey := func(a string) string { i := strings.LastIndex(a, ":"); return a[:i] + "." + a[i+1:] }
	resources := []string{
		"project/" + f.P1.String(),
		"project/" + f.P1.String() + "/agent/" + f.A1.id.String(),
		"project/" + f.P1.String() + "/agent/" + f.A2.id.String(),
		"project/" + f.P1.String() + "/environment/" + f.E1.String(),
		"project/" + f.P1.String() + "/environment/" + f.E1.String() + "/ssh-keys/k1",
		"user/" + uuid.NewString(),
		"project/" + f.P2.String(),
	}
	type probe struct {
		p        principal
		action   string
		resource string
	}
	want := map[probe]bool{}
	for _, p := range f.all {
		for _, a := range matrixActions {
			for _, r := range resources {
				want[probe{p, a, r}] = legacy.allowed(p, actionKey(a), r)
			}
		}
	}
	// What the migrated model must answer: the plain permission check, i.e.
	// the legacy answer without the restricted-access gate (restricted
	// resources are not converted and become open).
	open := map[probe]bool{}
	opened := 0
	for _, p := range f.all {
		for _, a := range matrixActions {
			for _, r := range resources {
				pr := probe{p, a, r}
				open[pr] = legacy.allowedUngated(p, actionKey(a), r)
				if open[pr] && !want[pr] {
					opened++
				}
			}
		}
	}
	legacyCounts := map[string]int{}
	for _, q := range []struct{ kind, sql string }{
		{"global", `SELECT count(*) FROM global_roles`},
		{"project", `SELECT count(*) FROM project_roles`},
	} {
		var n int
		if err := db.QueryRowContext(t.Context(), q.sql).Scan(&n); err != nil {
			t.Fatal(err)
		}
		legacyCounts[q.kind] = n
	}

	if err := database.RunMigrationsFS(db, migrationsThrough(t, iamMigrationFile)); err != nil {
		t.Fatalf("apply %s: %v", iamMigrationFile, err)
	}

	for _, c := range []struct{ table, column string }{{"users", "role_id"}, {"project_members", "project_role_id"}} {
		var nullable string
		if err := db.QueryRowContext(t.Context(), `SELECT is_nullable FROM information_schema.columns WHERE table_name = $1 AND column_name = $2`, c.table, c.column).Scan(&nullable); err != nil {
			t.Fatal(err)
		}
		if nullable != "YES" {
			t.Errorf("%s.%s is_nullable = %q, want YES", c.table, c.column, nullable)
		}
	}

	roles := loadRoles(t, db)

	t.Run("role counts and shapes", func(t *testing.T) {
		got := map[string]int{}
		for _, r := range roles {
			got[r.kind]++
		}
		wantCounts := map[string]int{
			"global":       legacyCounts["global"],
			"project":      legacyCounts["project"],
			"global_agent": 1, // SUPER_ADMIN, held by GA2
		}
		total := 0
		for k, n := range wantCounts {
			total += n
			if got[k] != n {
				t.Errorf("roles with legacy_kind %q = %d, want %d", k, got[k], n)
			}
		}
		if len(roles) != total {
			t.Errorf("roles = %d, want %d", len(roles), total)
		}

		reg := iam.NewRegistry()
		for _, r := range roles {
			if issues := iam.Validate(r.policy, reg, iam.NewAttributeSchema()); len(issues) > 0 {
				t.Errorf("role %q fails validation: %v (%s)", r.name, issues, r.raw)
			}
		}

		stmtIs := func(r rolePolicy, i int, effect iam.Effect, actions, resources []string) {
			t.Helper()
			if len(r.policy.Statements) <= i {
				t.Errorf("role %q: missing statement %d: %s", r.name, i, r.raw)
				return
			}
			s := r.policy.Statements[i]
			if s.Effect != effect || !slices.Equal(s.Actions, actions) || !slices.Equal(s.Resources, resources) {
				t.Errorf("role %q statement %d = %+v, want %s %v on %v", r.name, i, s, effect, actions, resources)
			}
		}
		roots := []string{"user", "user/*", "role", "role/*", "plugin", "plugin/*", "settings", "sso", "agent", "agent/*", "project"}
		p1, p2 := "project/"+f.P1.String(), "project/"+f.P2.String()

		sa := findRole(t, roles, "SUPER_ADMIN", nil)
		stmtIs(sa, 0, iam.EffectAllow, []string{"*"}, []string{"*"})
		stmtIs(findRole(t, roles, "SUPER_ADMIN (agents)", nil), 0, iam.EffectAllow, []string{"*"}, roots)
		stmtIs(findRole(t, roles, "USER", nil), 0, iam.EffectAllow, []string{"users:read"}, roots)
		stmtIs(findRole(t, roles, "FX_MIX", nil), 0, iam.EffectAllow,
			[]string{"agents:write", "projects:read", "tasks:*", "users:read"}, roots)
		for _, r := range roles {
			if r.kind != "global" {
				continue
			}
			for _, s := range r.policy.Statements {
				if slices.Contains(s.Resources, "project/*") {
					t.Errorf("global role %q reaches project/* (GHSA-hjcj): %s", r.name, r.raw)
				}
				if slices.Contains(s.Resources, "*") && !slices.Equal(s.Actions, []string{"*"}) {
					t.Errorf("global role %q grants named actions on *: %s", r.name, r.raw)
				}
			}
		}

		stmtIs(findRole(t, roles, "Admin", &f.P1), 0, iam.EffectAllow, []string{"*"}, []string{p1 + "/*"})
		stmtIs(findRole(t, roles, "Admin", &f.P2), 0, iam.EffectAllow, []string{"*"}, []string{p2 + "/*"})
		viewer := findRole(t, roles, "Viewer", &f.P1)
		stmtIs(viewer, 0, iam.EffectAllow, []string{
			"agents:read", "annotations:read", "conversations:read", "docs:read", "environments:read",
			"project.members:read", "projects:read", "sprints:read", "tasks:read", "views:read", "workflows:read",
		}, []string{p1 + "/*"})
		stmtIs(viewer, 1, iam.EffectAllow, []string{"roles:read"}, []string{p1 + "/role/*"})
		settings := findRole(t, roles, "Settings", &f.P1)
		stmtIs(settings, 0, iam.EffectAllow, []string{
			"project.settings.custom_fields:write", "project.settings.task_statuses:write",
			"project.settings.task_types:write", "projects:read", "tasks:read",
		}, []string{p1 + "/*"})
		stmtIs(settings, 1, iam.EffectAllow, []string{"roles:*"}, []string{p1 + "/role/*"})
		stmtIs(findRole(t, roles, "FX_TEMPLATE", nil), 0, iam.EffectAllow,
			[]string{"conversations:read", "environments:*", "projects:read", "tasks:*"}, []string{"project/*"})

		var system []string
		rows, err := db.QueryContext(t.Context(), `SELECT name FROM roles WHERE is_system ORDER BY name`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var n string
			_ = rows.Scan(&n)
			system = append(system, n)
		}
		_ = rows.Close()
		if !slices.Equal(system, []string{"Admin", "Admin", "SUPER_ADMIN"}) {
			t.Errorf("system roles = %v", system)
		}
		var defaults []string
		rows, err = db.QueryContext(t.Context(), `SELECT name FROM roles WHERE is_default`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var n string
			_ = rows.Scan(&n)
			defaults = append(defaults, n)
		}
		_ = rows.Close()
		if !slices.Equal(defaults, []string{"USER"}) {
			t.Errorf("default roles = %v, want [USER]", defaults)
		}
	})

	t.Run("restricted resources are not converted", func(t *testing.T) {
		for _, r := range roles {
			if strings.HasPrefix(r.name, "Restrict ") || strings.HasPrefix(r.name, "Access to ") {
				t.Errorf("role %q was generated from restricted access, which is not migrated: %s", r.name, r.raw)
			}
			if strings.Contains(r.raw, "/agent/"+f.A1.id.String()) || strings.Contains(r.raw, "/environment/"+f.E1.String()) {
				t.Errorf("role %q names a formerly restricted resource: %s", r.name, r.raw)
			}
		}
		// The fixture assertions cover the final consolidated migration, including
		// the now-nullable legacy role columns and normalized policy resources.
		var n int
		if err := db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM agents WHERE access_mode = 'restricted')
			+ (SELECT count(*) FROM environments WHERE access_mode = 'restricted')
			+ (SELECT count(*) FROM agent_access_grants) + (SELECT count(*) FROM environment_access_grants)`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 2+1+2+2 {
			t.Errorf("legacy restricted rows after migration = %d, want 7", n)
		}
		// Open: a non-grantee member (M1 on A2, no grants at all; a Viewer-
		// level M2 on A1) and a project Admin can use them.
		resA1 := "project/" + f.P1.String() + "/agent/" + f.A1.id.String()
		resA2 := "project/" + f.P1.String() + "/agent/" + f.A2.id.String()
		resE1 := "project/" + f.P1.String() + "/environment/" + f.E1.String()
		sshKey := resE1 + "/ssh-keys/k1"
		for _, c := range []struct {
			p        principal
			action   string
			resource string
		}{
			{f.M1, "conversations:write", resA2}, {f.AD, "conversations:read", resA2},
			{f.M2, "conversations:read", resA1}, {f.M1, "environments:connect", sshKey},
			{f.AD, "environments:write", resE1},
		} {
			if !iamAllowed(loadGrants(t, db, c.p), c.p, c.action, c.resource) {
				t.Errorf("%s denied %s on %s; restricted resources must be open after migration", c.p.label, c.action, c.resource)
			}
		}
		if opened == 0 {
			t.Error("fixture has no probe the legacy gate refused; it no longer covers restricted access")
		}
		t.Logf("%d probes the legacy gate refused are open after migration", opened)
	})

	t.Run("attachments", func(t *testing.T) {
		var n int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM role_attachments WHERE principal_id = $1 AND project_id = $2`,
			f.M3.id, f.P1).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("M3 (soft-deleted membership) has %d attachments in P1", n)
		}
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM role_attachments WHERE principal_id = $1`, f.DU.id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("DU (soft-deleted user) has %d attachments", n)
		}
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM role_attachments WHERE principal_id = $1 AND project_id IS NULL`,
			f.M3.id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("M3 should keep its platform USER attachment, got %d", n)
		}
	})

	t.Run("golden matrix", func(t *testing.T) {
		mismatches := 0
		for pr, wantAllowed := range open {
			got := iamAllowed(loadGrants(t, db, pr.p), pr.p, pr.action, pr.resource)
			if got != wantAllowed {
				mismatches++
				t.Errorf("%s %s on %s: expected %v (legacy permission check, restricted access ignored), iam=%v",
					pr.p.label, pr.action, pr.resource, wantAllowed, got)
			}
		}
		t.Logf("matrix: %d probes, %d mismatches", len(open), mismatches)
	})
}

// TestIAMMigrationKeyMapping checks that the migration's SQL key mapping is
// exactly legacyKeyToAction (iam_legacy_keys_test.go), and that its built-in key list maps onto
// exactly the registry's built-in actions.
func TestIAMMigrationKeyMapping(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := database.RunMigrationsFS(db, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, helpers, _ := markedBlock(t, iamMigrationSQL(t), "iam-migration-helpers")
	ctx := context.Background()
	conn, err := db.Conn(ctx) // pg_temp functions live in one session
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, helpers); err != nil {
		t.Fatalf("load helpers: %v", err)
	}

	var sqlKeysRaw string
	if err := conn.QueryRowContext(ctx, `SELECT array_to_json(pg_temp.legacy_builtin_keys())::text`).Scan(&sqlKeysRaw); err != nil {
		t.Fatal(err)
	}
	var sqlKeys []string
	if err := json.Unmarshal([]byte(sqlKeysRaw), &sqlKeys); err != nil {
		t.Fatal(err)
	}
	if goKeys, sk := slices.Sorted(slices.Values(legacyBuiltinKeys)), slices.Sorted(slices.Values(sqlKeys)); !slices.Equal(goKeys, sk) {
		t.Errorf("SQL legacy_builtin_keys %v,\nGo legacyBuiltinKeys %v", sk, goKeys)
	}
	mapped := map[string]bool{}
	for _, k := range sqlKeys {
		mapped[legacyKeyToAction(k)] = true
	}
	regActions := iam.NewRegistry().Actions()
	var mappedList []string
	for a := range mapped {
		mappedList = append(mappedList, a)
	}
	sort.Strings(mappedList)
	if !slices.Equal(mappedList, regActions) {
		t.Errorf("SQL builtin keys map to %v,\nregistry has %v", mappedList, regActions)
	}

	keys := slices.Clone(sqlKeys)
	for _, a := range regActions { // the registry's own (builtin-list) keys
		i := strings.LastIndex(a, ":")
		keys = append(keys, a[:i]+"."+a[i+1:])
	}
	keys = append(keys, "*", "users.*", "global_roles.*", "project.roles.*", "project.settings.*", "project.*",
		"tasks.*", "global_roles.", "project.roles.", "project.roles.x.y", "nodot", "a.b.c", "x.", ".x",
		"plugin.jev.sync", "time_logging.manage_all", "global_rolesx.read")
	for _, k := range keys {
		var got string
		if err := conn.QueryRowContext(ctx, `SELECT pg_temp.legacy_key_to_action($1)`, k).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := legacyKeyToAction(k); got != want {
			t.Errorf("SQL maps %q to %q, LegacyKeyToAction gives %q", k, got, want)
		}
	}

	// The self-check's matchers must agree with the evaluator's.
	patterns := []string{"*", "project/*", "project/p1/*", "project/*/role/*", "project/p1", "project/*/agent/a1",
		"user", "user/*", "role/*", "a.b/*", "project/p.1/*"}
	targets := []string{"project", "project/p1", "project/p1/role/r", "project/p2/role/r", "project/p1/agent/a1",
		"project/p1/", "project//role/x", "user", "user/u1", "role", "a.b", "axb/c", "project/pX1/x", "project/p.1"}
	for _, pat := range patterns {
		for _, res := range targets {
			var got bool
			if err := conn.QueryRowContext(ctx, `SELECT pg_temp.iam_resource_match($1, $2)`, pat, res).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if want := iam.MatchResource(pat, res); got != want {
				t.Errorf("SQL resource match(%q, %q) = %v, iam.MatchResource = %v", pat, res, got, want)
			}
		}
	}
	for _, pat := range []string{"*", "tasks:*", "tasks:read", "project.settings:*", "roles:*"} {
		for _, act := range []string{"tasks:read", "tasks:write", "tasksx:read", "project.settings.task_types:write", "roles:assign"} {
			var got bool
			if err := conn.QueryRowContext(ctx, `SELECT pg_temp.iam_action_match($1, $2)`, pat, act).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if want := iam.MatchAction(pat, act); got != want {
				t.Errorf("SQL action match(%q, %q) = %v, iam.MatchAction = %v", pat, act, got, want)
			}
		}
	}

	// Wildcard expansion examples.
	for perms, want := range map[string]string{
		`{"project.settings.*": true}`:          `["project.settings.custom_fields:write", "project.settings.task_statuses:write", "project.settings.task_types:write"]`,
		`{"tasks.*": true, "tasks.read": true}`: `["tasks:*", "tasks:read"]`,
		`{"global_roles.*": true}`:              `["roles:*"]`,
		`{"time_logging.*": true}`:              `["time_logging:*"]`,
		`["docs.read", " docs.write "]`:         `["docs:read", "docs:write"]`,
		`{"a.b": false, "c.d": 0, "e.f": "no"}`: `[]`,
	} {
		var got string
		if err := conn.QueryRowContext(ctx, `SELECT pg_temp.legacy_actions($1::jsonb)::text`, perms).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("legacy_actions(%s) = %s, want %s", perms, got, want)
		}
	}
}

// TestIAMMigrationSelfCheckAborts proves the in-SQL self-check catches a
// broken conversion: without the project-member attachments members lose the
// access their project role gave them, and the migration must roll back.
func TestIAMMigrationSelfCheckAborts(t *testing.T) {
	db, _ := prepareLegacyDB(t)
	before, _, after := markedBlock(t, iamMigrationSQL(t), "project-attachments")
	broken := fstest.MapFS{iamMigrationFile: &fstest.MapFile{Data: []byte(before + after)}}
	err := database.RunMigrationsFS(db, broken)
	if err == nil || !strings.Contains(err.Error(), "IAM migration self-check failed") {
		t.Fatalf("broken migration: err = %v, want the self-check to abort", err)
	}
	var exists bool
	if err := db.QueryRowContext(t.Context(), `SELECT to_regclass('roles') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("roles table exists after the self-check aborted; the migration was not rolled back")
	}
}

// TestIAMNotInEmptyOperand documents the evaluator semantics admins hit when
// recreating a restriction by hand: NotIn with an empty list matches every
// principal, but iam.Validate rejects an empty operand (use an unconditional
// Deny instead).
func TestIAMNotInEmptyOperand(t *testing.T) {
	res := "project/p/agent/a"
	grants := []iam.Grant{{RoleID: "r", ProjectID: "p", Policy: &iam.Policy{Statements: []iam.Statement{
		{Effect: iam.EffectAllow, Actions: []string{"*"}, Resources: []string{"project/p/*"}},
		{Effect: iam.EffectDeny, Actions: []string{"conversations:write"}, Resources: []string{res},
			Conditions: iam.Conditions{"NotIn": {"principal.id": {}}}},
	}}}}
	req := iam.Request{Action: "conversations:write", Resource: res, Attrs: map[string][]string{"principal.id": {"u1"}}}
	if iam.Evaluate(grants, req).Allowed {
		t.Error("NotIn [] should deny everyone")
	}
	if issues := iam.Validate(grants[0].Policy, iam.NewRegistry(), iam.NewAttributeSchema()); len(issues) == 0 {
		t.Error("expected Validate to reject an empty NotIn operand")
	}
}
