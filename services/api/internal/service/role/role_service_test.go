package rolesvc

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	roledom "github.com/Paca-AI/api/internal/domain/role"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// ---------------------------------------------------------------- fakes

type fakeRepo struct {
	roles    map[uuid.UUID]*roledom.Role
	users    map[uuid.UUID]bool
	agents   map[uuid.UUID]bool // global agents
	projects map[uuid.UUID]bool
	members  map[uuid.UUID]*roledom.Member

	createErr, updateErr, deleteErr, replaceErr error
	changed                                     []uuid.UUID
	replaced                                    []roledom.ReplaceAttachmentsInput
	attached                                    []*roledom.Role
	deleted                                     []uuid.UUID
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		roles: map[uuid.UUID]*roledom.Role{}, users: map[uuid.UUID]bool{}, agents: map[uuid.UUID]bool{},
		projects: map[uuid.UUID]bool{}, members: map[uuid.UUID]*roledom.Member{},
	}
}

func (f *fakeRepo) addRole(r *roledom.Role) *roledom.Role {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	f.roles[r.ID] = r
	return r
}

func (f *fakeRepo) ListPlatform(context.Context) ([]*roledom.Role, error) {
	var out []*roledom.Role
	for _, r := range f.roles {
		if r.ProjectID == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRepo) ListForProject(_ context.Context, p uuid.UUID) ([]*roledom.Role, error) {
	var out []*roledom.Role
	for _, r := range f.roles {
		if r.ProjectID == nil || *r.ProjectID == p {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRepo) FindByID(_ context.Context, id uuid.UUID, _ *uuid.UUID) (*roledom.Role, error) {
	r, ok := f.roles[id]
	if !ok {
		return nil, roledom.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (f *fakeRepo) FindByIDs(_ context.Context, ids []uuid.UUID) ([]*roledom.Role, error) {
	var out []*roledom.Role
	for _, id := range ids {
		if r, ok := f.roles[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRepo) Create(_ context.Context, r *roledom.Role) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.addRole(r)
	return nil
}

func (f *fakeRepo) Update(_ context.Context, r *roledom.Role) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	cur := f.roles[r.ID]
	cur.Name, cur.Description, cur.Policy = r.Name, r.Description, r.Policy
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, id uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, id)
	delete(f.roles, id)
	return nil
}

func (f *fakeRepo) SetDefault(_ context.Context, id uuid.UUID) error {
	for _, r := range f.roles {
		r.IsDefault = r.ID == id
	}
	return nil
}

func (f *fakeRepo) UserExists(_ context.Context, id uuid.UUID) (bool, error) { return f.users[id], nil }
func (f *fakeRepo) GlobalAgentExists(_ context.Context, id uuid.UUID) (bool, error) {
	return f.agents[id], nil
}
func (f *fakeRepo) ProjectExists(_ context.Context, id uuid.UUID) (bool, error) {
	return f.projects[id], nil
}

func (f *fakeRepo) FindMember(_ context.Context, project, member uuid.UUID) (*roledom.Member, error) {
	m, ok := f.members[member]
	if !ok || m.ProjectID != project {
		return nil, roledom.ErrMemberNotFound
	}
	return m, nil
}

func (f *fakeRepo) ListAttached(context.Context, string, uuid.UUID, *uuid.UUID) ([]*roledom.Role, error) {
	return f.attached, nil
}

func (f *fakeRepo) ReplaceAttachments(_ context.Context, in roledom.ReplaceAttachmentsInput) ([]uuid.UUID, error) {
	if f.replaceErr != nil {
		return nil, f.replaceErr
	}
	f.replaced = append(f.replaced, in)
	return f.changed, nil
}

func (f *fakeRepo) RolePolicies(context.Context, []uuid.UUID) (map[uuid.UUID][]byte, error) {
	return nil, nil
}

type fakeInv struct{ calls [][]string }

func (f *fakeInv) Invalidate(ids ...string) { f.calls = append(f.calls, ids) }

type fakeSim struct {
	gotPrincipal *iam.Principal
	gotPolicy    *iam.Policy
	res          iam.Result
	err          error
}

func (f *fakeSim) Simulate(_ context.Context, p *iam.Principal, pol *iam.Policy, _, _ string, _ map[string][]string) (iam.Result, error) {
	f.gotPrincipal, f.gotPolicy = p, pol
	return f.res, f.err
}

type fixture struct {
	svc  *Service
	repo *fakeRepo
	inv  *fakeInv
	sim  *fakeSim
}

func newFixture() *fixture {
	repo, inv, sim := newFakeRepo(), &fakeInv{}, &fakeSim{}
	return &fixture{svc: New(repo, inv, sim, iam.NewRegistry(), iam.NewAttributeSchema()), repo: repo, inv: inv, sim: sim}
}

const validPolicy = `{"version":"2026-10-01","statements":[{"sid":"R","effect":"Allow","actions":["tasks:read"],"resources":["project/*"]}]}`

func in(name, policy string) roledom.RoleInput {
	return roledom.RoleInput{Name: name, Description: " d ", Policy: json.RawMessage(policy)}
}

func issuesOf(t *testing.T, err error) []apierr.Issue {
	t.Helper()
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Code != apierr.CodeRolePolicyInvalid {
		t.Fatalf("want ROLE_POLICY_INVALID apierr, got %v", err)
	}
	return ae.Issues
}

// ---------------------------------------------------------------- roles

func TestCreate_NameValidation(t *testing.T) {
	f := newFixture()
	for _, name := range []string{"", "   ", strings.Repeat("x", 101)} {
		if _, err := f.svc.Create(t.Context(), nil, in(name, validPolicy)); !errors.Is(err, roledom.ErrNameInvalid) {
			t.Errorf("name %q: got %v, want ErrNameInvalid", name, err)
		}
	}
	r, err := f.svc.Create(t.Context(), nil, in("  Dev  ", validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "Dev" || r.Description != "d" || r.ProjectID != nil {
		t.Fatalf("name/description must be trimmed, scope platform: %+v", r)
	}
}

func TestCreate_PolicyIssues(t *testing.T) {
	f := newFixture()
	tests := []struct {
		name, policy, wantPath, wantMsg string
	}{
		{"missing", ``, "policy", "required"},
		{"null", `null`, "policy", "required"},
		{"not json", `{`, "policy", "parse policy"},
		{"unknown field", `{"statements":[],"extra":1}`, "policy", "unknown field"},
		{"bad effect", `{"statements":[{"effect":"Maybe","actions":["tasks:read"],"resources":["*"]}]}`, "policy", "effect"},
		{"unknown action", `{"statements":[{"effect":"Allow","actions":["tasks:read","nope:x"],"resources":["*"]}]}`, "statements[0].actions[1]", "unknown action"},
		{"bad resource", `{"statements":[{"effect":"Allow","actions":["tasks:read"],"resources":["bogus/1"]}]}`, "statements[0].resources[0]", "unknown root"},
		{"unknown condition key", `{"statements":[{"effect":"Allow","actions":["tasks:read"],"resources":["project/*/task/*"],"conditions":{"StringEquals":{"task.nope":"x"}}}]}`,
			"statements[0].conditions.StringEquals.task.nope", "unknown condition key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.svc.Create(t.Context(), nil, in("R-"+tc.name, tc.policy))
			issues := issuesOf(t, err)
			if len(issues) == 0 || issues[0].Path != tc.wantPath || !strings.Contains(issues[0].Message, tc.wantMsg) {
				t.Fatalf("issues = %+v, want first {%s ... %s}", issues, tc.wantPath, tc.wantMsg)
			}
		})
	}
	if len(f.repo.roles) != 0 {
		t.Fatal("nothing may be stored for an invalid policy")
	}
}

func TestCreate_StoresCanonicalPolicy(t *testing.T) {
	f := newFixture()
	r, err := f.svc.Create(t.Context(), nil, in("Dev", validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	p, err := iam.ParsePolicy(r.Policy)
	if err != nil || len(p.Statements) != 1 || p.Statements[0].Sid != "R" {
		t.Fatalf("stored policy must round-trip: %s err=%v", r.Policy, err)
	}
}

// projectPolicy is a valid policy for a role owned by project p.
func projectPolicy(p uuid.UUID) string {
	return `{"version":"2026-10-01","statements":[{"sid":"R","effect":"Allow","actions":["tasks:read"],"resources":["project/` + p.String() + `/*"]}]}`
}

func TestCreate_ProjectScope(t *testing.T) {
	f := newFixture()
	p := uuid.New()
	if _, err := f.svc.Create(t.Context(), &p, in("Dev", projectPolicy(p))); !errors.Is(err, roledom.ErrProjectNotFound) {
		t.Fatalf("unknown project: got %v", err)
	}
	f.repo.projects[p] = true
	r, err := f.svc.Create(t.Context(), &p, in("Dev", projectPolicy(p)))
	if err != nil || r.ProjectID == nil || *r.ProjectID != p {
		t.Fatalf("project role: %+v err=%v", r, err)
	}
}

func TestCreate_DuplicateNameIsConflict(t *testing.T) {
	f := newFixture()
	f.repo.createErr = roledom.ErrNameTaken
	if _, err := f.svc.Create(t.Context(), nil, in("Dev", validPolicy)); !errors.Is(err, roledom.ErrNameTaken) {
		t.Fatalf("got %v", err)
	}
}

func TestUpdate(t *testing.T) {
	f := newFixture()
	p := uuid.New()
	plat := f.repo.addRole(&roledom.Role{Name: "A", Policy: json.RawMessage(validPolicy)})
	proj := f.repo.addRole(&roledom.Role{Name: "B", Policy: json.RawMessage(validPolicy), ProjectID: &p})
	sys := f.repo.addRole(&roledom.Role{Name: "S", Policy: json.RawMessage(validPolicy), IsSystem: true})

	// scope mismatch hides the role
	if _, err := f.svc.Update(t.Context(), nil, proj.ID, in("X", validPolicy)); !errors.Is(err, roledom.ErrNotFound) {
		t.Errorf("project role via platform scope: %v", err)
	}
	other := uuid.New()
	if _, err := f.svc.Update(t.Context(), &other, proj.ID, in("X", validPolicy)); !errors.Is(err, roledom.ErrNotFound) {
		t.Errorf("project role via another project: %v", err)
	}
	if _, err := f.svc.Update(t.Context(), &p, plat.ID, in("X", validPolicy)); !errors.Is(err, roledom.ErrNotFound) {
		t.Errorf("platform role via a project scope: %v", err)
	}
	if len(f.inv.calls) != 0 {
		t.Fatalf("failed updates must not invalidate: %v", f.inv.calls)
	}
	// validation failures do not invalidate either
	if _, err := f.svc.Update(t.Context(), nil, plat.ID, in(" ", validPolicy)); !errors.Is(err, roledom.ErrNameInvalid) {
		t.Errorf("empty name: %v", err)
	}
	if len(f.inv.calls) != 0 {
		t.Fatalf("invalid input must not invalidate: %v", f.inv.calls)
	}
	// system roles can be edited too (they only cannot be deleted)
	if _, err := f.svc.Update(t.Context(), nil, sys.ID, in("S2", validPolicy)); err != nil {
		t.Errorf("system role: %v", err)
	}
	f.inv.calls = nil
	// success invalidates exactly this role
	if _, err := f.svc.Update(t.Context(), nil, plat.ID, in("A2", validPolicy)); err != nil {
		t.Fatal(err)
	}
	if len(f.inv.calls) != 1 || !slices.Equal(f.inv.calls[0], []string{plat.ID.String()}) {
		t.Fatalf("invalidate calls = %v", f.inv.calls)
	}
	// repo-level refusal (last full-access holder) propagates, no invalidation
	f.repo.updateErr = roledom.ErrLastWildcard
	if _, err := f.svc.Update(t.Context(), nil, plat.ID, in("A3", validPolicy)); !errors.Is(err, roledom.ErrLastWildcard) {
		t.Fatalf("got %v", err)
	}
	if len(f.inv.calls) != 1 {
		t.Fatalf("refused update must not invalidate: %v", f.inv.calls)
	}
}

func TestDelete(t *testing.T) {
	f := newFixture()
	p := uuid.New()
	plain := f.repo.addRole(&roledom.Role{Name: "A"})
	def := f.repo.addRole(&roledom.Role{Name: "D", IsDefault: true})
	sys := f.repo.addRole(&roledom.Role{Name: "S", IsSystem: true})
	proj := f.repo.addRole(&roledom.Role{Name: "P", ProjectID: &p})

	if err := f.svc.Delete(t.Context(), nil, sys.ID); !errors.Is(err, roledom.ErrSystemRole) {
		t.Errorf("system: %v", err)
	}
	if err := f.svc.Delete(t.Context(), nil, def.ID); !errors.Is(err, roledom.ErrIsDefault) {
		t.Errorf("default: %v", err)
	}
	if err := f.svc.Delete(t.Context(), nil, proj.ID); !errors.Is(err, roledom.ErrNotFound) {
		t.Errorf("project role via platform scope: %v", err)
	}
	if err := f.svc.Delete(t.Context(), nil, uuid.New()); !errors.Is(err, roledom.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	if len(f.repo.deleted) != 0 || len(f.inv.calls) != 0 {
		t.Fatalf("refused deletes must not reach the repo or invalidate")
	}
	f.repo.deleteErr = roledom.ErrLastWildcard
	if err := f.svc.Delete(t.Context(), nil, plain.ID); !errors.Is(err, roledom.ErrLastWildcard) {
		t.Errorf("last wildcard: %v", err)
	}
	if len(f.inv.calls) != 0 {
		t.Fatalf("refused delete must not invalidate: %v", f.inv.calls)
	}
	f.repo.deleteErr = nil
	if err := f.svc.Delete(t.Context(), &p, proj.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.inv.calls) != 1 || f.inv.calls[0][0] != proj.ID.String() {
		t.Fatalf("delete must invalidate the role: %v", f.inv.calls)
	}
}

func TestSetDefault(t *testing.T) {
	f := newFixture()
	p := uuid.New()
	a := f.repo.addRole(&roledom.Role{Name: "A", IsDefault: true})
	b := f.repo.addRole(&roledom.Role{Name: "B"})
	proj := f.repo.addRole(&roledom.Role{Name: "P", ProjectID: &p})
	if _, err := f.svc.SetDefault(t.Context(), proj.ID); !errors.Is(err, roledom.ErrNotFound) {
		t.Fatalf("project role cannot be the default: %v", err)
	}
	got, err := f.svc.SetDefault(t.Context(), b.ID)
	if err != nil || !got.IsDefault || f.repo.roles[a.ID].IsDefault {
		t.Fatalf("default swap: %+v err=%v old=%v", got, err, f.repo.roles[a.ID].IsDefault)
	}
	if _, err := f.svc.SetDefault(t.Context(), uuid.New()); !errors.Is(err, roledom.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestGet_Scope(t *testing.T) {
	f := newFixture()
	p, q := uuid.New(), uuid.New()
	plat := f.repo.addRole(&roledom.Role{Name: "A"})
	proj := f.repo.addRole(&roledom.Role{Name: "B", ProjectID: &p})
	if _, err := f.svc.Get(t.Context(), nil, plat.ID); err != nil {
		t.Errorf("platform role via platform scope: %v", err)
	}
	if _, err := f.svc.Get(t.Context(), &p, plat.ID); err != nil {
		t.Errorf("platform role is readable in a project scope: %v", err)
	}
	if _, err := f.svc.Get(t.Context(), &p, proj.ID); err != nil {
		t.Errorf("own project role: %v", err)
	}
	if _, err := f.svc.Get(t.Context(), nil, proj.ID); !errors.Is(err, roledom.ErrNotFound) {
		t.Errorf("project role via platform scope: %v", err)
	}
	if _, err := f.svc.Get(t.Context(), &q, proj.ID); !errors.Is(err, roledom.ErrNotFound) {
		t.Errorf("project role via another project: %v", err)
	}
}

func TestListForProject_UnknownProject(t *testing.T) {
	f := newFixture()
	if _, err := f.svc.ListForProject(t.Context(), uuid.New()); !errors.Is(err, roledom.ErrProjectNotFound) {
		t.Fatalf("got %v", err)
	}
}

// ---------------------------------------------------------------- attachments

func TestReplaceUserRoles(t *testing.T) {
	f := newFixture()
	u, p, by := uuid.New(), uuid.New(), uuid.New()
	plat := f.repo.addRole(&roledom.Role{Name: "A"})
	plat2 := f.repo.addRole(&roledom.Role{Name: "A2"})
	proj := f.repo.addRole(&roledom.Role{Name: "P", ProjectID: &p})

	if _, err := f.svc.ReplaceUserRoles(t.Context(), u, []uuid.UUID{plat.ID}, &by); !errors.Is(err, roledom.ErrUserNotFound) {
		t.Fatalf("unknown/deleted user: %v", err)
	}
	f.repo.users[u] = true
	if _, err := f.svc.ReplaceUserRoles(t.Context(), u, []uuid.UUID{plat.ID, uuid.New()}, &by); !errors.Is(err, roledom.ErrNotAttachable) {
		t.Fatalf("unknown role: %v", err)
	}
	if _, err := f.svc.ReplaceUserRoles(t.Context(), u, []uuid.UUID{proj.ID}, &by); !errors.Is(err, roledom.ErrNotAttachable) {
		t.Fatalf("project role platform-wide: %v", err)
	}
	if len(f.repo.replaced) != 0 || len(f.inv.calls) != 0 {
		t.Fatal("rejected input must not reach the repo")
	}
	f.repo.changed = []uuid.UUID{plat.ID, plat2.ID}
	if _, err := f.svc.ReplaceUserRoles(t.Context(), u, []uuid.UUID{plat.ID, plat2.ID, plat.ID}, &by); err != nil {
		t.Fatal(err)
	}
	got := f.repo.replaced[0]
	if got.PrincipalType != roledom.PrincipalUser || got.PrincipalID != u || got.ProjectID != nil ||
		len(got.RoleIDs) != 2 || got.CreatedBy == nil || *got.CreatedBy != by {
		t.Fatalf("replace input = %+v", got)
	}
	if len(f.inv.calls) != 1 || !slices.Equal(f.inv.calls[0], []string{plat.ID.String(), plat2.ID.String()}) {
		t.Fatalf("invalidate calls = %v", f.inv.calls)
	}
	// An empty set is valid and clears the user's roles.
	if _, err := f.svc.ReplaceUserRoles(t.Context(), u, nil, &by); err != nil {
		t.Fatalf("empty set: %v", err)
	}
	// The last-wildcard invariant surfaces as a conflict and invalidates nothing.
	f.inv.calls = nil
	f.repo.replaceErr = roledom.ErrLastWildcard
	if _, err := f.svc.ReplaceUserRoles(t.Context(), u, nil, &by); !errors.Is(err, roledom.ErrLastWildcard) {
		t.Fatalf("got %v", err)
	}
	if len(f.inv.calls) != 0 {
		t.Fatalf("refused replace must not invalidate: %v", f.inv.calls)
	}
}

func TestReplaceAgentRoles(t *testing.T) {
	f := newFixture()
	a, p := uuid.New(), uuid.New()
	plat := f.repo.addRole(&roledom.Role{Name: "A"})
	proj := f.repo.addRole(&roledom.Role{Name: "P", ProjectID: &p})
	if _, err := f.svc.ReplaceAgentRoles(t.Context(), a, nil, nil); !errors.Is(err, roledom.ErrAgentNotFound) {
		t.Fatalf("non-global/unknown agent: %v", err)
	}
	f.repo.agents[a] = true
	if _, err := f.svc.ReplaceAgentRoles(t.Context(), a, []uuid.UUID{proj.ID}, nil); !errors.Is(err, roledom.ErrNotAttachable) {
		t.Fatalf("project role platform-wide: %v", err)
	}
	if _, err := f.svc.ReplaceAgentRoles(t.Context(), a, []uuid.UUID{plat.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.repo.replaced[0]; got.PrincipalType != roledom.PrincipalAgent || got.PrincipalID != a || got.ProjectID != nil {
		t.Fatalf("replace input = %+v", got)
	}
}

func TestReplaceMemberRoles(t *testing.T) {
	f := newFixture()
	p, q, mUser, mAgent, agent, user := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	plat := f.repo.addRole(&roledom.Role{Name: "A"})
	own := f.repo.addRole(&roledom.Role{Name: "O", ProjectID: &p})
	foreign := f.repo.addRole(&roledom.Role{Name: "F", ProjectID: &q})

	if _, err := f.svc.ReplaceMemberRoles(t.Context(), p, mUser, nil, nil); !errors.Is(err, roledom.ErrProjectNotFound) {
		t.Fatalf("unknown project: %v", err)
	}
	f.repo.projects[p] = true
	if _, err := f.svc.ReplaceMemberRoles(t.Context(), p, mUser, nil, nil); !errors.Is(err, roledom.ErrMemberNotFound) {
		t.Fatalf("unknown member: %v", err)
	}
	f.repo.members[mUser] = &roledom.Member{ID: mUser, ProjectID: p, PrincipalType: roledom.PrincipalUser, PrincipalID: user}
	f.repo.members[mAgent] = &roledom.Member{ID: mAgent, ProjectID: p, PrincipalType: roledom.PrincipalAgent, PrincipalID: agent}

	if _, err := f.svc.ReplaceMemberRoles(t.Context(), p, mUser, []uuid.UUID{foreign.ID}, nil); !errors.Is(err, roledom.ErrNotAttachable) {
		t.Fatalf("another project's role: %v", err)
	}
	if _, err := f.svc.ReplaceMemberRoles(t.Context(), p, mUser, []uuid.UUID{plat.ID, own.ID}, nil); err != nil {
		t.Fatalf("platform + own project roles: %v", err)
	}
	if got := f.repo.replaced[0]; got.PrincipalType != roledom.PrincipalUser || got.PrincipalID != user || got.ProjectID == nil || *got.ProjectID != p {
		t.Fatalf("user member input = %+v", got)
	}
	if _, err := f.svc.ReplaceMemberRoles(t.Context(), p, mAgent, []uuid.UUID{own.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.repo.replaced[1]; got.PrincipalType != roledom.PrincipalAgent || got.PrincipalID != agent {
		t.Fatalf("agent member must be attached as an agent principal: %+v", got)
	}
	// A member of another project is not found through this project.
	if _, err := f.svc.ReplaceMemberRoles(t.Context(), q, mUser, nil, nil); !errors.Is(err, roledom.ErrProjectNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestListAttached_ChecksTargets(t *testing.T) {
	f := newFixture()
	if _, err := f.svc.ListUserRoles(t.Context(), uuid.New()); !errors.Is(err, roledom.ErrUserNotFound) {
		t.Errorf("user: %v", err)
	}
	if _, err := f.svc.ListAgentRoles(t.Context(), uuid.New()); !errors.Is(err, roledom.ErrAgentNotFound) {
		t.Errorf("agent: %v", err)
	}
	if _, err := f.svc.ListMemberRoles(t.Context(), uuid.New(), uuid.New()); !errors.Is(err, roledom.ErrProjectNotFound) {
		t.Errorf("member: %v", err)
	}
}

// ---------------------------------------------------------------- catalogue

func TestActionsAndAttributeDefs(t *testing.T) {
	f := newFixture()
	acts := f.svc.Actions()
	if !slices.IsSorted(acts) || !slices.Contains(acts, "tasks:read") {
		t.Fatalf("actions = %v", acts)
	}
	if err := f.svc.reg.Register("plugin.x:run"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.svc.Actions(), "plugin.x:run") {
		t.Fatal("plugin-registered actions must be listed")
	}
	defs := f.svc.AttributeDefs()
	var found bool
	for _, d := range defs {
		if d.Key == "doc.ancestor_folder_ids" {
			found = d.MultiValued && d.ResourceKind == "doc" && d.Type == "string" && d.LabelKey != ""
		}
	}
	if !found {
		t.Fatalf("attribute defs = %+v", defs)
	}
}

func TestValidatePolicy(t *testing.T) {
	f := newFixture()
	if got := f.svc.ValidatePolicy(json.RawMessage(validPolicy), nil); len(got) != 0 {
		t.Fatalf("valid policy has issues: %+v", got)
	}
	got := f.svc.ValidatePolicy(json.RawMessage(`{"statements":[{"effect":"Allow","actions":["x:y"],"resources":["*"]},{"effect":"Allow","actions":[],"resources":[]}]}`), nil)
	want := []roledom.Issue{
		{Path: "statements[0].actions[0]", Message: `unknown action "x:y"`},
		{Path: "statements[1].actions", Message: "at least one action is required"},
		{Path: "statements[1].resources", Message: "at least one resource is required"},
	}
	if len(got) != len(want) {
		t.Fatalf("issues = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("issue %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got := f.svc.ValidatePolicy(nil, nil); len(got) != 1 || got[0].Path != "policy" {
		t.Fatalf("missing policy: %+v", got)
	}
}

func TestSimulate(t *testing.T) {
	f := newFixture()
	uid := uuid.New()
	f.sim.res = iam.Result{Allowed: true, Matched: []iam.MatchedStatement{{RoleID: iam.PolicyMatchID, Sid: "R", Effect: iam.EffectAllow, Index: 0}}}
	res, err := f.svc.Simulate(t.Context(), roledom.SimulateInput{
		Policy: json.RawMessage(validPolicy), Action: "tasks:read", Resource: "project/p/task/t",
		Principal: &roledom.PrincipalRef{Type: "user", ID: strings.ToUpper(uid.String())},
	})
	if err != nil || !res.Allowed || len(res.Matched) != 1 || res.Matched[0] != (roledom.Matched{RoleID: "policy", Sid: "R", Effect: "Allow", Index: 0}) {
		t.Fatalf("res = %+v err=%v", res, err)
	}
	if f.sim.gotPrincipal == nil || f.sim.gotPrincipal.ID != uid.String() || f.sim.gotPrincipal.Type != "user" {
		t.Fatalf("principal must be canonicalised: %+v", f.sim.gotPrincipal)
	}
	// without a principal only the policy is simulated
	if _, err := f.svc.Simulate(t.Context(), roledom.SimulateInput{Policy: json.RawMessage(validPolicy), Action: "a:b", Resource: "*"}); err != nil || f.sim.gotPrincipal != nil {
		t.Fatalf("no principal: %v %+v", err, f.sim.gotPrincipal)
	}
	// invalid policy: 422 with issues
	_, err = f.svc.Simulate(t.Context(), roledom.SimulateInput{Policy: json.RawMessage(`{"statements":[{"effect":"Allow","actions":["x:y"],"resources":["*"]}]}`), Action: "a:b", Resource: "*"})
	if iss := issuesOf(t, err); len(iss) != 1 {
		t.Fatalf("issues = %+v", iss)
	}
	// bad requests
	for _, bad := range []roledom.SimulateInput{
		{Policy: json.RawMessage(validPolicy), Resource: "*"},
		{Policy: json.RawMessage(validPolicy), Action: "a:b"},
		{Policy: json.RawMessage(validPolicy), Action: "a:b", Resource: "*", Principal: &roledom.PrincipalRef{Type: "robot", ID: uid.String()}},
		{Policy: json.RawMessage(validPolicy), Action: "a:b", Resource: "*", Principal: &roledom.PrincipalRef{Type: "user", ID: "nope"}},
	} {
		var ae *apierr.Error
		if _, err := f.svc.Simulate(t.Context(), bad); !errors.As(err, &ae) || ae.Code != apierr.CodeBadRequest {
			t.Errorf("input %+v: got %v, want BAD_REQUEST", bad, err)
		}
	}
	// simulator failure is returned
	f.sim.err = errors.New("db")
	if _, err := f.svc.Simulate(t.Context(), roledom.SimulateInput{Policy: json.RawMessage(validPolicy), Action: "a:b", Resource: "*"}); err == nil {
		t.Fatal("want error")
	}
}

// A role that names only project resources ("project/*") is a project template.
// Attached platform-wide, or made the default, it would reach every project.
func TestProjectTemplateCannotBeAttachedPlatformWideOrDefault(t *testing.T) {
	f := newFixture()
	u, by := uuid.New(), uuid.New()
	f.repo.users[u] = true
	tmpl := f.repo.addRole(&roledom.Role{
		Name:   "Template",
		Policy: json.RawMessage(`{"statements":[{"effect":"Allow","actions":["*"],"resources":["project/*"]}]}`),
	})

	if _, err := f.svc.ReplaceUserRoles(t.Context(), u, []uuid.UUID{tmpl.ID}, &by); !errors.Is(err, roledom.ErrNotAttachable) {
		t.Fatalf("template attached platform-wide: %v", err)
	}
	if _, err := f.svc.SetDefault(t.Context(), tmpl.ID); !errors.Is(err, roledom.ErrNotAttachable) {
		t.Fatalf("template made the default: %v", err)
	}
	if len(f.repo.replaced) != 0 {
		t.Fatal("rejected input must not reach the repo")
	}
}

// A role owned by a project is attached only inside it, so every resource of
// its policy must lie inside that project; a workspace role may name any.
func TestProjectRoleResourcesMustBeInsideItsProject(t *testing.T) {
	f := newFixture()
	p, other := uuid.New(), uuid.New()
	f.repo.projects[p] = true
	pol := func(resource string) string {
		return `{"statements":[{"effect":"Allow","actions":["tasks:read"],"resources":["` + resource + `"]}]}`
	}
	bad := []string{
		"*",
		"project/*",
		"project/*/task/*",
		"project/" + other.String() + "/*",
		"project/" + p.String() + "x/*", // a longer id is another project
		"user/*",
	}
	for _, res := range bad {
		if _, err := f.svc.Create(t.Context(), &p, in("Dev", pol(res))); err == nil {
			t.Errorf("project role with resource %q must be rejected", res)
		}
		if issues := f.svc.ValidatePolicy(json.RawMessage(pol(res)), &p); len(issues) == 0 {
			t.Errorf("validate: resource %q must be reported for a project role", res)
		}
	}
	for _, res := range []string{
		"project/" + p.String(),
		"project/" + p.String() + "/*",
		"project/" + p.String() + "/task/*",
	} {
		if _, err := f.svc.Create(t.Context(), &p, in("R"+res[len(res)-3:], pol(res))); err != nil {
			t.Errorf("project role with resource %q: %v", res, err)
		}
	}
	// A workspace role is free to name any of them.
	if _, err := f.svc.Create(t.Context(), nil, in("Wide", pol("project/*"))); err != nil {
		t.Errorf("workspace role: %v", err)
	}
	if issues := f.svc.ValidatePolicy(json.RawMessage(pol("project/*")), nil); len(issues) != 0 {
		t.Errorf("workspace validate: %v", issues)
	}
}
