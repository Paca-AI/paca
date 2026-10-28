package gitsvc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

const testPublic = "https://paca.example.com"

func setupSvc(t *testing.T) (*Service, *fakeStore, *fakeProv) {
	t.Helper()
	svc, st, pr := setup(t)
	svc.WithPublicURL(testPublic + "/")
	pr.info = gitprovider.Info{ID: "p", Hosts: []string{"git.example"}}
	pr.begin = func(r gitprovider.BeginSetupRequest) (gitprovider.SetupStep, error) {
		return gitprovider.SetupStep{Kind: gitprovider.StepFormPost, URL: "https://git.example/new", State: "state-1"}, nil
	}
	return svc, st, pr
}

func shared(actor uuid.UUID) SetupInput {
	return SetupInput{ProviderID: "p", Scope: gitdom.ScopeShared, ActorID: actor, ReturnURL: testPublic + "/settings"}
}

func TestBeginIdentitySetup_ReservesEndpointAndTellsTheProviderWhere(t *testing.T) {
	svc, st, pr := setupSvc(t)
	var got gitprovider.BeginSetupRequest
	inner := pr.begin
	pr.begin = func(r gitprovider.BeginSetupRequest) (gitprovider.SetupStep, error) { got = r; return inner(r) }
	actor := uuid.New()

	step, err := svc.BeginIdentitySetup(context.Background(), shared(actor))
	if err != nil || step.State != "state-1" {
		t.Fatalf("%+v %v", step, err)
	}
	if got.Host != "git.example" || got.Scope != gitprovider.SetupShared || got.CallbackURL != testPublic+"/git/setup/callback" {
		t.Errorf("request = %+v", got)
	}
	if !strings.HasPrefix(got.WebhookURL, testPublic+"/api/v1/git/webhooks/") {
		t.Fatalf("webhook url = %q", got.WebhookURL)
	}
	key := strings.TrimPrefix(got.WebhookURL, testPublic+"/api/v1/git/webhooks/")
	if len(key) < 32 {
		t.Errorf("endpoint key %q is too guessable", key)
	}
	ep := st.endpoints[key]
	if ep == nil || ep.IdentityID != nil || ep.ProviderID != "p" {
		t.Fatalf("endpoint = %+v", ep)
	}
	s := st.sessions["state-1"]
	if s == nil || s.CreatedBy != actor || s.EndpointKey != key || s.Scope != gitdom.ScopeShared || !s.ExpiresAt.After(time.Now()) {
		t.Fatalf("session = %+v", s)
	}
}

func TestBeginIdentitySetup_Refusals(t *testing.T) {
	ctx := context.Background()
	actor := uuid.New()

	svc, st, _ := setupSvc(t)
	noURL := New(st, fakeProviders{p: &fakeProv{}})
	if _, err := noURL.BeginIdentitySetup(ctx, shared(actor)); !errors.Is(err, gitdom.ErrPublicURLRequired) {
		t.Errorf("no public url: %v", err)
	}
	in := shared(actor)
	in.ReturnURL = "https://evil.example/steal"
	if _, err := svc.BeginIdentitySetup(ctx, in); !errors.Is(err, gitdom.ErrInvalidReturnURL) {
		t.Errorf("open redirect: %v", err)
	}
	in.ReturnURL = "/relative"
	if _, err := svc.BeginIdentitySetup(ctx, in); !errors.Is(err, gitdom.ErrInvalidReturnURL) {
		t.Errorf("relative return url: %v", err)
	}
	in = shared(actor)
	in.Scope = "everyone"
	if _, err := svc.BeginIdentitySetup(ctx, in); err == nil {
		t.Error("bad scope")
	}
	in = shared(actor)
	in.Scope = gitdom.ScopeAgent
	if _, err := svc.BeginIdentitySetup(ctx, in); err == nil {
		t.Error("an agent identity needs an agent")
	}

	st.addIdentity("bot", "app-1") // a shared identity exists on git.example; more may be added
	if _, err := svc.BeginIdentitySetup(ctx, shared(actor)); err != nil {
		t.Errorf("second shared connection on the host: %v", err)
	}
	in = shared(actor)
	in.Scope = gitdom.ScopeProject
	if _, err := svc.BeginIdentitySetup(ctx, in); err == nil {
		t.Error("a project connection needs a project")
	}
	st.endpoints = map[string]*gitdom.WebhookEndpoint{}
	agent := uuid.New()
	st.agentIDs[agent] = &gitdom.Identity{}
	in = shared(actor)
	in.Scope, in.AgentID = gitdom.ScopeAgent, &agent
	if _, err := svc.BeginIdentitySetup(ctx, in); !errors.Is(err, gitdom.ErrIdentityExists) {
		t.Errorf("agent already has one: %v", err)
	}
	if len(st.endpoints) != 0 {
		t.Errorf("refused attempts must not leave endpoints behind: %d", len(st.endpoints))
	}
}

func TestBeginIdentitySetup_ProviderFailureLeavesNothingBehind(t *testing.T) {
	svc, st, pr := setupSvc(t)
	pr.begin = func(gitprovider.BeginSetupRequest) (gitprovider.SetupStep, error) {
		return gitprovider.SetupStep{}, &gitprovider.Error{Code: gitprovider.CodeValidation}
	}
	if _, err := svc.BeginIdentitySetup(context.Background(), shared(uuid.New())); !gitprovider.IsCode(err, gitprovider.CodeValidation) {
		t.Fatalf("got %v", err)
	}
	if len(st.endpoints) != 0 || len(st.sessions) != 0 {
		t.Errorf("endpoints=%d sessions=%d", len(st.endpoints), len(st.sessions))
	}
	pr.begin = func(gitprovider.BeginSetupRequest) (gitprovider.SetupStep, error) {
		return gitprovider.SetupStep{Kind: gitprovider.StepRedirect, URL: "x"}, nil // no state
	}
	if _, err := svc.BeginIdentitySetup(context.Background(), shared(uuid.New())); err == nil || len(st.endpoints) != 0 {
		t.Errorf("a step without state is unusable: %v", err)
	}
}

func twoLegProvider(pr *fakeProv) {
	id := gitprovider.Identity{Ref: gitprovider.IdentityRef{ID: "app-9", Handle: "paca-bot"}, Host: "git.example", Kind: gitprovider.IdentityApp, DisplayName: "Paca"}
	pr.complete = func(r gitprovider.CompleteSetupRequest) (gitprovider.Identity, error) {
		out := id
		if r.Params["code"] != "" {
			out.Next = &gitprovider.SetupStep{Kind: gitprovider.StepRedirect, URL: "https://git.example/install", State: r.State}
		}
		return out, nil
	}
}

func TestCompleteIdentitySetup_SharedTwoLegs(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setupSvc(t)
	twoLegProvider(pr)
	actor := uuid.New()
	if _, err := svc.BeginIdentitySetup(ctx, shared(actor)); err != nil {
		t.Fatal(err)
	}

	first, err := svc.CompleteIdentitySetup(ctx, actor, nil, "state-1", map[string]string{"code": "c"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Next == nil || first.Next.URL != "https://git.example/install" || first.Identity.Handle != "paca-bot" || first.Identity.Scope != gitdom.ScopeShared {
		t.Fatalf("leg 1 = %+v", first)
	}
	if len(st.byID) != 1 || st.sessions["state-1"].CompletedAt != nil {
		t.Fatalf("the flow is not finished after leg 1: identities=%d", len(st.byID))
	}
	for _, e := range st.endpoints {
		if e.IdentityID == nil || *e.IdentityID != first.Identity.ID {
			t.Errorf("endpoint not bound to the new identity: %+v", e)
		}
	}

	second, err := svc.CompleteIdentitySetup(ctx, actor, nil, "state-1", map[string]string{"installation_id": "1"})
	if err != nil || second.Next != nil || second.Identity.ID != first.Identity.ID || second.ReturnURL != testPublic+"/settings" {
		t.Fatalf("leg 2 = %+v %v", second, err)
	}
	if len(st.byID) != 1 {
		t.Errorf("the identity must be recorded once, got %d", len(st.byID))
	}
	if st.sessions["state-1"].CompletedAt == nil {
		t.Error("session should be marked finished")
	}
	if _, err := svc.CompleteIdentitySetup(ctx, actor, nil, "state-1", map[string]string{"installation_id": "1"}); !errors.Is(err, gitdom.ErrSetupExpired) {
		t.Errorf("a finished state is reusable: %v", err)
	}
}

func TestCompleteIdentitySetup_AgentIdentityIsMapped(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setupSvc(t)
	twoLegProvider(pr)
	actor, agent := uuid.New(), uuid.New()
	in := shared(actor)
	in.Scope, in.AgentID, in.AgentName = gitdom.ScopeAgent, &agent, "Dev"
	var got gitprovider.BeginSetupRequest
	pr.begin = func(r gitprovider.BeginSetupRequest) (gitprovider.SetupStep, error) {
		got = r
		return gitprovider.SetupStep{Kind: gitprovider.StepRedirect, URL: "u", State: "agent-state"}, nil
	}
	if _, err := svc.BeginIdentitySetup(ctx, in); err != nil {
		t.Fatal(err)
	}
	if got.AgentID != agent.String() || got.AgentName != "Dev" || got.Scope != gitprovider.SetupAgent {
		t.Errorf("request = %+v", got)
	}
	res, err := svc.CompleteIdentitySetup(ctx, actor, nil, "agent-state", map[string]string{"installation_id": "1"})
	if err != nil || res.Identity.Scope != gitdom.ScopeAgent {
		t.Fatalf("%+v %v", res, err)
	}
	if st.agentIDs[agent] == nil || st.agentIDs[agent].ID != res.Identity.ID {
		t.Error("the agent must be mapped to its identity")
	}
	if len(st.shared) != 0 {
		t.Error("an agent identity must not become the host's shared identity")
	}
}

func TestCompleteIdentitySetup_Guards(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setupSvc(t)
	twoLegProvider(pr)
	actor := uuid.New()
	_, _ = svc.BeginIdentitySetup(ctx, shared(actor))

	if _, err := svc.CompleteIdentitySetup(ctx, uuid.New(), nil, "state-1", map[string]string{"code": "c"}); !errors.Is(err, gitdom.ErrSetupExpired) {
		t.Errorf("another user: %v", err)
	}
	if _, err := svc.CompleteIdentitySetup(ctx, actor, nil, "nope", nil); !errors.Is(err, gitdom.ErrSetupExpired) {
		t.Errorf("unknown state: %v", err)
	}
	st.sessions["state-1"].ExpiresAt = time.Now().Add(-time.Second)
	if _, err := svc.CompleteIdentitySetup(ctx, actor, nil, "state-1", map[string]string{"code": "c"}); !errors.Is(err, gitdom.ErrSetupExpired) {
		t.Errorf("expired: %v", err)
	}
	if len(st.byID) != 0 {
		t.Error("nothing may be created for a refused completion")
	}

	st.sessions["state-1"].ExpiresAt = time.Now().Add(time.Hour)
	pr.complete = func(gitprovider.CompleteSetupRequest) (gitprovider.Identity, error) {
		return gitprovider.Identity{}, &gitprovider.Error{Code: gitprovider.CodeForbidden}
	}
	if _, err := svc.CompleteIdentitySetup(ctx, actor, nil, "state-1", nil); !gitprovider.IsCode(err, gitprovider.CodeForbidden) {
		t.Errorf("provider refusal: %v", err)
	}
	if st.sessions["state-1"].CompletedAt != nil {
		t.Error("a failed leg must leave the session open for a retry")
	}
}

func TestBeginIdentitySetup_ImmediateIdentity(t *testing.T) {
	svc, st, pr := setupSvc(t)
	pr.begin = func(gitprovider.BeginSetupRequest) (gitprovider.SetupStep, error) {
		return gitprovider.SetupStep{Kind: gitprovider.StepDone, Identity: &gitprovider.Identity{
			Ref: gitprovider.IdentityRef{ID: "tok-1", Handle: "svc"}, Host: "git.example", Kind: gitprovider.IdentityToken, DisplayName: "svc"}}, nil
	}
	step, err := svc.BeginIdentitySetup(context.Background(), shared(uuid.New()))
	if err != nil || step.Kind != gitprovider.StepDone || len(st.byID) != 1 || len(st.sessions) != 0 {
		t.Fatalf("%+v %v identities=%d sessions=%d", step, err, len(st.byID), len(st.sessions))
	}
	for _, e := range st.endpoints {
		if e.IdentityID == nil {
			t.Error("the endpoint must be bound")
		}
	}
}

func TestDeleteIdentity(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setupSvc(t)
	id := st.addIdentity("bot", "app-1")

	if err := svc.DeleteIdentity(ctx, id.ID); err != nil {
		t.Fatal(err)
	}
	if len(pr.deleted) != 1 || pr.deleted[0] != "app-1" || len(st.byID) != 0 {
		t.Errorf("provider asked to forget %v, store has %d", pr.deleted, len(st.byID))
	}
	if err := svc.DeleteIdentity(ctx, uuid.New()); !errors.Is(err, gitdom.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}

	// A provider that does not support deletion, or already forgot it, does not block removal.
	for _, code := range []gitprovider.ErrorCode{gitprovider.CodeUnsupported, gitprovider.CodeNotFound} {
		id := st.addIdentity("bot", "again-"+string(code))
		pr.deleteErr = &gitprovider.Error{Code: code}
		if err := svc.DeleteIdentity(ctx, id.ID); err != nil {
			t.Errorf("%s: %v", code, err)
		}
	}
	// A transient failure does: the platform-side identity would be left behind.
	id = st.addIdentity("bot", "transient")
	pr.deleteErr = &gitprovider.Error{Code: gitprovider.CodeTransient}
	if err := svc.DeleteIdentity(ctx, id.ID); !gitprovider.IsCode(err, gitprovider.CodeTransient) || st.byID[id.ID] == nil {
		t.Errorf("transient: %v", err)
	}
	// A missing plugin does not trap the operator with an undeletable record.
	down := New(st, fakeProviders{err: gitdom.ErrProviderUnavailable})
	if err := down.DeleteIdentity(ctx, id.ID); err != nil || st.byID[id.ID] != nil {
		t.Errorf("provider down: %v", err)
	}
}

func TestRemoveAgentIdentity(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setupSvc(t)
	agent := uuid.New()
	own := &gitdom.Identity{ID: uuid.New(), ProviderID: "p", ProviderIdentityID: "agent-app", Scope: gitdom.ScopeAgent}
	st.byID[own.ID] = own
	st.agentIDs[agent] = own

	if err := svc.RemoveAgentIdentity(ctx, agent, "p"); err != nil {
		t.Fatal(err)
	}
	if st.agentIDs[agent] != nil || st.byID[own.ID] != nil || len(pr.deleted) != 1 {
		t.Error("the mapping and the identity should both be gone")
	}
	if err := svc.RemoveAgentIdentity(ctx, agent, "p"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Errorf("none left: %v", err)
	}
	// An agent mapped to a shared identity only loses the mapping.
	sh := st.addIdentity("bot", "shared-app")
	st.agentIDs[agent] = sh
	if err := svc.RemoveAgentIdentity(ctx, agent, "p"); err != nil || st.byID[sh.ID] == nil {
		t.Errorf("the shared identity must survive: %v", err)
	}
}

func TestListings(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setupSvc(t)
	st.addIdentity("bot", "app-1")
	infos, err := svc.ListProviders(ctx)
	if err != nil || len(infos) != 1 {
		t.Fatalf("%v %v", infos, err)
	}
	ids, err := svc.ListIdentities(ctx)
	if err != nil || len(ids) != 1 {
		t.Fatalf("%v %v", ids, err)
	}
	agent := uuid.New()
	if l, _ := svc.ListAgentIdentities(ctx, agent); len(l) != 0 {
		t.Error("none yet")
	}
	st.agentIDs[agent] = st.shared[0]
	if l, _ := svc.ListAgentIdentities(ctx, agent); len(l) != 1 {
		t.Error("one mapped")
	}
}
