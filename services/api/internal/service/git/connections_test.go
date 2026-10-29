package gitsvc

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

func (f *fakeStore) addProjectConnection(project uuid.UUID, handle, ref string) *gitdom.Identity {
	id := &gitdom.Identity{ID: uuid.New(), ProviderID: "p", Host: "git.example", ProviderIdentityID: ref, Handle: handle,
		Scope: gitdom.ScopeProject, ProjectID: &project}
	f.byRef[ref] = id
	f.byID[id.ID] = id
	return id
}

func offered(r gitprovider.ResolveRepoRequest) []string {
	var out []string
	for _, c := range r.Candidates {
		out = append(out, c.ID)
	}
	return out
}

func TestLinkRepository_OffersWorkspaceAndOwnProjectConnections(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setup(t)
	project, other := uuid.New(), uuid.New()
	st.addIdentity("workspace-bot", "ws-app")
	st.addProjectConnection(project, "mine", "own-token")
	st.addProjectConnection(other, "theirs", "other-token")

	var seen []string
	pr.resolve = func(r gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		seen = offered(r)
		return gitprovider.ResolvedRepository{
			Repository: gitprovider.Repository{Ref: gitprovider.RepoRef{Host: "git.example", FullPath: "acme/web", DefaultBranch: "main"}},
			Identity:   gitprovider.IdentityRef{ID: "own-token"},
		}, nil
	}
	r, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", PathOrURL: "acme/web"})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("candidates = %v, want the workspace's and the project's own, never another project's", seen)
	}
	for _, id := range seen {
		if id == "other-token" {
			t.Fatal("another project's connection was offered")
		}
	}
	if r.IdentityID == nil || *r.IdentityID != st.byRef["own-token"].ID {
		t.Fatalf("the repository must be bound to the connection that saw it: %+v", r)
	}
}

func TestLinkRepository_ChosenConnectionIsTheOnlyCandidate(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setup(t)
	project := uuid.New()
	st.addIdentity("a", "app-a")
	b := st.addIdentity("b", "app-b")
	var seen []string
	pr.resolve = func(r gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		seen = offered(r)
		return gitprovider.ResolvedRepository{
			Repository: gitprovider.Repository{Ref: gitprovider.RepoRef{Host: "git.example", FullPath: "acme/web"}},
			Identity:   gitprovider.IdentityRef{ID: "app-b"},
		}, nil
	}
	if _, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", PathOrURL: "acme/web", IdentityID: &b.ID}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "app-b" {
		t.Fatalf("candidates = %v, want only the chosen connection", seen)
	}
	// A connection that belongs to another project is not choosable.
	theirs := st.addProjectConnection(uuid.New(), "theirs", "other-token")
	_, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", PathOrURL: "acme/web", IdentityID: &theirs.ID})
	if !errors.Is(err, gitdom.ErrNoIdentity) {
		t.Fatalf("another project's connection: %v", err)
	}
}

func TestListConnections_WorkspaceAndOwnOnly(t *testing.T) {
	svc, st, pr := setup(t)
	pr.info = gitprovider.Info{ID: "p"}
	project := uuid.New()
	st.addIdentity("ws", "ws-app")
	st.addProjectConnection(project, "mine", "own")
	st.addProjectConnection(uuid.New(), "theirs", "other")
	got, err := svc.ListConnections(context.Background(), project)
	if err != nil || len(got) != 2 {
		t.Fatalf("connections = %d %v", len(got), err)
	}
}

func TestDeleteProjectConnection_OnlyTheProjectsOwn(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setup(t)
	project := uuid.New()
	mine := st.addProjectConnection(project, "mine", "own")
	ws := st.addIdentity("ws", "ws-app")
	theirs := st.addProjectConnection(uuid.New(), "theirs", "other")

	for name, id := range map[string]uuid.UUID{"workspace connection": ws.ID, "another project's": theirs.ID} {
		if err := svc.DeleteProjectConnection(ctx, project, id); !errors.Is(err, gitdom.ErrNotFound) {
			t.Errorf("%s: %v, want not found", name, err)
		}
		if _, ok := st.byID[id]; !ok {
			t.Errorf("%s must survive", name)
		}
	}
	if err := svc.DeleteProjectConnection(ctx, project, mine.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.byID[mine.ID]; ok {
		t.Error("the project's own connection should be gone")
	}
}

func TestProjectSetup_IsBoundToItsProjectAndRoute(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setupSvc(t)
	twoLegProvider(pr)
	actor, project := uuid.New(), uuid.New()
	in := shared(actor)
	in.Scope, in.ProjectID = gitdom.ScopeProject, &project
	if _, err := svc.BeginIdentitySetup(ctx, in); err != nil {
		t.Fatal(err)
	}
	// Neither the workspace route nor another project's can finish it.
	other := uuid.New()
	for name, p := range map[string]*uuid.UUID{"workspace route": nil, "other project": &other} {
		if _, err := svc.CompleteIdentitySetup(ctx, actor, p, "state-1", map[string]string{"code": "c"}); !errors.Is(err, gitdom.ErrSetupExpired) {
			t.Errorf("%s: %v", name, err)
		}
	}
	res, err := svc.CompleteIdentitySetup(ctx, actor, &project, "state-1", map[string]string{"code": "c"})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Identity
	if id.Scope != gitdom.ScopeProject || id.ProjectID == nil || *id.ProjectID != project {
		t.Fatalf("identity = %+v", id)
	}
	if len(st.shared) != 0 {
		t.Error("a project connection must not become a workspace one")
	}
}

func TestBeginAccessGrant_NeedsAChoiceWhenSeveralConnections(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setupSvc(t)
	pr.info = gitprovider.Info{ID: "p", Hosts: []string{"git.example"}, Capabilities: gitprovider.Capabilities{AccessGrants: true}}
	project := uuid.New()
	a := st.addIdentity("a", "app-a")
	st.addIdentity("b", "app-b")
	_, err := svc.BeginAccessGrant(ctx, AccessInput{ProjectID: project, ProviderID: "p", Host: "git.example", ActorID: uuid.New()})
	if !errors.Is(err, gitdom.ErrIdentityRequired) {
		t.Fatalf("ambiguous: %v", err)
	}
	theirs := st.addProjectConnection(uuid.New(), "theirs", "other")
	_, err = svc.BeginAccessGrant(ctx, AccessInput{ProjectID: project, ProviderID: "p", IdentityID: &theirs.ID, ActorID: uuid.New()})
	if !errors.Is(err, gitdom.ErrNoIdentity) {
		t.Fatalf("another project's connection: %v", err)
	}
	_ = a
}

// A token connection (webhook per repository) gets its webhook registered when
// a repository is linked, and removed when the last link through it goes.
func TestRepoWebhook_RegisteredOnLinkAndRemovedWithTheLastLink(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setup(t)
	svc.WithPublicURL("https://paca.example.com")
	pr.info = gitprovider.Info{ID: "p", Capabilities: gitprovider.Capabilities{
		IdentityKinds: []gitprovider.IdentityKind{gitprovider.IdentityApp, gitprovider.IdentityToken}, WebhookScope: gitprovider.WebhookApp, WebhookManagement: true,
		Kinds: []gitprovider.KindProfile{{Kind: gitprovider.IdentityToken, CredentialModel: gitprovider.CredentialStatic, WebhookScope: gitprovider.WebhookRepo}},
	}}
	tok := st.addIdentity("pat", "tok")
	tok.Kind = "token"
	st.endpoints["key1"] = &gitdom.WebhookEndpoint{Key: "key1", ProviderID: "p", IdentityID: &tok.ID}
	pr.resolve = func(gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		return gitprovider.ResolvedRepository{
			Repository: gitprovider.Repository{Ref: gitprovider.RepoRef{Host: "git.example", FullPath: "acme/web"}},
			Identity:   gitprovider.IdentityRef{ID: "tok"},
		}, nil
	}
	p1, p2 := uuid.New(), uuid.New()
	r1, err := svc.LinkRepository(ctx, LinkInput{ProjectID: p1, ProviderID: "p", PathOrURL: "acme/web"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pr.ensured) != 1 || pr.ensured[0].URL != "https://paca.example.com"+WebhookPathPrefix+"key1" {
		t.Fatalf("ensure webhook = %+v", pr.ensured)
	}
	if !hasWebhook(r1) {
		t.Fatal("the registered webhook should be recorded on the repository")
	}
	r2, err := svc.LinkRepository(ctx, LinkInput{ProjectID: p2, ProviderID: "p", PathOrURL: "acme/web"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.UnlinkRepository(ctx, p1, r1.ID); err != nil {
		t.Fatal(err)
	}
	if len(pr.removed) != 0 {
		t.Fatal("another project still links the repository through the connection: keep the webhook")
	}
	if err := svc.UnlinkRepository(ctx, p2, r2.ID); err != nil {
		t.Fatal(err)
	}
	if len(pr.removed) != 1 {
		t.Fatalf("the last link going removes the webhook: %+v", pr.removed)
	}
}

func TestRepoWebhook_ListShowsTheURLWhenRegistrationFailed(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setup(t)
	svc.WithPublicURL("https://paca.example.com")
	pr.info = gitprovider.Info{ID: "p", Capabilities: gitprovider.Capabilities{
		IdentityKinds: []gitprovider.IdentityKind{gitprovider.IdentityToken}, WebhookScope: gitprovider.WebhookRepo, WebhookManagement: true,
	}}
	pr.ensureErr = &gitprovider.Error{Code: gitprovider.CodeForbidden, Message: "token cannot manage webhooks"}
	tok := st.addIdentity("pat", "tok")
	tok.Kind = "token"
	st.endpoints["key1"] = &gitdom.WebhookEndpoint{Key: "key1", ProviderID: "p", IdentityID: &tok.ID}
	pr.resolve = func(gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		return gitprovider.ResolvedRepository{
			Repository: gitprovider.Repository{Ref: gitprovider.RepoRef{Host: "git.example", FullPath: "acme/web"}},
			Identity:   gitprovider.IdentityRef{ID: "tok"},
		}, nil
	}
	project := uuid.New()
	if _, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", PathOrURL: "acme/web"}); err != nil {
		t.Fatalf("a webhook that cannot be registered must not fail the link: %v", err)
	}
	repos, err := svc.ListRepositories(ctx, project)
	if err != nil || len(repos) != 1 || repos[0].WebhookURL != "https://paca.example.com"+WebhookPathPrefix+"key1" {
		t.Fatalf("listing = %+v %v", repos, err)
	}
	if !repos[0].WebhookAttempted {
		t.Fatal("registering failed at link time: the attempt must be reported")
	}
	if _, err := svc.RegisterWebhook(ctx, project, repos[0].ID); !gitprovider.IsCode(err, gitprovider.CodeForbidden) {
		t.Fatalf("retry while still refused: %v", err)
	}
	pr.ensureErr = nil
	if r, err := svc.RegisterWebhook(ctx, project, repos[0].ID); err != nil || !hasWebhook(r) {
		t.Fatalf("retry: %+v %v", r, err)
	}
}

func TestBeginAccessGrant_ExplicitConnectionMustBeOfTheRequestedProvider(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setupSvc(t)
	pr.info = gitprovider.Info{ID: "p", Hosts: []string{"git.example"}, Capabilities: gitprovider.Capabilities{AccessGrants: true}}
	project := uuid.New()
	other := st.addIdentity("elsewhere", "other-app")
	other.ProviderID = "another-provider"
	_, err := svc.BeginAccessGrant(ctx, AccessInput{ProjectID: project, ProviderID: "p", IdentityID: &other.ID, ActorID: uuid.New()})
	if !errors.Is(err, gitdom.ErrNoIdentity) {
		t.Fatalf("a connection of another provider: %v", err)
	}
	otherHost := st.addIdentity("remote", "remote-app")
	otherHost.Host = "git.other.example"
	_, err = svc.BeginAccessGrant(ctx, AccessInput{ProjectID: project, ProviderID: "p", Host: "git.example", IdentityID: &otherHost.ID, ActorID: uuid.New()})
	if !errors.Is(err, gitdom.ErrNoIdentity) {
		t.Fatalf("a connection of another host: %v", err)
	}
}

// Two connections may report the same provider id; linking binds the repository
// to the connection that was offered and chosen, never to a lookup by that id.
func TestLinkRepository_BindsTheChosenRowWhenProviderIDsCollide(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setup(t)
	project := uuid.New()
	st.addIdentity("first", "same-ref")
	second := st.addProjectConnection(project, "second", "same-ref")
	st.byRef["same-ref"] = st.shared[0] // a lookup by ref would find the first
	pr.resolve = func(r gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		return gitprovider.ResolvedRepository{
			Repository: gitprovider.Repository{Ref: gitprovider.RepoRef{Host: "git.example", FullPath: "acme/web"}},
			Identity:   gitprovider.IdentityRef{ID: "same-ref"},
		}, nil
	}
	r, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", PathOrURL: "acme/web", IdentityID: &second.ID})
	if err != nil || r.IdentityID == nil || *r.IdentityID != second.ID {
		t.Fatalf("linked = %+v %v, want the chosen connection %v", r, err, second.ID)
	}
}

func TestFinishIdentity_NeverMergesAnotherConnection(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setupSvc(t)
	twoLegProvider(pr)
	actor, project := uuid.New(), uuid.New()
	st.addProjectConnection(uuid.New(), "someone-elses", "app-9") // same provider id, another project
	in := shared(actor)
	in.Scope, in.ProjectID = gitdom.ScopeProject, &project
	if _, err := svc.BeginIdentitySetup(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteIdentitySetup(ctx, actor, &project, "state-1", map[string]string{"code": "c"}); err == nil {
		t.Fatal("a provider id that already names another project's connection must not be reused")
	}
}

func TestFinishIdentity_NeverMergesAcrossHostsOrAgents(t *testing.T) {
	ctx := context.Background()
	actor := uuid.New()

	// Same scope and project, another host.
	svc, st, pr := setupSvc(t)
	twoLegProvider(pr)
	existing := st.addIdentity("bot", "app-9")
	existing.Host = "git.other.example"
	if _, err := svc.BeginIdentitySetup(ctx, shared(actor)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteIdentitySetup(ctx, actor, nil, "state-1", map[string]string{"code": "c"}); !errors.Is(err, errIdentityCollision) {
		t.Fatalf("another host's connection with the same id: %v", err)
	}

	// Another agent's identity with the same id: the second agent must not be
	// mapped onto the first one's credential.
	svc, st, pr = setupSvc(t)
	twoLegProvider(pr)
	first, second := uuid.New(), uuid.New()
	own := st.addIdentity("bot", "app-9")
	own.Scope = gitdom.ScopeAgent
	st.shared = nil
	st.agentIDs[first] = own
	in := shared(actor)
	in.Scope, in.AgentID, in.AgentName = gitdom.ScopeAgent, &second, "Second"
	if _, err := svc.BeginIdentitySetup(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteIdentitySetup(ctx, actor, nil, "state-1", map[string]string{"code": "c"}); !errors.Is(err, errIdentityCollision) {
		t.Fatalf("another agent's identity with the same id: %v", err)
	}
	if _, mapped := st.agentIDs[second]; mapped {
		t.Fatal("the second agent must not be mapped onto the first agent's identity")
	}
}

func tokenWebhookSetup(t *testing.T) (*Service, *fakeStore, *fakeProv, *gitdom.Identity) {
	t.Helper()
	svc, st, pr := setup(t)
	svc.WithPublicURL("https://paca.example.com")
	pr.info = gitprovider.Info{ID: "p", Capabilities: gitprovider.Capabilities{
		IdentityKinds: []gitprovider.IdentityKind{gitprovider.IdentityToken}, WebhookScope: gitprovider.WebhookRepo, WebhookManagement: true,
	}}
	tok := st.addIdentity("pat", "tok")
	tok.Kind = "token"
	st.endpoints["key1"] = &gitdom.WebhookEndpoint{Key: "key1", ProviderID: "p", IdentityID: &tok.ID}
	pr.resolve = func(gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		return gitprovider.ResolvedRepository{
			Repository: gitprovider.Repository{Ref: gitprovider.RepoRef{Host: "git.example", FullPath: "acme/web"}},
			Identity:   gitprovider.IdentityRef{ID: "tok"},
		}, nil
	}
	return svc, st, pr, tok
}

func TestListRepositories_AsksTheProviderOncePerConnection(t *testing.T) {
	ctx := context.Background()
	svc, st, pr, tok := tokenWebhookSetup(t)
	pr.ensureErr = &gitprovider.Error{Code: gitprovider.CodeForbidden}
	project := uuid.New()
	for _, path := range []string{"acme/a", "acme/b", "acme/c"} {
		st.repos[uuid.New()] = &gitdom.Repository{ID: uuid.New(), ProjectID: project, ProviderID: "p", Host: "git.example", FullPath: path, IdentityID: &tok.ID}
	}
	pr.describes = 0
	if _, err := svc.ListRepositories(ctx, project); err != nil {
		t.Fatal(err)
	}
	if pr.describes != 1 {
		t.Fatalf("describe calls = %d, want 1 for three repositories of one connection", pr.describes)
	}
}

// A repository carried over from a plugin's own storage never had a webhook
// registered by Paca: the listing says nobody tried, unlike for one that failed.
func TestListRepositories_TellsAWebhookNeverAttemptedFromOneThatFailed(t *testing.T) {
	ctx := context.Background()
	svc, st, pr, tok := tokenWebhookSetup(t)
	pr.ensureErr = &gitprovider.Error{Code: gitprovider.CodeForbidden}
	project := uuid.New()
	carried := &gitdom.Repository{ID: uuid.New(), ProjectID: project, ProviderID: "p", Host: "git.example", FullPath: "acme/carried", IdentityID: &tok.ID}
	st.repos[carried.ID] = carried
	linked, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", PathOrURL: "acme/web"})
	if err != nil {
		t.Fatal(err)
	}
	repos, err := svc.ListRepositories(ctx, project)
	if err != nil || len(repos) != 2 {
		t.Fatalf("listing = %+v %v", repos, err)
	}
	for _, r := range repos {
		if r.WebhookURL == "" {
			t.Fatalf("%s should still need a webhook", r.FullPath)
		}
		if want := r.ID == linked.ID; r.WebhookAttempted != want {
			t.Errorf("%s: WebhookAttempted = %v, want %v", r.FullPath, r.WebhookAttempted, want)
		}
	}
}

func TestRegisterWebhook_SaysWhyItCannotBeRegistered(t *testing.T) {
	ctx := context.Background()
	svc, _, pr, _ := tokenWebhookSetup(t)
	svc.WithPublicURL("")
	project := uuid.New()
	r, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", PathOrURL: "acme/web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RegisterWebhook(ctx, project, r.ID); !gitprovider.IsCode(err, gitprovider.CodeUnsupported) {
		t.Fatalf("no public URL: %v", err)
	}
	svc.WithPublicURL("https://paca.example.com")
	pr.ensureErr = &gitprovider.Error{Code: gitprovider.CodeTransient}
	if _, err := svc.RegisterWebhook(ctx, project, r.ID); !gitprovider.IsCode(err, gitprovider.CodeTransient) {
		t.Fatalf("a provider outage must not read as a permissions problem: %v", err)
	}
}

func TestDeleteIdentity_RemovesItsRepositoryWebhooks(t *testing.T) {
	ctx := context.Background()
	svc, _, pr, tok := tokenWebhookSetup(t)
	if _, err := svc.LinkRepository(ctx, LinkInput{ProjectID: uuid.New(), ProviderID: "p", PathOrURL: "acme/web"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteIdentity(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
	if len(pr.removed) != 1 {
		t.Fatalf("webhooks removed = %d, want 1", len(pr.removed))
	}
}

func TestPurgeProjectConnections_OnlyTheProjectsOwn(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setup(t)
	project := uuid.New()
	workspace := st.addIdentity("workspace", "ws")
	own := st.addProjectConnection(project, "own", "own-ref")
	other := st.addProjectConnection(uuid.New(), "other", "other-ref")
	if err := svc.PurgeProjectConnections(ctx, project); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.byID[own.ID]; ok {
		t.Fatal("the project's own connection should be gone")
	}
	if _, ok := st.byID[workspace.ID]; !ok {
		t.Fatal("a workspace connection is not the project's to purge")
	}
	if _, ok := st.byID[other.ID]; !ok {
		t.Fatal("another project's connection must stay")
	}
}

func TestReconnectRepository_RebindsARepositoryWhoseConnectionWasDeleted(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setup(t)
	project := uuid.New()
	old := st.addProjectConnection(project, "old", "old-app")
	pr.resolve = func(r gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		return gitprovider.ResolvedRepository{
			Repository: gitprovider.Repository{Ref: gitprovider.RepoRef{Host: "git.example", FullPath: "acme/web"}},
			Identity:   gitprovider.IdentityRef{ID: r.Candidates[0].ID},
		}, nil
	}
	repo, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", PathOrURL: "acme/web"})
	if err != nil || *repo.IdentityID != old.ID {
		t.Fatalf("link: %+v %v", repo, err)
	}
	// The connection is deleted: the database clears the repository's link.
	if err := svc.DeleteProjectConnection(ctx, project, old.ID); err != nil {
		t.Fatal(err)
	}
	st.repos[repo.ID].IdentityID = nil

	// Another project's repository is not this project's to rebind.
	if _, err := svc.ReconnectRepository(ctx, ReconnectInput{ProjectID: uuid.New(), RepositoryID: repo.ID}); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("other project: %v", err)
	}
	// With no connection left there is nothing to rebind to.
	if _, err := svc.ReconnectRepository(ctx, ReconnectInput{ProjectID: project, RepositoryID: repo.ID}); !errors.Is(err, gitdom.ErrNoIdentity) {
		t.Fatalf("no connection: %v", err)
	}
	fresh := st.addProjectConnection(project, "fresh", "fresh-app")
	got, err := svc.ReconnectRepository(ctx, ReconnectInput{ProjectID: project, RepositoryID: repo.ID})
	if err != nil || got.IdentityID == nil || *got.IdentityID != fresh.ID || st.repos[repo.ID].IdentityID == nil || *st.repos[repo.ID].IdentityID != fresh.ID {
		t.Fatalf("reconnect: %+v %v", got, err)
	}
}

func TestListConnections_CountsTheRepositoriesUsingEach(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setup(t)
	pr.info = gitprovider.Info{ID: "p"}
	project := uuid.New()
	used := st.addProjectConnection(project, "used", "used-app")
	unused := st.addProjectConnection(project, "unused", "unused-app")
	st.repos[uuid.New()] = &gitdom.Repository{ProjectID: project, IdentityID: &used.ID, FullPath: "a/b"}
	st.repos[uuid.New()] = &gitdom.Repository{ProjectID: uuid.New(), IdentityID: &used.ID, FullPath: "c/d"}
	got, err := svc.ListConnections(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	count := map[uuid.UUID]int{}
	for _, c := range got {
		count[c.ID] = c.RepositoryCount
	}
	if count[used.ID] != 2 || count[unused.ID] != 0 {
		t.Fatalf("counts = %v, want 2 and 0 (every project's repositories count)", count)
	}
}

// Re-pointing a repository that still has a connection must not leave the old
// connection's webhook behind.
func TestReconnectRepository_RemovesThePreviousConnectionsWebhook(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setup(t)
	svc.WithPublicURL("https://paca.example.com")
	pr.info = gitprovider.Info{ID: "p", Capabilities: gitprovider.Capabilities{
		IdentityKinds: []gitprovider.IdentityKind{gitprovider.IdentityToken}, WebhookScope: gitprovider.WebhookRepo, WebhookManagement: true,
	}}
	project := uuid.New()
	a := st.addProjectConnection(project, "a", "tok-a")
	a.Kind = "token"
	b := st.addProjectConnection(project, "b", "tok-b")
	b.Kind = "token"
	st.endpoints["ka"] = &gitdom.WebhookEndpoint{Key: "ka", ProviderID: "p", IdentityID: &a.ID}
	st.endpoints["kb"] = &gitdom.WebhookEndpoint{Key: "kb", ProviderID: "p", IdentityID: &b.ID}
	pr.resolve = func(r gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		return gitprovider.ResolvedRepository{
			Repository: gitprovider.Repository{Ref: gitprovider.RepoRef{Host: "git.example", FullPath: "acme/web"}},
			Identity:   gitprovider.IdentityRef{ID: r.Candidates[0].ID},
		}, nil
	}
	repo, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", PathOrURL: "acme/web", IdentityID: &a.ID})
	if err != nil || !hasWebhook(repo) {
		t.Fatalf("link: %+v %v", repo, err)
	}

	// The same connection again: nothing to remove.
	if _, err := svc.ReconnectRepository(ctx, ReconnectInput{ProjectID: project, RepositoryID: repo.ID, IdentityID: &a.ID}); err != nil {
		t.Fatal(err)
	}
	if len(pr.removed) != 0 {
		t.Fatalf("the same connection keeps its webhook: %+v", pr.removed)
	}

	// Another connection: the first one's webhook goes, the new one's is registered.
	got, err := svc.ReconnectRepository(ctx, ReconnectInput{ProjectID: project, RepositoryID: repo.ID, IdentityID: &b.ID})
	if err != nil || got.IdentityID == nil || *got.IdentityID != b.ID {
		t.Fatalf("reconnect: %+v %v", got, err)
	}
	if len(pr.removed) != 1 || pr.removed[0].Target.Identity.ID != "tok-a" {
		t.Fatalf("the old connection's webhook should be removed through the old connection: %+v", pr.removed)
	}
	if len(pr.ensured) < 2 || pr.ensured[len(pr.ensured)-1].URL != "https://paca.example.com"+WebhookPathPrefix+"kb" || !hasWebhook(got) {
		t.Fatalf("the new connection's webhook should be registered: %+v", pr.ensured)
	}
}
