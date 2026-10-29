package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/platform/gitprovider"
	gitsvc "github.com/Paca-AI/api/internal/service/git"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// maxGitWebhookBody caps a webhook delivery. Platforms send at most a few
// megabytes (GitHub: 25 MB for the largest events); larger bodies are refused
// before being read into memory.
const maxGitWebhookBody = 8 << 20

// GitService is what the Git handler needs from the Git service.
type GitService interface {
	LinkRepository(ctx context.Context, in gitsvc.LinkInput) (*gitdom.Repository, error)
	ListRepositories(ctx context.Context, projectID uuid.UUID) ([]*gitdom.Repository, error)
	UnlinkRepository(ctx context.Context, projectID, repositoryID uuid.UUID) error
	UpdateRepository(ctx context.Context, projectID, repositoryID uuid.UUID, description string) (*gitdom.Repository, error)
	MintCredential(ctx context.Context, in gitsvc.MintInput) (gitprovider.Credential, error)
	CheckWebhookEndpoint(ctx context.Context, key string) error
	HandleWebhook(ctx context.Context, endpointKey string, d gitsvc.Delivery) (*gitsvc.WebhookResult, error)

	ListProviders(ctx context.Context) ([]gitprovider.Info, error)
	ListIdentities(ctx context.Context) ([]*gitdom.Identity, error)
	ListAgentIdentities(ctx context.Context, agentID uuid.UUID) ([]*gitdom.Identity, error)
	BeginIdentitySetup(ctx context.Context, in gitsvc.SetupInput) (*gitprovider.SetupStep, error)
	CompleteIdentitySetup(ctx context.Context, actorID uuid.UUID, projectID *uuid.UUID, state string, params map[string]string) (*gitsvc.SetupCompletion, error)
	DeleteIdentity(ctx context.Context, id uuid.UUID) error
	ReconnectRepository(ctx context.Context, in gitsvc.ReconnectInput) (*gitdom.Repository, error)
	RegisterWebhook(ctx context.Context, projectID, repositoryID uuid.UUID) (*gitdom.Repository, error)
	ListConnections(ctx context.Context, projectID uuid.UUID) ([]*gitdom.Identity, error)
	DeleteProjectConnection(ctx context.Context, projectID, id uuid.UUID) error
	RemoveAgentIdentity(ctx context.Context, agentID uuid.UUID, providerID string) error

	BeginAccessGrant(ctx context.Context, in gitsvc.AccessInput) (*gitprovider.SetupStep, error)
	CompleteAccessGrant(ctx context.Context, actorID, projectID uuid.UUID, state string, params map[string]string) (*gitsvc.AccessCompletion, error)
}

// GitAgentLookup confirms an agent belongs to a project and names it. The
// router's gate authorizes the agent id in the URL; this makes sure the id is
// really one of the URL's project's agents.
type GitAgentLookup interface {
	AgentInProject(ctx context.Context, projectID, agentID uuid.UUID) (name string, found bool, err error)
}

// GitHandler serves the Git integration endpoints. Authorization is declared on
// the routes (router.go); the handler only does the work.
type GitHandler struct {
	svc    GitService
	agents GitAgentLookup
	ops    GitOps
}

// NewGitHandler returns a GitHandler.
func NewGitHandler(svc GitService) *GitHandler { return &GitHandler{svc: svc} }

// WithAgentLookup enables the per-agent identity endpoints.
func (h *GitHandler) WithAgentLookup(a GitAgentLookup) *GitHandler {
	h.agents = a
	return h
}

// providerError turns a provider failure into an API error with a stable code.
// Only a provider's validation message is forwarded, capped (see providerDetail); everything else is a fixed text.
func providerError(err error) error {
	var pe *gitprovider.Error
	if !errors.As(err, &pe) {
		return err
	}
	switch pe.Code {
	case gitprovider.CodeNotFound:
		return apierr.New(apierr.CodeGitNotFound, "repository or identity was not found on the platform")
	case gitprovider.CodeForbidden, gitprovider.CodeConflict:
		return apierr.New(apierr.CodeGitProviderRejected, "the platform refused the request for this identity")
	case gitprovider.CodeValidation, gitprovider.CodeUnsupported:
		// Our provider's own message says what to fix ("head branch not found"),
		// which an agent needs; it is capped so it cannot flood a transcript.
		return apierr.New(apierr.CodeBadRequest, "the platform rejected the request"+providerDetail(pe))
	case gitprovider.CodeRateLimited:
		return apierr.New(apierr.CodeTooManyRequests, "the platform is rate limiting this identity")
	case gitprovider.CodeTransient:
		return apierr.New(apierr.CodeGitProviderUnavailable, "the Git provider is temporarily unavailable")
	case gitprovider.CodeInvalidSignature, gitprovider.CodeInternal:
		// Not something the caller can act on.
	}
	return apierr.New(apierr.CodeInternalError, "git provider error")
}

// withProviderMessage attaches the provider's message (redacted, capped) as
// details.provider_message, for a client to show under the fixed text. The
// code and text are unchanged, and nothing is attached when it said nothing.
func withProviderMessage(e *apierr.Error, err error) *apierr.Error {
	if msg := gitsvc.ProviderMessage(err); msg != "" {
		e.Details = map[string]string{"provider_message": msg}
	}
	return e
}

// providerDetail is a provider's explanation, capped, for the cases where the
// caller can act on it.
func providerDetail(pe *gitprovider.Error) string {
	msg := strings.TrimSpace(gitsvc.RedactSecrets(pe.Message))
	if msg == "" {
		return ""
	}
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return ": " + msg
}

func (h *GitHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	presenter.Error(w, r, providerError(err))
}

// failWithProviderMessage is fail for the settings page. Unlike the agent-facing
// operations (which keep provider text out of transcripts), a person
// configuring a connection needs to know why the platform refused, so its
// message rides along as details.provider_message.
func (h *GitHandler) failWithProviderMessage(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := providerError(err)
	var pe *gitprovider.Error
	// Every provider code carries its message (a validation or unsupported
	// answer is exactly where the fixed text is wrong), except internal ones,
	// which stay opaque.
	if !errors.As(err, &pe) || pe.Code != gitprovider.CodeInternal {
		var ae *apierr.Error
		if errors.As(apiErr, &ae) {
			apiErr = withProviderMessage(ae, err)
		}
	}
	presenter.Error(w, r, apiErr)
}

// ListRepositories handles GET /projects/:projectId/git/repositories.
func (h *GitHandler) ListRepositories(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	repos, err := h.svc.ListRepositories(r.Context(), projectID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]dto.GitRepositoryResponse, 0, len(repos))
	for _, repo := range repos {
		out = append(out, dto.GitRepositoryFromEntity(repo))
	}
	presenter.OK(w, r, out)
}

// LinkRepository handles POST /projects/:projectId/git/repositories.
func (h *GitHandler) LinkRepository(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	actor, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.LinkGitRepositoryRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ProviderID) == "" || strings.TrimSpace(req.PathOrURL) == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "provider_id and path_or_url are required"))
		return
	}
	repo, err := h.svc.LinkRepository(r.Context(), gitsvc.LinkInput{
		ProjectID: projectID, ProviderID: strings.TrimSpace(req.ProviderID), Host: strings.TrimSpace(req.Host),
		PathOrURL: strings.TrimSpace(req.PathOrURL), IdentityID: req.IdentityID, ActorID: &actor,
		Description: req.Description,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.Created(w, r, dto.GitRepositoryFromEntity(repo))
}

// UpdateRepository handles PATCH /projects/:projectId/git/repositories/:repositoryId.
func (h *GitHandler) UpdateRepository(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	repoID, ok := gitRepositoryID(w, r)
	if !ok {
		return
	}
	var req dto.UpdateGitRepositoryRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	repo, err := h.svc.UpdateRepository(r.Context(), projectID, repoID, req.Description)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.OK(w, r, dto.GitRepositoryFromEntity(repo))
}

// UnlinkRepository handles DELETE /projects/:projectId/git/repositories/:repositoryId.
func (h *GitHandler) UnlinkRepository(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	repoID, ok := gitRepositoryID(w, r)
	if !ok {
		return
	}
	if err := h.svc.UnlinkRepository(r.Context(), projectID, repoID); err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// ReconnectRepository handles POST /projects/:projectId/git/repositories/:repositoryId/reconnect:
// point a repository whose connection was deleted at another connection.
func (h *GitHandler) ReconnectRepository(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	repoID, ok := gitRepositoryID(w, r)
	if !ok {
		return
	}
	var req dto.ReconnectGitRepositoryRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	repo, err := h.svc.ReconnectRepository(r.Context(), gitsvc.ReconnectInput{ProjectID: projectID, RepositoryID: repoID, IdentityID: req.IdentityID})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.OK(w, r, dto.GitRepositoryFromEntity(repo))
}

// RegisterWebhook handles POST /projects/:projectId/git/repositories/:repositoryId/webhook:
// try again to register the repository's webhook on the platform.
func (h *GitHandler) RegisterWebhook(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	repoID, ok := gitRepositoryID(w, r)
	if !ok {
		return
	}
	repo, err := h.svc.RegisterWebhook(r.Context(), projectID, repoID)
	if err != nil {
		h.failWithProviderMessage(w, r, err)
		return
	}
	presenter.OK(w, r, dto.GitRepositoryFromEntity(repo))
}

// ReadCredential handles POST .../repositories/:repositoryId/credential/read.
func (h *GitHandler) ReadCredential(w http.ResponseWriter, r *http.Request) {
	h.credential(w, r, gitprovider.AccessRead)
}

// WriteCredential handles POST .../repositories/:repositoryId/credential/write.
// It is a separate route so the router can require git:write for it.
func (h *GitHandler) WriteCredential(w http.ResponseWriter, r *http.Request) {
	h.credential(w, r, gitprovider.AccessWrite)
}

func (h *GitHandler) credential(w http.ResponseWriter, r *http.Request, access gitprovider.Access) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	repoID, ok := gitRepositoryID(w, r)
	if !ok {
		return
	}
	actor, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	cred, err := h.svc.MintCredential(r.Context(), gitsvc.MintInput{
		RepositoryID: repoID, Access: access, AgentID: &actor, ProjectID: &projectID,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	resp := dto.GitCredentialResponse{Username: cred.Username, Secret: cred.Secret}
	if !cred.ExpiresAt.IsZero() {
		t := cred.ExpiresAt
		resp.ExpiresAt = &t
	}
	w.Header().Set("Cache-Control", "no-store")
	presenter.OK(w, r, resp)
}

// ReceiveWebhook handles POST /git/webhooks/:endpointKey. It is public: the
// provider plugin authenticates the delivery (signature) and the endpoint key
// only says which identity's secret to verify against.
func (h *GitHandler) ReceiveWebhook(w http.ResponseWriter, r *http.Request) {
	// Refuse unknown endpoints before buffering up to maxGitWebhookBody.
	if err := h.svc.CheckWebhookEndpoint(r.Context(), chi.URLParam(r, "endpointKey")); err != nil {
		h.fail(w, r, err)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxGitWebhookBody))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "webhook body is unreadable or too large"))
		return
	}
	headers := make(map[string]string, len(r.Header))
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[strings.ToLower(k)] = v[0]
		}
	}
	query := map[string]string{}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			query[k] = v[0]
		}
	}
	res, err := h.svc.HandleWebhook(r.Context(), chi.URLParam(r, "endpointKey"), gitsvc.Delivery{
		Method: r.Method, Headers: headers, Query: query, Body: body,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.OK(w, r, dto.GitWebhookResponse{Events: res.Events, Stored: res.Stored, Replayed: res.Replayed})
}

func gitProjectID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid project ID"))
		return uuid.Nil, false
	}
	return id, true
}

func gitRepositoryID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "repositoryId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid repository ID"))
		return uuid.Nil, false
	}
	return id, true
}

// ListProviders handles GET /git/providers and GET /projects/:projectId/git/providers.
func (h *GitHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	infos, err := h.svc.ListProviders(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if infos == nil {
		infos = []gitprovider.Info{}
	}
	presenter.OK(w, r, infos)
}

// ListIdentities handles GET /git/identities.
func (h *GitHandler) ListIdentities(w http.ResponseWriter, r *http.Request) {
	ids, err := h.svc.ListIdentities(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.OK(w, r, identitiesResponse(ids))
}

func identitiesResponse(ids []*gitdom.Identity) []dto.GitIdentityResponse {
	out := make([]dto.GitIdentityResponse, 0, len(ids))
	for _, i := range ids {
		out = append(out, dto.GitIdentityFromEntity(i))
	}
	return out
}

// DeleteIdentity handles DELETE /git/identities/:identityId.
func (h *GitHandler) DeleteIdentity(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "identityId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid identity ID"))
		return
	}
	if err := h.svc.DeleteIdentity(r.Context(), id); err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// BeginSetup handles POST /git/identities/setup: a connection every project can use.
func (h *GitHandler) BeginSetup(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.BeginGitSetupRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	h.begin(w, r, gitsvc.SetupInput{ProviderID: strings.TrimSpace(req.ProviderID), Host: strings.TrimSpace(req.Host), Scope: gitdom.ScopeShared,
		ActorID: actor, ReturnURL: strings.TrimSpace(req.ReturnURL), Params: req.Params})
}

// BeginAgentSetup handles POST /projects/:projectId/agents/:agentId/git/identity/setup.
func (h *GitHandler) BeginAgentSetup(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	agentID, name, ok := h.agentOf(w, r)
	if !ok {
		return
	}
	var req dto.BeginGitSetupRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	h.begin(w, r, gitsvc.SetupInput{ProviderID: strings.TrimSpace(req.ProviderID), Host: strings.TrimSpace(req.Host), Scope: gitdom.ScopeAgent,
		AgentID: &agentID, AgentName: name, ActorID: actor, ReturnURL: strings.TrimSpace(req.ReturnURL), Params: req.Params})
}

func (h *GitHandler) begin(w http.ResponseWriter, r *http.Request, in gitsvc.SetupInput) {
	if in.ProviderID == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "provider_id is required"))
		return
	}
	step, err := h.svc.BeginIdentitySetup(r.Context(), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.Created(w, r, dto.GitSetupStepFromProvider(step))
}

// ListConnections handles GET /projects/:projectId/git/connections: the
// workspace's connections and the project's own.
func (h *GitHandler) ListConnections(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	ids, err := h.svc.ListConnections(r.Context(), projectID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.OK(w, r, identitiesResponse(ids))
}

// BeginProjectSetup handles POST /projects/:projectId/git/connections/setup: a
// connection only this project can use.
func (h *GitHandler) BeginProjectSetup(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	actor, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.BeginGitSetupRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	h.begin(w, r, gitsvc.SetupInput{ProviderID: strings.TrimSpace(req.ProviderID), Host: strings.TrimSpace(req.Host), Scope: gitdom.ScopeProject,
		ProjectID: &projectID, ActorID: actor, ReturnURL: strings.TrimSpace(req.ReturnURL), Params: req.Params})
}

// CompleteProjectSetup handles POST /projects/:projectId/git/connections/setup/complete.
func (h *GitHandler) CompleteProjectSetup(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	h.completeSetup(w, r, &projectID)
}

// DeleteProjectConnection handles DELETE /projects/:projectId/git/connections/:identityId.
func (h *GitHandler) DeleteProjectConnection(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "identityId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid connection ID"))
		return
	}
	if err := h.svc.DeleteProjectConnection(r.Context(), projectID, id); err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// CompleteSetup handles POST /git/identities/setup/complete.
func (h *GitHandler) CompleteSetup(w http.ResponseWriter, r *http.Request) {
	h.completeSetup(w, r, nil)
}

func (h *GitHandler) completeSetup(w http.ResponseWriter, r *http.Request, projectID *uuid.UUID) {
	actor, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.CompleteGitSetupRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.State) == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "state is required"))
		return
	}
	res, err := h.svc.CompleteIdentitySetup(r.Context(), actor, projectID, strings.TrimSpace(req.State), req.Params)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.OK(w, r, dto.GitSetupCompletionResponse{
		Identity: dto.GitIdentityFromEntity(res.Identity), Next: dto.GitSetupStepFromProvider(res.Next),
		ReturnURL: res.ReturnURL, Done: res.Next == nil,
	})
}

// agentOf parses :agentId and confirms the agent belongs to :projectId.
func (h *GitHandler) agentOf(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, bool) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return uuid.Nil, "", false
	}
	agentID, err := uuid.Parse(chi.URLParam(r, "agentId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid agent ID"))
		return uuid.Nil, "", false
	}
	if h.agents == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "agent lookup not configured"))
		return uuid.Nil, "", false
	}
	name, found, err := h.agents.AgentInProject(r.Context(), projectID, agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return uuid.Nil, "", false
	}
	if !found {
		presenter.Error(w, r, gitdom.ErrNotFound)
		return uuid.Nil, "", false
	}
	return agentID, name, true
}

// ListAgentIdentities handles GET /projects/:projectId/agents/:agentId/git/identity.
func (h *GitHandler) ListAgentIdentities(w http.ResponseWriter, r *http.Request) {
	agentID, _, ok := h.agentOf(w, r)
	if !ok {
		return
	}
	ids, err := h.svc.ListAgentIdentities(r.Context(), agentID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.OK(w, r, identitiesResponse(ids))
}

// DeleteAgentIdentity handles DELETE /projects/:projectId/agents/:agentId/git/identity?provider_id=.
func (h *GitHandler) DeleteAgentIdentity(w http.ResponseWriter, r *http.Request) {
	agentID, _, ok := h.agentOf(w, r)
	if !ok {
		return
	}
	provider := strings.TrimSpace(r.URL.Query().Get("provider_id"))
	if provider == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "provider_id is required"))
		return
	}
	if err := h.svc.RemoveAgentIdentity(r.Context(), agentID, provider); err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// BeginAccess handles POST /projects/:projectId/git/access/begin: the first leg
// of letting the shared identity see another owner's repositories.
func (h *GitHandler) BeginAccess(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	actor, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.BeginGitAccessRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ProviderID) == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "provider_id is required"))
		return
	}
	step, err := h.svc.BeginAccessGrant(r.Context(), gitsvc.AccessInput{
		ProjectID: projectID, ProviderID: strings.TrimSpace(req.ProviderID), Host: strings.TrimSpace(req.Host),
		Owner: strings.TrimSpace(req.Owner), IdentityID: req.IdentityID, ActorID: actor, ReturnURL: strings.TrimSpace(req.ReturnURL),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.Created(w, r, dto.GitSetupStepFromProvider(step))
}

// CompleteAccess handles POST /projects/:projectId/git/access/complete.
func (h *GitHandler) CompleteAccess(w http.ResponseWriter, r *http.Request) {
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	actor, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.CompleteGitAccessRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.State) == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "state is required"))
		return
	}
	res, err := h.svc.CompleteAccessGrant(r.Context(), actor, projectID, strings.TrimSpace(req.State), req.Params)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.OK(w, r, dto.GitAccessCompletionResponse{Owner: res.Owner, ReturnURL: res.ReturnURL})
}

// BeginAgentAccess handles POST /projects/:projectId/agents/:agentId/git/identity/access/begin.
// Finishing it uses the same /projects/:projectId/git/access/complete as a
// project's own grant: the stored state already names the identity.
func (h *GitHandler) BeginAgentAccess(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	projectID, ok := gitProjectID(w, r)
	if !ok {
		return
	}
	agentID, _, ok := h.agentOf(w, r)
	if !ok {
		return
	}
	var req dto.BeginAgentGitAccessRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ProviderID) == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "provider_id is required"))
		return
	}
	step, err := h.svc.BeginAccessGrant(r.Context(), gitsvc.AccessInput{
		ProjectID: projectID, ProviderID: strings.TrimSpace(req.ProviderID), Owner: strings.TrimSpace(req.Owner),
		AgentID: &agentID, ActorID: actor, ReturnURL: strings.TrimSpace(req.ReturnURL),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	presenter.Created(w, r, dto.GitSetupStepFromProvider(step))
}
