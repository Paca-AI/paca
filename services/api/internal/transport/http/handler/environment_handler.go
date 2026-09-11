package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// environmentTicketTTL bounds how long a minted environment ticket
// (terminal or stats) stays valid — short enough that a leaked ticket
// (e.g. captured in a proxy log) is useless within a minute of being
// issued, long enough that the browser has time to open the WebSocket
// right after requesting one.
const environmentTicketTTL = 60 * time.Second

// Ticket purposes — must match agent-runner's own
// ticketPurposeTerminal/ticketPurposeStats exactly
// (internal/acpbridge/ticket.go). A ticket is only ever valid for the one
// endpoint it was minted for: a terminal ticket (gated on
// environments.connect, since it grants a shell) must not also unlock the
// stats socket (gated on the weaker environments.read, since viewing usage
// numbers isn't an interactive session), and vice versa.
const (
	ticketPurposeTerminal = "terminal"
	ticketPurposeStats    = "stats"
)

// EnvironmentHandler handles static environment management endpoints (see
// docs/ai-agent/environment-management.md).
type EnvironmentHandler struct {
	svc environmentdom.Service
	// aiAgentInternalKey signs terminal tickets — the same shared
	// AI_AGENT_INTERNAL_KEY agent-runner verifies internal-only calls with
	// (see agent_handler.go's aiAgentInternalKey), reused here as an HMAC
	// key rather than a bearer token so agent-runner can verify a ticket
	// itself without a round-trip back to services/api.
	aiAgentInternalKey string
	// sshBastionHost/portForwardHost back GetConfig — see that method's own
	// doc comment. Set via WithDeploymentConfig; zero-valued (both "") is
	// safe and just means GetConfig reports the feature(s) as unconfigured.
	sshBastionHost  string
	portForwardHost string
	// memberRepo resolves the caller (human or agent) to their
	// project_members.id for AccessGranted response decoration — see
	// resolveActorMemberIDForDecoration. Optional: nil just means every
	// response's AccessGranted falls back to dto.EnvironmentFromEntity's
	// own caller-agnostic approximation.
	memberRepo projectdom.MemberRepository
}

// NewEnvironmentHandler returns an EnvironmentHandler wired to the
// environment service. aiAgentInternalKey must match agent-runner's own
// INTERNAL_API_KEY — see TerminalTicket's doc comment for how it's used.
func NewEnvironmentHandler(svc environmentdom.Service, aiAgentInternalKey string) *EnvironmentHandler {
	return &EnvironmentHandler{svc: svc, aiAgentInternalKey: aiAgentInternalKey}
}

// WithMemberRepo attaches the project member repository used to resolve the
// authenticated caller's project_members.id — mirrors AgentHandler's own
// WithMemberRepo.
func (h *EnvironmentHandler) WithMemberRepo(repo projectdom.MemberRepository) *EnvironmentHandler {
	h.memberRepo = repo
	return h
}

// WithDeploymentConfig sets the values GetConfig reports — config.Config's
// own SSHBastionHost/PortForwardHost, sourced from the exact same
// SSH_BASTION_HOST/PORT_FORWARD_HOST env vars already set on agent-runner
// for the same deployment. Mirrors this codebase's established
// With*-chained-onto-New* convention (see e.g.
// SettingsHandler.WithAvatarService) for an optional dependency the
// constructor itself shouldn't need to grow a parameter for.
func (h *EnvironmentHandler) WithDeploymentConfig(sshBastionHost, portForwardHost string) *EnvironmentHandler {
	h.sshBastionHost = sshBastionHost
	h.portForwardHost = portForwardHost
	return h
}

// GetConfig handles GET /environments/config — public, no auth required
// (read on every environment detail/connect page load, the same "public,
// deployment-wide, read pre-any-particular-resource" shape as GET
// /branding). Reports the two purely descriptive values needed to show a
// user a real `ssh` command and a real port-forward host instead of a
// placeholder: agent-runner is what actually owns routing either one (see
// docs/ai-agent/environment-management.md), this endpoint only echoes the
// same config values back so the frontend doesn't have to guess or
// hardcode them. Either field empty means that feature isn't configured on
// this deployment — the frontend treats that as "not available" rather
// than an error.
func (h *EnvironmentHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	presenter.OK(w, r, dto.EnvironmentDeploymentConfigResponse{
		SSHBastionHost:  h.sshBastionHost,
		PortForwardHost: h.portForwardHost,
	})
}

// --- Environments -------------------------------------------------------

// ListEnvironments handles GET /projects/:projectId/environments.
func (h *EnvironmentHandler) ListEnvironments(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	envs, err := h.svc.ListEnvironments(r.Context(), projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	grantedIDs := h.callerGrantedEnvironmentIDs(r, projectID)
	resp := make([]dto.EnvironmentResponse, 0, len(envs))
	for _, e := range envs {
		resp = append(resp, h.toEnvironmentResponseForCaller(e, grantedIDs))
	}
	presenter.OK(w, r, map[string]any{"environments": resp})
}

// toEnvironmentResponseForCaller is dto.EnvironmentFromEntity plus an
// accurate AccessGranted for one specific caller — see
// AgentHandler.toAgentResponseForCaller's identical reasoning.
func (h *EnvironmentHandler) toEnvironmentResponseForCaller(env *environmentdom.Environment, grantedIDs map[uuid.UUID]bool) dto.EnvironmentResponse {
	resp := dto.EnvironmentFromEntity(env)
	if resp.AccessMode == environmentdom.AccessModeRestricted {
		resp.AccessGranted = grantedIDs[env.ID]
		if !resp.AccessGranted {
			// SSHPort is per-instance connection metadata — combined with the
			// deployment-wide (non-secret) bastion host GetConfig reports,
			// it's enough to know where to attempt an SSH connection into
			// this specific container. A member the admin explicitly locked
			// out of a restricted environment shouldn't learn this just from
			// the environment still being visible in list/detail views —
			// same "alternate access path" rubric that already gates SSH
			// keys/terminal/port-forwards behind RequireEnvironmentAccess
			// rather than leaving them on environments.read alone.
			resp.SSHPort = nil
		}
	}
	return resp
}

// callerGrantedEnvironmentIDs mirrors AgentHandler.callerGrantedAgentIDs.
func (h *EnvironmentHandler) callerGrantedEnvironmentIDs(r *http.Request, projectID uuid.UUID) map[uuid.UUID]bool {
	memberID := h.resolveActorMemberIDForDecoration(r, projectID)
	if memberID == uuid.Nil {
		return map[uuid.UUID]bool{}
	}
	ids, err := h.svc.ListGrantedEnvironmentIDsForMember(r.Context(), memberID)
	if err != nil {
		return map[uuid.UUID]bool{}
	}
	set := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// resolveActorMemberIDForDecoration mirrors
// AgentHandler.resolveActorMemberIDForDecoration exactly — see its doc
// comment.
func (h *EnvironmentHandler) resolveActorMemberIDForDecoration(r *http.Request, projectID uuid.UUID) uuid.UUID {
	if h.memberRepo == nil {
		return uuid.Nil
	}
	if agentID, ok := middleware.AgentIDFromRequest(r); ok {
		m, err := h.memberRepo.FindMemberByActor(r.Context(), projectID, uuid.Nil, &agentID)
		if err != nil {
			return uuid.Nil
		}
		return m.ID
	}
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		return uuid.Nil
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil
	}
	m, err := h.memberRepo.FindMemberByActor(r.Context(), projectID, userID, nil)
	if err != nil {
		return uuid.Nil
	}
	return m.ID
}

// CreateEnvironment handles POST /projects/:projectId/environments.
func (h *EnvironmentHandler) CreateEnvironment(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.CreateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.Name == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "name is required"))
		return
	}
	claims := middleware.ClaimsFrom(r)
	callerID, _ := uuid.Parse(claims.Subject)

	env, err := h.svc.CreateEnvironment(r.Context(), projectID, environmentdom.CreateEnvironmentInput{
		Name:          req.Name,
		Image:         req.Image,
		CPULimit:      req.CPULimit,
		MemoryLimit:   req.MemoryLimit,
		DiskLimitGB:   req.DiskLimitGB,
		DockerEnabled: req.DockerEnabled,
		CreatedBy:     &callerID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.EnvironmentFromEntity(env))
}

// GetEnvironment handles GET /projects/:projectId/environments/:environmentId.
func (h *EnvironmentHandler) GetEnvironment(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	env, err := h.svc.GetEnvironment(r.Context(), projectID, environmentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toEnvironmentResponseForCaller(env, h.callerGrantedEnvironmentIDs(r, projectID)))
}

// UpdateEnvironment handles PATCH /projects/:projectId/environments/:environmentId.
func (h *EnvironmentHandler) UpdateEnvironment(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.UpdateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	env, err := h.svc.UpdateEnvironment(r.Context(), projectID, environmentID, environmentdom.UpdateEnvironmentInput{
		Name:               req.Name,
		IdleTimeoutMinutes: req.IdleTimeoutMinutes,
		AccessMode:         req.AccessMode,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.EnvironmentFromEntity(env))
}

// DeleteEnvironment handles DELETE /projects/:projectId/environments/:environmentId.
func (h *EnvironmentHandler) DeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteEnvironment(r.Context(), projectID, environmentID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// StartEnvironment handles POST /projects/:projectId/environments/:environmentId/start.
func (h *EnvironmentHandler) StartEnvironment(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	env, err := h.svc.StartEnvironment(r.Context(), projectID, environmentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.EnvironmentFromEntity(env))
}

// StopEnvironment handles POST /projects/:projectId/environments/:environmentId/stop.
func (h *EnvironmentHandler) StopEnvironment(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	env, err := h.svc.StopEnvironment(r.Context(), projectID, environmentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.EnvironmentFromEntity(env))
}

// RestartEnvironment handles POST
// /projects/:projectId/environments/:environmentId/restart — applies any
// pending port-forward changes to a currently-running environment (see
// environmentdom.EnvironmentService.RestartEnvironment's own doc comment).
func (h *EnvironmentHandler) RestartEnvironment(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	env, err := h.svc.RestartEnvironment(r.Context(), projectID, environmentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.EnvironmentFromEntity(env))
}

// Heartbeat handles POST /projects/:projectId/environments/:environmentId/heartbeat.
func (h *EnvironmentHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.Heartbeat(r.Context(), projectID, environmentID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// VerifyCLILogin handles POST
// /projects/:projectId/environments/:environmentId/verify-cli-login?cli_provider=<provider>.
// The environment-scoped sibling of AgentHandler.VerifyCLILogin — that one
// resolves cli_provider from an already-created provider_cli agent's own
// DefaultEnvironmentID/CLIProvider fields, which doesn't exist yet for the
// create-agent dialog's own "Verify login" button (the agent it's
// configuring hasn't been created). Takes both directly as parameters
// instead, so login can be verified against the environment/provider
// combination the user has picked before submitting the form. Does not
// persist a verification timestamp anywhere (there is no agent row yet to
// persist it against) — purely a live probe.
func (h *EnvironmentHandler) VerifyCLILogin(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	cliProvider := r.URL.Query().Get("cli_provider")
	if cliProvider == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "cli_provider is required"))
		return
	}
	authenticated, err := h.svc.VerifyCLIAuth(r.Context(), projectID, environmentID, cliProvider)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.VerifyCLILoginResponse{Authenticated: authenticated})
}

// --- Folders --------------------------------------------------------------

// ListFolders handles GET /projects/:projectId/environments/:environmentId/folders.
func (h *EnvironmentHandler) ListFolders(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	folders, err := h.svc.ListFolders(r.Context(), projectID, environmentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.EnvironmentFolderResponse, 0, len(folders))
	for _, f := range folders {
		resp = append(resp, dto.EnvironmentFolderFromEntity(f))
	}
	presenter.OK(w, r, map[string]any{"folders": resp})
}

// AddFolder handles POST /projects/:projectId/environments/:environmentId/folders.
func (h *EnvironmentHandler) AddFolder(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AddFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.Path == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "path is required"))
		return
	}
	claims := middleware.ClaimsFrom(r)
	callerID, _ := uuid.Parse(claims.Subject)

	folder, err := h.svc.AddFolder(r.Context(), projectID, environmentID, environmentdom.AddFolderInput{
		Path:      req.Path,
		CreatedBy: &callerID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.EnvironmentFolderFromEntity(folder))
}

// DeleteFolder handles DELETE
// /projects/:projectId/environments/:environmentId/folders/:folderId.
func (h *EnvironmentHandler) DeleteFolder(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	folderID, err := parseParamUUID(r, "folderId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteFolder(r.Context(), projectID, environmentID, folderID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// BrowseFolder handles GET
// /projects/:projectId/environments/:environmentId/browse?path=... — lists
// the immediate children of path inside the environment's own running
// container/Pod, for the folder-creation UI's "browse instead of typing
// blind" affordance. Mirrors TerminalTicket's own "fetch env, require
// StatusRunning" shape, since there's no live filesystem to read from a
// stopped container/Pod.
func (h *EnvironmentHandler) BrowseFolder(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resolvedPath, entries, err := h.svc.Browse(r.Context(), projectID, environmentID, r.URL.Query().Get("path"))
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := dto.BrowseFolderResponse{
		Path:    resolvedPath,
		Entries: make([]dto.BrowseEntryResponse, 0, len(entries)),
	}
	for _, e := range entries {
		resp.Entries = append(resp.Entries, dto.BrowseEntryResponse{Name: e.Name, IsDir: e.IsDir})
	}
	presenter.OK(w, r, resp)
}

// --- Access grants ------------------------------------------------------

// ListEnvironmentAccessGrants handles GET
// /projects/:projectId/environments/:environmentId/access-grants.
func (h *EnvironmentHandler) ListEnvironmentAccessGrants(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	grants, err := h.svc.ListEnvironmentAccessGrants(r.Context(), projectID, environmentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.EnvironmentAccessGrantResponse, 0, len(grants))
	for _, g := range grants {
		resp = append(resp, dto.EnvironmentAccessGrantFromEntity(g))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// AddEnvironmentAccessGrant handles POST
// /projects/:projectId/environments/:environmentId/access-grants.
func (h *EnvironmentHandler) AddEnvironmentAccessGrant(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AddEnvironmentAccessGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.MemberID == uuid.Nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "member_id is required"))
		return
	}
	var grantedBy *uuid.UUID
	if claims := middleware.ClaimsFrom(r); claims != nil {
		if id, err := uuid.Parse(claims.Subject); err == nil {
			grantedBy = &id
		}
	}
	g, err := h.svc.AddEnvironmentAccessGrant(r.Context(), projectID, environmentID, req.MemberID, grantedBy)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.EnvironmentAccessGrantFromEntity(g))
}

// RemoveEnvironmentAccessGrant handles DELETE
// /projects/:projectId/environments/:environmentId/access-grants/:memberId.
func (h *EnvironmentHandler) RemoveEnvironmentAccessGrant(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	memberID, err := parseParamUUID(r, "memberId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.RemoveEnvironmentAccessGrant(r.Context(), projectID, environmentID, memberID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "access grant removed"})
}

// --- SSH keys ---------------------------------------------------------------

// ListSSHKeys handles GET /projects/:projectId/environments/:environmentId/ssh-keys.
func (h *EnvironmentHandler) ListSSHKeys(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	keys, err := h.svc.ListSSHKeys(r.Context(), projectID, environmentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.EnvironmentSSHKeyResponse, 0, len(keys))
	for _, k := range keys {
		resp = append(resp, dto.EnvironmentSSHKeyFromEntity(k))
	}
	presenter.OK(w, r, map[string]any{"ssh_keys": resp})
}

// AddSSHKey handles POST /projects/:projectId/environments/:environmentId/ssh-keys.
func (h *EnvironmentHandler) AddSSHKey(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AddSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.Label == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "label is required"))
		return
	}
	if req.PublicKey == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "public_key is required"))
		return
	}
	claims := middleware.ClaimsFrom(r)
	callerID, _ := uuid.Parse(claims.Subject)

	key, err := h.svc.AddSSHKey(r.Context(), projectID, environmentID, environmentdom.AddSSHKeyInput{
		Label:     req.Label,
		PublicKey: req.PublicKey,
		CreatedBy: &callerID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.EnvironmentSSHKeyFromEntity(key))
}

// DeleteSSHKey handles DELETE
// /projects/:projectId/environments/:environmentId/ssh-keys/:keyId.
func (h *EnvironmentHandler) DeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	keyID, err := parseParamUUID(r, "keyId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteSSHKey(r.Context(), projectID, environmentID, keyID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// --- Port forwards -----------------------------------------------------------

// ListPortForwards handles GET
// /projects/:projectId/environments/:environmentId/port-forwards.
func (h *EnvironmentHandler) ListPortForwards(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	forwards, err := h.svc.ListPortForwards(r.Context(), projectID, environmentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.EnvironmentPortForwardResponse, 0, len(forwards))
	for _, pf := range forwards {
		resp = append(resp, dto.EnvironmentPortForwardFromEntity(pf))
	}
	presenter.OK(w, r, map[string]any{"port_forwards": resp})
}

// GetPortForward handles GET
// /projects/:projectId/environments/:environmentId/port-forwards/:portForwardId
// — backs the web app's port-forward detail page.
func (h *EnvironmentHandler) GetPortForward(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	portForwardID, err := parseParamUUID(r, "portForwardId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	pf, err := h.svc.GetPortForward(r.Context(), projectID, environmentID, portForwardID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.EnvironmentPortForwardFromEntity(pf))
}

// AddPortForward handles POST
// /projects/:projectId/environments/:environmentId/port-forwards.
func (h *EnvironmentHandler) AddPortForward(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AddPortForwardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.ContainerPort == 0 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "container_port is required"))
		return
	}
	claims := middleware.ClaimsFrom(r)
	callerID, _ := uuid.Parse(claims.Subject)

	pf, err := h.svc.AddPortForward(r.Context(), projectID, environmentID, environmentdom.AddPortForwardInput{
		Label:         req.Label,
		ContainerPort: req.ContainerPort,
		CreatedBy:     &callerID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.EnvironmentPortForwardFromEntity(pf))
}

// DeletePortForward handles DELETE
// /projects/:projectId/environments/:environmentId/port-forwards/:portForwardId.
func (h *EnvironmentHandler) DeletePortForward(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	portForwardID, err := parseParamUUID(r, "portForwardId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeletePortForward(r.Context(), projectID, environmentID, portForwardID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// --- Environment tickets -----------------------------------------------------
//
// Both endpoints below mint a short-lived signed ticket one of
// agent-runner's browser-facing WebSocket endpoints verifies on its own
// (agent-runner has no user-session concept — see
// docs/ai-agent/environment-management.md's Terminal / SSH Access
// section). Same shape, different purpose and permission: TerminalTicket
// requires environments.connect (a shell is a live interactive session)
// and unlocks only the terminal; StatsTicket only requires
// environments.read (viewing usage numbers isn't) and unlocks only the
// live-stats stream — see mintEnvironmentTicket's own doc comment for why
// a ticket can't be used for the other endpoint.
//
// Ticket format (must match agent-runner's verifier exactly — confirmed
// byte-for-byte against agent-runner's own round-trip tests):
//
//	base64url_nopad( purpose + "|" + environment_id + "|" + expires_unix_ts + "|" +
//	  hex(hmac_sha256(AI_AGENT_INTERNAL_KEY, purpose + "|" + environment_id + "|" + expires_unix_ts)) )
//
// purpose is "terminal" or "stats"; environment_id is the canonical
// lowercase UUID string (uuid.UUID.String() is already lowercase);
// expires_unix_ts is decimal seconds-since-epoch; the HMAC payload is
// exactly "<purpose>|<environment_id>|<expires_unix_ts>" (nothing else);
// the hex digest is lowercase; the final encoding is unpadded base64url
// (base64.RawURLEncoding) — not standard padded base64, and not
// base64.URLEncoding (which pads with '=').

// TerminalTicket handles POST
// /projects/:projectId/environments/:environmentId/terminal-ticket. The
// router's permission middleware already establishes the caller has
// project-level environments:connect; GetEnvironment on top of that
// verifies this specific environment belongs to projectID (the
// resource-level check a coarse permission alone can't express), and only
// then is the environment's own Status checked — no point minting a
// ticket for a stopped environment, since there's nothing on the other
// end to connect to.
func (h *EnvironmentHandler) TerminalTicket(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	env, err := h.svc.GetEnvironment(r.Context(), projectID, environmentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if env.Status != environmentdom.StatusRunning {
		presenter.Error(w, r, environmentdom.ErrEnvironmentNotRunning)
		return
	}

	ticket := mintEnvironmentTicket(h.aiAgentInternalKey, ticketPurposeTerminal, env.ID, environmentTicketTTL)
	presenter.OK(w, r, dto.EnvironmentTicketResponse{
		Ticket: ticket,
		// Relative path — the frontend resolves it against its own origin,
		// same convention as /agent-bridge/ws (see deploy/caddy/Caddyfile's
		// /environments/*/terminal/ws handle_path block, added alongside
		// agent-runner's terminal WebSocket handler).
		WSURL: fmt.Sprintf("/environments/%s/terminal/ws?ticket=%s", env.ID, ticket),
	})
}

// StatsTicket handles POST
// /projects/:projectId/environments/:environmentId/stats-ticket — same
// shape as TerminalTicket above, gated on the weaker environments:read
// instead (see this section's own doc comment), for agent-runner's live
// CPU/memory/disk usage stream (internal/acpbridge/stats.go).
func (h *EnvironmentHandler) StatsTicket(w http.ResponseWriter, r *http.Request) {
	projectID, environmentID, err := h.parseEnvironment(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	env, err := h.svc.GetEnvironment(r.Context(), projectID, environmentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if env.Status != environmentdom.StatusRunning {
		presenter.Error(w, r, environmentdom.ErrEnvironmentNotRunning)
		return
	}

	ticket := mintEnvironmentTicket(h.aiAgentInternalKey, ticketPurposeStats, env.ID, environmentTicketTTL)
	presenter.OK(w, r, dto.EnvironmentTicketResponse{
		Ticket: ticket,
		WSURL:  fmt.Sprintf("/environments/%s/stats/ws?ticket=%s", env.ID, ticket),
	})
}

// mintEnvironmentTicket builds a signed, time-limited ticket for
// environmentID, scoped to purpose — see this file's "Environment
// tickets" section doc comment for the exact wire format. Binding the
// signature to purpose is what stops a ticket minted for one endpoint
// (say, a terminal ticket, gated on agents.write) from also being replayed
// against the other (the stats endpoint, gated on the weaker agents.read)
// even though both are otherwise identical HMAC-signed
// environment_id+expiry tickets.
func mintEnvironmentTicket(internalKey, purpose string, environmentID uuid.UUID, ttl time.Duration) string {
	expiresAt := time.Now().Add(ttl).Unix()
	payload := fmt.Sprintf("%s|%s|%d", purpose, environmentID.String(), expiresAt)

	mac := hmac.New(sha256.New, []byte(internalKey))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	// RawURLEncoding: unpadded base64url, per the verified wire format above.
	return base64.RawURLEncoding.EncodeToString([]byte(payload + "|" + sig))
}

// parseEnvironment resolves projectId/environmentId from the URL, the
// {project,agent}-scoped equivalent of parseAgentForProject.
func (h *EnvironmentHandler) parseEnvironment(r *http.Request) (projectID, environmentID uuid.UUID, err error) {
	projectID, err = parseProjectID(r)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	environmentID, err = parseParamUUID(r, "environmentId")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return projectID, environmentID, nil
}
