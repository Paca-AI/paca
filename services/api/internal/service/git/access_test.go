package gitsvc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

func accessSvc(t *testing.T) (*Service, *fakeStore, *fakeProv, *gitdom.Identity) {
	t.Helper()
	svc, st, pr := setup(t)
	svc.WithPublicURL(testPublic)
	pr.info = gitprovider.Info{ID: "p", Hosts: []string{"git.example"}, Capabilities: gitprovider.Capabilities{AccessGrants: true}}
	ident := st.addIdentity("bot", "app-1")
	pr.beginAccess = func(r gitprovider.BeginAccessRequest) (gitprovider.SetupStep, error) {
		return gitprovider.SetupStep{Kind: gitprovider.StepRedirect, URL: "https://git.example/install", State: "grant-1"}, nil
	}
	pr.completeAccess = func(r gitprovider.CompleteAccessRequest) (gitprovider.AccessGrant, error) {
		return gitprovider.AccessGrant{Owner: "acme"}, nil
	}
	return svc, st, pr, ident
}

func accessIn(project, actor uuid.UUID) AccessInput {
	return AccessInput{ProjectID: project, ProviderID: "p", Owner: " acme ", ActorID: actor, ReturnURL: testPublic + "/projects/x"}
}

func TestBeginAccessGrant_BindsTheStateToUserAndProject(t *testing.T) {
	svc, st, pr, ident := accessSvc(t)
	var got gitprovider.BeginAccessRequest
	inner := pr.beginAccess
	pr.beginAccess = func(r gitprovider.BeginAccessRequest) (gitprovider.SetupStep, error) { got = r; return inner(r) }
	project, actor := uuid.New(), uuid.New()

	step, err := svc.BeginAccessGrant(context.Background(), accessIn(project, actor))
	if err != nil || step.State != "grant-1" {
		t.Fatalf("%+v %v", step, err)
	}
	if got.Identity.ID != "app-1" || got.Owner != "acme" || got.CallbackURL != testPublic+"/git/setup/callback" || got.ReturnURL != testPublic+"/projects/x" {
		t.Errorf("request = %+v", got)
	}
	s := st.access["grant-1"]
	if s == nil || s.CreatedBy != actor || s.ProjectID != project || s.IdentityID != ident.ID || s.Owner != "acme" || !s.ExpiresAt.After(time.Now()) {
		t.Fatalf("session = %+v", s)
	}
}

func TestBeginAccessGrant_Refusals(t *testing.T) {
	ctx := context.Background()
	project, actor := uuid.New(), uuid.New()

	svc, st, pr, _ := accessSvc(t)
	if _, err := New(st, fakeProviders{p: pr}).BeginAccessGrant(ctx, accessIn(project, actor)); !errors.Is(err, gitdom.ErrPublicURLRequired) {
		t.Errorf("no public url: %v", err)
	}
	in := accessIn(project, actor)
	in.ReturnURL = "https://evil.example/x"
	if _, err := svc.BeginAccessGrant(ctx, in); !errors.Is(err, gitdom.ErrInvalidReturnURL) {
		t.Errorf("open redirect: %v", err)
	}
	pr.info.Capabilities.AccessGrants = false
	if _, err := svc.BeginAccessGrant(ctx, accessIn(project, actor)); !gitprovider.IsCode(err, gitprovider.CodeUnsupported) {
		t.Errorf("provider without grants: %v", err)
	}
	pr.info.Capabilities.AccessGrants = true
	st.shared, st.byRef = nil, map[string]*gitdom.Identity{}
	if _, err := svc.BeginAccessGrant(ctx, accessIn(project, actor)); !errors.Is(err, gitdom.ErrNoIdentity) {
		t.Errorf("no identity yet: %v", err)
	}
	if len(st.access) != 0 {
		t.Error("refused attempts must not leave sessions behind")
	}

	st.addIdentity("bot", "app-2")
	pr.beginAccess = func(gitprovider.BeginAccessRequest) (gitprovider.SetupStep, error) {
		return gitprovider.SetupStep{Kind: gitprovider.StepRedirect, URL: "u"}, nil // no state
	}
	if _, err := svc.BeginAccessGrant(ctx, accessIn(project, actor)); err == nil || len(st.access) != 0 {
		t.Errorf("a step without state is unusable: %v", err)
	}
	pr.beginAccess = func(gitprovider.BeginAccessRequest) (gitprovider.SetupStep, error) {
		return gitprovider.SetupStep{}, &gitprovider.Error{Code: gitprovider.CodeNotFound}
	}
	if _, err := svc.BeginAccessGrant(ctx, accessIn(project, actor)); !gitprovider.IsCode(err, gitprovider.CodeNotFound) {
		t.Errorf("provider error: %v", err)
	}
}

func TestCompleteAccessGrant(t *testing.T) {
	ctx := context.Background()
	svc, st, pr, _ := accessSvc(t)
	project, actor := uuid.New(), uuid.New()
	_, _ = svc.BeginAccessGrant(ctx, accessIn(project, actor))
	params := map[string]string{"installation_id": "9"}
	var got gitprovider.CompleteAccessRequest
	inner := pr.completeAccess
	pr.completeAccess = func(r gitprovider.CompleteAccessRequest) (gitprovider.AccessGrant, error) { got = r; return inner(r) }

	// Another user, another project, an unknown state: all the same answer.
	for name, fn := range map[string]func() error{
		"another user": func() error {
			_, err := svc.CompleteAccessGrant(ctx, uuid.New(), project, "grant-1", params)
			return err
		},
		"another project": func() error { _, err := svc.CompleteAccessGrant(ctx, actor, uuid.New(), "grant-1", params); return err },
		"unknown state":   func() error { _, err := svc.CompleteAccessGrant(ctx, actor, project, "nope", params); return err },
	} {
		if err := fn(); !errors.Is(err, gitdom.ErrSetupExpired) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got.State != "" {
		t.Fatal("the provider must not be called for a refused completion")
	}

	res, err := svc.CompleteAccessGrant(ctx, actor, project, "grant-1", params)
	if err != nil || res.Owner != "acme" || res.ReturnURL != testPublic+"/projects/x" {
		t.Fatalf("%+v %v", res, err)
	}
	if got.State != "grant-1" || got.Params["installation_id"] != "9" || got.Identity.ID != "app-1" {
		t.Errorf("provider request = %+v", got)
	}
	if st.access["grant-1"].CompletedAt == nil {
		t.Error("session not marked finished")
	}
	if _, err := svc.CompleteAccessGrant(ctx, actor, project, "grant-1", params); !errors.Is(err, gitdom.ErrSetupExpired) {
		t.Errorf("a finished state is reusable: %v", err)
	}
}

func TestCompleteAccessGrant_ExpiryAndProviderFailure(t *testing.T) {
	ctx := context.Background()
	svc, st, pr, _ := accessSvc(t)
	project, actor := uuid.New(), uuid.New()
	_, _ = svc.BeginAccessGrant(ctx, accessIn(project, actor))

	pr.completeAccess = func(gitprovider.CompleteAccessRequest) (gitprovider.AccessGrant, error) {
		return gitprovider.AccessGrant{}, &gitprovider.Error{Code: gitprovider.CodeConflict}
	}
	if _, err := svc.CompleteAccessGrant(ctx, actor, project, "grant-1", nil); !gitprovider.IsCode(err, gitprovider.CodeConflict) {
		t.Errorf("provider failure: %v", err)
	}
	if st.access["grant-1"].CompletedAt != nil {
		t.Error("a failed completion (for example an approval still pending) must leave the session open for a retry")
	}
	st.access["grant-1"].ExpiresAt = time.Now().Add(-time.Second)
	if _, err := svc.CompleteAccessGrant(ctx, actor, project, "grant-1", nil); !errors.Is(err, gitdom.ErrSetupExpired) {
		t.Errorf("expired: %v", err)
	}
}

func TestLinkRepository_NotFoundOnAGrantPlatformAsksForAccess(t *testing.T) {
	ctx := context.Background()
	svc, _, pr, _ := accessSvc(t)
	for _, code := range []gitprovider.ErrorCode{gitprovider.CodeNotFound, gitprovider.CodeForbidden} {
		pr.resolve = func(gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
			return gitprovider.ResolvedRepository{}, &gitprovider.Error{Code: code}
		}
		_, err := svc.LinkRepository(ctx, LinkInput{ProjectID: uuid.New(), ProviderID: "p", PathOrURL: "acme/web"})
		if !errors.Is(err, gitdom.ErrAccessNotGranted) {
			t.Errorf("%s: got %v, want ErrAccessNotGranted", code, err)
		}
	}
	// Other failures are still reported as they are.
	pr.resolve = func(gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		return gitprovider.ResolvedRepository{}, &gitprovider.Error{Code: gitprovider.CodeTransient}
	}
	if _, err := svc.LinkRepository(ctx, LinkInput{ProjectID: uuid.New(), ProviderID: "p", PathOrURL: "acme/web"}); !gitprovider.IsCode(err, gitprovider.CodeTransient) {
		t.Errorf("transient: %v", err)
	}
	// A platform without per-owner grants has nothing to offer: the connections
	// simply do not reach the repository.
	pr.info.Capabilities.AccessGrants = false
	pr.resolve = func(gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		return gitprovider.ResolvedRepository{}, &gitprovider.Error{Code: gitprovider.CodeNotFound}
	}
	if _, err := svc.LinkRepository(ctx, LinkInput{ProjectID: uuid.New(), ProviderID: "p", PathOrURL: "acme/web"}); !errors.Is(err, gitdom.ErrNotVisible) {
		t.Errorf("no grants: %v", err)
	}
}

func TestBeginAccessGrant_ForAnAgentUsesItsOwnIdentity(t *testing.T) {
	ctx := context.Background()
	svc, st, pr, shared := accessSvc(t)
	agent, project, actor := uuid.New(), uuid.New(), uuid.New()
	var got gitprovider.BeginAccessRequest
	inner := pr.beginAccess
	pr.beginAccess = func(r gitprovider.BeginAccessRequest) (gitprovider.SetupStep, error) { got = r; return inner(r) }

	// The agent has no identity of its own yet: the shared one must not be used in its place.
	in := accessIn(project, actor)
	in.AgentID = &agent
	if _, err := svc.BeginAccessGrant(ctx, in); !errors.Is(err, gitdom.ErrNoIdentity) {
		t.Fatalf("no own identity: %v", err)
	}
	if len(st.access) != 0 {
		t.Fatal("nothing may be stored for a refused grant")
	}

	own := &gitdom.Identity{ID: uuid.New(), ProviderID: "p", Host: "git.example", ProviderIdentityID: "agent-app", Scope: gitdom.ScopeAgent}
	st.byID[own.ID], st.agentIDs[agent] = own, own
	if _, err := svc.BeginAccessGrant(ctx, in); err != nil {
		t.Fatal(err)
	}
	if got.Identity.ID != "agent-app" || got.Identity.ID == shared.ProviderIdentityID {
		t.Errorf("provider was asked about %q, want the agent's own identity", got.Identity.ID)
	}
	if s := st.access["grant-1"]; s == nil || s.IdentityID != own.ID {
		t.Errorf("the session must name the agent's identity so completion grants the right one: %+v", s)
	}
}
