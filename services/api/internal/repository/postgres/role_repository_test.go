package postgres

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	roledom "github.com/Paca-AI/api/internal/domain/role"
)

const (
	// rolePolicyUsersRead is a workspace role: it names platform resources, so it
	// can be attached platform-wide (rolePolicyRead is a project template).
	rolePolicyUsersRead    = `{"version":"2026-10-01","statements":[{"effect":"Allow","actions":["users:read"],"resources":["user","user/*"]}]}`
	rolePolicyTemplateStar = `{"version":"2026-10-01","statements":[{"effect":"Allow","actions":["*"],"resources":["project/*"]}]}`
	rolePolicyRead         = `{"version":"2026-10-01","statements":[{"effect":"Allow","actions":["tasks:read"],"resources":["project/*"]}]}`
	rolePolicyStar         = `{"version":"2026-10-01","statements":[{"effect":"Allow","actions":["*"],"resources":["*"]}]}`
)

func newRole(t *testing.T, repo *RoleRepository, name, policy string, project *uuid.UUID) *roledom.Role {
	t.Helper()
	r := &roledom.Role{Name: name, Description: "d", Policy: json.RawMessage(policy), ProjectID: project}
	if err := repo.Create(t.Context(), r); err != nil {
		t.Fatalf("create role %q: %v", name, err)
	}
	return r
}

func attachmentRows(t *testing.T, db *sqlx.DB, role uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM role_attachments WHERE role_id = $1`, role); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRoleRepository_CRUDAndNameScopes(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewRoleRepository(db)
	p1, p2 := fx.project(), fx.project()

	a := newRole(t, repo, "Dev", rolePolicyRead, nil)
	if a.ID == uuid.Nil || a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		t.Fatalf("create must fill id and timestamps: %+v", a)
	}
	// unique within the platform scope
	if err := repo.Create(t.Context(), &roledom.Role{Name: "Dev", Policy: json.RawMessage(rolePolicyRead)}); !errors.Is(err, roledom.ErrNameTaken) {
		t.Fatalf("duplicate platform name: %v", err)
	}
	// the same name may exist as a project role, once per project
	b := newRole(t, repo, "Dev", rolePolicyRead, &p1)
	newRole(t, repo, "Dev", rolePolicyRead, &p2)
	if err := repo.Create(t.Context(), &roledom.Role{Name: "Dev", Policy: json.RawMessage(rolePolicyRead), ProjectID: &p1}); !errors.Is(err, roledom.ErrNameTaken) {
		t.Fatalf("duplicate project name: %v", err)
	}
	// unknown owner project
	missing := uuid.New()
	if err := repo.Create(t.Context(), &roledom.Role{Name: "X", Policy: json.RawMessage(rolePolicyRead), ProjectID: &missing}); !errors.Is(err, roledom.ErrProjectNotFound) {
		t.Fatalf("unknown project: %v", err)
	}

	got, err := repo.FindByID(t.Context(), b.ID, nil)
	if err != nil || got.ProjectID == nil || *got.ProjectID != p1 || got.Name != "Dev" || got.Description != "d" {
		t.Fatalf("find: %+v err=%v", got, err)
	}
	var pol struct{ Version string }
	if err := json.Unmarshal(got.Policy, &pol); err != nil || pol.Version != "2026-10-01" {
		t.Fatalf("policy must come back as JSON: %s err=%v", got.Policy, err)
	}
	if _, err := repo.FindByID(t.Context(), uuid.New(), nil); !errors.Is(err, roledom.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}

	// update
	a.Name, a.Description, a.Policy = "Developer", "new", json.RawMessage(rolePolicyStar)
	if err := repo.Update(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.FindByID(t.Context(), a.ID, nil)
	if got.Name != "Developer" || got.Description != "new" || !got.UpdatedAt.After(a.CreatedAt.Add(-1)) {
		t.Fatalf("update not applied: %+v", got)
	}
	if string(mustPolicyJSON(t, got.Policy)) != string(mustPolicyJSON(t, json.RawMessage(rolePolicyStar))) {
		t.Fatalf("policy not updated: %s", got.Policy)
	}
	// renaming onto an existing name in the scope conflicts
	c := newRole(t, repo, "Other", rolePolicyRead, nil)
	c.Name = "Developer"
	if err := repo.Update(t.Context(), c); !errors.Is(err, roledom.ErrNameTaken) {
		t.Fatalf("rename conflict: %v", err)
	}
	missingRole := &roledom.Role{ID: uuid.New(), Name: "Z", Policy: json.RawMessage(rolePolicyRead)}
	if err := repo.Update(t.Context(), missingRole); !errors.Is(err, roledom.ErrNotFound) {
		t.Fatalf("update unknown: %v", err)
	}

	// listings
	plat, err := repo.ListPlatform(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range plat {
		if r.ProjectID != nil {
			t.Fatalf("ListPlatform returned a project role: %+v", r)
		}
	}
	forP1, err := repo.ListForProject(t.Context(), p1)
	if err != nil {
		t.Fatal(err)
	}
	var sawOwn, sawPlatform, sawOther bool
	for _, r := range forP1 {
		switch {
		case r.ID == b.ID:
			sawOwn = true
		case r.ProjectID == nil:
			sawPlatform = true
		case *r.ProjectID != p1:
			sawOther = true
		}
	}
	// A platform role is not listed in a project it is not attached in.
	if !sawOwn || sawPlatform || sawOther {
		t.Fatalf("ListForProject own=%v platform=%v other=%v", sawOwn, sawPlatform, sawOther)
	}
	// owned roles come first, platform roles after
	if forP1[0].ProjectID == nil {
		t.Fatalf("project's own roles must be listed first: %+v", forP1[0])
	}

	// delete
	if err := repo.Delete(t.Context(), c.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(t.Context(), c.ID); !errors.Is(err, roledom.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func mustPolicyJSON(t *testing.T, raw json.RawMessage) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	return b
}

func TestRoleRepository_SystemRoleCanBeEditedButNotDeleted(t *testing.T) {
	db := newIAMPGTestDB(t)
	repo := NewRoleRepository(db)
	r := newRole(t, repo, "Sys", rolePolicyRead, nil)
	pgExec(t, db, `UPDATE roles SET is_system = true WHERE id = $1`, r.ID)

	r.Name = "Renamed"
	if err := repo.Update(t.Context(), r); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := repo.Delete(t.Context(), r.ID); !errors.Is(err, roledom.ErrSystemRole) {
		t.Fatalf("delete: %v", err)
	}
	got, _ := repo.FindByID(t.Context(), r.ID, nil)
	if got.Name != "Renamed" {
		t.Fatalf("system role not edited: %+v", got)
	}
}

func TestRoleRepository_DefaultRole(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewRoleRepository(db)
	a := newRole(t, repo, "A", rolePolicyRead, nil)
	b := newRole(t, repo, "B", rolePolicyRead, nil)
	owned := newRole(t, repo, "P", rolePolicyRead, ptr(fx.project()))

	defaults := func() []uuid.UUID {
		var ids []uuid.UUID
		if err := db.Select(&ids, `SELECT id FROM roles WHERE is_default`); err != nil {
			t.Fatal(err)
		}
		return ids
	}
	if err := repo.SetDefault(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	if ids := defaults(); len(ids) != 1 || ids[0] != a.ID {
		t.Fatalf("defaults = %v, want [A]", ids)
	}
	// swapping keeps exactly one default, in one step
	if err := repo.SetDefault(t.Context(), b.ID); err != nil {
		t.Fatal(err)
	}
	if ids := defaults(); len(ids) != 1 || ids[0] != b.ID {
		t.Fatalf("defaults = %v, want [B]", ids)
	}
	if err := repo.SetDefault(t.Context(), b.ID); err != nil {
		t.Fatalf("setting the current default again must succeed: %v", err)
	}
	if err := repo.SetDefault(t.Context(), owned.ID); !errors.Is(err, roledom.ErrNotFound) {
		t.Fatalf("project role cannot be the default: %v", err)
	}
	if err := repo.SetDefault(t.Context(), uuid.New()); !errors.Is(err, roledom.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if ids := defaults(); len(ids) != 1 || ids[0] != b.ID {
		t.Fatalf("failed set-default changed the default: %v", ids)
	}
	if err := repo.Delete(t.Context(), b.ID); !errors.Is(err, roledom.ErrIsDefault) {
		t.Fatalf("delete default: %v", err)
	}
	if err := repo.Delete(t.Context(), a.ID); err != nil {
		t.Fatalf("delete non-default: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestRoleRepository_ReplaceAttachments(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewRoleRepository(db)
	p, q := fx.project(), fx.project()
	user, by := fx.user(false), fx.user(false)
	r1 := newRole(t, repo, "R1", rolePolicyUsersRead, nil)
	r2 := newRole(t, repo, "R2", rolePolicyUsersRead, nil)
	r3 := newRole(t, repo, "R3", rolePolicyUsersRead, nil)
	own := newRole(t, repo, "Own", rolePolicyRead, &p)
	foreign := newRole(t, repo, "Foreign", rolePolicyRead, &q)

	replace := func(project *uuid.UUID, createdBy *uuid.UUID, ids ...uuid.UUID) ([]uuid.UUID, error) {
		return repo.ReplaceAttachments(t.Context(), roledom.ReplaceAttachmentsInput{
			PrincipalType: roledom.PrincipalUser, PrincipalID: user, ProjectID: project, RoleIDs: ids, CreatedBy: createdBy,
		})
	}
	// platform-wide: {R1,R2} with created_by = by
	changed, err := replace(nil, &by, r1.ID, r2.ID)
	if err != nil || len(changed) != 2 {
		t.Fatalf("first replace: %v changed=%v", err, changed)
	}
	var cb []string
	if err := db.Select(&cb, `SELECT created_by::text FROM role_attachments WHERE principal_id = $1 AND project_id IS NULL`, user); err != nil || len(cb) != 2 || cb[0] != by.String() || cb[1] != by.String() {
		t.Fatalf("created_by = %v err=%v", cb, err)
	}
	// {R2,R3}: R1 removed, R3 added, R2 untouched (same row, same created_by)
	var r2Row string
	_ = db.Get(&r2Row, `SELECT id::text FROM role_attachments WHERE principal_id = $1 AND role_id = $2`, user, r2.ID)
	other := fx.user(false)
	changed, err = replace(nil, &other, r2.ID, r3.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 2 || !contains(changed, r1.ID) || !contains(changed, r3.ID) {
		t.Fatalf("changed must be exactly the added/removed roles: %v", changed)
	}
	var r2Row2, r2By, r3By string
	_ = db.Get(&r2Row2, `SELECT id::text FROM role_attachments WHERE principal_id = $1 AND role_id = $2`, user, r2.ID)
	_ = db.Get(&r2By, `SELECT created_by::text FROM role_attachments WHERE principal_id = $1 AND role_id = $2`, user, r2.ID)
	_ = db.Get(&r3By, `SELECT created_by::text FROM role_attachments WHERE principal_id = $1 AND role_id = $2`, user, r3.ID)
	if r2Row != r2Row2 || r2By != by.String() || r3By != other.String() {
		t.Fatalf("unchanged attachment must keep its row and creator: row %s->%s by=%s r3by=%s", r2Row, r2Row2, r2By, r3By)
	}
	if attachmentRows(t, db, r1.ID) != 0 {
		t.Fatal("R1 attachment should be gone")
	}
	// the same set again changes nothing
	if changed, err = replace(nil, &by, r2.ID, r3.ID); err != nil || len(changed) != 0 {
		t.Fatalf("idempotent replace: %v %v", err, changed)
	}

	// project scope: independent of the platform set; allows platform + own roles
	if changed, err = replace(&p, &by, r1.ID, own.ID); err != nil || len(changed) != 2 {
		t.Fatalf("project replace: %v %v", err, changed)
	}
	var platformCount, projectCount int
	_ = db.Get(&platformCount, `SELECT COUNT(*) FROM role_attachments WHERE principal_id = $1 AND project_id IS NULL`, user)
	_ = db.Get(&projectCount, `SELECT COUNT(*) FROM role_attachments WHERE principal_id = $1 AND project_id = $2`, user, p)
	if platformCount != 2 || projectCount != 2 {
		t.Fatalf("scopes must be independent: platform=%d project=%d", platformCount, projectCount)
	}
	// replacing the project set to empty leaves the platform set alone
	if _, err = replace(&p, &by); err != nil {
		t.Fatal(err)
	}
	_ = db.Get(&platformCount, `SELECT COUNT(*) FROM role_attachments WHERE principal_id = $1 AND project_id IS NULL`, user)
	if platformCount != 2 {
		t.Fatalf("platform set disturbed: %d", platformCount)
	}

	// rejected inputs change nothing
	if _, err = replace(nil, &by, r2.ID, own.ID); !errors.Is(err, roledom.ErrNotAttachable) {
		t.Fatalf("project-owned role platform-wide: %v", err)
	}
	if _, err = replace(&p, &by, foreign.ID); !errors.Is(err, roledom.ErrNotAttachable) {
		t.Fatalf("another project's role: %v", err)
	}
	if _, err = replace(nil, &by, uuid.New()); !errors.Is(err, roledom.ErrNotAttachable) {
		t.Fatalf("unknown role: %v", err)
	}
	_ = db.Get(&platformCount, `SELECT COUNT(*) FROM role_attachments WHERE principal_id = $1 AND project_id IS NULL`, user)
	if platformCount != 2 {
		t.Fatalf("failed replace must roll back: %d", platformCount)
	}
}

func contains(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func TestRoleRepository_AgentPrincipalAndListAttached(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewRoleRepository(db)
	p := fx.project()
	agent := fx.agent(nil, false)
	r1 := newRole(t, repo, "R1", rolePolicyUsersRead, nil)
	own := newRole(t, repo, "Own", rolePolicyRead, &p)

	if _, err := repo.ReplaceAttachments(t.Context(), roledom.ReplaceAttachmentsInput{
		PrincipalType: roledom.PrincipalAgent, PrincipalID: agent, RoleIDs: []uuid.UUID{r1.ID},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReplaceAttachments(t.Context(), roledom.ReplaceAttachmentsInput{
		PrincipalType: roledom.PrincipalAgent, PrincipalID: agent, ProjectID: &p, RoleIDs: []uuid.UUID{own.ID},
	}); err != nil {
		t.Fatal(err)
	}
	var ptype string
	if err := db.Get(&ptype, `SELECT principal_type FROM role_attachments WHERE role_id = $1`, r1.ID); err != nil || ptype != "agent" {
		t.Fatalf("principal_type = %q err=%v", ptype, err)
	}
	platform, err := repo.ListAttached(t.Context(), roledom.PrincipalAgent, agent, nil)
	if err != nil || len(platform) != 1 || platform[0].ID != r1.ID || platform[0].AttachmentCount != 1 {
		t.Fatalf("platform attached = %+v err=%v", platform, err)
	}
	inProject, err := repo.ListAttached(t.Context(), roledom.PrincipalAgent, agent, &p)
	if err != nil || len(inProject) != 1 || inProject[0].ID != own.ID {
		t.Fatalf("project attached = %+v err=%v", inProject, err)
	}
	// counts: platform listing counts all scopes, project listing only the project
	listed, _ := repo.ListPlatform(t.Context())
	for _, r := range listed {
		if r.ID == r1.ID && r.AttachmentCount != 1 {
			t.Fatalf("platform count = %d", r.AttachmentCount)
		}
	}
	forProject, _ := repo.ListForProject(t.Context(), p)
	for _, r := range forProject {
		if (r.ID == own.ID && r.AttachmentCount != 1) || (r.ID == r1.ID && r.AttachmentCount != 0) {
			t.Fatalf("project-context count for %s = %d", r.Name, r.AttachmentCount)
		}
	}
}

func TestRoleRepository_DeleteCascadesAttachments(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewRoleRepository(db)
	user := fx.user(false)
	r := newRole(t, repo, "R", rolePolicyRead, nil)
	fx.attach(r.ID, "user", user, nil)
	got, _ := repo.FindByID(t.Context(), r.ID, nil)
	if got.AttachmentCount != 1 {
		t.Fatalf("count = %d", got.AttachmentCount)
	}
	if err := repo.Delete(t.Context(), r.ID); err != nil {
		t.Fatal(err)
	}
	if n := attachmentRows(t, db, r.ID); n != 0 {
		t.Fatalf("attachments must cascade, %d left", n)
	}
}

func TestRoleRepository_LastFullAccessHolderInvariant(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewRoleRepository(db)
	admin1, admin2 := fx.user(false), fx.user(false)
	star := newRole(t, repo, "Everything", rolePolicyStar, nil)
	read := newRole(t, repo, "Reader", rolePolicyUsersRead, nil)
	holds := func(u uuid.UUID, ids ...uuid.UUID) error {
		_, err := repo.ReplaceAttachments(t.Context(), roledom.ReplaceAttachmentsInput{PrincipalType: roledom.PrincipalUser, PrincipalID: u, RoleIDs: ids})
		return err
	}

	// A workspace with no holder is not blocked from anything.
	if err := holds(admin1, read.ID); err != nil {
		t.Fatalf("no holder yet: %v", err)
	}
	if err := holds(admin1, star.ID); err != nil {
		t.Fatal(err)
	}

	// admin1 is the only holder.
	if err := holds(admin1, read.ID); !errors.Is(err, roledom.ErrLastWildcard) {
		t.Fatalf("swapping away the last holder's role: %v", err)
	}
	if err := holds(admin1); !errors.Is(err, roledom.ErrLastWildcard) {
		t.Fatalf("clearing the last holder: %v", err)
	}
	if err := repo.Delete(t.Context(), star.ID); !errors.Is(err, roledom.ErrLastWildcard) {
		t.Fatalf("deleting the last full-access role: %v", err)
	}
	star.Policy = json.RawMessage(rolePolicyRead)
	if err := repo.Update(t.Context(), star); !errors.Is(err, roledom.ErrLastWildcard) {
		t.Fatalf("narrowing the last full-access role: %v", err)
	}
	var stillStar bool
	_ = db.Get(&stillStar, `SELECT policy::text LIKE '%"*"%' FROM roles WHERE id = $1`, star.ID)
	if !stillStar || attachmentRows(t, db, star.ID) != 1 {
		t.Fatal("refused changes must roll back")
	}
	// A project-scoped attachment of a full-access role is not platform-wide.
	// Replacing within a project is never blocked.
	proj := fx.project()
	if _, err := repo.ReplaceAttachments(t.Context(), roledom.ReplaceAttachmentsInput{
		PrincipalType: roledom.PrincipalUser, PrincipalID: admin1, ProjectID: &proj, RoleIDs: nil,
	}); err != nil {
		t.Fatalf("project-scoped replace: %v", err)
	}

	// With a second holder every one of those is allowed.
	if err := holds(admin2, star.ID); err != nil {
		t.Fatal(err)
	}
	if err := holds(admin1); err != nil {
		t.Fatalf("a second holder remains: %v", err)
	}
	// ... but then admin2 is the last one.
	if err := holds(admin2); !errors.Is(err, roledom.ErrLastWildcard) {
		t.Fatalf("got %v", err)
	}
}

func TestRoleRepository_FullAccessHolderCounting(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewRoleRepository(db)
	real := fx.user(false)
	deleted := fx.user(true)
	bot := fx.user(false)
	pgExec(t, db, `UPDATE users SET password_hash = '!' WHERE id = $1`, bot)
	cond := newRole(t, repo, "Conditional", `{"statements":[{"effect":"Allow","actions":["*"],"resources":["*"],"conditions":{"StringEquals":{"principal.type":"user"}}}]}`, nil)
	narrow := newRole(t, repo, "NotEverything", `{"statements":[{"effect":"Allow","actions":["*"],"resources":["project/*"]}]}`, nil)
	star := newRole(t, repo, "Everything", rolePolicyStar, nil)
	agent := fx.agent(nil, false)

	count := func() int {
		tx, err := db.BeginTxx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		n, err := wildcardHolders(t.Context(), tx)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	fx.attach(cond.ID, "user", real, nil)
	fx.attach(narrow.ID, "user", real, nil)
	if n := count(); n != 0 {
		t.Fatalf("conditional / non-root-resource roles do not count: %d", n)
	}
	fx.attach(star.ID, "user", deleted, nil)
	fx.attach(star.ID, "user", bot, nil)
	fx.attach(star.ID, "agent", agent, nil)
	pr := fx.project()
	fx.attach(star.ID, "user", real, &pr)
	if n := count(); n != 0 {
		t.Fatalf("deleted users, the bot account, agents and project-scoped attachments do not count: %d", n)
	}
	fx.attach(star.ID, "user", real, nil)
	if n := count(); n != 1 {
		t.Fatalf("a live user with a platform-wide full-access role counts: %d", n)
	}
}

func TestRoleRepository_TargetLookups(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewRoleRepository(db)
	p := fx.project()
	live, dead := fx.user(false), fx.user(true)
	global, projectAgent, deadGlobal := fx.agent(nil, false), fx.agent(&p, false), fx.agent(nil, true)

	for _, tc := range []struct {
		name string
		got  func() (bool, error)
		want bool
	}{
		{"live user", func() (bool, error) { return repo.UserExists(t.Context(), live) }, true},
		{"soft-deleted user", func() (bool, error) { return repo.UserExists(t.Context(), dead) }, false},
		{"unknown user", func() (bool, error) { return repo.UserExists(t.Context(), uuid.New()) }, false},
		{"global agent", func() (bool, error) { return repo.GlobalAgentExists(t.Context(), global) }, true},
		{"project agent is not global", func() (bool, error) { return repo.GlobalAgentExists(t.Context(), projectAgent) }, false},
		{"soft-deleted global agent", func() (bool, error) { return repo.GlobalAgentExists(t.Context(), deadGlobal) }, false},
		{"project", func() (bool, error) { return repo.ProjectExists(t.Context(), p) }, true},
		{"unknown project", func() (bool, error) { return repo.ProjectExists(t.Context(), uuid.New()) }, false},
	} {
		if got, err := tc.got(); err != nil || got != tc.want {
			t.Errorf("%s: got %v err=%v, want %v", tc.name, got, err, tc.want)
		}
	}
	pgExec(t, db, `UPDATE projects SET deleted_at = NOW() WHERE id = $1`, fx.project())
	gone := fx.project()
	pgExec(t, db, `UPDATE projects SET deleted_at = NOW() WHERE id = $1`, gone)
	if ok, _ := repo.ProjectExists(t.Context(), gone); ok {
		t.Error("soft-deleted project must not exist")
	}

	// members
	userMember := fx.member(p, live, false, false)
	agentMember := fx.member(p, projectAgent, true, false)
	deletedMember := fx.member(p, fx.user(false), false, true)
	deadUserMember := fx.member(p, dead, false, false)
	m, err := repo.FindMember(t.Context(), p, userMember)
	if err != nil || m.PrincipalType != roledom.PrincipalUser || m.PrincipalID != live || m.ProjectID != p {
		t.Fatalf("user member = %+v err=%v", m, err)
	}
	m, err = repo.FindMember(t.Context(), p, agentMember)
	if err != nil || m.PrincipalType != roledom.PrincipalAgent || m.PrincipalID != projectAgent {
		t.Fatalf("agent member = %+v err=%v", m, err)
	}
	for name, id := range map[string]uuid.UUID{"soft-deleted member": deletedMember, "member of a deleted user": deadUserMember, "unknown member": uuid.New()} {
		if _, err := repo.FindMember(t.Context(), p, id); !errors.Is(err, roledom.ErrMemberNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// a member of another project is not found through this one
	if _, err := repo.FindMember(t.Context(), fx.project(), userMember); !errors.Is(err, roledom.ErrMemberNotFound) {
		t.Errorf("wrong project: %v", err)
	}
}

func TestRoleRepository_RolePoliciesAndFindByIDs(t *testing.T) {
	db := newIAMPGTestDB(t)
	repo := NewRoleRepository(db)
	a := newRole(t, repo, "A", rolePolicyRead, nil)
	b := newRole(t, repo, "B", rolePolicyStar, nil)

	pols, err := repo.RolePolicies(t.Context(), []uuid.UUID{a.ID, b.ID, uuid.New()})
	if err != nil || len(pols) != 2 {
		t.Fatalf("policies = %v err=%v", pols, err)
	}
	if string(mustPolicyJSON(t, pols[b.ID])) != string(mustPolicyJSON(t, json.RawMessage(rolePolicyStar))) {
		t.Fatalf("policy bytes = %s", pols[b.ID])
	}
	if got, err := repo.RolePolicies(t.Context(), nil); err != nil || len(got) != 0 {
		t.Fatalf("empty: %v %v", got, err)
	}
	roles, err := repo.FindByIDs(t.Context(), []uuid.UUID{a.ID, uuid.New()})
	if err != nil || len(roles) != 1 || roles[0].ID != a.ID {
		t.Fatalf("FindByIDs = %+v err=%v", roles, err)
	}
	if roles, err := repo.FindByIDs(t.Context(), nil); err != nil || len(roles) != 0 {
		t.Fatalf("empty FindByIDs: %v %v", roles, err)
	}
}

func TestRoleRepository_ProjectTemplateIsNeverAttachedPlatformWide(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewRoleRepository(db)
	p := fx.project()
	user := fx.user(false)
	// "project/*" with a full-access action: platform-wide it would reach every project.
	tmpl := newRole(t, repo, "Template", rolePolicyTemplateStar, nil)

	_, err := repo.ReplaceAttachments(t.Context(), roledom.ReplaceAttachmentsInput{
		PrincipalType: roledom.PrincipalUser, PrincipalID: user, RoleIDs: []uuid.UUID{tmpl.ID},
	})
	if !errors.Is(err, roledom.ErrNotAttachable) {
		t.Fatalf("platform-wide attach of a template: %v", err)
	}
	// Per project it is exactly what a template is for.
	if _, err := repo.ReplaceAttachments(t.Context(), roledom.ReplaceAttachmentsInput{
		PrincipalType: roledom.PrincipalUser, PrincipalID: user, ProjectID: &p, RoleIDs: []uuid.UUID{tmpl.ID},
	}); err != nil {
		t.Fatalf("project-scoped attach of a template: %v", err)
	}
}
