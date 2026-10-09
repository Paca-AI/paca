package middleware

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

type fakeEnvLookup struct {
	env      *uuid.UUID
	err      error
	called   int
	gotAgent uuid.UUID
	gotProj  uuid.UUID
	gotSess  uuid.UUID
}

func (f *fakeEnvLookup) DefaultEnvironmentID(_ context.Context, p, a uuid.UUID) (*uuid.UUID, error) {
	f.called++
	f.gotProj, f.gotAgent = p, a
	return f.env, f.err
}

func (f *fakeEnvLookup) SessionEnvironmentID(_ context.Context, p, a, s uuid.UUID) (*uuid.UUID, error) {
	f.called++
	f.gotProj, f.gotAgent, f.gotSess = p, a, s
	return f.env, f.err
}

const chatPattern = "/projects/{projectId}/agents/{agentId}/chat-sessions"

// serveChat POSTs body through gate and returns the recorder plus the body
// the downstream handler saw.
func serveChat(t *testing.T, pattern, path, body string, mws ...func(http.Handler) http.Handler) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var seen string
	r := chi.NewRouter()
	r.With(mws...).Post(pattern, func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		seen = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body)))
	return rec, seen
}

func TestChatEnvironmentResource(t *testing.T) {
	projectID, agentID, userID := uuid.New(), uuid.New(), uuid.New()
	bodyEnv, defaultEnv := uuid.New(), uuid.New()
	user := iam.Principal{Type: "user", ID: userID.String()}
	p := "project/" + projectID.String()
	path := "/projects/" + projectID.String() + "/agents/" + agentID.String() + "/chat-sessions"

	member := projectGrant(projectID.String(), iam.Statement{Effect: iam.EffectAllow, Actions: []string{"environments:read"}, Resources: []string{p + "/*"}})
	denyBody := iam.Statement{Effect: iam.EffectDeny, Actions: []string{"environments:read"}, Resources: []string{p + "/environment/" + bodyEnv.String() + "/*"}}
	denyDefault := iam.Statement{Effect: iam.EffectDeny, Actions: []string{"environments:read"}, Resources: []string{p + "/environment/" + defaultEnv.String() + "/*"}}
	withDeny := func(s iam.Statement) []iam.Grant { return []iam.Grant{member, projectGrant(projectID.String(), s)} }

	cases := []struct {
		name       string
		grants     []iam.Grant
		storeErr   error
		body       string
		lookupEnv  *uuid.UUID
		lookupErr  error
		want       int
		wantLookup bool
	}{
		{"body environment allowed", []iam.Grant{member}, nil, `{"message":"hi","environment_id":"` + bodyEnv.String() + `"}`, nil, nil, http.StatusNoContent, false},
		{"body environment denied by a Deny", withDeny(denyBody), nil, `{"message":"hi","environment_id":"` + bodyEnv.String() + `"}`, nil, nil, http.StatusForbidden, false},
		{"body wins over a denied default", withDeny(denyDefault), nil, `{"message":"hi","environment_id":"` + bodyEnv.String() + `"}`, &defaultEnv, nil, http.StatusNoContent, false},
		{"no grant at all", nil, nil, `{"message":"hi","environment_id":"` + bodyEnv.String() + `"}`, nil, nil, http.StatusForbidden, false},
		{"default environment allowed", []iam.Grant{member}, nil, `{"message":"hi"}`, &defaultEnv, nil, http.StatusNoContent, true},
		{"default environment denied", withDeny(denyDefault), nil, `{"message":"hi"}`, &defaultEnv, nil, http.StatusForbidden, true},
		{"null environment_id falls back to the default", withDeny(denyDefault), nil, `{"message":"hi","environment_id":null}`, &defaultEnv, nil, http.StatusForbidden, true},
		{"no environment anywhere: nothing to check", nil, nil, `{"message":"hi"}`, nil, nil, http.StatusNoContent, true},
		{"empty body, no default", nil, nil, ``, nil, nil, http.StatusNoContent, true},
		{"first JSON value decides, as the handler decodes it", withDeny(denyBody), nil, `{"environment_id":"` + bodyEnv.String() + `"} trailing`, nil, nil, http.StatusForbidden, false},
		{"environment_id key matches case-insensitively, as the handler decodes it", withDeny(denyBody), nil, `{"Environment_ID":"` + bodyEnv.String() + `"}`, nil, nil, http.StatusForbidden, false},
		{"invalid JSON is left to the handler", nil, nil, `{"message":`, nil, nil, http.StatusNoContent, true},
		{"environment_id not a uuid is a 400", []iam.Grant{member}, nil, `{"environment_id":"nope"}`, nil, nil, http.StatusBadRequest, false},
		{"environment_id of the wrong type is a 400", []iam.Grant{member}, nil, `{"environment_id":7}`, nil, nil, http.StatusBadRequest, false},
		{"oversized body is a 400", []iam.Grant{member}, nil, `{"message":"` + strings.Repeat("a", 1<<20) + `"}`, nil, nil, http.StatusBadRequest, false},
		{"agent not found is a 404", []iam.Grant{member}, nil, `{"message":"hi"}`, nil, agentdom.ErrAgentNotFound, http.StatusNotFound, true},
		{"authorizer error denies", nil, errors.New("db down"), `{"environment_id":"` + bodyEnv.String() + `"}`, nil, nil, http.StatusInternalServerError, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lookup := &fakeEnvLookup{env: tc.lookupEnv, err: tc.lookupErr}
			a := newFakeIAM(&fakeIAMStore{grants: map[iam.Principal][]iam.Grant{user: tc.grants}, err: tc.storeErr})
			gate := RequireAction(a, "environments:read", ChatEnvironmentResource(lookup, "projectId", "agentId"))
			rec, seen := serveChat(t, chatPattern, path, tc.body, asCaller(userID, uuid.Nil), gate)
			if rec.Code != tc.want {
				t.Fatalf("got %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
			if (lookup.called > 0) != tc.wantLookup {
				t.Errorf("default-environment lookup called %d times, want called=%v", lookup.called, tc.wantLookup)
			}
			if lookup.called > 0 && (lookup.gotProj != projectID || lookup.gotAgent != agentID) {
				t.Errorf("lookup scoped by %s/%s, want the URL's %s/%s", lookup.gotProj, lookup.gotAgent, projectID, agentID)
			}
			if rec.Code == http.StatusNoContent && seen != tc.body {
				t.Errorf("handler saw body %q, want the original %q", seen, tc.body)
			}
		})
	}
}

func TestSessionEnvironmentResource(t *testing.T) {
	projectID, agentID, sessionID, userID, envID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	user := iam.Principal{Type: "user", ID: userID.String()}
	p := "project/" + projectID.String()
	path := "/projects/" + projectID.String() + "/agents/" + agentID.String() + "/chat-sessions/" + sessionID.String() + "/messages"
	pattern := chatPattern + "/{sessionId}/messages"

	member := projectGrant(projectID.String(), iam.Statement{Effect: iam.EffectAllow, Actions: []string{"environments:read"}, Resources: []string{p + "/*"}})
	deny := projectGrant(projectID.String(), iam.Statement{Effect: iam.EffectDeny, Actions: []string{"environments:read"}, Resources: []string{p + "/environment/" + envID.String() + "/*"}})

	cases := []struct {
		name      string
		grants    []iam.Grant
		storeErr  error
		env       *uuid.UUID
		lookupErr error
		want      int
	}{
		{"allowed", []iam.Grant{member}, nil, &envID, nil, http.StatusNoContent},
		{"denied by a Deny on the environment", []iam.Grant{member, deny}, nil, &envID, nil, http.StatusForbidden},
		{"no grant", nil, nil, &envID, nil, http.StatusForbidden},
		{"session without an environment", nil, nil, nil, nil, http.StatusNoContent},
		{"session not found is a 404", []iam.Grant{member}, nil, nil, agentdom.ErrChatSessionNotFound, http.StatusNotFound},
		{"authorizer error denies", nil, errors.New("db down"), &envID, nil, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lookup := &fakeEnvLookup{env: tc.env, err: tc.lookupErr}
			a := newFakeIAM(&fakeIAMStore{grants: map[iam.Principal][]iam.Grant{user: tc.grants}, err: tc.storeErr})
			gate := RequireAction(a, "environments:read", SessionEnvironmentResource(lookup, "projectId", "agentId", "sessionId"))
			rec, _ := serveChat(t, pattern, path, "", asCaller(userID, uuid.Nil), gate)
			if rec.Code != tc.want {
				t.Fatalf("got %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
			if lookup.gotProj != projectID || lookup.gotAgent != agentID || lookup.gotSess != sessionID {
				t.Errorf("lookup scoped by %s/%s/%s, want the URL's ids", lookup.gotProj, lookup.gotAgent, lookup.gotSess)
			}
		})
	}
}

// A gate whose resolver says there is nothing to authorize still requires an
// authenticated caller, and the other checks of the same gate still apply.
func TestRequireActions_SkipsNoResourceChecks(t *testing.T) {
	projectID, userID := uuid.New(), uuid.New()
	user := iam.Principal{Type: "user", ID: userID.String()}
	path := "/projects/" + projectID.String() + "/agents/" + uuid.NewString() + "/chat-sessions"
	none := func(*http.Request) (string, error) { return "", ErrNoResource }
	a := newFakeIAM(&fakeIAMStore{grants: map[iam.Principal][]iam.Grant{user: nil}})

	only := RequireAction(a, "environments:read", none)
	if rec, _ := serveChat(t, chatPattern, path, "", only); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: got %d, want 401", rec.Code)
	}
	if rec, _ := serveChat(t, chatPattern, path, "", asCaller(userID, uuid.Nil), only); rec.Code != http.StatusNoContent {
		t.Fatalf("only skipped checks: got %d, want 204", rec.Code)
	}
	both := RequireActions(a, ActionCheck{Action: "environments:read", Resource: none},
		ActionCheck{Action: "tasks:read", Resource: ProjectResource("projectId")})
	if rec, _ := serveChat(t, chatPattern, path, "", asCaller(userID, uuid.Nil), both); rec.Code != http.StatusForbidden {
		t.Fatalf("remaining check must still apply: got %d, want 403", rec.Code)
	}
}

func TestRoleResourceResolvers(t *testing.T) {
	p, r := uuid.New(), uuid.New()
	upper := strings.ToUpper(r.String())
	cases := []struct {
		name, pattern, path string
		res                 ResourceResolver
		want                string
		wantErr             bool
	}{
		{"collection", "/projects/{projectId}/roles", "/projects/" + p.String() + "/roles", ProjectChildCollection("projectId", "role"), "project/" + p.String() + "/role/*", false},
		{"collection bad project", "/projects/{projectId}/roles", "/projects/nope/roles", ProjectChildCollection("projectId", "role"), "", true},
		{"platform entity canonicalised", "/roles/{roleId}", "/roles/" + upper, PlatformChildResource("role", "roleId"), "role/" + r.String(), false},
		{"platform entity bad id", "/roles/{roleId}", "/roles/x", PlatformChildResource("role", "roleId"), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			var gotErr error
			router := chi.NewRouter()
			router.Get(tc.pattern, func(_ http.ResponseWriter, req *http.Request) { got, gotErr = tc.res(req) })
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil))
			if (gotErr != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("got %q err=%v, want %q err=%v", got, gotErr, tc.want, tc.wantErr)
			}
		})
	}
}
