package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
	gitsvc "github.com/Paca-AI/api/internal/service/git"

	"github.com/Paca-AI/api/internal/transport/http/middleware"
)

type fakeGitSvc struct {
	checkErr    error // answered by CheckWebhookEndpoint
	link        gitsvc.LinkInput
	repos       []*gitdom.Repository
	err         error
	unlinked    [2]uuid.UUID
	updated     [2]uuid.UUID
	updatedDesc string
	reconnect   gitsvc.ReconnectInput
	mint        gitsvc.MintInput
	cred        gitprovider.Credential
	key         string
	delivery    gitsvc.Delivery

	providers  []gitprovider.Info
	identities []*gitdom.Identity
	setupIn    gitsvc.SetupInput
	step       *gitprovider.SetupStep
	completion *gitsvc.SetupCompletion
	completeBy uuid.UUID
	completeSt string
	completeP  map[string]string
	deletedID  uuid.UUID
	removed    [2]string
	agentIDs   uuid.UUID

	accessIn  gitsvc.AccessInput
	accessRes *gitsvc.AccessCompletion
	accessBy  [2]uuid.UUID
	accessSt  string
	accessP   map[string]string
}

func (f *fakeGitSvc) BeginAccessGrant(_ context.Context, in gitsvc.AccessInput) (*gitprovider.SetupStep, error) {
	f.accessIn = in
	return f.step, f.err
}
func (f *fakeGitSvc) CompleteAccessGrant(_ context.Context, actor, project uuid.UUID, st string, p map[string]string) (*gitsvc.AccessCompletion, error) {
	f.accessBy, f.accessSt, f.accessP = [2]uuid.UUID{actor, project}, st, p
	return f.accessRes, f.err
}

func (f *fakeGitSvc) ListProviders(context.Context) ([]gitprovider.Info, error) {
	return f.providers, f.err
}
func (f *fakeGitSvc) ListIdentities(context.Context) ([]*gitdom.Identity, error) {
	return f.identities, f.err
}
func (f *fakeGitSvc) ListAgentIdentities(_ context.Context, a uuid.UUID) ([]*gitdom.Identity, error) {
	f.agentIDs = a
	return f.identities, f.err
}
func (f *fakeGitSvc) BeginIdentitySetup(_ context.Context, in gitsvc.SetupInput) (*gitprovider.SetupStep, error) {
	f.setupIn = in
	return f.step, f.err
}
func (f *fakeGitSvc) CompleteIdentitySetup(_ context.Context, actor uuid.UUID, _ *uuid.UUID, st string, p map[string]string) (*gitsvc.SetupCompletion, error) {
	f.completeBy, f.completeSt, f.completeP = actor, st, p
	return f.completion, f.err
}
func (f *fakeGitSvc) DeleteIdentity(_ context.Context, id uuid.UUID) error {
	f.deletedID = id
	return f.err
}
func (f *fakeGitSvc) ReconnectRepository(_ context.Context, in gitsvc.ReconnectInput) (*gitdom.Repository, error) {
	f.reconnect = in
	if f.err != nil {
		return nil, f.err
	}
	return &gitdom.Repository{ID: in.RepositoryID, ProjectID: in.ProjectID, ProviderID: "p", Host: "h", FullPath: "a/b", IdentityID: in.IdentityID}, nil
}
func (f *fakeGitSvc) RegisterWebhook(_ context.Context, p, r uuid.UUID) (*gitdom.Repository, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &gitdom.Repository{ID: r, ProjectID: p, ProviderID: "p", Host: "h", FullPath: "a/b"}, nil
}
func (f *fakeGitSvc) ListConnections(context.Context, uuid.UUID) ([]*gitdom.Identity, error) {
	return f.identities, f.err
}
func (f *fakeGitSvc) DeleteProjectConnection(_ context.Context, _, id uuid.UUID) error {
	f.deletedID = id
	return f.err
}
func (f *fakeGitSvc) RemoveAgentIdentity(_ context.Context, a uuid.UUID, p string) error {
	f.removed = [2]string{a.String(), p}
	return f.err
}

type fakeAgents struct {
	project, agent uuid.UUID
}

func (f fakeAgents) AgentInProject(_ context.Context, p, a uuid.UUID) (string, bool, error) {
	return "Dev", p == f.project && a == f.agent, nil
}

func (f *fakeGitSvc) LinkRepository(_ context.Context, in gitsvc.LinkInput) (*gitdom.Repository, error) {
	f.link = in
	if f.err != nil {
		return nil, f.err
	}
	return &gitdom.Repository{ID: uuid.New(), ProjectID: in.ProjectID, ProviderID: in.ProviderID, Host: "h", FullPath: "a/b",
		ProviderRef: json.RawMessage(`{"secret":"internal"}`)}, nil
}
func (f *fakeGitSvc) ListRepositories(context.Context, uuid.UUID) ([]*gitdom.Repository, error) {
	return f.repos, f.err
}
func (f *fakeGitSvc) UnlinkRepository(_ context.Context, p, r uuid.UUID) error {
	f.unlinked = [2]uuid.UUID{p, r}
	return f.err
}
func (f *fakeGitSvc) UpdateRepository(_ context.Context, p, r uuid.UUID, d string) (*gitdom.Repository, error) {
	f.updated, f.updatedDesc = [2]uuid.UUID{p, r}, d
	if f.err != nil {
		return nil, f.err
	}
	return &gitdom.Repository{ID: r, ProjectID: p, FullPath: "a/b", Description: d}, nil
}
func (f *fakeGitSvc) MintCredential(_ context.Context, in gitsvc.MintInput) (gitprovider.Credential, error) {
	f.mint = in
	return f.cred, f.err
}
func (f *fakeGitSvc) CheckWebhookEndpoint(context.Context, string) error { return f.checkErr }

func (f *fakeGitSvc) HandleWebhook(_ context.Context, k string, d gitsvc.Delivery) (*gitsvc.WebhookResult, error) {
	f.key, f.delivery = k, d
	if f.err != nil {
		return nil, f.err
	}
	return &gitsvc.WebhookResult{Events: 2, Stored: 1, Replayed: 1}, nil
}

// serveGit routes one request through chi so URL params resolve, as the router does.
var agentLookup GitAgentLookup = fakeAgents{}

func serveGit(t *testing.T, f *fakeGitSvc, method, target, body string, actor *uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	h := NewGitHandler(f).WithAgentLookup(agentLookup)
	r := chi.NewRouter()
	r.Get("/git/providers", h.ListProviders)
	r.Get("/git/identities", h.ListIdentities)
	r.Post("/git/identities/setup", h.BeginSetup)
	r.Post("/git/identities/setup/complete", h.CompleteSetup)
	r.Delete("/git/identities/{identityId}", h.DeleteIdentity)
	r.Get("/projects/{projectId}/agents/{agentId}/git/identity", h.ListAgentIdentities)
	r.Post("/projects/{projectId}/agents/{agentId}/git/identity/setup", h.BeginAgentSetup)
	r.Delete("/projects/{projectId}/agents/{agentId}/git/identity", h.DeleteAgentIdentity)
	r.Post("/projects/{projectId}/agents/{agentId}/git/identity/access/begin", h.BeginAgentAccess)
	r.Post("/projects/{projectId}/git/access/begin", h.BeginAccess)
	r.Post("/projects/{projectId}/git/access/complete", h.CompleteAccess)
	r.Get("/projects/{projectId}/git/connections", h.ListConnections)
	r.Post("/projects/{projectId}/git/connections/setup", h.BeginProjectSetup)
	r.Post("/projects/{projectId}/git/connections/setup/complete", h.CompleteProjectSetup)
	r.Delete("/projects/{projectId}/git/connections/{identityId}", h.DeleteProjectConnection)
	r.Post("/projects/{projectId}/git/repositories/{repositoryId}/webhook", h.RegisterWebhook)
	r.Post("/projects/{projectId}/git/repositories/{repositoryId}/reconnect", h.ReconnectRepository)
	r.Get("/projects/{projectId}/git/repositories", h.ListRepositories)
	r.Post("/projects/{projectId}/git/repositories", h.LinkRepository)
	r.Delete("/projects/{projectId}/git/repositories/{repositoryId}", h.UnlinkRepository)
	r.Patch("/projects/{projectId}/git/repositories/{repositoryId}", h.UpdateRepository)
	r.Post("/projects/{projectId}/git/repositories/{repositoryId}/credential/read", h.ReadCredential)
	r.Post("/projects/{projectId}/git/repositories/{repositoryId}/credential/write", h.WriteCredential)
	r.Post("/git/webhooks/{endpointKey}", h.ReceiveWebhook)
	req := httptest.NewRequestWithContext(t.Context(), method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if actor != nil {
		req = req.WithContext(middleware.WithActorID(req.Context(), *actor))
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func errCode(rec *httptest.ResponseRecorder) string {
	var env struct {
		ErrorCode string `json:"error_code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return env.ErrorCode
}

func TestGitHandler_LinkListUnlink(t *testing.T) {
	actor, project := uuid.New(), uuid.New()
	f := &fakeGitSvc{}
	rec := serveGit(t, f, "POST", "/projects/"+project.String()+"/git/repositories",
		`{"provider_id":" com.paca.github ","host":"github.com","path_or_url":" acme/web "}`, &actor)
	if rec.Code != http.StatusCreated {
		t.Fatalf("link: %d %s", rec.Code, rec.Body)
	}
	if f.link.ProjectID != project || f.link.ProviderID != "com.paca.github" || f.link.PathOrURL != "acme/web" || f.link.ActorID == nil || *f.link.ActorID != actor {
		t.Fatalf("input = %+v", f.link)
	}
	if strings.Contains(rec.Body.String(), "internal") || strings.Contains(rec.Body.String(), "provider_ref") {
		t.Fatalf("the provider's opaque reference must not reach clients: %s", rec.Body)
	}

	if rec := serveGit(t, f, "POST", "/projects/"+project.String()+"/git/repositories", `{"provider_id":"p"}`, &actor); rec.Code != 400 {
		t.Fatalf("missing path: %d", rec.Code)
	}
	f.repos = []*gitdom.Repository{{ID: uuid.New(), FullPath: "acme/web"}}
	rec = serveGit(t, f, "GET", "/projects/"+project.String()+"/git/repositories", "", &actor)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "acme/web") {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	repo := uuid.New()
	if rec := serveGit(t, f, "DELETE", "/projects/"+project.String()+"/git/repositories/"+repo.String(), "", &actor); rec.Code != 204 || f.unlinked != [2]uuid.UUID{project, repo} {
		t.Fatalf("unlink: %d %v", rec.Code, f.unlinked)
	}
	if rec := serveGit(t, f, "DELETE", "/projects/"+project.String()+"/git/repositories/nope", "", &actor); rec.Code != 400 {
		t.Fatalf("bad id: %d", rec.Code)
	}
}

func TestGitHandler_UpdateRepositoryDescription(t *testing.T) {
	actor, project, repo := uuid.New(), uuid.New(), uuid.New()
	f := &fakeGitSvc{}
	target := "/projects/" + project.String() + "/git/repositories/" + repo.String()
	rec := serveGit(t, f, "PATCH", target, `{"description":"Web client"}`, &actor)
	if rec.Code != 200 || f.updated != [2]uuid.UUID{project, repo} || f.updatedDesc != "Web client" || !strings.Contains(rec.Body.String(), `"description":"Web client"`) {
		t.Fatalf("update: %d %v %q %s", rec.Code, f.updated, f.updatedDesc, rec.Body)
	}
	f.err = gitdom.ErrDescriptionTooLong
	if rec := serveGit(t, f, "PATCH", target, `{"description":"x"}`, &actor); rec.Code != 400 || !strings.Contains(rec.Body.String(), "GIT_DESCRIPTION_TOO_LONG") {
		t.Fatalf("too long: %d %s", rec.Code, rec.Body)
	}
	if rec := serveGit(t, f, "PATCH", "/projects/"+project.String()+"/git/repositories/nope", `{}`, &actor); rec.Code != 400 {
		t.Fatalf("bad id: %d", rec.Code)
	}
}

func TestGitHandler_ErrorMapping(t *testing.T) {
	actor, project := uuid.New(), uuid.New().String()
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{gitdom.ErrAlreadyLinked, 409, "GIT_ALREADY_LINKED"},
		{gitdom.ErrNoIdentity, 409, "GIT_NO_IDENTITY"},
		{gitdom.ErrNotVisible, 409, "GIT_REPOSITORY_NOT_VISIBLE"},
		{gitdom.ErrNotFound, 404, "GIT_NOT_FOUND"},
		{gitdom.ErrProviderUnavailable, 503, "GIT_PROVIDER_UNAVAILABLE"},
		{&gitprovider.Error{Code: gitprovider.CodeNotFound}, 404, "GIT_NOT_FOUND"},
		{&gitprovider.Error{Code: gitprovider.CodeForbidden}, 422, "GIT_PROVIDER_REJECTED"},
		{&gitprovider.Error{Code: gitprovider.CodeRateLimited}, 429, "TOO_MANY_REQUESTS"},
		{&gitprovider.Error{Code: gitprovider.CodeTransient}, 503, "GIT_PROVIDER_UNAVAILABLE"},
		{&gitprovider.Error{Code: gitprovider.CodeInternal, Message: "secret detail"}, 500, "INTERNAL_ERROR"},
	}
	for _, c := range cases {
		rec := serveGit(t, &fakeGitSvc{err: c.err}, "POST", "/projects/"+project+"/git/repositories", `{"provider_id":"p","path_or_url":"a/b"}`, &actor)
		if rec.Code != c.status || errCode(rec) != c.code {
			t.Errorf("%v: got %d %s, want %d %s", c.err, rec.Code, errCode(rec), c.status, c.code)
		}
		if strings.Contains(rec.Body.String(), "secret detail") {
			t.Errorf("%v: a provider message leaked to the client: %s", c.err, rec.Body)
		}
	}
}

func TestGitHandler_CredentialIsScopedAndNotCacheable(t *testing.T) {
	actor, project, repo := uuid.New(), uuid.New(), uuid.New()
	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	f := &fakeGitSvc{cred: gitprovider.Credential{Username: "x-access-token", Secret: "tok", ExpiresAt: exp}}
	base := "/projects/" + project.String() + "/git/repositories/" + repo.String() + "/credential/"

	rec := serveGit(t, f, "POST", base+"read", "", &actor)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" || !strings.Contains(rec.Body.String(), `"secret":"tok"`) {
		t.Fatalf("read: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	if f.mint.Access != gitprovider.AccessRead || f.mint.RepositoryID != repo || f.mint.ProjectID == nil || *f.mint.ProjectID != project || f.mint.AgentID == nil || *f.mint.AgentID != actor {
		t.Fatalf("mint input = %+v", f.mint)
	}
	serveGit(t, f, "POST", base+"write", "", &actor)
	if f.mint.Access != gitprovider.AccessWrite {
		t.Fatalf("write route must ask for write access: %+v", f.mint)
	}
	f.cred = gitprovider.Credential{Username: "u", Secret: "s"} // static credential: no expiry
	if rec := serveGit(t, f, "POST", base+"read", "", &actor); !strings.Contains(rec.Body.String(), `"expires_at":null`) {
		t.Fatalf("static credential: %s", rec.Body)
	}
	if rec := serveGit(t, f, "POST", base+"read", "", nil); rec.Code != 401 {
		t.Fatalf("unauthenticated: %d", rec.Code)
	}
}

func TestGitHandler_Webhook(t *testing.T) {
	f := &fakeGitSvc{}
	h := NewGitHandler(f)
	r := chi.NewRouter()
	r.Post("/git/webhooks/{endpointKey}", h.ReceiveWebhook)
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/git/webhooks/key-1?x=1", strings.NewReader(`{"a":1}`))
	req.Header.Set("X-Hub-Signature-256", "sha256=abc")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if f.key != "key-1" || string(f.delivery.Body) != `{"a":1}` || f.delivery.Headers["x-hub-signature-256"] != "sha256=abc" || f.delivery.Query["x"] != "1" || f.delivery.Method != "POST" {
		t.Fatalf("delivery = %+v key=%s", f.delivery, f.key)
	}

	f.err = gitdom.ErrInvalidSignature
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "POST", "/git/webhooks/key-1", strings.NewReader("{}")))
	if rec.Code != 401 {
		t.Fatalf("bad signature: %d", rec.Code)
	}
	f.err = &gitprovider.Error{Code: gitprovider.CodeTransient}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "POST", "/git/webhooks/key-1", strings.NewReader("{}")))
	if rec.Code != 503 {
		t.Fatalf("transient errors must be retryable by the platform: %d", rec.Code)
	}

	f.err = nil
	f.key = ""
	big := bytes.NewReader(make([]byte, maxGitWebhookBody+1))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "POST", "/git/webhooks/key-1", big))
	if rec.Code != 400 || f.key != "" {
		t.Fatalf("oversized body must be refused before reaching the service: %d key=%q", rec.Code, f.key)
	}
}

func TestGitHandler_SharedSetupAndCompletion(t *testing.T) {
	actor := uuid.New()
	f := &fakeGitSvc{step: &gitprovider.SetupStep{Kind: gitprovider.StepFormPost, URL: "https://github.com/settings/apps/new", Fields: map[string]string{"manifest": "{}"}, State: "st"}}
	rec := serveGit(t, f, "POST", "/git/identities/setup", `{"provider_id":" com.paca.github ","host":"github.com","return_url":"https://paca.example.com/x","params":{"name":"Acme"}}`, &actor)
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	in := f.setupIn
	if in.ProviderID != "com.paca.github" || in.Scope != gitdom.ScopeShared || in.ActorID != actor || in.AgentID != nil || in.Params["name"] != "Acme" || in.ReturnURL != "https://paca.example.com/x" {
		t.Fatalf("input = %+v", in)
	}
	if !strings.Contains(rec.Body.String(), `"kind":"form_post"`) || !strings.Contains(rec.Body.String(), `"state":"st"`) {
		t.Errorf("step missing: %s", rec.Body)
	}
	if rec := serveGit(t, f, "POST", "/git/identities/setup", `{"host":"x"}`, &actor); rec.Code != 400 {
		t.Errorf("missing provider: %d", rec.Code)
	}
	if rec := serveGit(t, f, "POST", "/git/identities/setup", `{"provider_id":"p"}`, nil); rec.Code != 401 {
		t.Errorf("unauthenticated: %d", rec.Code)
	}

	id := &gitdom.Identity{ID: uuid.New(), ProviderID: "p", Host: "h", Handle: "bot", Scope: gitdom.ScopeShared, ProviderRef: json.RawMessage(`{"secret":"internal"}`)}
	f.completion = &gitsvc.SetupCompletion{Identity: id, ReturnURL: "https://paca.example.com/x",
		Next: &gitprovider.SetupStep{Kind: gitprovider.StepRedirect, URL: "https://github.com/apps/bot/installations/new", State: "st"}}
	rec = serveGit(t, f, "POST", "/git/identities/setup/complete", `{"state":" st ","params":{"code":"c0de"}}`, &actor)
	if rec.Code != 200 || f.completeBy != actor || f.completeSt != "st" || f.completeP["code"] != "c0de" {
		t.Fatalf("complete: %d %s by=%v st=%q", rec.Code, rec.Body, f.completeBy, f.completeSt)
	}
	if !strings.Contains(rec.Body.String(), `"done":false`) || !strings.Contains(rec.Body.String(), "installations/new") {
		t.Errorf("leg 1 has a next step: %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "internal") || strings.Contains(rec.Body.String(), "provider_ref") {
		t.Errorf("the provider's opaque reference must not reach clients: %s", rec.Body)
	}
	f.completion.Next = nil
	if rec := serveGit(t, f, "POST", "/git/identities/setup/complete", `{"state":"st"}`, &actor); !strings.Contains(rec.Body.String(), `"done":true`) {
		t.Errorf("finished: %s", rec.Body)
	}
	if rec := serveGit(t, f, "POST", "/git/identities/setup/complete", `{"params":{}}`, &actor); rec.Code != 400 {
		t.Errorf("state required: %d", rec.Code)
	}
	f.err = gitdom.ErrSetupExpired
	if rec := serveGit(t, f, "POST", "/git/identities/setup/complete", `{"state":"st"}`, &actor); rec.Code != 400 || errCode(rec) != "GIT_SETUP_INVALID" {
		t.Errorf("expired: %d %s", rec.Code, errCode(rec))
	}
}

func TestGitHandler_SetupErrors(t *testing.T) {
	actor := uuid.New()
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{gitdom.ErrIdentityExists, 409, "GIT_IDENTITY_EXISTS"},
		{gitdom.ErrPublicURLRequired, 422, "GIT_PUBLIC_URL_REQUIRED"},
		{gitdom.ErrInvalidReturnURL, 400, "BAD_REQUEST"},
		{&gitprovider.Error{Code: gitprovider.CodeUnsupported}, 400, "BAD_REQUEST"},
	}
	for _, c := range cases {
		rec := serveGit(t, &fakeGitSvc{err: c.err}, "POST", "/git/identities/setup", `{"provider_id":"p"}`, &actor)
		if rec.Code != c.status || errCode(rec) != c.code {
			t.Errorf("%v: %d %s, want %d %s", c.err, rec.Code, errCode(rec), c.status, c.code)
		}
	}
}

func TestGitHandler_ListAndDeleteIdentities(t *testing.T) {
	actor := uuid.New()
	f := &fakeGitSvc{providers: nil, identities: []*gitdom.Identity{{ID: uuid.New(), Handle: "bot", ProviderRef: json.RawMessage(`{"k":"v"}`)}}}
	if rec := serveGit(t, f, "GET", "/git/providers", "", &actor); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"data":[]`) && !strings.Contains(rec.Body.String(), "[]") {
		t.Errorf("no providers is an empty list: %d %s", rec.Code, rec.Body)
	}
	rec := serveGit(t, f, "GET", "/git/identities", "", &actor)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"handle":"bot"`) || strings.Contains(rec.Body.String(), `"k":"v"`) {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}
	id := uuid.New()
	if rec := serveGit(t, f, "DELETE", "/git/identities/"+id.String(), "", &actor); rec.Code != 204 || f.deletedID != id {
		t.Errorf("delete: %d %v", rec.Code, f.deletedID)
	}
	if rec := serveGit(t, f, "DELETE", "/git/identities/not-a-uuid", "", &actor); rec.Code != 400 {
		t.Errorf("bad id: %d", rec.Code)
	}
}

func TestGitHandler_AgentIdentityStaysWithinItsProject(t *testing.T) {
	actor, project, agent := uuid.New(), uuid.New(), uuid.New()
	old := agentLookup
	agentLookup = fakeAgents{project: project, agent: agent}
	defer func() { agentLookup = old }()
	base := "/projects/" + project.String() + "/agents/" + agent.String() + "/git/identity"

	f := &fakeGitSvc{step: &gitprovider.SetupStep{Kind: gitprovider.StepRedirect, URL: "u", State: "s"}}
	if rec := serveGit(t, f, "POST", base+"/setup", `{"provider_id":"p"}`, &actor); rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if f.setupIn.Scope != gitdom.ScopeAgent || f.setupIn.AgentID == nil || *f.setupIn.AgentID != agent || f.setupIn.AgentName != "Dev" {
		t.Errorf("input = %+v", f.setupIn)
	}
	if rec := serveGit(t, f, "GET", base, "", &actor); rec.Code != 200 || f.agentIDs != agent {
		t.Errorf("list: %d", rec.Code)
	}
	if rec := serveGit(t, f, "DELETE", base+"?provider_id=p", "", &actor); rec.Code != 204 || f.removed != [2]string{agent.String(), "p"} {
		t.Errorf("delete: %d %v", rec.Code, f.removed)
	}
	if rec := serveGit(t, f, "DELETE", base, "", &actor); rec.Code != 400 {
		t.Errorf("provider_id required: %d", rec.Code)
	}

	// The same agent id under another project's URL is not that project's agent.
	other := "/projects/" + uuid.NewString() + "/agents/" + agent.String() + "/git/identity"
	g := &fakeGitSvc{step: f.step}
	for _, rec := range []*httptest.ResponseRecorder{
		serveGit(t, g, "GET", other, "", &actor),
		serveGit(t, g, "POST", other+"/setup", `{"provider_id":"p"}`, &actor),
		serveGit(t, g, "DELETE", other+"?provider_id=p", "", &actor),
	} {
		if rec.Code != 404 {
			t.Errorf("cross-project agent: %d %s", rec.Code, rec.Body)
		}
	}
	if g.setupIn.ProviderID != "" || g.agentIDs != uuid.Nil || g.removed[0] != "" {
		t.Error("nothing may reach the service for an agent outside the project")
	}
}

func TestGitHandler_AccessGrant(t *testing.T) {
	actor, project := uuid.New(), uuid.New()
	base := "/projects/" + project.String() + "/git/access/"
	f := &fakeGitSvc{step: &gitprovider.SetupStep{Kind: gitprovider.StepRedirect, URL: "https://github.com/apps/x/installations/new", State: "g1"}}

	rec := serveGit(t, f, "POST", base+"begin", `{"provider_id":" com.paca.github ","owner":" acme ","return_url":"https://paca.example.com/p"}`, &actor)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"state":"g1"`) {
		t.Fatalf("begin: %d %s", rec.Code, rec.Body)
	}
	if in := f.accessIn; in.ProjectID != project || in.ActorID != actor || in.ProviderID != "com.paca.github" || in.Owner != "acme" || in.ReturnURL != "https://paca.example.com/p" {
		t.Fatalf("input = %+v", in)
	}
	if rec := serveGit(t, f, "POST", base+"begin", `{"owner":"acme"}`, &actor); rec.Code != 400 {
		t.Errorf("provider required: %d", rec.Code)
	}
	if rec := serveGit(t, f, "POST", base+"begin", `{"provider_id":"p"}`, nil); rec.Code != 401 {
		t.Errorf("unauthenticated: %d", rec.Code)
	}

	f.accessRes = &gitsvc.AccessCompletion{Owner: "acme", ReturnURL: "https://paca.example.com/p"}
	rec = serveGit(t, f, "POST", base+"complete", `{"state":" g1 ","params":{"installation_id":"9"}}`, &actor)
	if rec.Code != 200 || f.accessBy != [2]uuid.UUID{actor, project} || f.accessSt != "g1" || f.accessP["installation_id"] != "9" {
		t.Fatalf("complete: %d %s by=%v st=%q", rec.Code, rec.Body, f.accessBy, f.accessSt)
	}
	if !strings.Contains(rec.Body.String(), `"owner":"acme"`) {
		t.Errorf("owner missing: %s", rec.Body)
	}
	if rec := serveGit(t, f, "POST", base+"complete", `{"params":{}}`, &actor); rec.Code != 400 {
		t.Errorf("state required: %d", rec.Code)
	}
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{gitdom.ErrSetupExpired, 400, "GIT_SETUP_INVALID"},
		{gitdom.ErrNoIdentity, 409, "GIT_NO_IDENTITY"},
		{gitdom.ErrAccessNotGranted, 409, "GIT_ACCESS_NOT_GRANTED"},
		{&gitprovider.Error{Code: gitprovider.CodeConflict}, 422, "GIT_PROVIDER_REJECTED"},
		{&gitprovider.Error{Code: gitprovider.CodeUnsupported}, 400, "BAD_REQUEST"},
	} {
		g := &fakeGitSvc{err: c.err}
		if rec := serveGit(t, g, "POST", base+"complete", `{"state":"s"}`, &actor); rec.Code != c.status || errCode(rec) != c.code {
			t.Errorf("%v: %d %s, want %d %s", c.err, rec.Code, errCode(rec), c.status, c.code)
		}
	}
}

func TestGitHandler_LinkAccessNotGrantedIsAConflict(t *testing.T) {
	actor, project := uuid.New(), uuid.NewString()
	rec := serveGit(t, &fakeGitSvc{err: gitdom.ErrAccessNotGranted}, "POST", "/projects/"+project+"/git/repositories", `{"provider_id":"p","path_or_url":"a/b"}`, &actor)
	if rec.Code != 409 || errCode(rec) != "GIT_ACCESS_NOT_GRANTED" {
		t.Errorf("%d %s", rec.Code, errCode(rec))
	}
}

func TestGitHandler_AgentAccessGrant(t *testing.T) {
	actor, project, agent := uuid.New(), uuid.New(), uuid.New()
	old := agentLookup
	agentLookup = fakeAgents{project: project, agent: agent}
	defer func() { agentLookup = old }()
	base := "/projects/" + project.String() + "/agents/" + agent.String() + "/git/identity/access/begin"
	f := &fakeGitSvc{step: &gitprovider.SetupStep{Kind: gitprovider.StepRedirect, URL: "https://github.com/apps/x/installations/new", State: "ag1"}}

	rec := serveGit(t, f, "POST", base, `{"provider_id":" com.paca.github ","owner":" acme ","return_url":"https://paca.example.com/a"}`, &actor)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"state":"ag1"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if in := f.accessIn; in.AgentID == nil || *in.AgentID != agent || in.ProjectID != project || in.ActorID != actor || in.ProviderID != "com.paca.github" || in.Owner != "acme" {
		t.Fatalf("input = %+v", in)
	}
	if rec := serveGit(t, f, "POST", base, `{"owner":"acme"}`, &actor); rec.Code != 400 {
		t.Errorf("provider required: %d", rec.Code)
	}
	// An agent that is not this project's never reaches the service.
	g := &fakeGitSvc{step: f.step}
	other := "/projects/" + uuid.NewString() + "/agents/" + agent.String() + "/git/identity/access/begin"
	if rec := serveGit(t, g, "POST", other, `{"provider_id":"p"}`, &actor); rec.Code != 404 || g.accessIn.ProviderID != "" {
		t.Errorf("cross-project agent: %d %+v", rec.Code, g.accessIn)
	}
	g.err = gitdom.ErrNoIdentity
	if rec := serveGit(t, g, "POST", base, `{"provider_id":"p"}`, &actor); rec.Code != 409 || errCode(rec) != "GIT_NO_IDENTITY" {
		t.Errorf("agent without an identity: %d %s", rec.Code, errCode(rec))
	}
}

func TestGitHandler_WebhookRefusesAnUnknownEndpointBeforeReadingTheBody(t *testing.T) {
	f := &fakeGitSvc{checkErr: gitdom.ErrNotFound}
	r := chi.NewRouter()
	r.Post("/git/webhooks/{endpointKey}", NewGitHandler(f).ReceiveWebhook)
	body := &countingReader{r: strings.NewReader(strings.Repeat("x", 1024))}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "POST", "/git/webhooks/nope", body))
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
	if body.n != 0 {
		t.Fatalf("the body was read (%d bytes) for an unknown endpoint", body.n)
	}
	if f.key != "" {
		t.Fatalf("the delivery reached the service: key=%q", f.key)
	}
}

type countingReader struct {
	r interface{ Read([]byte) (int, error) }
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestGitHandler_ProjectConnections(t *testing.T) {
	actor, project := uuid.New(), uuid.New()
	base := "/projects/" + project.String() + "/git/connections"
	f := &fakeGitSvc{step: &gitprovider.SetupStep{Kind: gitprovider.StepTokenForm, State: "st"},
		identities: []*gitdom.Identity{{ID: uuid.New(), Handle: "bot", Scope: gitdom.ScopeProject, ProjectID: &project, ProviderRef: json.RawMessage(`{"k":"v"}`)}}}

	rec := serveGit(t, f, "GET", base, "", &actor)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"scope":"project"`) || strings.Contains(rec.Body.String(), `"k":"v"`) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}

	rec = serveGit(t, f, "POST", base+"/setup", `{"provider_id":"p","host":"github.com","params":{"kind":"token"}}`, &actor)
	if rec.Code != 201 {
		t.Fatalf("begin: %d %s", rec.Code, rec.Body)
	}
	if f.setupIn.Scope != gitdom.ScopeProject || f.setupIn.ProjectID == nil || *f.setupIn.ProjectID != project || f.setupIn.Params["kind"] != "token" {
		t.Fatalf("setup input = %+v", f.setupIn)
	}

	f.completion = &gitsvc.SetupCompletion{Identity: f.identities[0]}
	if rec := serveGit(t, f, "POST", base+"/setup/complete", `{"state":"st"}`, &actor); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"done":true`) {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body)
	}

	if rec := serveGit(t, f, "DELETE", base+"/"+f.identities[0].ID.String(), "", &actor); rec.Code != 204 || f.deletedID != f.identities[0].ID {
		t.Fatalf("delete: %d", rec.Code)
	}
	f.err = gitdom.ErrNotFound
	if rec := serveGit(t, f, "DELETE", base+"/"+uuid.NewString(), "", &actor); rec.Code != 404 {
		t.Fatalf("a connection that is not the project's: %d", rec.Code)
	}
	f.err = nil
	if rec := serveGit(t, f, "POST", "/projects/"+project.String()+"/git/repositories/"+uuid.NewString()+"/webhook", "", &actor); rec.Code != 200 {
		t.Fatalf("register webhook: %d %s", rec.Code, rec.Body)
	}
}

func TestGitHandler_ReconnectRepository(t *testing.T) {
	actor, project, repo, conn := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f := &fakeGitSvc{}
	url := "/projects/" + project.String() + "/git/repositories/" + repo.String() + "/reconnect"
	if rec := serveGit(t, f, "POST", url, `{"identity_id":"`+conn.String()+`"}`, &actor); rec.Code != 200 {
		t.Fatalf("reconnect: %d %s", rec.Code, rec.Body)
	}
	if f.reconnect.ProjectID != project || f.reconnect.RepositoryID != repo || f.reconnect.IdentityID == nil || *f.reconnect.IdentityID != conn {
		t.Fatalf("input = %+v", f.reconnect)
	}
	f.err = gitdom.ErrIdentityRequired
	if rec := serveGit(t, f, "POST", url, `{}`, &actor); rec.Code != 409 || errCode(rec) != "GIT_IDENTITY_REQUIRED" {
		t.Fatalf("several connections: %d %s", rec.Code, errCode(rec))
	}
}

func TestGitHandler_ProviderMessageIsSurfacedAsDetails(t *testing.T) {
	actor, project := uuid.New(), uuid.New().String()
	post := func(err error) *httptest.ResponseRecorder {
		return serveGit(t, &fakeGitSvc{err: err}, "POST", "/projects/"+project+"/git/repositories/"+uuid.New().String()+"/webhook", "", &actor)
	}
	details := func(rec *httptest.ResponseRecorder) map[string]string {
		var env struct {
			ErrorDetails map[string]string `json:"error_details"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		return env.ErrorDetails
	}

	// A webhook the platform refused: same code, plus what the platform said.
	rec := post(&gitprovider.Error{Code: gitprovider.CodeForbidden, Message: "the token needs the Maintainer role"})
	if rec.Code != 422 || errCode(rec) != "GIT_PROVIDER_REJECTED" || details(rec)["provider_message"] != "the token needs the Maintainer role" {
		t.Fatalf("forbidden: %d %s", rec.Code, rec.Body)
	}
	// A repository no connection sees: wrapped sentinel keeps its code.
	rec = post(gitdom.WithDetail(gitdom.ErrNotVisible, "The connected project token covers only acme/ui."))
	if rec.Code != 409 || errCode(rec) != "GIT_REPOSITORY_NOT_VISIBLE" || details(rec)["provider_message"] != "The connected project token covers only acme/ui." {
		t.Fatalf("not visible: %d %s", rec.Code, rec.Body)
	}
	// Secrets in the message are masked, an overlong one is capped.
	rec = post(&gitprovider.Error{Code: gitprovider.CodeForbidden, Message: "bad token ghp_" + strings.Repeat("a", 30) + " " + strings.Repeat("x", 900)})
	msg := details(rec)["provider_message"]
	if strings.Contains(msg, "ghp_") || len([]rune(msg)) > 501 {
		t.Fatalf("not redacted or capped: %q", msg)
	}
	// Webhook failures where the fixed text would be wrong carry the platform's reason too.
	for _, pe := range []*gitprovider.Error{
		{Code: gitprovider.CodeValidation, Message: "Invalid url given"},
		{Code: gitprovider.CodeUnsupported, Message: "this instance has no public URL, so the platform cannot reach it"},
	} {
		if rec := post(pe); details(rec)["provider_message"] != pe.Message {
			t.Fatalf("%s: %d %s", pe.Code, rec.Code, rec.Body)
		}
	}
	// No message, no details; internal errors still leak nothing.
	for _, err := range []error{&gitprovider.Error{Code: gitprovider.CodeForbidden}, gitdom.ErrNotVisible, gitdom.WithDetail(gitdom.ErrNotVisible, "")} {
		if rec := post(err); strings.Contains(rec.Body.String(), "error_details") {
			t.Fatalf("details without a message: %s", rec.Body)
		}
	}
	if rec := post(&gitprovider.Error{Code: gitprovider.CodeInternal, Message: "secret detail"}); strings.Contains(rec.Body.String(), "secret detail") {
		t.Fatalf("internal leaked: %s", rec.Body)
	}
}
