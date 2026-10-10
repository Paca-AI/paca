package middleware

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

type fakeRolePolicies struct {
	policies map[uuid.UUID][]byte
	err      error
	called   int
}

func (f *fakeRolePolicies) RolePolicies(_ context.Context, ids []uuid.UUID) (map[uuid.UUID][]byte, error) {
	f.called++
	if f.err != nil {
		return nil, f.err
	}
	out := map[uuid.UUID][]byte{}
	for _, id := range ids {
		if p, ok := f.policies[id]; ok {
			out[id] = p
		}
	}
	return out, nil
}

func policyBody(statement string) string {
	return `{"name":"R","description":"","policy":{"version":"2026-10-01","statements":[` + statement + `]}}`
}

func allowJSON(actions, resources string) string {
	return `{"effect":"Allow","actions":[` + actions + `],"resources":[` + resources + `]}`
}

func TestRequireActionsForSimulatedPrincipal(t *testing.T) {
	userID := uuid.New()
	user := iam.Principal{Type: "user", ID: userID.String()}
	check := ActionCheck{Action: iam.ActionRolesRead, Resource: StaticResource("role/*")}
	reader := []iam.Grant{platformGrant([]string{"roles:read"}, []string{"role/*"})}
	withPrincipal := `{"principal":{"type":"user","id":"` + uuid.NewString() + `"},"action":"a:b","resource":"*"}`
	cases := []struct {
		name   string
		grants []iam.Grant
		body   string
		want   int
	}{
		{"no principal needs nothing", nil, `{"action":"a:b","resource":"*"}`, http.StatusNoContent},
		{"null principal needs nothing", nil, `{"principal":null}`, http.StatusNoContent},
		{"not JSON passes to the handler", nil, `{`, http.StatusNoContent},
		{"principal without roles:read is refused", nil, withPrincipal, http.StatusForbidden},
		{"principal with roles:read passes", reader, withPrincipal, http.StatusNoContent},
		{"unrelated grant is refused", []iam.Grant{platformGrant([]string{"tasks:read"}, []string{"*"})}, withPrincipal, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeIAMStore{grants: map[iam.Principal][]iam.Grant{user: tc.grants}}
			rec, seen := serveChat(t, "/x", "/x", tc.body, asCaller(userID, uuid.Nil), RequireActionsForSimulatedPrincipal(newFakeIAM(store), check))
			if rec.Code != tc.want {
				t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body.String(), tc.want)
			}
			if tc.want == http.StatusNoContent && seen != tc.body {
				t.Fatalf("body must be restored: %q != %q", seen, tc.body)
			}
		})
	}
}

func TestRequireGrantableRoleInPath(t *testing.T) {
	userID := uuid.New()
	user := iam.Principal{Type: "user", ID: userID.String()}
	big, broken := uuid.New(), uuid.New()
	lookup := &fakeRolePolicies{policies: map[uuid.UUID][]byte{
		big:    []byte(`{"statements":[` + allowJSON(`"users:write"`, `"user/*"`) + `]}`),
		broken: []byte(`{"statements":[{"effect":"Maybe"}]}`),
	}}
	star := []iam.Grant{platformGrant([]string{"*"}, []string{"*"})}
	small := []iam.Grant{platformGrant([]string{"tasks:*"}, []string{"*"})}
	cases := []struct {
		name   string
		grants []iam.Grant
		path   string
		lookup RolePolicyLookup
		want   int
	}{
		{"holder passes", star, "/roles/" + big.String(), lookup, http.StatusNoContent},
		{"caller without the power is refused", small, "/roles/" + big.String(), lookup, http.StatusForbidden},
		{"unparsable role is refused", star, "/roles/" + broken.String(), lookup, http.StatusForbidden},
		{"unknown role passes (404 from the handler)", small, "/roles/" + uuid.NewString(), lookup, http.StatusNoContent},
		{"bad id is a 400", star, "/roles/nope", lookup, http.StatusBadRequest},
		{"lookup failure is 500", star, "/roles/" + big.String(), &fakeRolePolicies{err: errors.New("db")}, http.StatusInternalServerError},
		{"no lookup fails closed", star, "/roles/" + big.String(), nil, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeIAMStore{grants: map[iam.Principal][]iam.Grant{user: tc.grants}}
			rec, _ := serveChat(t, "/roles/{roleId}", tc.path, "", asCaller(userID, uuid.Nil), RequireGrantableRoleInPath(tc.lookup, newFakeIAM(store), "roleId"))
			if rec.Code != tc.want {
				t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body.String(), tc.want)
			}
		})
	}
}

func TestRequireActionsForSimulatedPrincipal_ProjectScopedCaller(t *testing.T) {
	pj := uuid.NewString()
	userID := uuid.New()
	user := iam.Principal{Type: "user", ID: userID.String()}
	check := ActionCheck{Action: iam.ActionRolesRead, Resource: StaticResource("role/*")}
	projectReader := []iam.Grant{projectGrant(pj, iam.Statement{Effect: iam.EffectAllow, Actions: []string{"roles:read"}, Resources: []string{"project/" + pj + "/*"}})}
	named := `{"principal":{"type":"user","id":"` + uuid.NewString() + `"},"action":"a:b","resource":"*"}`
	run := func(grants []iam.Grant, body string) int {
		store := &fakeIAMStore{grants: map[iam.Principal][]iam.Grant{user: grants}}
		rec, _ := serveChat(t, "/x", "/x", body, asCaller(userID, uuid.Nil), RequireActionsForSimulatedPrincipal(newFakeIAM(store), check))
		return rec.Code
	}
	if got := run(projectReader, named); got != http.StatusForbidden {
		t.Errorf("project-scoped roles:read naming a principal: %d, want 403", got)
	}
	if got := run(projectReader, `{"action":"a:b","resource":"*"}`); got != http.StatusNoContent {
		t.Errorf("project-scoped roles:read without principal: %d, want pass", got)
	}
	if got := run([]iam.Grant{platformGrant([]string{"roles:read"}, []string{"role/*"})}, named); got != http.StatusNoContent {
		t.Errorf("platform roles:read naming a principal: %d, want pass", got)
	}
}
