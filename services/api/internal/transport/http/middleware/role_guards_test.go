package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
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

func TestRequireGrantablePolicy(t *testing.T) {
	pid := uuid.New().String()
	other := uuid.New().String()
	userID := uuid.New()
	user := iam.Principal{Type: "user", ID: userID.String()}
	starCaller := []iam.Grant{platformGrant([]string{"*"}, []string{"*"})}
	projectAdmin := []iam.Grant{projectGrant(pid, iam.Statement{Effect: iam.EffectAllow, Actions: []string{"*"}, Resources: []string{"project/" + pid + "/*"}})}

	cases := []struct {
		name     string
		grants   []iam.Grant
		storeErr error
		body     string
		want     int
		wantCode string
	}{
		{"star caller may grant anything", starCaller,
			nil, policyBody(allowJSON(`"users:write"`, `"user/*"`)), http.StatusNoContent, ""},
		{"project admin may grant inside its project", projectAdmin,
			nil, policyBody(allowJSON(`"tasks:*"`, `"project/`+pid+`/task/*"`)), http.StatusNoContent, ""},
		{"project admin cannot grant users:write", projectAdmin,
			nil, policyBody(allowJSON(`"users:write"`, `"user/*"`)), http.StatusForbidden, "FORBIDDEN"},
		{"project admin cannot grant another project", projectAdmin,
			nil, policyBody(allowJSON(`"tasks:read"`, `"project/`+other+`/task/*"`)), http.StatusForbidden, "FORBIDDEN"},
		{"project admin cannot grant star", projectAdmin,
			nil, policyBody(allowJSON(`"tasks:read"`, `"*"`)), http.StatusForbidden, "FORBIDDEN"},
		{"deny statements are always grantable", nil,
			nil, policyBody(`{"effect":"Deny","actions":["*"],"resources":["*"]}`), http.StatusNoContent, ""},
		{"caller with nothing cannot grant", nil,
			nil, policyBody(allowJSON(`"tasks:read"`, `"project/*"`)), http.StatusForbidden, "FORBIDDEN"},
		{"second statement escalates", projectAdmin,
			nil, policyBody(allowJSON(`"tasks:read"`, `"project/`+pid+`/*"`) + "," + allowJSON(`"users:read"`, `"user/*"`)), http.StatusForbidden, "FORBIDDEN"},
		{"conditions need the same coverage", projectAdmin,
			nil, policyBody(`{"effect":"Allow","actions":["users:read"],"resources":["user/*"],"conditions":{"StringEquals":{"principal.type":"user"}}}`), http.StatusForbidden, "FORBIDDEN"},
		// pass-through: the handler/service answer these
		{"not JSON", nil, nil, `{`, http.StatusNoContent, ""},
		{"no body policy", nil, nil, `{"name":"R"}`, http.StatusNoContent, ""},
		{"null policy", nil, nil, `{"name":"R","policy":null}`, http.StatusNoContent, ""},
		{"policy not an object", nil, nil, `{"policy":"x"}`, http.StatusNoContent, ""},
		{"unparsable policy", nil, nil, `{"policy":{"statements":[{"effect":"Maybe"}]}}`, http.StatusNoContent, ""},
		{"unknown policy field", nil, nil, `{"policy":{"bogus":1}}`, http.StatusNoContent, ""},
		{"empty body", nil, nil, ``, http.StatusNoContent, ""},
		// the first JSON value is what the handler binds, trailing data is ignored
		{"trailing data after escalating policy", projectAdmin,
			nil, policyBody(allowJSON(`"users:write"`, `"user/*"`)) + `{"policy":{}}`, http.StatusForbidden, "FORBIDDEN"},
		{"case-insensitive key", projectAdmin,
			nil, `{"Policy":{"statements":[` + allowJSON(`"users:write"`, `"user/*"`) + `]}}`, http.StatusForbidden, "FORBIDDEN"},
		// fail closed
		{"store error is 500", starCaller,
			errors.New("db down"), policyBody(allowJSON(`"tasks:read"`, `"*"`)), http.StatusInternalServerError, "INTERNAL_ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeIAMStore{grants: map[iam.Principal][]iam.Grant{user: tc.grants}, err: tc.storeErr}
			guard := RequireGrantablePolicy(newFakeIAM(store))
			rec, seen := serveChat(t, "/x", "/x", tc.body, asCaller(userID, uuid.Nil), guard)
			if rec.Code != tc.want {
				t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body.String(), tc.want)
			}
			if tc.wantCode != "" && !strings.Contains(rec.Body.String(), tc.wantCode) {
				t.Fatalf("body = %s, want code %s", rec.Body.String(), tc.wantCode)
			}
			if tc.want == http.StatusNoContent && seen != tc.body {
				t.Fatalf("body must be restored for the handler: %q != %q", seen, tc.body)
			}
		})
	}
}

func TestRequireGrantablePolicy_UnauthenticatedAndOversized(t *testing.T) {
	g := newFakeIAM(&fakeIAMStore{})
	rec, _ := serveChat(t, "/x", "/x", policyBody(allowJSON(`"tasks:read"`, `"*"`)), RequireGrantablePolicy(g))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d", rec.Code)
	}
	big := `{"policy":{"statements":[]},"pad":"` + strings.Repeat("x", maxPeekBody) + `"}`
	rec, _ = serveChat(t, "/x", "/x", big, asCaller(uuid.New(), uuid.Nil), RequireGrantablePolicy(g))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized body: %d", rec.Code)
	}
	// no authorizer wired: a policy that needs checking fails closed
	rec, _ = serveChat(t, "/x", "/x", policyBody(allowJSON(`"tasks:read"`, `"*"`)), asCaller(uuid.New(), uuid.Nil), RequireGrantablePolicy(nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("no granter: %d", rec.Code)
	}
}

func TestRequireGrantablePolicy_AgentCallerIsJudgedAsTheAgent(t *testing.T) {
	userID, agentID := uuid.New(), uuid.New()
	user := iam.Principal{Type: "user", ID: userID.String()}
	agent := iam.Principal{Type: "agent", ID: agentID.String()}
	store := &fakeIAMStore{grants: map[iam.Principal][]iam.Grant{
		user: {platformGrant([]string{"*"}, []string{"*"})}, // the bot user behind the key
	}}
	guard := RequireGrantablePolicy(newFakeIAM(store))
	rec, _ := serveChat(t, "/x", "/x", policyBody(allowJSON(`"tasks:read"`, `"*"`)), asCaller(userID, agentID), guard)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("agent without grants must not borrow the key user's: %d", rec.Code)
	}
	store.grants[agent] = []iam.Grant{platformGrant([]string{"tasks:*"}, []string{"*"})}
	rec, _ = serveChat(t, "/x", "/x", policyBody(allowJSON(`"tasks:read"`, `"*"`)), asCaller(userID, agentID), guard)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("agent holding tasks:* may grant tasks:read: %d", rec.Code)
	}
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
