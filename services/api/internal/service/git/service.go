// Package gitsvc orchestrates the platform-neutral Git integration: it links
// repositories to projects, ingests webhook deliveries from provider plugins
// into the event inbox, and mints short-lived credentials for the workspace
// tools. Platform specifics stay behind gitprovider.Client.
package gitsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

// credentialCacheTTL bounds how long a minted credential is reused in memory.
// It is never persisted and never longer than the credential's own lifetime.
const credentialCacheTTL = 30 * time.Second

// ProviderAPI is the part of gitprovider.Client the service uses.
type ProviderAPI interface {
	ResolveRepository(ctx context.Context, req gitprovider.ResolveRepoRequest) (gitprovider.ResolvedRepository, error)
	VerifyAndParseWebhook(ctx context.Context, req gitprovider.WebhookRequest) ([]gitprovider.Event, error)
	MintCredential(ctx context.Context, req gitprovider.MintRequest) (gitprovider.Credential, error)
	BeginIdentitySetup(ctx context.Context, req gitprovider.BeginSetupRequest) (gitprovider.SetupStep, error)
	CompleteIdentitySetup(ctx context.Context, req gitprovider.CompleteSetupRequest) (gitprovider.Identity, error)
	DeleteIdentity(ctx context.Context, req gitprovider.DeleteIdentityRequest) error
	BeginAccessGrant(ctx context.Context, req gitprovider.BeginAccessRequest) (gitprovider.SetupStep, error)
	CompleteAccessGrant(ctx context.Context, req gitprovider.CompleteAccessRequest) (gitprovider.AccessGrant, error)
	EnsureWebhook(ctx context.Context, req gitprovider.EnsureWebhookRequest) (gitprovider.WebhookRef, error)
	RemoveWebhook(ctx context.Context, req gitprovider.RemoveWebhookRequest) error
}

// Providers finds the plugin that implements the Git provider SPI for a platform.
type Providers interface {
	Provider(ctx context.Context, providerID string) (ProviderAPI, error)
	// Describe returns a provider's static information and capabilities.
	Describe(ctx context.Context, providerID string) (gitprovider.Info, error)
	// List describes every enabled provider.
	List(ctx context.Context) ([]gitprovider.Info, error)
}

// Service is the Git integration use-case layer.
type Service struct {
	store     gitdom.Store
	providers Providers
	now       func() time.Time
	publicURL string // the instance's externally reachable base URL, no trailing slash

	namer AgentNamer // optional: names the acting agent in comment footers
	dir   AgentDirectory

	mu     sync.Mutex
	cache  map[credKey]cachedCred
	reruns map[uuid.UUID][]time.Time // check re-runs per repository, last hour
	ci     map[ciKey]cachedCI        // CI summaries of change-request heads
}

// New returns a Service.
func New(store gitdom.Store, providers Providers) *Service {
	return &Service{store: store, providers: providers, now: time.Now, cache: map[credKey]cachedCred{}, reruns: map[uuid.UUID][]time.Time{}}
}

type credKey struct {
	identity uuid.UUID
	repo     uuid.UUID
	access   gitprovider.Access
}

type cachedCred struct {
	cred    gitprovider.Credential
	expires time.Time
}

func identityRef(id *gitdom.Identity) gitprovider.IdentityRef {
	return gitprovider.IdentityRef{ID: id.ProviderIdentityID, Handle: id.Handle, ProviderRef: id.ProviderRef}
}

func repoRef(r *gitdom.Repository) gitprovider.RepoRef {
	return gitprovider.RepoRef{Host: r.Host, FullPath: r.FullPath, ExternalID: r.ExternalID,
		DefaultBranch: r.DefaultBranch, CloneURL: r.CloneURL, ProviderRef: r.ProviderRef}
}

// LinkInput describes a repository a user wants to link to a project.
type LinkInput struct {
	ProjectID  uuid.UUID
	ProviderID string
	Host       string
	PathOrURL  string
	// IdentityID names the connection to link through. Empty: any connection the
	// project can use is tried, and the provider reports which one sees it.
	IdentityID *uuid.UUID
	ActorID    *uuid.UUID
	// Description says what the repository is for (optional, at most
	// MaxDescriptionRunes characters).
	Description string
}

// MaxDescriptionRunes caps a repository description.
const MaxDescriptionRunes = 1000

// normalizeDescription trims a description and rejects one that is too long.
func normalizeDescription(d string) (string, error) {
	d = strings.TrimSpace(d)
	if utf8.RuneCountInString(d) > MaxDescriptionRunes {
		return "", fmt.Errorf("%w: at most %d characters", gitdom.ErrDescriptionTooLong, MaxDescriptionRunes)
	}
	return d, nil
}

// UpdateRepository changes the description of a repository linked to the project.
func (s *Service) UpdateRepository(ctx context.Context, projectID, repositoryID uuid.UUID, description string) (*gitdom.Repository, error) {
	description, err := normalizeDescription(description)
	if err != nil {
		return nil, err
	}
	repo, err := s.store.FindRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if repo.ProjectID != projectID {
		return nil, gitdom.ErrNotFound
	}
	if err := s.store.SetRepositoryDescription(ctx, projectID, repositoryID, description); err != nil {
		return nil, err
	}
	repo.Description = description
	return repo, nil
}

// LinkRepository resolves a user-supplied path or URL through the provider,
// choosing the identity that can see it, and links it to the project.
func (s *Service) LinkRepository(ctx context.Context, in LinkInput) (*gitdom.Repository, error) {
	if strings.TrimSpace(in.PathOrURL) == "" {
		return nil, fmt.Errorf("git: repository is required")
	}
	description, err := normalizeDescription(in.Description)
	if err != nil {
		return nil, err
	}
	ident, res, err := s.resolveThroughConnections(ctx, in.ProjectID, in.ProviderID, in.Host, in.PathOrURL, in.IdentityID)
	if err != nil {
		return nil, err
	}
	ref := res.Repository.Ref
	r := &gitdom.Repository{
		ProjectID: in.ProjectID, ProviderID: in.ProviderID, Host: ref.Host, ExternalID: ref.ExternalID,
		FullPath: ref.FullPath, DefaultBranch: ref.DefaultBranch, CloneURL: ref.CloneURL,
		Description: description, IdentityID: &ident.ID, ProviderRef: ref.ProviderRef, CreatedBy: in.ActorID,
	}
	if err := s.store.CreateRepository(ctx, r); err != nil {
		return nil, err
	}
	// A connection whose platform needs a webhook per repository (a token) gets
	// one registered now. Failing to is not failing to link: the repository is
	// listed with the URL to register by hand, and RegisterWebhook retries.
	s.registerWebhook(ctx, r, ident)
	return r, nil
}

// resolveThroughConnections asks the provider which of the connections a project
// can use (or just the chosen one) sees a repository, and returns that
// connection's row with what the provider resolved.
func (s *Service) resolveThroughConnections(ctx context.Context, projectID uuid.UUID, providerID, host, pathOrURL string, identityID *uuid.UUID) (*gitdom.Identity, gitprovider.ResolvedRepository, error) {
	prov, err := s.providers.Provider(ctx, providerID)
	if err != nil {
		return nil, gitprovider.ResolvedRepository{}, err
	}
	// Candidates: the workspace's connections plus the project's own, or just
	// the one the caller chose.
	all, err := s.store.ListProjectIdentities(ctx, projectID)
	if err != nil {
		return nil, gitprovider.ResolvedRepository{}, err
	}
	var cands []gitprovider.IdentityRef
	var offered []*gitdom.Identity // parallel to cands
	for _, id := range all {
		if id.ProviderID != providerID || (host != "" && !strings.EqualFold(id.Host, host)) {
			continue
		}
		if identityID != nil && id.ID != *identityID {
			continue
		}
		cands = append(cands, identityRef(id))
		offered = append(offered, id)
	}
	if len(cands) == 0 {
		return nil, gitprovider.ResolvedRepository{}, gitdom.ErrNoIdentity
	}
	res, err := prov.ResolveRepository(ctx, gitprovider.ResolveRepoRequest{Host: host, PathOrURL: pathOrURL, Candidates: cands})
	if err != nil {
		// On a platform where a user grants an identity access per account (a
		// GitHub App is installed on an account), "not found" usually means the
		// grant is missing: say so, so the caller can offer it.
		if gitprovider.IsCode(err, gitprovider.CodeNotFound) || gitprovider.IsCode(err, gitprovider.CodeForbidden) {
			// The provider's own message (which token covers what) rides along as
			// a detail; the sentinel, and so the error code, is unchanged.
			detail := ProviderMessage(err)
			if info, derr := s.providers.Describe(ctx, providerID); derr == nil && info.Capabilities.AccessGrants {
				return nil, gitprovider.ResolvedRepository{}, gitdom.WithDetail(gitdom.ErrAccessNotGranted, detail)
			}
			// No grant to offer: the connections simply do not reach it (a project
			// token of another project, say). Say so instead of "refused".
			return nil, gitprovider.ResolvedRepository{}, gitdom.WithDetail(gitdom.ErrNotVisible, detail)
		}
		return nil, gitprovider.ResolvedRepository{}, err
	}
	// The plugin chooses which candidate saw the repository; it must be one we
	// offered, and the row is the offered one (not a lookup by the provider's id).
	for i, c := range cands {
		if c.ID == res.Identity.ID {
			return offered[i], res, nil
		}
	}
	return nil, gitprovider.ResolvedRepository{}, fmt.Errorf("git: provider resolved the repository with an identity that was not offered")
}

// ReconnectInput points a linked repository at a connection again.
type ReconnectInput struct {
	ProjectID    uuid.UUID
	RepositoryID uuid.UUID
	// IdentityID names the connection to use; empty: any the project can use.
	IdentityID *uuid.UUID
}

// ReconnectRepository rebinds a linked repository to a connection, for one
// whose own was deleted (it has no credential until then). The connection must
// see the repository, as when linking; the repository keeps its tasks, branches
// and change requests.
func (s *Service) ReconnectRepository(ctx context.Context, in ReconnectInput) (*gitdom.Repository, error) {
	repo, err := s.store.FindRepository(ctx, in.RepositoryID)
	if err != nil {
		return nil, err
	}
	if repo.ProjectID != in.ProjectID {
		return nil, gitdom.ErrNotFound
	}
	ident, res, err := s.resolveThroughConnections(ctx, in.ProjectID, repo.ProviderID, repo.Host, repo.FullPath, in.IdentityID)
	if err != nil {
		return nil, err
	}
	// The same repository: a platform path can be re-pointed, not swapped.
	if !strings.EqualFold(res.Repository.Ref.FullPath, repo.FullPath) {
		return nil, gitdom.ErrNotFound
	}
	// A repository that still has a connection may be re-pointed at another: the
	// webhook registered through the old one must not be left behind to deliver
	// every event a second time. (A repository whose connection was deleted has
	// none left to remove: deleting the connection removed it.)
	if repo.IdentityID != nil && *repo.IdentityID != ident.ID {
		s.unregisterWebhook(ctx, repo)
	}
	if err := s.store.SetRepositoryIdentity(ctx, repo.ID, ident.ID, res.Repository.Ref.ProviderRef); err != nil {
		return nil, err
	}
	repo.IdentityID, repo.ProviderRef, repo.WebhookRef = &ident.ID, res.Repository.Ref.ProviderRef, nil
	s.registerWebhook(ctx, repo, ident)
	repo.IdentityHandle = ident.Handle
	return repo, nil
}

// Delivery is one raw webhook request as received by the public endpoint.
type Delivery struct {
	Method  string
	Headers map[string]string // lower-cased keys
	Query   map[string]string
	Body    []byte
}

// WebhookResult summarizes an ingested delivery.
type WebhookResult struct {
	Events   int // normalized events the provider produced
	Stored   int // new inbox rows (events × linked repositories)
	Replayed int // rows already seen (redelivery)
	Unlinked int // events for repositories no project linked
}

// plausibleWebhookKey reports whether key could be one this instance issued
// (hex today, from randomKey in setup.go; letters, digits, "-" and "_" are
// allowed, so keep this in step with that generator). The endpoint is public,
// so anything else is turned away before it reaches the database, which would
// reject a NUL byte as an error instead of a miss.
func plausibleWebhookKey(key string) bool {
	if key == "" || len(key) > 128 {
		return false
	}
	for _, c := range key {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// CheckWebhookEndpoint reports whether key names a webhook endpoint that can
// receive deliveries, so the public handler can refuse before reading a body.
func (s *Service) CheckWebhookEndpoint(ctx context.Context, key string) error {
	if !plausibleWebhookKey(key) {
		return gitdom.ErrNotFound
	}
	ep, err := s.store.FindWebhookEndpoint(ctx, key)
	if err != nil {
		return err
	}
	if ep.IdentityID == nil {
		return gitdom.ErrNotFound
	}
	return nil
}

// HandleWebhook verifies a delivery through the provider and stores its events
// in the inbox, once per linked repository. A redelivery is a no-op.
func (s *Service) HandleWebhook(ctx context.Context, endpointKey string, d Delivery) (*WebhookResult, error) {
	if !plausibleWebhookKey(endpointKey) {
		return nil, gitdom.ErrNotFound
	}
	ep, err := s.store.FindWebhookEndpoint(ctx, endpointKey)
	if err != nil {
		return nil, err
	}
	if ep.IdentityID == nil {
		return nil, gitdom.ErrNotFound // setup has not completed; nothing to verify against
	}
	ident, err := s.store.FindIdentity(ctx, *ep.IdentityID)
	if err != nil {
		return nil, err
	}
	prov, err := s.providers.Provider(ctx, ep.ProviderID)
	if err != nil {
		return nil, err
	}
	events, err := prov.VerifyAndParseWebhook(ctx, gitprovider.WebhookRequest{
		Identity: identityRef(ident), Method: d.Method, Headers: d.Headers, Query: d.Query, Body: d.Body,
	})
	if err != nil {
		if gitprovider.IsCode(err, gitprovider.CodeInvalidSignature) {
			return nil, gitdom.ErrInvalidSignature
		}
		return nil, err
	}

	res := &WebhookResult{Events: len(events)}
	for i := range events {
		ev := &events[i]
		repos, err := s.store.FindLinkedRepositories(ctx, ep.ProviderID, ev.Repo.Host, ev.Repo.FullPath)
		if err != nil {
			return nil, err
		}
		if len(repos) == 0 {
			res.Unlinked++
			continue
		}
		payload, _ := json.Marshal(ev)
		for _, repo := range repos {
			ok, err := s.store.InsertInboxEvent(ctx, &gitdom.InboxEvent{
				EventID: ev.ID, RepositoryID: repo.ID, EndpointKey: endpointKey,
				Kind: string(ev.Kind), Action: ev.Action, Payload: payload, Status: gitdom.InboxReceived,
			})
			if err != nil {
				return nil, err
			}
			if !ok {
				res.Replayed++
				continue
			}
			res.Stored++
			if err := s.trackChangeRequest(ctx, repo, ev); err != nil {
				// A redelivery is dropped as a replay, so a row whose cache update
				// failed must not stay behind: remove it and let the platform retry.
				_ = s.store.DeleteInboxEvent(ctx, ev.ID, repo.ID)
				return nil, err
			}
		}
	}
	return res, nil
}

var taskRefRE = regexp.MustCompile(`(?i)\b([a-z][a-z0-9]{0,9})-(\d{1,7})\b`)

// taskRef is a task key (project prefix and number) found in free text.
type taskRef struct {
	key    string
	number int
}

// findTaskRefs extracts PREFIX-N task references from text. A match directly
// preceded by "-" is dropped when the segment before that dash is an
// all-uppercase token (TRIM-UI-200: UI-200 is the tail of a longer key, not a
// task). Lowercase or mixed segments keep the match, so fix-PACA-12-login and
// feature-PACA-12 still link. Trade-off: an uppercase prefix followed by "-"
// and another key (e.g. HOTFIX-PACA-12) is always ignored. RE2 has no
// lookbehind, so the preceding text is checked here.
func findTaskRefs(text string) []taskRef {
	var refs []taskRef
	for _, m := range taskRefRE.FindAllStringSubmatchIndex(text, -1) {
		if m[0] > 0 && text[m[0]-1] == '-' && upperSegmentBefore(text, m[0]-1) {
			continue
		}
		n, _ := strconv.Atoi(text[m[4]:m[5]])
		refs = append(refs, taskRef{key: text[m[2]:m[3]], number: n})
	}
	return refs
}

// upperSegmentBefore reports whether the alphanumeric run ending just before
// index end matches [A-Z][A-Z0-9]*.
func upperSegmentBefore(text string, end int) bool {
	start := end
	for start > 0 && isASCIIAlnum(text[start-1]) {
		start--
	}
	if start == end || text[start] < 'A' || text[start] > 'Z' {
		return false
	}
	for i := start; i < end; i++ {
		if c := text[i]; (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func isASCIIAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// trackChangeRequest keeps the change-request cache current from webhook events.
func (s *Service) trackChangeRequest(ctx context.Context, repo *gitdom.Repository, ev *gitprovider.Event) error {
	if ev.Kind != gitprovider.EventChangeRequest && ev.Kind != gitprovider.EventReview {
		return nil
	}
	sub := ev.Subject
	if sub == nil || sub.Kind != gitprovider.SubjectChangeRequest {
		return nil
	}
	cr := &gitdom.ChangeRequest{RepositoryID: repo.ID, Number: sub.Number, Draft: sub.Draft, Author: sub.Author, BaseRef: sub.Base, State: crState(ev), URL: sub.URL}
	if sub.Head != nil {
		cr.HeadRef, cr.HeadSHA = sub.Head.Ref, sub.Head.SHA
	}
	switch ev.Kind { //nolint:exhaustive // the early return above leaves only these two kinds
	case gitprovider.EventChangeRequest:
		cr.Title = ev.Text
		if ev.Action == "merged" {
			now := s.now().UTC()
			cr.MergedAt = &now
		} else if cr.State == "" {
			// An edit, a push or a ready-for-review does not change the
			// lifecycle: keep the cached state (a request never seen is open).
			cr.State = "open"
			cur, err := s.store.FindChangeRequest(ctx, repo.ID, sub.Number)
			switch {
			case err == nil:
				cr.State, cr.MergedAt = cur.State, cur.MergedAt
			case !errors.Is(err, gitdom.ErrNotFound):
				return err
			}
		}
	case gitprovider.EventReview:
		// A review says nothing about the request's lifecycle or draft state:
		// keep what the cache has rather than reset a merged request to open.
		cur, err := s.store.FindChangeRequest(ctx, repo.ID, sub.Number)
		switch {
		case err == nil:
			cr.State, cr.Draft, cr.MergedAt = cur.State, cur.Draft, cur.MergedAt
		case !errors.Is(err, gitdom.ErrNotFound):
			return err
		}
	}
	_, err := s.saveChangeRequest(ctx, repo, cr)
	return err
}

// saveChangeRequest caches a change request and links it to the task its
// branch or title names ("feat/PACA-12-x").
func (s *Service) saveChangeRequest(ctx context.Context, repo *gitdom.Repository, cr *gitdom.ChangeRequest) (*gitdom.ChangeRequest, error) {
	saved, err := s.store.UpsertChangeRequest(ctx, cr)
	if err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range findTaskRefs(cr.HeadRef + " " + cr.Title) {
		task, found, err := s.store.FindTaskByRef(ctx, repo.ProjectID, ref.key, ref.number)
		if err != nil {
			return nil, err
		}
		if !found || seen[task] {
			continue
		}
		seen[task] = true
		if err := s.store.LinkTaskToChangeRequest(ctx, task, saved.ID); err != nil {
			return nil, err
		}
		if cr.HeadRef != "" {
			if err := s.store.LinkTaskBranch(ctx, task, repo.ID, cr.HeadRef); err != nil {
				return nil, err
			}
		}
	}
	return saved, nil
}

// crState is the lifecycle state an event sets, or "" when the event leaves
// it unchanged (edited, synchronized, ready, ...).
func crState(ev *gitprovider.Event) string {
	switch ev.Action {
	case "opened", "created", "reopened":
		return "open"
	case "closed":
		return "closed"
	case "merged":
		return "merged"
	}
	return ""
}

// MintInput asks for a credential to use on a linked repository.
type MintInput struct {
	RepositoryID uuid.UUID
	Access       gitprovider.Access
	AgentID      *uuid.UUID // the acting agent: its own identity wins over the shared one
	ProjectID    *uuid.UUID // when set, the repository must belong to this project
}

// MintCredential returns a credential for the repository. The identity is the
// agent's own when it has one, else the connection the repository was linked through. Credentials are cached in memory for at most thirty seconds.
func (s *Service) MintCredential(ctx context.Context, in MintInput) (gitprovider.Credential, error) {
	repo, err := s.store.FindRepository(ctx, in.RepositoryID)
	if err != nil {
		return gitprovider.Credential{}, err
	}
	if in.ProjectID != nil && repo.ProjectID != *in.ProjectID {
		return gitprovider.Credential{}, gitdom.ErrNotFound // do not reveal other projects' repositories
	}
	ident, err := s.pickIdentity(ctx, repo, in.AgentID)
	if err != nil {
		return gitprovider.Credential{}, err
	}
	key := credKey{identity: ident.ID, repo: repo.ID, access: in.Access}
	if c, ok := s.cached(key); ok {
		s.audit(ctx, repo, in.AgentID, "credential.mint", string(in.Access)+" (cached)", nil)
		return c, nil
	}
	prov, err := s.providers.Provider(ctx, repo.ProviderID)
	if err != nil {
		return gitprovider.Credential{}, err
	}
	cred, err := prov.MintCredential(ctx, gitprovider.MintRequest{
		Target: gitprovider.Target{Identity: identityRef(ident), Repo: repoRef(repo)}, Access: in.Access,
	})
	s.audit(ctx, repo, in.AgentID, "credential.mint", string(in.Access), err)
	if err != nil {
		return gitprovider.Credential{}, err
	}
	s.remember(key, cred)
	return cred, nil
}

func (s *Service) pickIdentity(ctx context.Context, repo *gitdom.Repository, agent *uuid.UUID) (*gitdom.Identity, error) {
	if agent != nil {
		id, err := s.store.FindAgentIdentity(ctx, *agent, repo.ProviderID)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, gitdom.ErrNotFound) {
			return nil, err
		}
	}
	if repo.IdentityID != nil {
		id, err := s.store.FindIdentity(ctx, *repo.IdentityID)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, gitdom.ErrNotFound) {
			return nil, err
		}
	}
	// The connection the repository was linked through is gone (deleted): there
	// is no other one to fall back on, since credentials are per connection.
	return nil, gitdom.ErrNoIdentity
}

func (s *Service) cached(k credKey) (gitprovider.Credential, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cache[k]
	if !ok || !s.now().Before(c.expires) {
		delete(s.cache, k)
		return gitprovider.Credential{}, false
	}
	return c.cred, true
}

func (s *Service) remember(k credKey, c gitprovider.Credential) {
	exp := s.now().Add(credentialCacheTTL)
	// Never outlive the credential itself, leaving a margin for in-flight git use.
	if !c.ExpiresAt.IsZero() && c.ExpiresAt.Add(-credentialCacheTTL).Before(exp) {
		exp = c.ExpiresAt.Add(-credentialCacheTTL)
	}
	if !exp.After(s.now()) {
		return
	}
	s.mu.Lock()
	s.cache[k] = cachedCred{cred: c, expires: exp}
	s.mu.Unlock()
}

// audit records an action. A failure to log must not fail the action.
func (s *Service) audit(ctx context.Context, repo *gitdom.Repository, agent *uuid.UUID, tool, detail string, err error) {
	e := &gitdom.ActionLogEntry{ProjectID: &repo.ProjectID, RepositoryID: &repo.ID, ActorAccountID: agent,
		Tool: tool, Subject: repo.FullPath, OK: err == nil, Detail: detail}
	if err != nil {
		var pe *gitprovider.Error
		if errors.As(err, &pe) {
			e.Detail = detail + ": " + string(pe.Code)
		}
	}
	_ = s.store.AppendActionLog(ctx, e)
}

// ListRepositories returns the repositories a project has linked. A repository
// that needs a webhook nobody registered carries the URL to register.
func (s *Service) ListRepositories(ctx context.Context, projectID uuid.UUID) ([]*gitdom.Repository, error) {
	repos, err := s.store.ListProjectRepositories(ctx, projectID)
	if err != nil {
		return nil, err
	}
	attempts, err := s.store.WebhookAttempts(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// Asked once per connection: finding out costs a call to the provider.
	needs := map[uuid.UUID]webhookNeed{}
	for _, r := range repos {
		if hasWebhook(r) || r.IdentityID == nil {
			continue
		}
		n, ok := needs[*r.IdentityID]
		if !ok {
			if ident, err := s.store.FindIdentity(ctx, *r.IdentityID); err == nil {
				if n.need, _ = s.needsRepoWebhook(ctx, ident); n.need {
					n.url = s.webhookURL(ctx, ident)
				}
			}
			needs[*r.IdentityID] = n
		}
		r.WebhookURL = n.url
		r.WebhookAttempted = attempts[r.ID]
	}
	return repos, nil
}

// UnlinkRepository removes a project's link to a repository. The repository
// and the connection stay on the platform; a webhook Paca registered for the
// repository is removed unless another project still links it through the same
// connection.
func (s *Service) UnlinkRepository(ctx context.Context, projectID, repositoryID uuid.UUID) error {
	repo, err := s.store.FindRepository(ctx, repositoryID)
	if err != nil {
		return err
	}
	if repo.ProjectID != projectID {
		return gitdom.ErrNotFound
	}
	if err := s.store.DeleteRepository(ctx, projectID, repositoryID); err != nil {
		return err
	}
	s.unregisterWebhook(ctx, repo)
	return nil
}

// WithPublicURL sets the instance's public base URL, which webhook endpoints
// and setup redirects are built from.
func (s *Service) WithPublicURL(u string) *Service {
	s.publicURL = strings.TrimRight(strings.TrimSpace(u), "/")
	return s
}
