package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/bootstrap/defaultroles"
	roledom "github.com/Paca-AI/api/internal/domain/role"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// fakeAttachments is a RoleAttachmentLookup with one fixed answer per kind.
type fakeAttachments struct {
	held  []uuid.UUID
	err   error
	calls int
}

func (f *fakeAttachments) UserRoleIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	f.calls++
	return f.held, f.err
}

func (f *fakeAttachments) AgentRoleIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	return f.UserRoleIDs(ctx, id)
}

func (f *fakeAttachments) MemberRoleIDs(ctx context.Context, _, id uuid.UUID) ([]uuid.UUID, error) {
	return f.UserRoleIDs(ctx, id)
}

func idsBody(ids ...uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = `"` + id.String() + `"`
	}
	return `{"role_ids":[` + strings.Join(parts, ",") + `]}`
}

func assignGrant(resources ...string) []iam.Grant {
	quoted := make([]string, len(resources))
	copy(quoted, resources)
	return []iam.Grant{platformGrant([]string{"roles:assign"}, quoted)}
}

func TestRequireAssignRoles_PlatformScope(t *testing.T) {
	target := uuid.New()
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	star := assignGrant("*")
	onlyA := assignGrant("role/" + a.String())

	cases := []struct {
		name     string
		grants   []iam.Grant
		held     []uuid.UUID
		lookup   *fakeAttachments
		storeErr error
		body     string
		want     int
	}{
		{"roles:assign on * assigns any role", star, nil, nil, nil, idsBody(a, b), http.StatusNoContent},
		{"scoped to A: A is allowed", onlyA, nil, nil, nil, idsBody(a), http.StatusNoContent},
		{"scoped to A: B is refused", onlyA, nil, nil, nil, idsBody(b), http.StatusForbidden},
		{"scoped to A: A together with B is refused", onlyA, nil, nil, nil, idsBody(a, b), http.StatusForbidden},
		{"role/* covers every role", assignGrant("role/*"), nil, nil, nil, idsBody(a, b, c), http.StatusNoContent},
		{"the project form of the resource is not the platform form", assignGrant("project/" + uuid.NewString() + "/role/*"), nil, nil, nil, idsBody(a), http.StatusForbidden},
		{"another action on * is not enough", []iam.Grant{platformGrant([]string{"roles:write", "users:write"}, []string{"*"})}, nil, nil, nil, idsBody(a), http.StatusForbidden},
		{"caller with nothing is refused", nil, nil, nil, nil, idsBody(a), http.StatusForbidden},
		{"holding the role's permissions is irrelevant", []iam.Grant{platformGrant([]string{"users:write"}, []string{"user/*"})}, nil, nil, nil, idsBody(a), http.StatusForbidden},
		{"unchanged roles need no permission", nil, []uuid.UUID{a, b}, nil, nil, idsBody(b, a), http.StatusNoContent},
		{"only the added role is checked", onlyA, []uuid.UUID{b}, nil, nil, idsBody(a, b), http.StatusNoContent},
		{"removing a role needs permission for it", onlyA, []uuid.UUID{a, b}, nil, nil, idsBody(a), http.StatusForbidden},
		{"removing a role with permission for it", onlyA, []uuid.UUID{a}, nil, nil, idsBody(), http.StatusNoContent},
		{"empty role_ids that detaches needs permission", nil, []uuid.UUID{a}, nil, nil, `{"role_ids":[]}`, http.StatusForbidden},
		{"absent role_ids is a detach-all for the handler, so it is judged the same", nil, []uuid.UUID{a}, nil, nil, `{}`, http.StatusForbidden},
		{"null role_ids likewise", nil, []uuid.UUID{a}, nil, nil, `{"role_ids":null}`, http.StatusForbidden},
		{"empty set on a target with nothing is a no-op", nil, nil, nil, nil, `{"role_ids":[]}`, http.StatusNoContent},
		{"duplicates in the request count once", onlyA, nil, nil, nil, idsBody(a, a), http.StatusNoContent},
		{"unknown ids are checked like any other (service answers 422 afterwards)", star, nil, nil, nil, idsBody(uuid.New()), http.StatusNoContent},
		{"unknown id still needs permission", nil, nil, nil, nil, idsBody(uuid.New()), http.StatusForbidden},
		{"invalid uuid passes (handler 400)", nil, nil, nil, nil, `{"role_ids":["nope"]}`, http.StatusNoContent},
		{"not JSON passes (handler 400)", nil, nil, nil, nil, `{`, http.StatusNoContent},
		{"empty body passes (handler 400)", nil, nil, nil, nil, ``, http.StatusNoContent},
		{"first JSON value is what counts", onlyA, nil, nil, nil, idsBody(b) + idsBody(a), http.StatusForbidden},
		{"case-insensitive key like the handler", onlyA, nil, nil, nil, `{"Role_IDs":["` + b.String() + `"]}`, http.StatusForbidden},
		{"lookup failure is 500", star, nil, &fakeAttachments{err: errors.New("db")}, nil, idsBody(a), http.StatusInternalServerError},
		{"authorizer failure is 500", star, nil, nil, errors.New("db"), idsBody(a), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userID := uuid.New()
			store := &fakeIAMStore{grants: map[iam.Principal][]iam.Grant{{Type: "user", ID: userID.String()}: tc.grants}, err: tc.storeErr}
			lookup := tc.lookup
			if lookup == nil {
				lookup = &fakeAttachments{held: tc.held}
			}
			gate := RequireAssignRoles(newFakeIAM(store), UserRolesTarget(lookup, "userId"))
			rec, seen := serveChat(t, "/users/{userId}/roles", "/users/"+target.String()+"/roles", tc.body, asCaller(userID, uuid.Nil), gate)
			if rec.Code != tc.want {
				t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body.String(), tc.want)
			}
			if tc.want == http.StatusNoContent && seen != tc.body {
				t.Fatalf("body must be restored: %q != %q", seen, tc.body)
			}
		})
	}
}

func TestRequireAssignRoles_FailsClosedWhenNotWired(t *testing.T) {
	userID := uuid.New()
	a := uuid.New()
	store := &fakeIAMStore{grants: map[iam.Principal][]iam.Grant{{Type: "user", ID: userID.String()}: assignGrant("*")}}
	// no lookup
	rec, _ := serveChat(t, "/u/{userId}", "/u/"+uuid.NewString(), idsBody(a), asCaller(userID, uuid.Nil),
		RequireAssignRoles(newFakeIAM(store), UserRolesTarget(nil, "userId")))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("no lookup: %d", rec.Code)
	}
	// no authorizer
	rec, _ = serveChat(t, "/u/{userId}", "/u/"+uuid.NewString(), idsBody(a), asCaller(userID, uuid.Nil),
		RequireAssignRoles(nil, UserRolesTarget(&fakeAttachments{}, "userId")))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("no authorizer: %d", rec.Code)
	}
	// unauthenticated
	rec, _ = serveChat(t, "/u/{userId}", "/u/"+uuid.NewString(), idsBody(a),
		RequireAssignRoles(newFakeIAM(store), UserRolesTarget(&fakeAttachments{}, "userId")))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d", rec.Code)
	}
	// malformed target id
	rec, _ = serveChat(t, "/u/{userId}", "/u/nope", idsBody(a), asCaller(userID, uuid.Nil),
		RequireAssignRoles(newFakeIAM(store), UserRolesTarget(&fakeAttachments{}, "userId")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad target id: %d", rec.Code)
	}
	// oversized body
	big := `{"role_ids":[],"pad":"` + strings.Repeat("x", maxPeekBody) + `"}`
	rec, _ = serveChat(t, "/u/{userId}", "/u/"+uuid.NewString(), big, asCaller(userID, uuid.Nil),
		RequireAssignRoles(newFakeIAM(store), UserRolesTarget(&fakeAttachments{}, "userId")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized body: %d", rec.Code)
	}
}

func TestRequireAssignRoles_AgentCallerIsJudgedAsTheAgent(t *testing.T) {
	userID, agentID := uuid.New(), uuid.New()
	a := uuid.New()
	store := &fakeIAMStore{grants: map[iam.Principal][]iam.Grant{
		{Type: "user", ID: userID.String()}: assignGrant("*"), // the bot user behind the key
	}}
	gate := RequireAssignRoles(newFakeIAM(store), UserRolesTarget(&fakeAttachments{}, "userId"))
	run := func() int {
		rec, _ := serveChat(t, "/u/{userId}", "/u/"+uuid.NewString(), idsBody(a), asCaller(userID, agentID), gate)
		return rec.Code
	}
	if got := run(); got != http.StatusForbidden {
		t.Fatalf("agent must not borrow the key user's grants: %d", got)
	}
	store.grants[iam.Principal{Type: "agent", ID: agentID.String()}] = assignGrant("role/" + a.String())
	if got := run(); got != http.StatusNoContent {
		t.Fatalf("agent holding its own roles:assign: %d", got)
	}
}

func serveProjectAssignGate(t *testing.T, project uuid.UUID, caller []iam.Grant, target func(string) AssignTarget, body string) int {
	t.Helper()
	userID := uuid.New()
	store := &fakeIAMStore{grants: map[iam.Principal][]iam.Grant{{Type: "user", ID: userID.String()}: caller}}
	gate := RequireAssignRoles(newFakeIAM(store), target("projectId"))
	rec, _ := serveChat(t, "/projects/{projectId}/members/{memberId}/roles", "/projects/"+project.String()+"/members/"+uuid.NewString()+"/roles", body, asCaller(userID, uuid.Nil), gate)
	return rec.Code
}

func TestRequireAssignRoles_ProjectScope(t *testing.T) {
	p, q := uuid.New(), uuid.New()
	a, b := uuid.New(), uuid.New()
	pj := p.String()
	projectAdmin := []iam.Grant{projectGrant(pj, iam.Statement{Effect: iam.EffectAllow, Actions: []string{"*"}, Resources: []string{"project/" + pj + "/*"}})}
	scoped := []iam.Grant{projectGrant(pj, iam.Statement{Effect: iam.EffectAllow, Actions: []string{"roles:assign"}, Resources: []string{"project/" + pj + "/role/" + a.String()}})}
	wholeProject := []iam.Grant{projectGrant(pj, iam.Statement{Effect: iam.EffectAllow, Actions: []string{"roles:assign"}, Resources: []string{"project/" + pj + "/role/*"}})}
	platformRoleStar := assignGrant("role/*") // the platform form says nothing about project P

	existing := func(held ...uuid.UUID) func(string) AssignTarget {
		return func(param string) AssignTarget {
			return MemberRolesTarget(&fakeAttachments{held: held}, param, "memberId")
		}
	}
	cases := []struct {
		name   string
		caller []iam.Grant
		target func(string) AssignTarget
		body   string
		want   int
	}{
		{"project admin assigns any role of the project", projectAdmin, existing(), idsBody(a, b), http.StatusNoContent},
		{"scoped statement assigns exactly its role", scoped, existing(), idsBody(a), http.StatusNoContent},
		{"scoped statement refuses another role", scoped, existing(), idsBody(b), http.StatusForbidden},
		{"project/<P>/role/* assigns any role in the project", wholeProject, existing(), idsBody(a, b), http.StatusNoContent},
		{"platform role/* does not reach inside the project", platformRoleStar, existing(), idsBody(a), http.StatusForbidden},
		{"admin of another project is refused", []iam.Grant{projectGrant(q.String(), iam.Statement{Effect: iam.EffectAllow, Actions: []string{"*"}, Resources: []string{"project/" + q.String() + "/*"}})}, existing(), idsBody(a), http.StatusForbidden},
		{"unchanged roles need nothing", nil, existing(a, b), idsBody(b, a), http.StatusNoContent},
		{"removing a role needs permission for it", scoped, existing(a, b), idsBody(a), http.StatusForbidden},
		{"removing everything needs permission for each", scoped, existing(a), `{"role_ids":[]}`, http.StatusNoContent},
		{"removing everything without permission", nil, existing(a), `{"role_ids":[]}`, http.StatusForbidden},
		{"new member: every role in role_ids is judged", scoped, func(p string) AssignTarget { return NewProjectPrincipalTarget(p) }, idsBody(a, b), http.StatusForbidden},
		{"new member: allowed roles pass", scoped, func(p string) AssignTarget { return NewProjectPrincipalTarget(p) }, idsBody(a), http.StatusNoContent},
		{"new member without role_ids assigns nothing", nil, func(p string) AssignTarget { return NewProjectPrincipalTarget(p) }, `{"user_id":"x"}`, http.StatusNoContent},
		{"new member with empty role_ids assigns nothing", nil, func(p string) AssignTarget { return NewProjectPrincipalTarget(p) }, `{"role_ids":[]}`, http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := serveProjectAssignGate(t, p, tc.caller, tc.target, tc.body); got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}
	// a malformed project id is a 400 before anything else
	userID := uuid.New()
	store := &fakeIAMStore{grants: map[iam.Principal][]iam.Grant{{Type: "user", ID: userID.String()}: projectAdmin}}
	rec, _ := serveChat(t, "/projects/{projectId}/members", "/projects/nope/members", idsBody(a), asCaller(userID, uuid.Nil),
		RequireAssignRoles(newFakeIAM(store), NewProjectPrincipalTarget("projectId")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad project id: %d", rec.Code)
	}
}

// Roles of any owner are judged as seen inside the project: an Admin of the
// project may attach a workspace role or template as well as a project role.
// The shipped project templates and the Admin policy are what make that work
// out of the box.
func TestRequireAssignRoles_ShippedProjectRoles(t *testing.T) {
	p := uuid.New()
	pj := p.String()
	ids := map[string]uuid.UUID{}
	policies := map[string]*iam.Policy{}
	for _, r := range defaultroles.Instantiate(p) {
		id := uuid.New()
		ids[r.Name] = id
		pol, err := iam.ParsePolicy(r.Policy)
		if err != nil {
			t.Fatal(err)
		}
		policies[r.Name] = pol
	}
	holding := func(name string) []iam.Grant {
		return []iam.Grant{{RoleID: ids[name].String(), ProjectID: pj, Policy: policies[name]}}
	}
	target := func(string) AssignTarget { return MemberRolesTarget(&fakeAttachments{}, "projectId", "memberId") }
	foreign := uuid.New() // a workspace role or a template, owned by nobody here
	for _, name := range []string{"Admin", "Editor", "Viewer"} {
		if got := serveProjectAssignGate(t, p, holding(defaultroles.ProjectAdmin), target, idsBody(ids[name])); got != http.StatusNoContent {
			t.Errorf("project admin assigning %s: %d, want 204", name, got)
		}
	}
	if got := serveProjectAssignGate(t, p, holding(defaultroles.ProjectAdmin), target, idsBody(foreign)); got != http.StatusNoContent {
		t.Errorf("project admin assigning a workspace role inside the project: %d, want 204", got)
	}
	for _, name := range []string{"Editor", "Viewer"} {
		if got := serveProjectAssignGate(t, p, holding(name), target, idsBody(ids["Viewer"])); got != http.StatusForbidden {
			t.Errorf("%s must not gain roles:assign: %d, want 403", name, got)
		}
	}
}

func TestRoleServiceAttachments(t *testing.T) {
	r1, r2 := uuid.New(), uuid.New()
	svc := &fakeRoleService{roles: []*roledom.Role{{ID: r1}, {ID: r2}}}
	l := NewRoleServiceAttachments(svc)
	ctx := t.Context()
	for name, f := range map[string]func() ([]uuid.UUID, error){
		"user":   func() ([]uuid.UUID, error) { return l.UserRoleIDs(ctx, uuid.New()) },
		"agent":  func() ([]uuid.UUID, error) { return l.AgentRoleIDs(ctx, uuid.New()) },
		"member": func() ([]uuid.UUID, error) { return l.MemberRoleIDs(ctx, uuid.New(), uuid.New()) },
	} {
		got, err := f()
		if err != nil || len(got) != 2 || got[0] != r1 || got[1] != r2 {
			t.Errorf("%s: %v, %v", name, got, err)
		}
	}
	// a missing target holds nothing (the handler answers 404)
	for _, e := range []error{roledom.ErrUserNotFound, roledom.ErrAgentNotFound, roledom.ErrMemberNotFound, roledom.ErrProjectNotFound} {
		svc.err = e
		got, err := l.UserRoleIDs(ctx, uuid.New())
		if err != nil || len(got) != 0 {
			t.Errorf("%v: %v, %v", e, got, err)
		}
	}
	svc.err = errors.New("db")
	if _, err := l.MemberRoleIDs(ctx, uuid.New(), uuid.New()); err == nil {
		t.Error("a real failure must be returned")
	}
}

type fakeRoleService struct {
	roles []*roledom.Role
	err   error
}

func (f *fakeRoleService) ListUserRoles(context.Context, uuid.UUID) ([]*roledom.Role, error) {
	return f.roles, f.err
}

func (f *fakeRoleService) ListAgentRoles(context.Context, uuid.UUID) ([]*roledom.Role, error) {
	return f.roles, f.err
}

func (f *fakeRoleService) ListMemberRoles(context.Context, uuid.UUID, uuid.UUID) ([]*roledom.Role, error) {
	return f.roles, f.err
}
