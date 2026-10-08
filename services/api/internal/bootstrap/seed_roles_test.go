package bootstrap

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"

	"github.com/Paca-AI/api/internal/bootstrap/defaultroles"
	"github.com/Paca-AI/api/internal/config"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/platform/database"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	"github.com/Paca-AI/api/migrations"
)

var quietLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// newSeedTestDB creates a database on PACA_TEST_PG_DSN's server with the
// migrations strictly before `before` applied ("" = none applied yet is not
// supported; pass a file name), dropped on cleanup.
func newSeedTestDB(t *testing.T) (*sqlx.DB, func(upTo string)) {
	t.Helper()
	dsn := os.Getenv("PACA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PACA_TEST_PG_DSN not set; skipping Postgres-backed seeding test")
	}
	admin, err := sqlx.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	name := "seed_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(dsn)
	u.Path = "/" + name
	db, err := sqlx.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
	})
	// apply(upTo) applies every embedded migration whose file name sorts
	// strictly before upTo ("~" = all of them).
	apply := func(upTo string) {
		t.Helper()
		sub := fstest.MapFS{}
		entries, err := fs.ReadDir(migrations.FS, ".")
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() >= upTo {
				continue
			}
			b, err := fs.ReadFile(migrations.FS, e.Name())
			if err != nil {
				t.Fatal(err)
			}
			sub[e.Name()] = &fstest.MapFile{Data: b}
		}
		if err := database.RunMigrationsFS(db.DB, sub); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	return db, apply
}

func must(t *testing.T, db *sqlx.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// roleRow is a snapshot of one role row.
type roleRow struct {
	ID        string          `db:"id"`
	Name      string          `db:"name"`
	Desc      string          `db:"description"`
	Policy    json.RawMessage `db:"policy"`
	ProjectID *string         `db:"project_id"`
	IsSystem  bool            `db:"is_system"`
	IsDefault bool            `db:"is_default"`
	Updated   string          `db:"updated"`
}

func snapshot(t *testing.T, db *sqlx.DB) map[string]roleRow {
	t.Helper()
	var rows []roleRow
	if err := db.Select(&rows, `SELECT id::text AS id, name, description, policy, project_id::text AS project_id,
		is_system, is_default, updated_at::text AS updated FROM roles`); err != nil {
		t.Fatal(err)
	}
	out := map[string]roleRow{}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

func attachmentCount(t *testing.T, db *sqlx.DB) int {
	t.Helper()
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM role_attachments`); err != nil {
		t.Fatal(err)
	}
	return n
}

var shipped = map[string]bool{defaultroles.SuperAdmin: true, defaultroles.Admin: true, defaultroles.User: true}

type invRecorder struct{ ids []string }

func (r *invRecorder) Invalidate(ids ...string) { r.ids = append(r.ids, ids...) }

// legacyFixture writes a realistic pre-000064 database: the three seeded
// global roles with the permissions the legacy startup sync stored, a custom
// role, the legacy project role templates, a project with its roles and
// members, and users holding the roles.
func legacyFixture(t *testing.T, db *sqlx.DB) (adminUser, plainUser, customUser uuid.UUID) {
	t.Helper()
	// What the legacy startup sync wrote into the shipped roles.
	must(t, db, `UPDATE global_roles SET permissions = '{"users.*":true,"global_roles.read":true,"projects.*":true,"settings.write":true,"agents.*":true,"plugins.*":true}' WHERE name = 'ADMIN'`)
	must(t, db, `UPDATE global_roles SET permissions = '{"users.read":true}', is_default = true WHERE name = 'USER'`)
	must(t, db, `INSERT INTO global_roles (name, permissions) VALUES ('Support', '{"users.read":true,"projects.read":true}')`)
	for name, perms := range map[string]string{
		"PROJECT_OWNER":  `{"projects.*":true,"tasks.*":true}`,
		"PROJECT_MEMBER": `{"tasks.read":true,"tasks.write":true}`,
	} {
		must(t, db, `INSERT INTO project_roles (id, project_id, role_name, permissions) VALUES (gen_random_uuid(), NULL, $1, $2::jsonb)`, name, perms)
	}
	var superAdmin, adminRole, userRole, support string
	for name, dst := range map[string]*string{"SUPER_ADMIN": &superAdmin, "ADMIN": &adminRole, "USER": &userRole, "Support": &support} {
		if err := db.Get(dst, `SELECT id::text FROM global_roles WHERE name = $1`, name); err != nil {
			t.Fatal(err)
		}
	}
	adminUser, plainUser, customUser = uuid.New(), uuid.New(), uuid.New()
	for u, role := range map[uuid.UUID]string{adminUser: adminRole, plainUser: userRole, customUser: support} {
		must(t, db, `INSERT INTO users (id, username, password_hash, role_id) VALUES ($1, $2, 'x', $3)`, u, "u_"+u.String()[:8], role)
	}
	// The configured admin predates the migration holding ADMIN, not SUPER_ADMIN.
	must(t, db, `UPDATE users SET username = 'admin' WHERE id = $1`, adminUser)

	p := uuid.New()
	must(t, db, `INSERT INTO projects (id, name) VALUES ($1, 'Legacy')`, p)
	pr := uuid.New()
	must(t, db, `INSERT INTO project_roles (id, project_id, role_name, permissions) VALUES ($1, $2, 'Admin', '{"*":true}')`, pr, p)
	must(t, db, `INSERT INTO project_members (id, project_id, user_id, member_type, project_role_id) VALUES (gen_random_uuid(), $1, $2, 'human', $3)`, p, plainUser, pr)
	return adminUser, plainUser, customUser
}

// The seeder must reconcile roles migration 000064 converted by name, leave
// every other role untouched, and be a no-op on the second run.
func TestSeedPlatformRoles_ReconcilesMigratedRoles(t *testing.T) {
	db, apply := newSeedTestDB(t)
	apply("000064_iam_roles.sql")
	legacyFixture(t, db)
	apply("~")

	before := snapshot(t, db)
	attBefore := attachmentCount(t, db)
	byName := map[string]roleRow{}
	for _, r := range before {
		if r.ProjectID == nil {
			byName[r.Name] = r
		}
	}
	for n := range shipped {
		r, ok := byName[n]
		if !ok {
			t.Fatalf("migration did not create platform role %s", n)
		}
		if n != defaultroles.SuperAdmin && r.IsSystem {
			t.Fatalf("%s already system before seeding", n)
		}
	}
	if !byName["USER"].IsDefault {
		t.Fatal("fixture: USER should be the migrated default")
	}

	inv := &invRecorder{}
	seeder := pgRepo.NewRoleSeedRepository(db)
	ids, err := seedPlatformRoles(context.Background(), seeder, inv, quietLog)
	if err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, db)

	// No duplicates: still exactly one platform role per shipped name, same id.
	for n := range shipped {
		if ids[n].String() != byName[n].ID {
			t.Errorf("%s was re-created (id %s -> %s) instead of reconciled", n, byName[n].ID, ids[n])
		}
		var c int
		if err := db.Get(&c, `SELECT COUNT(*) FROM roles WHERE project_id IS NULL AND name = $1`, n); err != nil || c != 1 {
			t.Errorf("%s: %d platform rows (%v)", n, c, err)
		}
		r := after[ids[n].String()]
		if !r.IsSystem {
			t.Errorf("%s is not a system role after seeding", n)
		}
	}
	if len(after) != len(before) {
		t.Errorf("role count %d -> %d", len(before), len(after))
	}
	// Every role that is not a shipped one is byte-for-byte unchanged.
	for id, r := range before {
		if r.ProjectID == nil && shipped[r.Name] {
			continue
		}
		if !reflect.DeepEqual(r, after[id]) {
			t.Errorf("role %q was modified:\nbefore %+v\nafter  %+v", r.Name, r, after[id])
		}
	}
	// Holders keep their roles, default flag is kept.
	if got := attachmentCount(t, db); got != attBefore {
		t.Errorf("attachments %d -> %d", attBefore, got)
	}
	if !after[ids["USER"].String()].IsDefault {
		t.Error("USER lost the default flag")
	}
	var defaults int
	if err := db.Get(&defaults, `SELECT COUNT(*) FROM roles WHERE is_default`); err != nil || defaults != 1 {
		t.Errorf("default roles = %d (%v)", defaults, err)
	}
	// A role that exists keeps the policy it has (here the converted one):
	// an administrator may have edited it, and a release must not undo that.
	for _, def := range defaultroles.Platform() {
		id := ids[def.Name].String()
		if !reflect.DeepEqual(before[id].Policy, after[id].Policy) {
			t.Errorf("%s policy was rewritten at startup:\nbefore %s\nafter  %s", def.Name, before[id].Policy, after[id].Policy)
		}
		if _, err := iam.ParsePolicy(after[id].Policy); err != nil {
			t.Errorf("%s: stored policy does not parse: %v", def.Name, err)
		}
	}
	if len(inv.ids) == 0 {
		t.Error("policy cache was not invalidated for the changed system roles")
	}

	// Second run: nothing changes at all, not even updated_at, no invalidation.
	inv2 := &invRecorder{}
	if _, err := seedPlatformRoles(context.Background(), seeder, inv2, quietLog); err != nil {
		t.Fatal(err)
	}
	if again := snapshot(t, db); !reflect.DeepEqual(after, again) {
		t.Error("second seeding run changed role rows")
	}
	if len(inv2.ids) != 0 {
		t.Errorf("second run invalidated %v", inv2.ids)
	}
}

// The migrated action sets of the legacy defaults (what 000064 produced) are
// the shipped ones: compare before the seeder overwrites them.
func TestShippedPlatformRolesMatchMigrationOutput(t *testing.T) {
	db, apply := newSeedTestDB(t)
	apply("000064_iam_roles.sql")
	legacyFixture(t, db)
	apply("~")
	for _, def := range defaultroles.Platform() {
		var raw []byte
		if err := db.Get(&raw, `SELECT policy FROM roles WHERE project_id IS NULL AND name = $1`, def.Name); err != nil {
			t.Fatal(err)
		}
		mig, _ := iam.ParsePolicy(raw)
		ship, _ := iam.ParsePolicy(def.Policy)
		flat := func(p *iam.Policy) map[string]bool {
			out := map[string]bool{}
			for _, s := range p.Statements {
				for _, a := range s.Actions {
					for _, r := range s.Resources {
						out[string(s.Effect)+" "+a+" on "+r] = true
					}
				}
			}
			return out
		}
		if !reflect.DeepEqual(flat(mig), flat(ship)) {
			t.Errorf("%s: shipped %v != migrated %v", def.Name, flat(ship), flat(mig))
		}
	}
}

func TestSeed_FreshInstall_AdminAndBot(t *testing.T) {
	db, apply := newSeedTestDB(t)
	apply("~")
	ctx := context.Background()
	seeder := pgRepo.NewRoleSeedRepository(db)
	users := pgRepo.NewUserRepository(db)

	ids, err := seedPlatformRoles(ctx, seeder, nil, quietLog)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.AdminConfig{Username: "admin", Password: "supersecret"}
	for i := 0; i < 2; i++ { // idempotent
		if err := seedAdmin(ctx, users, seeder, ids[defaultroles.SuperAdmin], cfg, quietLog); err != nil {
			t.Fatal(err)
		}
		if err := seedAgentBotUser(ctx, users, seeder, ids[defaultroles.SuperAdmin], quietLog); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"admin", agentBotUsername} {
		u, err := users.FindByUsername(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if len(u.Roles) != 1 || u.Roles[0].Name != defaultroles.SuperAdmin {
			t.Errorf("%s roles = %+v, want only SUPER_ADMIN", name, u.Roles)
		}
		if u.RoleClaim() != "SUPER_ADMIN" {
			t.Errorf("role claim = %q", u.RoleClaim())
		}
	}
	var legacy sql.NullString
	if err := db.Get(&legacy, `SELECT role_id::text FROM users WHERE username = 'admin'`); err != nil || legacy.Valid {
		t.Errorf("legacy users.role_id written: %v %v", legacy, err)
	}
	// The default role exists and is USER.
	var def string
	if err := db.Get(&def, `SELECT name FROM roles WHERE is_default AND project_id IS NULL`); err != nil || def != "USER" {
		t.Errorf("default = %q (%v)", def, err)
	}
	// A new account gets exactly the default role.
	nu := &userdom.User{ID: uuid.New(), Username: "bob", PasswordHash: "x"}
	if err := users.Create(ctx, nu); err != nil {
		t.Fatal(err)
	}
	got, _ := users.FindByID(ctx, nu.ID)
	if len(got.Roles) != 1 || got.Roles[0].Name != "USER" {
		t.Errorf("new user roles = %+v", got.Roles)
	}
}

// An existing admin gains SUPER_ADMIN additively and keeps what it holds; a
// default role an administrator picked is never overridden.
func TestSeed_ExistingAdminAndChosenDefault(t *testing.T) {
	db, apply := newSeedTestDB(t)
	apply("000064_iam_roles.sql")
	adminUser, _, _ := legacyFixture(t, db)
	apply("~")
	// The operator made "Support" the default.
	must(t, db, `UPDATE roles SET is_default = FALSE WHERE is_default`)
	must(t, db, `UPDATE roles SET is_default = TRUE WHERE name = 'Support'`)

	ctx := context.Background()
	seeder := pgRepo.NewRoleSeedRepository(db)
	users := pgRepo.NewUserRepository(db)
	ids, err := seedPlatformRoles(ctx, seeder, nil, quietLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedAdmin(ctx, users, seeder, ids[defaultroles.SuperAdmin], config.AdminConfig{Username: "admin", Password: "pw-pw-pw-pw"}, quietLog); err != nil {
		t.Fatal(err)
	}
	u, _ := users.FindByID(ctx, adminUser)
	names := u.RoleNames()
	if len(names) != 2 || names[0] != "ADMIN" || names[1] != "SUPER_ADMIN" {
		t.Errorf("admin roles = %v, want ADMIN + SUPER_ADMIN", names)
	}
	var def string
	if err := db.Get(&def, `SELECT name FROM roles WHERE is_default`); err != nil || def != "Support" {
		t.Errorf("default = %q, want the operator's choice (%v)", def, err)
	}
	// Soft-deleted admin accounts are left alone.
	must(t, db, `UPDATE users SET deleted_at = NOW() WHERE id = $1`, adminUser)
	must(t, db, `DELETE FROM role_attachments WHERE principal_id = $1`, adminUser)
	if err := seedAdmin(ctx, users, seeder, ids[defaultroles.SuperAdmin], config.AdminConfig{Username: "admin", Password: "pw-pw-pw-pw"}, quietLog); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.Get(&n, `SELECT COUNT(*) FROM role_attachments WHERE principal_id = $1`, adminUser)
	if n != 0 {
		t.Errorf("deleted admin got %d attachments", n)
	}
}
