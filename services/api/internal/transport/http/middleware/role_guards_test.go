package middleware

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

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
