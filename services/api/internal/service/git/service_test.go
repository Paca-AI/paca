package gitsvc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

// fakeStore is an in-memory gitdom.Store covering what the service touches.
type fakeStore struct {
	gitdom.Store // unimplemented methods panic: the service must not call them

	shared    []*gitdom.Identity
	byRef     map[string]*gitdom.Identity
	byID      map[uuid.UUID]*gitdom.Identity
	agentIDs  map[uuid.UUID]*gitdom.Identity
	endpoints map[string]*gitdom.WebhookEndpoint
	repos     map[uuid.UUID]*gitdom.Repository
	inbox     map[string]bool
	crs       []*gitdom.ChangeRequest
	tasks     map[string]uuid.UUID // "prefix-number" (lower)
	taskLinks []uuid.UUID
	branches  []string
	log       []*gitdom.ActionLogEntry

	sessions map[string]*gitdom.SetupSession
	access   map[string]*gitdom.AccessSession
	bound    map[string]uuid.UUID
}

func newFakeStore() *fakeStore {
	return &fakeStore{byRef: map[string]*gitdom.Identity{}, byID: map[uuid.UUID]*gitdom.Identity{},
		agentIDs: map[uuid.UUID]*gitdom.Identity{}, endpoints: map[string]*gitdom.WebhookEndpoint{},
		repos: map[uuid.UUID]*gitdom.Repository{}, inbox: map[string]bool{}, tasks: map[string]uuid.UUID{}}
}

func (f *fakeStore) addIdentity(handle, ref string) *gitdom.Identity {
	id := &gitdom.Identity{ID: uuid.New(), ProviderID: "p", Host: "git.example", ProviderIdentityID: ref, Handle: handle, Scope: gitdom.ScopeShared}
	f.shared = append(f.shared, id)
	f.byRef[ref] = id
	f.byID[id.ID] = id
	return id
}

func (f *fakeStore) ListSharedIdentities(context.Context, string) ([]*gitdom.Identity, error) {
	return f.shared, nil
}
func (f *fakeStore) FindIdentityByProviderRef(_ context.Context, _, ref string) (*gitdom.Identity, error) {
	if i, ok := f.byRef[ref]; ok {
		return i, nil
	}
	return nil, gitdom.ErrNotFound
}
func (f *fakeStore) FindIdentity(_ context.Context, id uuid.UUID) (*gitdom.Identity, error) {
	if i, ok := f.byID[id]; ok {
		return i, nil
	}
	return nil, gitdom.ErrNotFound
}
func (f *fakeStore) ListProjectIdentities(_ context.Context, project uuid.UUID) ([]*gitdom.Identity, error) {
	out := append([]*gitdom.Identity(nil), f.shared...)
	for _, i := range f.byID {
		if i.Scope == gitdom.ScopeProject && i.ProjectID != nil && *i.ProjectID == project {
			out = append(out, i)
		}
	}
	return out, nil
}
func (f *fakeStore) FindAgentIdentity(_ context.Context, a uuid.UUID, _ string) (*gitdom.Identity, error) {
	if i, ok := f.agentIDs[a]; ok {
		return i, nil
	}
	return nil, gitdom.ErrNotFound
}
func (f *fakeStore) CreateIdentity(_ context.Context, i *gitdom.Identity) error {
	i.ID = uuid.New()
	if i.Scope == gitdom.ScopeShared {
		f.shared = append(f.shared, i)
	}
	f.byRef[i.ProviderIdentityID] = i
	f.byID[i.ID] = i
	return nil
}
func (f *fakeStore) SetAgentIdentity(_ context.Context, a uuid.UUID, _ string, id uuid.UUID) error {
	f.agentIDs[a] = f.byID[id]
	return nil
}
func (f *fakeStore) DeleteAgentIdentity(_ context.Context, a uuid.UUID, _ string) error {
	if _, ok := f.agentIDs[a]; !ok {
		return gitdom.ErrNotFound
	}
	delete(f.agentIDs, a)
	return nil
}
func (f *fakeStore) DeleteIdentity(_ context.Context, id uuid.UUID) error {
	i, ok := f.byID[id]
	if !ok {
		return gitdom.ErrNotFound
	}
	delete(f.byID, id)
	delete(f.byRef, i.ProviderIdentityID)
	for k, e := range f.shared {
		if e.ID == id {
			f.shared = append(f.shared[:k], f.shared[k+1:]...)
			break
		}
	}
	return nil
}
func (f *fakeStore) CreateWebhookEndpoint(_ context.Context, e *gitdom.WebhookEndpoint) error {
	f.endpoints[e.Key] = e
	return nil
}
func (f *fakeStore) DeleteWebhookEndpoint(_ context.Context, k string) error {
	delete(f.endpoints, k)
	return nil
}
func (f *fakeStore) BindWebhookEndpoint(_ context.Context, k string, id uuid.UUID) error {
	e, ok := f.endpoints[k]
	if !ok {
		return gitdom.ErrNotFound
	}
	e.IdentityID = &id
	return nil
}
func (f *fakeStore) CreateSetupSession(_ context.Context, s *gitdom.SetupSession) error {
	if f.sessions == nil {
		f.sessions = map[string]*gitdom.SetupSession{}
	}
	f.sessions[s.State] = s
	return nil
}
func (f *fakeStore) FindSetupSession(_ context.Context, st string) (*gitdom.SetupSession, error) {
	if s, ok := f.sessions[st]; ok {
		return s, nil
	}
	return nil, gitdom.ErrNotFound
}
func (f *fakeStore) CompleteSetupSession(_ context.Context, st string) error {
	s, ok := f.sessions[st]
	if !ok {
		return gitdom.ErrNotFound
	}
	t := time.Now()
	s.CompletedAt = &t
	return nil
}
func (f *fakeStore) CreateAccessSession(_ context.Context, s *gitdom.AccessSession) error {
	if f.access == nil {
		f.access = map[string]*gitdom.AccessSession{}
	}
	f.access[s.State] = s
	return nil
}
func (f *fakeStore) FindAccessSession(_ context.Context, st string) (*gitdom.AccessSession, error) {
	if s, ok := f.access[st]; ok {
		return s, nil
	}
	return nil, gitdom.ErrNotFound
}
func (f *fakeStore) CompleteAccessSession(_ context.Context, st string) error {
	s, ok := f.access[st]
	if !ok {
		return gitdom.ErrNotFound
	}
	t := time.Now()
	s.CompletedAt = &t
	return nil
}
func (f *fakeStore) FindWebhookEndpoint(_ context.Context, k string) (*gitdom.WebhookEndpoint, error) {
	if e, ok := f.endpoints[k]; ok {
		return e, nil
	}
	return nil, gitdom.ErrNotFound
}
func (f *fakeStore) CreateRepository(_ context.Context, r *gitdom.Repository) error {
	for _, e := range f.repos {
		if e.ProjectID == r.ProjectID && strings.EqualFold(e.FullPath, r.FullPath) {
			return gitdom.ErrAlreadyLinked
		}
	}
	r.ID = uuid.New()
	f.repos[r.ID] = r
	return nil
}
func (f *fakeStore) FindRepository(_ context.Context, id uuid.UUID) (*gitdom.Repository, error) {
	if r, ok := f.repos[id]; ok {
		return r, nil
	}
	return nil, gitdom.ErrNotFound
}
func (f *fakeStore) DeleteRepository(_ context.Context, project, id uuid.UUID) error {
	r, ok := f.repos[id]
	if !ok || r.ProjectID != project {
		return gitdom.ErrNotFound
	}
	delete(f.repos, id)
	return nil
}
func (f *fakeStore) SetRepositoryDescription(_ context.Context, project, id uuid.UUID, d string) error {
	r, ok := f.repos[id]
	if !ok || r.ProjectID != project {
		return gitdom.ErrNotFound
	}
	r.Description = d
	return nil
}
func (f *fakeStore) ListIdentityRepositories(_ context.Context, id uuid.UUID) ([]*gitdom.Repository, error) {
	var out []*gitdom.Repository
	for _, r := range f.repos {
		if r.IdentityID != nil && *r.IdentityID == id {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeStore) ListProjectRepositories(_ context.Context, project uuid.UUID) ([]*gitdom.Repository, error) {
	var out []*gitdom.Repository
	for _, r := range f.repos {
		if r.ProjectID == project {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeStore) SetRepositoryIdentity(_ context.Context, id, identity uuid.UUID, ref json.RawMessage) error {
	r, ok := f.repos[id]
	if !ok {
		return gitdom.ErrNotFound
	}
	r.IdentityID, r.ProviderRef, r.WebhookRef = &identity, ref, nil
	return nil
}
func (f *fakeStore) CountRepositories(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	for _, r := range f.repos {
		for _, id := range ids {
			if r.IdentityID != nil && *r.IdentityID == id {
				out[id]++
			}
		}
	}
	return out, nil
}
func (f *fakeStore) SetRepositoryWebhook(_ context.Context, id uuid.UUID, ref json.RawMessage) error {
	r, ok := f.repos[id]
	if !ok {
		return gitdom.ErrNotFound
	}
	r.WebhookRef = ref
	return nil
}
func (f *fakeStore) FindWebhookEndpointForIdentity(_ context.Context, id uuid.UUID) (*gitdom.WebhookEndpoint, error) {
	for _, e := range f.endpoints {
		if e.IdentityID != nil && *e.IdentityID == id {
			return e, nil
		}
	}
	return nil, gitdom.ErrNotFound
}
func (f *fakeStore) FindLinkedRepositories(_ context.Context, _, _, path string) ([]*gitdom.Repository, error) {
	var out []*gitdom.Repository
	for _, r := range f.repos {
		if strings.EqualFold(r.FullPath, path) {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeStore) InsertInboxEvent(_ context.Context, e *gitdom.InboxEvent) (bool, error) {
	k := e.EventID + "|" + e.RepositoryID.String()
	if f.inbox[k] {
		return false, nil
	}
	f.inbox[k] = true
	return true, nil
}
func (f *fakeStore) UpsertChangeRequest(_ context.Context, cr *gitdom.ChangeRequest) (*gitdom.ChangeRequest, error) {
	cr.ID = uuid.New()
	f.crs = append(f.crs, cr)
	return cr, nil
}
func (f *fakeStore) FindTaskByRef(_ context.Context, _ uuid.UUID, prefix string, n int) (uuid.UUID, bool, error) {
	id, ok := f.tasks[strings.ToLower(prefix)+"-"+itoa(n)]
	return id, ok, nil
}
func (f *fakeStore) LinkTaskToChangeRequest(_ context.Context, task, _ uuid.UUID) error {
	f.taskLinks = append(f.taskLinks, task)
	return nil
}
func (f *fakeStore) LinkTaskBranch(_ context.Context, _, _ uuid.UUID, b string) error {
	f.branches = append(f.branches, b)
	return nil
}
func (f *fakeStore) AppendActionLog(_ context.Context, e *gitdom.ActionLogEntry) error {
	f.log = append(f.log, e)
	return nil
}

func (f *fakeStore) WebhookAttempts(_ context.Context, project uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	for _, e := range f.log {
		if e.Tool == "webhook.register" && e.RepositoryID != nil && e.ProjectID != nil && *e.ProjectID == project {
			out[*e.RepositoryID] = true
		}
	}
	return out, nil
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

type fakeProv struct {
	begin     func(gitprovider.BeginSetupRequest) (gitprovider.SetupStep, error)
	complete  func(gitprovider.CompleteSetupRequest) (gitprovider.Identity, error)
	deleteErr error
	deleted   []string
	info      gitprovider.Info

	beginAccess    func(gitprovider.BeginAccessRequest) (gitprovider.SetupStep, error)
	completeAccess func(gitprovider.CompleteAccessRequest) (gitprovider.AccessGrant, error)
	resolve        func(gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error)
	events         []gitprovider.Event
	verifyEr       error
	mints          int
	mintEr         error
	exp            time.Time
	lastMint       gitprovider.MintRequest

	ensured   []gitprovider.EnsureWebhookRequest
	ensureErr error
	removed   []gitprovider.RemoveWebhookRequest
	describes int
}

func (p *fakeProv) EnsureWebhook(_ context.Context, r gitprovider.EnsureWebhookRequest) (gitprovider.WebhookRef, error) {
	if p.ensureErr != nil {
		return gitprovider.WebhookRef{}, p.ensureErr
	}
	p.ensured = append(p.ensured, r)
	return gitprovider.WebhookRef{ID: "hook-1"}, nil
}
func (p *fakeProv) RemoveWebhook(_ context.Context, r gitprovider.RemoveWebhookRequest) error {
	p.removed = append(p.removed, r)
	return nil
}

func (p *fakeProv) BeginIdentitySetup(_ context.Context, r gitprovider.BeginSetupRequest) (gitprovider.SetupStep, error) {
	return p.begin(r)
}
func (p *fakeProv) CompleteIdentitySetup(_ context.Context, r gitprovider.CompleteSetupRequest) (gitprovider.Identity, error) {
	return p.complete(r)
}
func (p *fakeProv) BeginAccessGrant(_ context.Context, r gitprovider.BeginAccessRequest) (gitprovider.SetupStep, error) {
	return p.beginAccess(r)
}
func (p *fakeProv) CompleteAccessGrant(_ context.Context, r gitprovider.CompleteAccessRequest) (gitprovider.AccessGrant, error) {
	return p.completeAccess(r)
}
func (p *fakeProv) DeleteIdentity(_ context.Context, r gitprovider.DeleteIdentityRequest) error {
	p.deleted = append(p.deleted, r.Identity.ID)
	return p.deleteErr
}
func (p *fakeProv) ResolveRepository(_ context.Context, r gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
	return p.resolve(r)
}
func (p *fakeProv) VerifyAndParseWebhook(context.Context, gitprovider.WebhookRequest) ([]gitprovider.Event, error) {
	return p.events, p.verifyEr
}
func (p *fakeProv) MintCredential(_ context.Context, r gitprovider.MintRequest) (gitprovider.Credential, error) {
	p.mints++
	p.lastMint = r
	if p.mintEr != nil {
		return gitprovider.Credential{}, p.mintEr
	}
	return gitprovider.Credential{Username: "x", Secret: "tok" + itoa(p.mints), ExpiresAt: p.exp}, nil
}

type fakeProviders struct {
	p   ProviderAPI
	err error
}

func (f fakeProviders) Provider(context.Context, string) (ProviderAPI, error) { return f.p, f.err }
func (f fakeProviders) Describe(context.Context, string) (gitprovider.Info, error) {
	if f.err != nil {
		return gitprovider.Info{}, f.err
	}
	if fp, ok := f.p.(*fakeProv); ok {
		fp.describes++
		return fp.info, nil
	}
	if fp, ok := f.p.(interface{ providerInfo() gitprovider.Info }); ok {
		return fp.providerInfo(), nil
	}
	return gitprovider.Info{}, nil
}
func (f fakeProviders) List(ctx context.Context) ([]gitprovider.Info, error) {
	if f.err != nil {
		return nil, f.err
	}
	i, _ := f.Describe(ctx, "")
	return []gitprovider.Info{i}, nil
}

func setup(t *testing.T) (*Service, *fakeStore, *fakeProv) {
	t.Helper()
	st, pr := newFakeStore(), &fakeProv{}
	return New(st, fakeProviders{p: pr}), st, pr
}

func TestLinkRepository(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setup(t)
	shared := st.addIdentity("paca-bot", "app-1")
	pr.resolve = func(r gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		if len(r.Candidates) != 1 || r.Candidates[0].ID != "app-1" {
			t.Errorf("candidates = %+v", r.Candidates)
		}
		return gitprovider.ResolvedRepository{
			Repository: gitprovider.Repository{Ref: gitprovider.RepoRef{Host: "git.example", FullPath: "acme/web", ExternalID: "9", DefaultBranch: "main", CloneURL: "https://git.example/acme/web.git"}},
			Identity:   gitprovider.IdentityRef{ID: "app-1"},
		}, nil
	}
	project := uuid.New()
	r, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", Host: "git.example", PathOrURL: "https://git.example/acme/web"})
	if err != nil {
		t.Fatal(err)
	}
	if r.IdentityID == nil || *r.IdentityID != shared.ID || r.DefaultBranch != "main" || r.ProjectID != project {
		t.Fatalf("linked = %+v", r)
	}
	if _, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", Host: "git.example", PathOrURL: "acme/web"}); !errors.Is(err, gitdom.ErrAlreadyLinked) {
		t.Fatalf("second link: %v", err)
	}
	if _, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", PathOrURL: "  "}); err == nil {
		t.Fatal("empty path must be rejected")
	}
}

func TestLinkRepository_Description(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setup(t)
	st.addIdentity("paca-bot", "app-1")
	pr.resolve = func(r gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		return gitprovider.ResolvedRepository{
			Repository: gitprovider.Repository{Ref: gitprovider.RepoRef{Host: "git.example", FullPath: "acme/web"}},
			Identity:   gitprovider.IdentityRef{ID: "app-1"},
		}, nil
	}
	project := uuid.New()
	long := strings.Repeat("é", MaxDescriptionRunes+1)
	if _, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", PathOrURL: "acme/web", Description: long}); !errors.Is(err, gitdom.ErrDescriptionTooLong) {
		t.Fatalf("too long: %v", err)
	}
	r, err := svc.LinkRepository(ctx, LinkInput{ProjectID: project, ProviderID: "p", PathOrURL: "acme/web", Description: "  Web client  "})
	if err != nil || r.Description != "Web client" {
		t.Fatalf("link = %+v %v", r, err)
	}
}

func TestUpdateRepository(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := setup(t)
	project := uuid.New()
	repo := &gitdom.Repository{ProjectID: project, FullPath: "acme/web"}
	if err := st.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	got, err := svc.UpdateRepository(ctx, project, repo.ID, "  API  ")
	if err != nil || got.Description != "API" || st.repos[repo.ID].Description != "API" {
		t.Fatalf("update = %+v %v", got, err)
	}
	if _, err := svc.UpdateRepository(ctx, project, repo.ID, strings.Repeat("x", MaxDescriptionRunes)); err != nil {
		t.Fatalf("the limit itself is allowed: %v", err)
	}
	if _, err := svc.UpdateRepository(ctx, project, repo.ID, strings.Repeat("x", MaxDescriptionRunes+1)); !errors.Is(err, gitdom.ErrDescriptionTooLong) {
		t.Fatalf("too long: %v", err)
	}
	if _, err := svc.UpdateRepository(ctx, uuid.New(), repo.ID, "x"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("another project's repository: %v", err)
	}
	if _, err := svc.UpdateRepository(ctx, project, uuid.New(), "x"); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("unknown repository: %v", err)
	}
}

func TestLinkRepository_NoIdentityForHost(t *testing.T) {
	svc, st, _ := setup(t)
	st.addIdentity("bot", "a")
	_, err := svc.LinkRepository(context.Background(), LinkInput{ProjectID: uuid.New(), ProviderID: "p", Host: "other.example", PathOrURL: "x/y"})
	if !errors.Is(err, gitdom.ErrNoIdentity) {
		t.Fatalf("got %v", err)
	}
}

func TestLinkRepository_ProviderErrorsPropagate(t *testing.T) {
	svc, st, pr := setup(t)
	st.addIdentity("bot", "a")
	pr.resolve = func(gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		return gitprovider.ResolvedRepository{}, &gitprovider.Error{Code: gitprovider.CodeNotFound}
	}
	_, err := svc.LinkRepository(context.Background(), LinkInput{ProjectID: uuid.New(), ProviderID: "p", PathOrURL: "x/y"})
	if !errors.Is(err, gitdom.ErrNotVisible) {
		t.Fatalf("a repository no connection sees (and no grant to offer): got %v", err)
	}
	pr.resolve = func(gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		return gitprovider.ResolvedRepository{}, &gitprovider.Error{Code: gitprovider.CodeRateLimited}
	}
	if _, err := svc.LinkRepository(context.Background(), LinkInput{ProjectID: uuid.New(), ProviderID: "p", PathOrURL: "x/y"}); !gitprovider.IsCode(err, gitprovider.CodeRateLimited) {
		t.Fatalf("other provider errors still propagate: got %v", err)
	}
	down := New(st, fakeProviders{err: gitdom.ErrProviderUnavailable})
	if _, err := down.LinkRepository(context.Background(), LinkInput{ProviderID: "p", PathOrURL: "x/y"}); !errors.Is(err, gitdom.ErrProviderUnavailable) {
		t.Fatalf("provider down: %v", err)
	}
}

func linked(st *fakeStore, project uuid.UUID, path string) *gitdom.Repository {
	r := &gitdom.Repository{ProjectID: project, ProviderID: "p", Host: "git.example", FullPath: path}
	_ = st.CreateRepository(context.Background(), r)
	return r
}

func prEvent(id, action, head, title string) gitprovider.Event {
	return gitprovider.Event{ID: id, Kind: gitprovider.EventChangeRequest, Action: action, Text: title,
		Repo: gitprovider.RepoRef{Host: "git.example", FullPath: "Acme/Web"},
		Subject: &gitprovider.EventSubject{Kind: gitprovider.SubjectChangeRequest, Number: "7", Base: "main",
			Head: &gitprovider.Branch{Ref: head, SHA: "abc"}, URL: "https://git.example/acme/web/pull/7"}}
}

func TestHandleWebhook_FansOutAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setup(t)
	ident := st.addIdentity("bot", "a")
	st.endpoints["k"] = &gitdom.WebhookEndpoint{Key: "k", ProviderID: "p", IdentityID: &ident.ID}
	p1, p2 := uuid.New(), uuid.New()
	linked(st, p1, "acme/web")
	linked(st, p2, "acme/web")
	task := uuid.New()
	st.tasks["paca-12"] = task
	pr.events = []gitprovider.Event{prEvent("e1", "opened", "feat/PACA-12-login", "Add login"),
		{ID: "e2", Kind: gitprovider.EventPush, Repo: gitprovider.RepoRef{Host: "git.example", FullPath: "other/repo"}}}

	res, err := svc.HandleWebhook(ctx, "k", Delivery{Method: "POST", Body: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Events != 2 || res.Stored != 2 || res.Unlinked != 1 || res.Replayed != 0 {
		t.Fatalf("result = %+v", res)
	}
	if len(st.crs) != 2 || st.crs[0].HeadRef != "feat/PACA-12-login" || st.crs[0].State != "open" || st.crs[0].Title != "Add login" || st.crs[0].URL != "https://git.example/acme/web/pull/7" {
		t.Fatalf("change requests = %+v", st.crs)
	}
	if len(st.taskLinks) != 2 || st.branches[0] != "feat/PACA-12-login" {
		t.Fatalf("task links=%v branches=%v", st.taskLinks, st.branches)
	}

	again, err := svc.HandleWebhook(ctx, "k", Delivery{Method: "POST"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Stored != 0 || again.Replayed != 2 || len(st.crs) != 2 {
		t.Fatalf("redelivery must change nothing: %+v crs=%d", again, len(st.crs))
	}
}

func TestHandleWebhook_RejectsBadInput(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setup(t)
	ident := st.addIdentity("bot", "a")
	st.endpoints["k"] = &gitdom.WebhookEndpoint{Key: "k", ProviderID: "p", IdentityID: &ident.ID}
	st.endpoints["pending"] = &gitdom.WebhookEndpoint{Key: "pending", ProviderID: "p"}

	if _, err := svc.HandleWebhook(ctx, "missing", Delivery{}); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("unknown endpoint: %v", err)
	}
	if _, err := svc.HandleWebhook(ctx, "pending", Delivery{}); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("unbound endpoint: %v", err)
	}
	pr.verifyEr = &gitprovider.Error{Code: gitprovider.CodeInvalidSignature}
	if _, err := svc.HandleWebhook(ctx, "k", Delivery{}); !errors.Is(err, gitdom.ErrInvalidSignature) {
		t.Fatalf("bad signature: %v", err)
	}
	if len(st.inbox) != 0 {
		t.Fatal("nothing may be stored for an unverified delivery")
	}
	pr.verifyEr = &gitprovider.Error{Code: gitprovider.CodeTransient}
	if _, err := svc.HandleWebhook(ctx, "k", Delivery{}); !gitprovider.IsCode(err, gitprovider.CodeTransient) {
		t.Fatalf("transient: %v", err)
	}
}

func TestHandleWebhook_ClosedAndUnknownTaskRef(t *testing.T) {
	ctx := context.Background()
	svc, st, pr := setup(t)
	ident := st.addIdentity("bot", "a")
	st.endpoints["k"] = &gitdom.WebhookEndpoint{Key: "k", ProviderID: "p", IdentityID: &ident.ID}
	linked(st, uuid.New(), "acme/web")
	pr.events = []gitprovider.Event{prEvent("e1", "closed", "fix/NOPE-99", "x")}
	if _, err := svc.HandleWebhook(ctx, "k", Delivery{}); err != nil {
		t.Fatal(err)
	}
	if st.crs[0].State != "closed" || len(st.taskLinks) != 0 {
		t.Fatalf("state=%s links=%v", st.crs[0].State, st.taskLinks)
	}
}

func mintSetup(t *testing.T) (*Service, *fakeStore, *fakeProv, *gitdom.Repository, *time.Time) {
	svc, st, pr := setup(t)
	shared := st.addIdentity("bot", "shared-app")
	repo := &gitdom.Repository{ProjectID: uuid.New(), ProviderID: "p", Host: "git.example", FullPath: "acme/web", IdentityID: &shared.ID}
	_ = st.CreateRepository(context.Background(), repo)
	clock := time.Unix(1_700_000_000, 0)
	svc.now = func() time.Time { return clock }
	return svc, st, pr, repo, &clock
}

func TestMintCredential_CachesBriefly(t *testing.T) {
	ctx := context.Background()
	svc, st, pr, repo, clock := mintSetup(t)
	in := MintInput{RepositoryID: repo.ID, Access: gitprovider.AccessRead}

	a, err := svc.MintCredential(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := svc.MintCredential(ctx, in)
	if pr.mints != 1 || a.Secret != b.Secret {
		t.Fatalf("within the TTL the credential is reused: mints=%d", pr.mints)
	}
	if _, _ = svc.MintCredential(ctx, MintInput{RepositoryID: repo.ID, Access: gitprovider.AccessWrite}); pr.mints != 2 {
		t.Fatal("write access must not reuse a read credential")
	}
	*clock = clock.Add(credentialCacheTTL + time.Second)
	if _, _ = svc.MintCredential(ctx, in); pr.mints != 3 {
		t.Fatal("expired cache entry must be re-minted")
	}
	if pr.lastMint.Target.Repo.FullPath != "acme/web" || pr.lastMint.Target.Identity.ID != "shared-app" {
		t.Fatalf("target = %+v", pr.lastMint.Target)
	}
	// Every issue is audited, including the one served from the cache.
	if len(st.log) != 4 || !st.log[0].OK || st.log[0].Tool != "credential.mint" {
		t.Fatalf("audit = %+v", st.log)
	}
	if st.log[0].Detail != "read" || st.log[1].Detail != "read (cached)" {
		t.Fatalf("audit details = %q, %q", st.log[0].Detail, st.log[1].Detail)
	}
}

func TestMintCredential_NeverCachedPastExpiry(t *testing.T) {
	ctx := context.Background()
	svc, _, pr, repo, clock := mintSetup(t)
	pr.exp = clock.Add(40 * time.Second) // within TTL + margin: reuse window shrinks to 10s
	in := MintInput{RepositoryID: repo.ID, Access: gitprovider.AccessRead}
	_, _ = svc.MintCredential(ctx, in)
	*clock = clock.Add(11 * time.Second)
	_, _ = svc.MintCredential(ctx, in)
	if pr.mints != 2 {
		t.Fatalf("mints = %d; a nearly-expired credential must not be reused", pr.mints)
	}
	pr.exp = clock.Add(5 * time.Second) // already inside the margin: not cached at all
	_, _ = svc.MintCredential(ctx, MintInput{RepositoryID: repo.ID, Access: gitprovider.AccessWrite})
	_, _ = svc.MintCredential(ctx, MintInput{RepositoryID: repo.ID, Access: gitprovider.AccessWrite})
	if pr.mints != 4 {
		t.Fatalf("mints = %d", pr.mints)
	}
}

func TestMintCredential_IdentityChoice(t *testing.T) {
	ctx := context.Background()
	svc, st, pr, repo, _ := mintSetup(t)
	agent := uuid.New()
	own := &gitdom.Identity{ID: uuid.New(), ProviderID: "p", Host: "git.example", ProviderIdentityID: "agent-app", Scope: gitdom.ScopeAgent}
	st.agentIDs[agent] = own

	if _, err := svc.MintCredential(ctx, MintInput{RepositoryID: repo.ID, Access: gitprovider.AccessRead, AgentID: &agent}); err != nil {
		t.Fatal(err)
	}
	if pr.lastMint.Target.Identity.ID != "agent-app" {
		t.Fatalf("an agent's own identity wins: %+v", pr.lastMint.Target.Identity)
	}
	other := uuid.New() // no identity of its own: falls back to the repository's
	_, _ = svc.MintCredential(ctx, MintInput{RepositoryID: repo.ID, Access: gitprovider.AccessRead, AgentID: &other})
	if pr.lastMint.Target.Identity.ID != "shared-app" {
		t.Fatalf("fallback: %+v", pr.lastMint.Target.Identity)
	}
	// A repository whose connection is gone has no credential: another
	// connection of the host is not its own, so there is no fallback.
	repo.IdentityID = nil
	pr.mints = 0
	svc2 := New(st, fakeProviders{p: pr})
	if _, err := svc2.MintCredential(ctx, MintInput{RepositoryID: repo.ID, Access: gitprovider.AccessRead}); !errors.Is(err, gitdom.ErrNoIdentity) || pr.mints != 0 {
		t.Fatalf("no fallback to another connection: %v mints=%d", err, pr.mints)
	}
}

func TestMintCredential_ScopedToProjectAndAudited(t *testing.T) {
	ctx := context.Background()
	svc, st, pr, repo, _ := mintSetup(t)
	wrong := uuid.New()
	if _, err := svc.MintCredential(ctx, MintInput{RepositoryID: repo.ID, Access: gitprovider.AccessRead, ProjectID: &wrong}); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("other project's repository: %v", err)
	}
	if pr.mints != 0 {
		t.Fatal("must not mint for a repository outside the caller's project")
	}
	pr.mintEr = &gitprovider.Error{Code: gitprovider.CodeForbidden, Message: "no"}
	_, err := svc.MintCredential(ctx, MintInput{RepositoryID: repo.ID, Access: gitprovider.AccessWrite, ProjectID: &repo.ProjectID})
	if !gitprovider.IsCode(err, gitprovider.CodeForbidden) {
		t.Fatalf("got %v", err)
	}
	last := st.log[len(st.log)-1]
	if last.OK || !strings.Contains(last.Detail, "forbidden") {
		t.Fatalf("failure must be audited with its code: %+v", last)
	}
	if _, err := svc.MintCredential(ctx, MintInput{RepositoryID: uuid.New(), Access: gitprovider.AccessRead}); !errors.Is(err, gitdom.ErrNotFound) {
		t.Fatalf("unknown repository: %v", err)
	}
}

// trackStore lets a test read back and break the change-request cache.
type trackStore struct {
	*fakeStore
	cur       *gitdom.ChangeRequest
	upsertErr error
	deleted   []string
}

func (s *trackStore) FindChangeRequest(context.Context, uuid.UUID, string) (*gitdom.ChangeRequest, error) {
	if s.cur == nil {
		return nil, gitdom.ErrNotFound
	}
	return s.cur, nil
}
func (s *trackStore) UpsertChangeRequest(ctx context.Context, cr *gitdom.ChangeRequest) (*gitdom.ChangeRequest, error) {
	if s.upsertErr != nil {
		return nil, s.upsertErr
	}
	return s.fakeStore.UpsertChangeRequest(ctx, cr)
}
func (s *trackStore) DeleteInboxEvent(_ context.Context, id string, repo uuid.UUID) error {
	s.deleted = append(s.deleted, id)
	delete(s.inbox, id+"|"+repo.String())
	return nil
}

func TestHandleWebhook_AReviewNeverRewindsALifecycleState(t *testing.T) {
	ctx := context.Background()
	fs, pr := newFakeStore(), &fakeProv{}
	ts := &trackStore{fakeStore: fs}
	svc := New(ts, fakeProviders{p: pr})
	ident := fs.addIdentity("bot", "a")
	fs.endpoints["k"] = &gitdom.WebhookEndpoint{Key: "k", ProviderID: "p", IdentityID: &ident.ID}
	linked(fs, uuid.New(), "acme/web")
	merged := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ts.cur = &gitdom.ChangeRequest{Number: "7", State: "merged", Draft: false, MergedAt: &merged}

	review := prEvent("r1", "submitted", "feat/x", "")
	review.Kind = gitprovider.EventReview
	pr.events = []gitprovider.Event{review}
	if _, err := svc.HandleWebhook(ctx, "k", Delivery{Method: "POST"}); err != nil {
		t.Fatal(err)
	}
	saved := fs.crs[len(fs.crs)-1]
	if saved.State != "merged" || saved.MergedAt == nil || !saved.MergedAt.Equal(merged) {
		t.Fatalf("a review on a merged request must keep it merged: %+v", saved)
	}

	// A "merged" event records when.
	pr.events = []gitprovider.Event{prEvent("m1", "merged", "feat/x", "Add")}
	_, _ = svc.HandleWebhook(ctx, "k", Delivery{Method: "POST"})
	if got := fs.crs[len(fs.crs)-1]; got.State != "merged" || got.MergedAt == nil {
		t.Fatalf("merged event: %+v", got)
	}
	// And a reopening clears it.
	pr.events = []gitprovider.Event{prEvent("o1", "reopened", "feat/x", "Add")}
	_, _ = svc.HandleWebhook(ctx, "k", Delivery{Method: "POST"})
	if got := fs.crs[len(fs.crs)-1]; got.State != "open" || got.MergedAt != nil {
		t.Fatalf("an update of an open request: %+v", got)
	}
}

func TestHandleWebhook_EditsAndPushesKeepTheCachedState(t *testing.T) {
	ctx := context.Background()
	merged := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	cases := []struct {
		name, cached, action, want string
		mergedAt                   bool
	}{
		{"edited after merged", "merged", "edited", "merged", true},
		{"synchronized after closed", "closed", "synchronized", "closed", false},
		{"reopened after closed", "closed", "reopened", "open", false},
		{"reopened after merged", "merged", "reopened", "open", false},
		{"edited on a request never seen", "", "edited", "open", false},
		{"synchronized on an open request", "open", "synchronized", "open", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs, pr := newFakeStore(), &fakeProv{}
			ts := &trackStore{fakeStore: fs}
			svc := New(ts, fakeProviders{p: pr})
			ident := fs.addIdentity("bot", "a")
			fs.endpoints["k"] = &gitdom.WebhookEndpoint{Key: "k", ProviderID: "p", IdentityID: &ident.ID}
			linked(fs, uuid.New(), "acme/web")
			if c.cached != "" {
				ts.cur = &gitdom.ChangeRequest{Number: "7", State: c.cached}
				if c.cached == "merged" {
					ts.cur.MergedAt = &merged
				}
			}
			pr.events = []gitprovider.Event{prEvent("e1", c.action, "feat/x", "Add")}
			if _, err := svc.HandleWebhook(ctx, "k", Delivery{Method: "POST"}); err != nil {
				t.Fatal(err)
			}
			got := fs.crs[len(fs.crs)-1]
			if got.State != c.want || (got.MergedAt != nil) != c.mergedAt {
				t.Fatalf("state %q mergedAt %v, want %q (merged_at set: %v)", got.State, got.MergedAt, c.want, c.mergedAt)
			}
		})
	}
}

func TestHandleWebhook_AFailedCacheUpdateDoesNotMakeTheRetryAReplay(t *testing.T) {
	ctx := context.Background()
	fs, pr := newFakeStore(), &fakeProv{}
	ts := &trackStore{fakeStore: fs, upsertErr: errors.New("db down")}
	svc := New(ts, fakeProviders{p: pr})
	ident := fs.addIdentity("bot", "a")
	fs.endpoints["k"] = &gitdom.WebhookEndpoint{Key: "k", ProviderID: "p", IdentityID: &ident.ID}
	linked(fs, uuid.New(), "acme/web")
	pr.events = []gitprovider.Event{prEvent("e1", "opened", "feat/x", "Add")}

	if _, err := svc.HandleWebhook(ctx, "k", Delivery{Method: "POST"}); err == nil {
		t.Fatal("the failure must reach the platform so it retries")
	}
	if len(ts.deleted) != 1 || ts.deleted[0] != "e1" {
		t.Fatalf("the inbox row must be removed: %v", ts.deleted)
	}
	ts.upsertErr = nil
	res, err := svc.HandleWebhook(ctx, "k", Delivery{Method: "POST"})
	if err != nil || res.Stored != 1 || res.Replayed != 0 || len(fs.crs) != 1 {
		t.Fatalf("the redelivery must be processed, not dropped: %+v %v crs=%d", res, err, len(fs.crs))
	}
}

func TestLinkRepository_RejectsAnIdentityThatWasNotOffered(t *testing.T) {
	svc, st, pr := setup(t)
	st.addIdentity("bot", "app-1")
	pr.resolve = func(gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		return gitprovider.ResolvedRepository{
			Repository: gitprovider.Repository{Ref: gitprovider.RepoRef{Host: "git.example", FullPath: "acme/web"}},
			Identity:   gitprovider.IdentityRef{ID: "someone-elses-app"},
		}, nil
	}
	if _, err := svc.LinkRepository(context.Background(), LinkInput{ProjectID: uuid.New(), ProviderID: "p", PathOrURL: "acme/web"}); err == nil {
		t.Fatal("an identity the provider was not offered must be rejected")
	}
}

func TestCheckWebhookEndpoint(t *testing.T) {
	svc, st, _ := setup(t)
	ident := st.addIdentity("bot", "app-1")
	st.endpoints["ready"] = &gitdom.WebhookEndpoint{Key: "ready", ProviderID: "p", IdentityID: &ident.ID}
	st.endpoints["pending"] = &gitdom.WebhookEndpoint{Key: "pending", ProviderID: "p"}
	ctx := context.Background()
	if err := svc.CheckWebhookEndpoint(ctx, "ready"); err != nil {
		t.Errorf("ready: %v", err)
	}
	for _, k := range []string{"pending", "unknown"} {
		if err := svc.CheckWebhookEndpoint(ctx, k); !errors.Is(err, gitdom.ErrNotFound) {
			t.Errorf("%s: %v", k, err)
		}
	}
}

func TestWebhookKeysThatCannotBeOursAreNotFound(t *testing.T) {
	svc, st, _ := setup(t)
	ident := st.addIdentity("bot", "app-1")
	st.endpoints["ready"] = &gitdom.WebhookEndpoint{Key: "ready", ProviderID: "p", IdentityID: &ident.ID}
	ctx := context.Background()
	for _, k := range []string{"", "a\x00b", strings.Repeat("a", 129), "a b", "../x", "k'--", "\u2603"} {
		if err := svc.CheckWebhookEndpoint(ctx, k); !errors.Is(err, gitdom.ErrNotFound) {
			t.Errorf("Check(%q) = %v", k, err)
		}
		if _, err := svc.HandleWebhook(ctx, k, Delivery{}); !errors.Is(err, gitdom.ErrNotFound) {
			t.Errorf("Handle(%q) = %v", k, err)
		}
	}
	if err := svc.CheckWebhookEndpoint(ctx, "ready"); err != nil {
		t.Errorf("a real key: %v", err)
	}
}

func TestLinkRepository_ProviderMessageRidesAlongNotVisible(t *testing.T) {
	svc, st, pr := setup(t)
	st.addIdentity("bot", "a")
	const msg = "The connected project token covers only acme/ui. Add a connection for acme/api."
	for _, code := range []gitprovider.ErrorCode{gitprovider.CodeNotFound, gitprovider.CodeForbidden} {
		pr.resolve = func(gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
			return gitprovider.ResolvedRepository{}, &gitprovider.Error{Code: code, Message: msg}
		}
		_, err := svc.LinkRepository(context.Background(), LinkInput{ProjectID: uuid.New(), ProviderID: "p", PathOrURL: "acme/api"})
		var de *gitdom.DetailedError
		if !errors.Is(err, gitdom.ErrNotVisible) || !errors.As(err, &de) || de.Detail != msg {
			t.Fatalf("%s: got %v (%+v)", code, err, de)
		}
		if err.Error() != gitdom.ErrNotVisible.Error() {
			t.Fatalf("text must stay the sentinel's: %q", err.Error())
		}
	}
	// No message from the provider: the bare sentinel, as before.
	pr.resolve = func(gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error) {
		return gitprovider.ResolvedRepository{}, &gitprovider.Error{Code: gitprovider.CodeNotFound}
	}
	_, err := svc.LinkRepository(context.Background(), LinkInput{ProjectID: uuid.New(), ProviderID: "p", PathOrURL: "acme/api"})
	if err != gitdom.ErrNotVisible {
		t.Fatalf("got %#v", err)
	}
}

func TestProviderMessage(t *testing.T) {
	if ProviderMessage(errors.New("plain")) != "" || ProviderMessage(nil) != "" {
		t.Fatal("only provider failures carry a message")
	}
	got := ProviderMessage(&gitprovider.Error{Message: " a\nb\x00 ghp_" + strings.Repeat("z", 30) + " "})
	if got != "a b ***" {
		t.Fatalf("got %q", got)
	}
	long := ProviderMessage(&gitprovider.Error{Message: strings.Repeat("é", 700)})
	if r := []rune(long); len(r) != 501 || r[500] != '…' {
		t.Fatalf("cap: %d", len(r))
	}
}

func TestFindTaskRefs(t *testing.T) {
	cases := []struct {
		in   string
		want []taskRef
	}{
		{"feat/PACA-12-login", []taskRef{{"PACA", 12}}},
		{"Fix PACA-12", []taskRef{{"PACA", 12}}},
		{"paca/PACA-1", []taskRef{{"PACA", 1}}},
		{"fix-PACA-12-login", []taskRef{{"PACA", 12}}},
		{"feat-paca-12", []taskRef{{"paca", 12}}},
		{"TRIM-UI-200", nil},
		{"fix TRIM-UI-200 and PACA-7", []taskRef{{"PACA", 7}}},
	}
	for _, c := range cases {
		got := findTaskRefs(c.in)
		if len(got) != len(c.want) {
			t.Errorf("findTaskRefs(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("findTaskRefs(%q) = %v, want %v", c.in, got, c.want)
			}
		}
	}
}
