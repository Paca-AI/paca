// Package agentsvc implements the AI Agent application service.
package agentsvc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/messaging"
	"github.com/Paca-AI/api/internal/platform/secret"
)

// projectMemberWriter is the minimal interface this service needs to bust the
// member list cache after an agent is added or removed.
type projectMemberWriter interface {
	InvalidateMembersCache(ctx context.Context, projectID uuid.UUID) error
}

// pluginFinder is the minimal interface to find VCS plugins.
type pluginFinder interface {
	FindByCapability(ctx context.Context, capability string) ([]*plugindom.Plugin, error)
}

// Service is the concrete AI Agent service.
type Service struct {
	repo       agentdom.Repository
	projRepo   projectMemberWriter
	publisher  *messaging.Publisher
	pluginRepo pluginFinder
	encryptor  *secret.Encryptor
}

// New returns a configured agent service.
func New(repo agentdom.Repository, projRepo projectMemberWriter, publisher *messaging.Publisher, pluginRepo pluginFinder) *Service {
	return &Service{repo: repo, projRepo: projRepo, publisher: publisher, pluginRepo: pluginRepo}
}

// WithEncryptor configures AES-256-GCM encryption for the LLM API key stored at rest.
func (s *Service) WithEncryptor(enc *secret.Encryptor) *Service {
	s.encryptor = enc
	return s
}

// encryptKey encrypts plaintext if an encryptor is configured; otherwise returns plaintext unchanged.
func (s *Service) encryptKey(plaintext string) (string, error) {
	if s.encryptor == nil || plaintext == "" {
		return plaintext, nil
	}
	return s.encryptor.Encrypt(plaintext)
}

// -------------------------------------------------------------------------
// Agents
// -------------------------------------------------------------------------

// ListAgents returns agents visible in the given project, optionally
// narrowed to a single AgentScope. See AgentRepository.ListAgents.
func (s *Service) ListAgents(ctx context.Context, projectID uuid.UUID, scope agentdom.AgentScope) ([]*agentdom.Agent, error) {
	return s.repo.ListAgents(ctx, projectID, scope)
}

// GetAgent returns a single agent visible in projectID — its own
// project-scoped agent, or a global agent currently invited into the
// project (see FindVisibleAgentInProject) — so a project's agent detail
// page resolves the same agents its list view shows, rather than 404ing on
// an invited global agent.
func (s *Service) GetAgent(ctx context.Context, projectID, agentID uuid.UUID) (*agentdom.Agent, error) {
	return s.repo.FindVisibleAgentInProject(ctx, projectID, agentID)
}

// CreateAgent validates input, creates the agent, and sets up project membership.
func (s *Service) CreateAgent(ctx context.Context, projectID uuid.UUID, in agentdom.CreateAgentInput) (*agentdom.Agent, error) {
	handle := strings.TrimSpace(in.Handle)
	if handle == "" {
		return nil, agentdom.ErrAgentHandleInvalid
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, agentdom.ErrAgentNameInvalid
	}

	// Check handle uniqueness
	if existing, err := s.repo.FindAgentByHandle(ctx, projectID, handle); err == nil && existing != nil {
		return nil, agentdom.ErrAgentHandleTaken
	}

	agentType := in.AgentType
	if agentType == "" {
		agentType = agentdom.AgentTypeLLM
	}
	if agentType != agentdom.AgentTypeLLM && agentType != agentdom.AgentTypeACP {
		return nil, agentdom.ErrAgentTypeInvalid
	}

	now := time.Now()
	a := &agentdom.Agent{
		ID:             uuid.New(),
		ProjectID:      projectID,
		Name:           name,
		Handle:         handle,
		AgentType:      agentType,
		MaxIterations:  in.MaxIterations,
		TimeoutMinutes: in.TimeoutMinutes,
		CreatedBy:      in.CreatedBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if agentType == agentdom.AgentTypeACP {
		if !agentdom.ValidACPProviders[in.ACPProvider] {
			return nil, agentdom.ErrACPProviderInvalid
		}
		if in.ACPProvider == agentdom.ACPProviderCustom && len(in.ACPCommand) == 0 {
			return nil, agentdom.ErrACPCommandRequired
		}
		provider := in.ACPProvider
		a.ACPProvider = &provider
		a.ACPCommand = in.ACPCommand
	} else {
		encryptedKey, err := s.encryptKey(in.LLMAPIKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt LLM API key: %w", err)
		}
		a.LLMProvider = in.LLMProvider
		a.LLMModel = in.LLMModel
		a.LLMAPIKeySecret = encryptedKey
		a.LLMBaseURL = in.LLMBaseURL
		// System prompt and git committer identity are LLM-only (see the
		// doc comment on Agent.SystemPrompt) — an ACP agent's local CLI
		// owns both of these itself, so they're left unset for ACP agents
		// rather than accepting values that would never take effect.
		a.SystemPrompt = in.SystemPrompt
		a.GitCommitterName = in.GitCommitterName
		a.GitCommitterEmail = in.GitCommitterEmail
		if a.GitCommitterName == "" {
			a.GitCommitterName = "paca-agent"
		}
		if a.GitCommitterEmail == "" {
			a.GitCommitterEmail = "280579135+paca-agent@users.noreply.github.com"
		}
	}
	const maxIterationsLimit = 500
	const defaultMaxIterations = 500
	const timeoutMinutesLimit = 480 // 8 hours

	if a.MaxIterations <= 0 {
		a.MaxIterations = defaultMaxIterations
	} else if a.MaxIterations > maxIterationsLimit {
		a.MaxIterations = maxIterationsLimit
	}
	if a.TimeoutMinutes <= 0 {
		a.TimeoutMinutes = 30
	} else if a.TimeoutMinutes > timeoutMinutesLimit {
		a.TimeoutMinutes = timeoutMinutesLimit
	}

	// Atomically create the agent and its project membership in one transaction.
	memberID := uuid.New()
	if err := s.repo.CreateAgentWithMembership(ctx, a, memberID, projectID, in.ProjectRoleID); err != nil {
		return nil, fmt.Errorf("create agent with membership: %w", err)
	}
	a.MemberID = &memberID

	// Best-effort cache invalidation so the new member appears immediately.
	_ = s.projRepo.InvalidateMembersCache(ctx, projectID)

	return a, nil
}

// UpdateAgent patches mutable fields of an existing agent.
func (s *Service) UpdateAgent(ctx context.Context, projectID, agentID uuid.UUID, in agentdom.UpdateAgentInput) (*agentdom.Agent, error) {
	a, err := s.GetAgent(ctx, projectID, agentID)
	if err != nil {
		return nil, err
	}

	if in.Name != nil {
		a.Name = strings.TrimSpace(*in.Name)
	}
	if in.Handle != nil {
		h := strings.TrimSpace(*in.Handle)
		if h != a.Handle {
			if existing, err := s.repo.FindAgentByHandle(ctx, projectID, h); err == nil && existing != nil {
				return nil, agentdom.ErrAgentHandleTaken
			}
			a.Handle = h
		}
	}
	// LLM/ACP fields are guarded by the agent's existing (immutable) type —
	// agent_type can't be changed through this API, so applying the other
	// shape's fields would only ever leave stale/wrong data on the agent
	// (e.g. an encrypted LLM API key sitting unused on an ACP agent). A
	// request that happens to include both sets of fields (e.g. a generic
	// client payload) silently has the irrelevant half ignored rather than
	// erroring, matching CreateAgent's per-type field selection. Anything
	// other than the explicit ACP type is treated as LLM (its default, as
	// in CreateAgent) so an agent loaded with an unset AgentType isn't
	// silently locked out of updating its LLM fields. SystemPrompt and the
	// git committer identity fields ride along in this same block — like
	// the LLM fields, they're meaningless on an ACP agent (see the doc
	// comment on Agent.SystemPrompt), so a request that sets them on one is
	// silently ignored too.
	if a.AgentType != agentdom.AgentTypeACP {
		if in.LLMProvider != nil {
			a.LLMProvider = *in.LLMProvider
		}
		if in.LLMModel != nil {
			a.LLMModel = *in.LLMModel
		}
		if in.LLMAPIKey != nil {
			encryptedKey, err := s.encryptKey(*in.LLMAPIKey)
			if err != nil {
				return nil, fmt.Errorf("encrypt LLM API key: %w", err)
			}
			a.LLMAPIKeySecret = encryptedKey
		}
		if in.LLMBaseURL != nil {
			a.LLMBaseURL = *in.LLMBaseURL
		}
		if in.SystemPrompt != nil {
			a.SystemPrompt = *in.SystemPrompt
		}
		if in.GitCommitterName != nil {
			a.GitCommitterName = *in.GitCommitterName
		}
		if in.GitCommitterEmail != nil {
			a.GitCommitterEmail = *in.GitCommitterEmail
		}
	}
	if a.AgentType == agentdom.AgentTypeACP {
		if in.ACPProvider != nil {
			if !agentdom.ValidACPProviders[*in.ACPProvider] {
				return nil, agentdom.ErrACPProviderInvalid
			}
			a.ACPProvider = in.ACPProvider
		}
		if in.ACPCommand != nil {
			a.ACPCommand = in.ACPCommand
		}
		if a.ACPProvider != nil && *a.ACPProvider == agentdom.ACPProviderCustom && len(a.ACPCommand) == 0 {
			return nil, agentdom.ErrACPCommandRequired
		}
	}
	const maxIterationsLimit = 500
	const defaultMaxIterations = 500
	const timeoutMinutesLimit = 480

	if in.MaxIterations != nil {
		v := *in.MaxIterations
		if v <= 0 {
			v = defaultMaxIterations
		} else if v > maxIterationsLimit {
			v = maxIterationsLimit
		}
		a.MaxIterations = v
	}
	if in.TimeoutMinutes != nil {
		v := *in.TimeoutMinutes
		if v <= 0 {
			v = 30
		} else if v > timeoutMinutesLimit {
			v = timeoutMinutesLimit
		}
		a.TimeoutMinutes = v
	}
	a.UpdatedAt = time.Now()

	if err := s.repo.UpdateAgent(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// DeleteAgent soft-deletes an agent and its membership.
func (s *Service) DeleteAgent(ctx context.Context, projectID, agentID uuid.UUID) error {
	a, err := s.GetAgent(ctx, projectID, agentID)
	if err != nil {
		return err
	}
	// Atomically soft-delete the agent and its project membership in one transaction.
	if err := s.repo.SoftDeleteAgentWithMembership(ctx, projectID, a.ID); err != nil {
		return err
	}
	// Best-effort cache invalidation so the deleted member disappears immediately.
	_ = s.projRepo.InvalidateMembersCache(ctx, projectID)
	return nil
}

// -------------------------------------------------------------------------
// Global agents (AgentScope == AgentScopeGlobal)
//
// These are intentionally self-contained rather than sharing bodies with
// CreateAgent/UpdateAgent/DeleteAgent above: the two shapes diverge in how
// they establish project access (a project-scoped agent gets exactly one
// project_members row at creation time; a global agent gets zero, and is
// attached to projects later via the invite flow, see
// project.MemberService.AddMember), and keeping them separate means the
// existing, tested project-scoped methods are never touched by this change.
// -------------------------------------------------------------------------

// ListGlobalAgents returns all global-scope agents.
func (s *Service) ListGlobalAgents(ctx context.Context) ([]*agentdom.Agent, error) {
	return s.repo.ListGlobalAgents(ctx)
}

// GetGlobalAgent returns a single agent after verifying it is global-scope.
func (s *Service) GetGlobalAgent(ctx context.Context, agentID uuid.UUID) (*agentdom.Agent, error) {
	a, err := s.repo.FindAgentByID(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if a.AgentScope != agentdom.AgentScopeGlobal {
		return nil, agentdom.ErrAgentNotFound
	}
	return a, nil
}

// CreateGlobalAgent validates input and creates a global-scope agent. Unlike
// CreateAgent, no project_members row is created — the agent starts out
// invited into zero projects.
func (s *Service) CreateGlobalAgent(ctx context.Context, in agentdom.CreateGlobalAgentInput) (*agentdom.Agent, error) {
	handle := strings.TrimSpace(in.Handle)
	if handle == "" {
		return nil, agentdom.ErrAgentHandleInvalid
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, agentdom.ErrAgentNameInvalid
	}

	if existing, err := s.repo.FindGlobalAgentByHandle(ctx, handle); err == nil && existing != nil {
		return nil, agentdom.ErrAgentHandleTaken
	}

	agentType := in.AgentType
	if agentType == "" {
		agentType = agentdom.AgentTypeLLM
	}
	if agentType != agentdom.AgentTypeLLM && agentType != agentdom.AgentTypeACP {
		return nil, agentdom.ErrAgentTypeInvalid
	}

	now := time.Now()
	a := &agentdom.Agent{
		ID:             uuid.New(),
		AgentScope:     agentdom.AgentScopeGlobal,
		GlobalRoleID:   in.GlobalRoleID,
		Name:           name,
		Handle:         handle,
		AgentType:      agentType,
		MaxIterations:  in.MaxIterations,
		TimeoutMinutes: in.TimeoutMinutes,
		CreatedBy:      in.CreatedBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if agentType == agentdom.AgentTypeACP {
		if !agentdom.ValidACPProviders[in.ACPProvider] {
			return nil, agentdom.ErrACPProviderInvalid
		}
		if in.ACPProvider == agentdom.ACPProviderCustom && len(in.ACPCommand) == 0 {
			return nil, agentdom.ErrACPCommandRequired
		}
		provider := in.ACPProvider
		a.ACPProvider = &provider
		a.ACPCommand = in.ACPCommand
	} else {
		encryptedKey, err := s.encryptKey(in.LLMAPIKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt LLM API key: %w", err)
		}
		a.LLMProvider = in.LLMProvider
		a.LLMModel = in.LLMModel
		a.LLMAPIKeySecret = encryptedKey
		a.LLMBaseURL = in.LLMBaseURL
		a.SystemPrompt = in.SystemPrompt
		a.GitCommitterName = in.GitCommitterName
		a.GitCommitterEmail = in.GitCommitterEmail
		if a.GitCommitterName == "" {
			a.GitCommitterName = "paca-agent"
		}
		if a.GitCommitterEmail == "" {
			a.GitCommitterEmail = "280579135+paca-agent@users.noreply.github.com"
		}
	}
	const maxIterationsLimit = 500
	const defaultMaxIterations = 500
	const timeoutMinutesLimit = 480 // 8 hours

	if a.MaxIterations <= 0 {
		a.MaxIterations = defaultMaxIterations
	} else if a.MaxIterations > maxIterationsLimit {
		a.MaxIterations = maxIterationsLimit
	}
	if a.TimeoutMinutes <= 0 {
		a.TimeoutMinutes = 30
	} else if a.TimeoutMinutes > timeoutMinutesLimit {
		a.TimeoutMinutes = timeoutMinutesLimit
	}

	if err := s.repo.CreateGlobalAgent(ctx, a); err != nil {
		return nil, fmt.Errorf("create global agent: %w", err)
	}
	return a, nil
}

// UpdateGlobalAgent patches mutable fields of an existing global agent,
// including GlobalRoleID (set in.GlobalRoleID to &uuid.Nil to clear it).
func (s *Service) UpdateGlobalAgent(ctx context.Context, agentID uuid.UUID, in agentdom.UpdateAgentInput) (*agentdom.Agent, error) {
	a, err := s.GetGlobalAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}

	if in.Name != nil {
		a.Name = strings.TrimSpace(*in.Name)
	}
	if in.Handle != nil {
		h := strings.TrimSpace(*in.Handle)
		if h != a.Handle {
			if existing, err := s.repo.FindGlobalAgentByHandle(ctx, h); err == nil && existing != nil {
				return nil, agentdom.ErrAgentHandleTaken
			}
			a.Handle = h
		}
	}
	// See the equivalent block in UpdateAgent for why LLM/ACP fields are
	// guarded by the agent's existing (immutable) type.
	if a.AgentType != agentdom.AgentTypeACP {
		if in.LLMProvider != nil {
			a.LLMProvider = *in.LLMProvider
		}
		if in.LLMModel != nil {
			a.LLMModel = *in.LLMModel
		}
		if in.LLMAPIKey != nil {
			encryptedKey, err := s.encryptKey(*in.LLMAPIKey)
			if err != nil {
				return nil, fmt.Errorf("encrypt LLM API key: %w", err)
			}
			a.LLMAPIKeySecret = encryptedKey
		}
		if in.LLMBaseURL != nil {
			a.LLMBaseURL = *in.LLMBaseURL
		}
		if in.SystemPrompt != nil {
			a.SystemPrompt = *in.SystemPrompt
		}
		if in.GitCommitterName != nil {
			a.GitCommitterName = *in.GitCommitterName
		}
		if in.GitCommitterEmail != nil {
			a.GitCommitterEmail = *in.GitCommitterEmail
		}
	}
	if a.AgentType == agentdom.AgentTypeACP {
		if in.ACPProvider != nil {
			if !agentdom.ValidACPProviders[*in.ACPProvider] {
				return nil, agentdom.ErrACPProviderInvalid
			}
			a.ACPProvider = in.ACPProvider
		}
		if in.ACPCommand != nil {
			a.ACPCommand = in.ACPCommand
		}
		if a.ACPProvider != nil && *a.ACPProvider == agentdom.ACPProviderCustom && len(a.ACPCommand) == 0 {
			return nil, agentdom.ErrACPCommandRequired
		}
	}
	const maxIterationsLimit = 500
	const defaultMaxIterations = 500
	const timeoutMinutesLimit = 480

	if in.MaxIterations != nil {
		v := *in.MaxIterations
		if v <= 0 {
			v = defaultMaxIterations
		} else if v > maxIterationsLimit {
			v = maxIterationsLimit
		}
		a.MaxIterations = v
	}
	if in.TimeoutMinutes != nil {
		v := *in.TimeoutMinutes
		if v <= 0 {
			v = 30
		} else if v > timeoutMinutesLimit {
			v = timeoutMinutesLimit
		}
		a.TimeoutMinutes = v
	}
	if in.GlobalRoleID != nil {
		if *in.GlobalRoleID == uuid.Nil {
			a.GlobalRoleID = nil
		} else {
			a.GlobalRoleID = in.GlobalRoleID
		}
	}
	a.UpdatedAt = time.Now()

	if err := s.repo.UpdateAgent(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// DeleteGlobalAgent soft-deletes a global agent and every project_members
// row referencing it, across every project it was invited into.
func (s *Service) DeleteGlobalAgent(ctx context.Context, agentID uuid.UUID) error {
	a, err := s.GetGlobalAgent(ctx, agentID)
	if err != nil {
		return err
	}
	// Snapshot affected projects before the cascade delete so their
	// member-list caches can be invalidated once membership is actually gone.
	projectIDs, err := s.repo.ListInvitedProjectIDs(ctx, a.ID)
	if err != nil {
		return err
	}
	if err := s.repo.SoftDeleteGlobalAgentCascade(ctx, a.ID); err != nil {
		return err
	}
	for _, projectID := range projectIDs {
		_ = s.projRepo.InvalidateMembersCache(ctx, projectID)
	}
	return nil
}

// ListInvitedProjectIDs returns the IDs of every project a global agent
// currently has an active project_members row in.
func (s *Service) ListInvitedProjectIDs(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error) {
	return s.repo.ListInvitedProjectIDs(ctx, agentID)
}

// generateHashedSecret returns a fresh 32 random bytes as both its hex
// plaintext and the hex SHA-256 hash of that plaintext — the shared
// generation step behind every "issue a new bridge token / MCP key" method
// below, all of which persist only the hash and return the plaintext once.
func generateHashedSecret() (plaintext, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	plaintext = hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plaintext))
	return plaintext, hex.EncodeToString(sum[:]), nil
}

// GenerateACPBridgeToken issues a new local-bridge auth token for an ACP-type
// agent, replacing any existing one. Only the token's SHA-256 hash is
// persisted (services/ai-agent hashes an incoming token the same way to
// verify it) — the plaintext is returned once here and cannot be recovered
// afterward.
func (s *Service) GenerateACPBridgeToken(ctx context.Context, projectID, agentID uuid.UUID) (string, error) {
	a, err := s.GetAgent(ctx, projectID, agentID)
	if err != nil {
		return "", err
	}
	if a.AgentType != agentdom.AgentTypeACP {
		return "", agentdom.ErrAgentTypeInvalid
	}
	plaintext, hash, err := generateHashedSecret()
	if err != nil {
		return "", fmt.Errorf("generate bridge token: %w", err)
	}
	if err := s.repo.SetACPBridgeTokenHash(ctx, agentID, hash); err != nil {
		return "", fmt.Errorf("store bridge token hash: %w", err)
	}
	return plaintext, nil
}

// GenerateGlobalACPBridgeToken is GenerateACPBridgeToken's global-agent
// sibling — identical token generation, ownership verified via
// GetGlobalAgent (AgentScope == global) instead of a projectID match.
func (s *Service) GenerateGlobalACPBridgeToken(ctx context.Context, agentID uuid.UUID) (string, error) {
	a, err := s.GetGlobalAgent(ctx, agentID)
	if err != nil {
		return "", err
	}
	if a.AgentType != agentdom.AgentTypeACP {
		return "", agentdom.ErrAgentTypeInvalid
	}
	plaintext, hash, err := generateHashedSecret()
	if err != nil {
		return "", fmt.Errorf("generate bridge token: %w", err)
	}
	if err := s.repo.SetACPBridgeTokenHash(ctx, agentID, hash); err != nil {
		return "", fmt.Errorf("store bridge token hash: %w", err)
	}
	return plaintext, nil
}

// GenerateAgentMCPKey issues a new MCP API key for an ACP-type agent,
// replacing any existing one, and returns the plaintext once — only its
// SHA-256 hash is persisted. Overwriting the hash means the previous key
// stops authenticating immediately (see AgentRepository.SetMCPAPIKeyHash).
func (s *Service) GenerateAgentMCPKey(ctx context.Context, projectID, agentID uuid.UUID) (string, error) {
	a, err := s.GetAgent(ctx, projectID, agentID)
	if err != nil {
		return "", err
	}
	if a.AgentType != agentdom.AgentTypeACP {
		return "", agentdom.ErrAgentTypeInvalid
	}
	plaintext, hash, err := generateHashedSecret()
	if err != nil {
		return "", fmt.Errorf("generate MCP API key: %w", err)
	}
	if err := s.repo.SetMCPAPIKeyHash(ctx, agentID, hash); err != nil {
		return "", fmt.Errorf("store MCP API key hash: %w", err)
	}
	return plaintext, nil
}

// GenerateGlobalAgentMCPKey is GenerateAgentMCPKey's global-agent sibling —
// ownership verified via GetGlobalAgent instead of a projectID match.
func (s *Service) GenerateGlobalAgentMCPKey(ctx context.Context, agentID uuid.UUID) (string, error) {
	a, err := s.GetGlobalAgent(ctx, agentID)
	if err != nil {
		return "", err
	}
	if a.AgentType != agentdom.AgentTypeACP {
		return "", agentdom.ErrAgentTypeInvalid
	}
	plaintext, hash, err := generateHashedSecret()
	if err != nil {
		return "", fmt.Errorf("generate MCP API key: %w", err)
	}
	if err := s.repo.SetMCPAPIKeyHash(ctx, agentID, hash); err != nil {
		return "", fmt.Errorf("store MCP API key hash: %w", err)
	}
	return plaintext, nil
}

// requireNonACPAgent rejects MCP server / skill / environment variable
// mutations targeting an ACP-type agent. ACP agents run entirely in the
// user's own local CLI via paca-acp-bridge; services/ai-agent's
// acp_dispatch.py never reads any of these tables when dispatching an ACP
// turn, so accepting the write here would silently no-op rather than have
// any effect — better to reject it outright. Read (List*) operations are
// left permissive since returning an empty list is harmless.
func (s *Service) requireNonACPAgent(ctx context.Context, agentID uuid.UUID) error {
	agent, err := s.repo.FindAgentByID(ctx, agentID)
	if err != nil {
		return err
	}
	if agent.AgentType == agentdom.AgentTypeACP {
		return agentdom.ErrNotSupportedForACPAgent
	}
	return nil
}

// -------------------------------------------------------------------------
// MCP Servers
// -------------------------------------------------------------------------

// ListMCPServers returns all MCP servers for the given agent.
func (s *Service) ListMCPServers(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentMCPServer, error) {
	return s.repo.ListMCPServers(ctx, agentID)
}

// AddMCPServer creates a new MCP server for the given agent.
func (s *Service) AddMCPServer(ctx context.Context, agentID uuid.UUID, in agentdom.AddMCPServerInput) (*agentdom.AgentMCPServer, error) {
	if in.Transport == "stdio" && (in.Command == nil || *in.Command == "") {
		return nil, agentdom.ErrMCPServerCommandRequired
	}
	if err := s.requireNonACPAgent(ctx, agentID); err != nil {
		return nil, err
	}

	now := time.Now()
	srv := &agentdom.AgentMCPServer{
		ID:         uuid.New(),
		AgentID:    agentID,
		ServerName: strings.TrimSpace(in.ServerName),
		Transport:  in.Transport,
		Command:    in.Command,
		Args:       in.Args,
		URL:        in.URL,
		Env:        in.Env,
		IsEnabled:  true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if srv.Args == nil {
		srv.Args = []string{}
	}
	if srv.Env == nil {
		srv.Env = map[string]string{}
	}
	if err := s.repo.CreateMCPServer(ctx, srv); err != nil {
		return nil, err
	}
	return srv, nil
}

// UpdateMCPServer patches mutable fields of an existing MCP server.
func (s *Service) UpdateMCPServer(ctx context.Context, agentID, serverID uuid.UUID, in agentdom.UpdateMCPServerInput) (*agentdom.AgentMCPServer, error) {
	srv, err := s.repo.FindMCPServerByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if srv.AgentID != agentID {
		return nil, agentdom.ErrMCPServerNotFound
	}
	if err := s.requireNonACPAgent(ctx, agentID); err != nil {
		return nil, err
	}
	if in.Command != nil {
		srv.Command = in.Command
	}
	if in.Args != nil {
		srv.Args = in.Args
	}
	if in.URL != nil {
		srv.URL = in.URL
	}
	if in.Env != nil {
		srv.Env = in.Env
	}
	if in.IsEnabled != nil {
		srv.IsEnabled = *in.IsEnabled
	}
	srv.UpdatedAt = time.Now()
	if err := s.repo.UpdateMCPServer(ctx, srv); err != nil {
		return nil, err
	}
	return srv, nil
}

// DeleteMCPServer removes an MCP server after verifying ownership.
func (s *Service) DeleteMCPServer(ctx context.Context, agentID, serverID uuid.UUID) error {
	srv, err := s.repo.FindMCPServerByID(ctx, serverID)
	if err != nil {
		return err
	}
	if srv.AgentID != agentID {
		return agentdom.ErrMCPServerNotFound
	}
	if err := s.requireNonACPAgent(ctx, agentID); err != nil {
		return err
	}
	return s.repo.DeleteMCPServer(ctx, serverID)
}

// -------------------------------------------------------------------------
// Skills
// -------------------------------------------------------------------------

func validateSkillName(name string) error {
	if agentdom.IsReservedSkillName(name) {
		return agentdom.ErrSkillNameReserved
	}
	return nil
}

// ListSkills returns all skills for the given agent.
func (s *Service) ListSkills(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentSkill, error) {
	return s.repo.ListSkills(ctx, agentID)
}

// AddSkill creates a new skill for the given agent.
func (s *Service) AddSkill(ctx context.Context, agentID uuid.UUID, in agentdom.AddSkillInput) (*agentdom.AgentSkill, error) {
	name := strings.TrimSpace(in.SkillName)
	if err := validateSkillName(name); err != nil {
		return nil, err
	}
	if err := s.requireNonACPAgent(ctx, agentID); err != nil {
		return nil, err
	}
	now := time.Now()
	skill := &agentdom.AgentSkill{
		ID:           uuid.New(),
		AgentID:      agentID,
		SkillName:    name,
		SkillSource:  in.SkillSource,
		SkillContent: in.SkillContent,
		SourceURL:    in.SourceURL,
		Triggers:     in.Triggers,
		IsEnabled:    true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if skill.Triggers == nil {
		skill.Triggers = []string{}
	}
	if err := s.repo.CreateSkill(ctx, skill); err != nil {
		return nil, err
	}
	return skill, nil
}

// UpdateSkill patches mutable fields of an existing skill.
func (s *Service) UpdateSkill(ctx context.Context, agentID, skillID uuid.UUID, in agentdom.UpdateSkillInput) (*agentdom.AgentSkill, error) {
	skill, err := s.repo.FindSkillByID(ctx, skillID)
	if err != nil {
		return nil, err
	}
	if skill.AgentID != agentID {
		return nil, agentdom.ErrSkillNotFound
	}
	if err := s.requireNonACPAgent(ctx, agentID); err != nil {
		return nil, err
	}
	if in.SkillContent != nil {
		skill.SkillContent = *in.SkillContent
	}
	if in.Triggers != nil {
		skill.Triggers = in.Triggers
	}
	if in.IsEnabled != nil {
		skill.IsEnabled = *in.IsEnabled
	}
	skill.UpdatedAt = time.Now()
	if err := s.repo.UpdateSkill(ctx, skill); err != nil {
		return nil, err
	}
	return skill, nil
}

// DeleteSkill removes a skill after verifying ownership.
func (s *Service) DeleteSkill(ctx context.Context, agentID, skillID uuid.UUID) error {
	skill, err := s.repo.FindSkillByID(ctx, skillID)
	if err != nil {
		return err
	}
	if skill.AgentID != agentID {
		return agentdom.ErrSkillNotFound
	}
	if err := s.requireNonACPAgent(ctx, agentID); err != nil {
		return err
	}
	return s.repo.DeleteSkill(ctx, skillID)
}

// -------------------------------------------------------------------------
// Environment Variables
// -------------------------------------------------------------------------

// envVarKeyPattern matches valid shell environment variable names.
var envVarKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// reservedEnvVarKeys are names the sandbox container already sets for its own
// operation (git identity, MCP wiring, secrets). User-supplied variables may
// not use these names, so a misconfigured agent can never shadow them.
// Keep in sync with services/ai-agent/src/agent/docker_workspace.py and
// services/ai-agent/src/agent/builder.py.
var reservedEnvVarKeys = map[string]bool{
	"OH_SECRET_KEY":             true,
	"OPENHANDS_SUPPRESS_BANNER": true,
	"GIT_AUTHOR_NAME":           true,
	"GIT_AUTHOR_EMAIL":          true,
	"GIT_COMMITTER_NAME":        true,
	"GIT_COMMITTER_EMAIL":       true,
	"OH_EXTRA_PYTHON_PATH":      true,
}

// validateEnvVarKey checks that key is a well-formed, non-reserved shell
// environment variable name. The reserved-name check is case-insensitive so
// a lookalike like "oh_secret_key" can't sit alongside the real uppercase
// infra variable and confuse anyone inspecting the container's environment.
func validateEnvVarKey(key string) error {
	if !envVarKeyPattern.MatchString(key) {
		return agentdom.ErrEnvVarKeyInvalid
	}
	upperKey := strings.ToUpper(key)
	if reservedEnvVarKeys[upperKey] || strings.HasPrefix(upperKey, "PACA_") {
		return agentdom.ErrEnvVarKeyReserved
	}
	return nil
}

// ListEnvVars returns all secret environment variables for the given agent.
func (s *Service) ListEnvVars(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentEnvironmentVariable, error) {
	return s.repo.ListEnvVars(ctx, agentID)
}

// AddEnvVar creates a new secret environment variable for the given agent.
func (s *Service) AddEnvVar(ctx context.Context, agentID uuid.UUID, in agentdom.AddEnvVarInput) (*agentdom.AgentEnvironmentVariable, error) {
	key := strings.TrimSpace(in.Key)
	if err := validateEnvVarKey(key); err != nil {
		return nil, err
	}
	if err := s.requireNonACPAgent(ctx, agentID); err != nil {
		return nil, err
	}
	if existing, err := s.repo.FindEnvVarByKey(ctx, agentID, key); err == nil && existing != nil {
		return nil, agentdom.ErrEnvVarKeyTaken
	}
	encryptedValue, err := s.encryptKey(in.Value)
	if err != nil {
		return nil, fmt.Errorf("encrypt environment variable value: %w", err)
	}
	now := time.Now()
	v := &agentdom.AgentEnvironmentVariable{
		ID:             uuid.New(),
		AgentID:        agentID,
		Key:            key,
		EncryptedValue: encryptedValue,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.repo.CreateEnvVar(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

// UpdateEnvVar replaces the value of an existing environment variable.
func (s *Service) UpdateEnvVar(ctx context.Context, agentID, envVarID uuid.UUID, in agentdom.UpdateEnvVarInput) (*agentdom.AgentEnvironmentVariable, error) {
	v, err := s.repo.FindEnvVarByID(ctx, envVarID)
	if err != nil {
		return nil, err
	}
	if v.AgentID != agentID {
		return nil, agentdom.ErrEnvVarNotFound
	}
	if err := s.requireNonACPAgent(ctx, agentID); err != nil {
		return nil, err
	}
	encryptedValue, err := s.encryptKey(in.Value)
	if err != nil {
		return nil, fmt.Errorf("encrypt environment variable value: %w", err)
	}
	v.EncryptedValue = encryptedValue
	v.UpdatedAt = time.Now()
	if err := s.repo.UpdateEnvVar(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

// DeleteEnvVar removes an environment variable after verifying ownership.
func (s *Service) DeleteEnvVar(ctx context.Context, agentID, envVarID uuid.UUID) error {
	v, err := s.repo.FindEnvVarByID(ctx, envVarID)
	if err != nil {
		return err
	}
	if v.AgentID != agentID {
		return agentdom.ErrEnvVarNotFound
	}
	if err := s.requireNonACPAgent(ctx, agentID); err != nil {
		return err
	}
	return s.repo.DeleteEnvVar(ctx, envVarID)
}

// -------------------------------------------------------------------------
// Conversations
// -------------------------------------------------------------------------

// ListConversations returns a page of conversations matching the filter.
func (s *Service) ListConversations(ctx context.Context, in agentdom.ListConversationsFilter, limit int) ([]*agentdom.AgentConversation, bool, error) {
	return s.repo.ListConversations(ctx, in, limit)
}

// ListAgentActivities returns a page of an agent's unified task+doc activity feed.
func (s *Service) ListAgentActivities(ctx context.Context, in agentdom.ListAgentActivitiesFilter, limit int) ([]*agentdom.ActivityFeedItem, bool, error) {
	return s.repo.ListAgentActivities(ctx, in, limit)
}

// GetConversation returns a single conversation after verifying project ownership.
func (s *Service) GetConversation(ctx context.Context, projectID, conversationID uuid.UUID) (*agentdom.AgentConversation, error) {
	c, err := s.repo.FindConversationByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if c.ProjectID != projectID {
		return nil, agentdom.ErrConversationNotFound
	}
	return c, nil
}

// ListConversationEvents returns a paginated list of events for a conversation.
func (s *Service) ListConversationEvents(ctx context.Context, conversationID uuid.UUID, offset, limit int) ([]*agentdom.AgentConversationEvent, int64, error) {
	return s.repo.ListConversationEvents(ctx, conversationID, offset, limit)
}

// StopConversation stops a conversation that is not already finished.
//
// Unlike every other terminal-status transition (finished/failed, and a
// paused-chat "stopped"), this one is decided and written here rather than
// by ai-agent's own turn-end logic — ai-agent's _post_turn_status
// deliberately no-ops on a full-teardown stop since this call already owns
// the write. That means this is also the only place that can durably notify
// worker.AutomationConsumer (via StreamAgentConversationStatus) that a
// trigger_ai_agent-started conversation it might be waiting on just reached
// a terminal status — ai-agent has no turn-end hook to do it from here,
// since the stop can land while no turn is even in flight.
func (s *Service) StopConversation(ctx context.Context, projectID, conversationID uuid.UUID) error {
	c, err := s.GetConversation(ctx, projectID, conversationID)
	if err != nil {
		return err
	}
	if agentdom.ConversationStatus(c.Status).IsTerminal() {
		return agentdom.ErrConversationAlreadyStopped
	}
	if err := s.repo.UpdateConversationStatus(ctx, conversationID, string(agentdom.ConversationStatusStopped)); err != nil {
		return err
	}
	// Best-effort: a failure here shouldn't fail the stop itself (the
	// conversation is already marked stopped and ai-agent is about to be
	// told to tear it down) — same posture as sprintsvc.publishSprintActivity.
	// A graph walk genuinely left waiting on this conversation stays paused
	// until the automation is edited/deactivated; there's no separate
	// timeout/reaper for a pending wait today.
	_ = s.publisher.AppendFlat(ctx, events.StreamAgentConversationStatus, map[string]any{
		"conversation_id": conversationID.String(),
		"status":          string(agentdom.ConversationStatusStopped),
	})
	return s.publishTrigger(ctx, events.TopicAgentStop, map[string]any{
		"conversation_id": conversationID.String(),
		"project_id":      projectID.String(),
	})
}

// PauseConversation interrupts a conversation's in-flight turn without
// touching its sandbox — it goes back to "paused" once ai-agent processes
// the interrupt. No DB write here: ai-agent's run_conversation writes the
// resulting status itself once the turn actually pauses.
func (s *Service) PauseConversation(ctx context.Context, projectID, conversationID uuid.UUID) error {
	c, err := s.GetConversation(ctx, projectID, conversationID)
	if err != nil {
		return err
	}
	if agentdom.ConversationStatus(c.Status) != agentdom.ConversationStatusRunning {
		return agentdom.ErrConversationNotRunning
	}
	return s.publishTrigger(ctx, events.TopicAgentPause, map[string]any{
		"conversation_id": conversationID.String(),
		"project_id":      projectID.String(),
	})
}

// Heartbeat refreshes a chat conversation's idle timer. Fires on a ~30s
// interval per open browser tab (see apps/web) — deliberately does not touch
// Postgres; ai-agent cross-checks project_id in-memory before honoring it
// (see worker._handle_control in services/ai-agent). GetConversation is
// still called here so the API layer itself enforces project ownership,
// rather than resting the whole authorization boundary on ai-agent's
// in-memory check.
func (s *Service) Heartbeat(ctx context.Context, projectID, conversationID uuid.UUID) error {
	if _, err := s.GetConversation(ctx, projectID, conversationID); err != nil {
		return err
	}
	return s.publishTrigger(ctx, events.TopicAgentHeartbeat, map[string]any{
		"conversation_id": conversationID.String(),
		"project_id":      projectID.String(),
	})
}

// SendConversationMessage publishes a chat message to an active conversation.
//
// ACP-type agents route through sendACPConversationMessage instead: unlike an
// LLM agent (where a follow-up message only ever makes sense while a turn is
// actually running), an ACP agent's local bridge daemon keeps a conversation
// alive by conversation_id regardless of which trigger type started it
// (task_assigned, comment_mention, description_write, automation_message —
// not just chat_message), so it can always be resumed here too — mirroring
// SendChatMessage's terminal-status resume for chat sessions.
func (s *Service) SendConversationMessage(ctx context.Context, projectID, conversationID uuid.UUID, message string, memberID uuid.UUID) error {
	c, err := s.GetConversation(ctx, projectID, conversationID)
	if err != nil {
		return err
	}

	agent, err := s.repo.FindAgentByID(ctx, c.AgentID)
	if err != nil {
		return err
	}
	if agent.AgentType == agentdom.AgentTypeACP {
		return s.sendACPConversationMessage(ctx, c, message, memberID)
	}

	if agentdom.ConversationStatus(c.Status) != agentdom.ConversationStatusRunning {
		return agentdom.ErrConversationNotRunning
	}
	return s.publishTrigger(ctx, events.TopicAgentChatMessage, map[string]any{
		"conversation_id": conversationID.String(),
		"project_id":      projectID.String(),
		"message":         message,
		"member_id":       memberID.String(),
	})
}

// sendACPConversationMessage resumes an ACP conversation of any trigger type
// so it can be continued from the chat box, not just chat_message ones.
func (s *Service) sendACPConversationMessage(ctx context.Context, c *agentdom.AgentConversation, message string, memberID uuid.UUID) error {
	status := agentdom.ConversationStatus(c.Status)
	if status == agentdom.ConversationStatusRunning || status == agentdom.ConversationStatusQueued {
		// Still mid-turn (or not yet picked up by the worker) — reject
		// instead of dispatching a second start_turn on top of one the
		// bridge hasn't finished: ConversationRunner.start_turn's own
		// "still running" guard would report the *in-flight* turn as
		// failed, not queue this message behind it.
		return agentdom.ErrConversationBusy
	}
	// Claim atomically so two concurrent replies can't both win and
	// double-publish a resume trigger for the same conversation_id — same
	// race guard as SendChatMessage's resume paths.
	claimed, err := s.repo.ClaimConversationStatus(ctx, c.ID, string(status), string(agentdom.ConversationStatusRunning))
	if err != nil {
		return err
	}
	if !claimed {
		return agentdom.ErrConversationBusy
	}
	return s.publishTrigger(ctx, events.TopicAgentChatMessage, map[string]any{
		"conversation_id": c.ID.String(),
		"project_id":      c.ProjectID.String(),
		"agent_id":        c.AgentID.String(),
		"trigger_type":    c.TriggerType,
		"actor_member_id": memberID.String(),
		"message":         message,
		"repo_plugin_ids": strings.Join(s.gatherRepoPluginIDs(ctx), ","),
	})
}

// -------------------------------------------------------------------------
// Global Conversations (ProjectID == uuid.Nil) — siblings of the
// Conversations methods above, scoped to "no project" instead of a given
// projectID, with the actor identified by ActorUserID instead of a
// project_members.id. Global-chat conversations never gather repo/PR tools
// (repo_plugin_ids is omitted from their trigger payloads) — repository
// access is inherently project-shaped and out of scope for a conversation
// with no project context.
// -------------------------------------------------------------------------

// ListGlobalConversations returns a page of the caller's own global-chat
// conversations matching the filter. GlobalOnly and ActorUserID are forced
// server-side — a caller can never list another user's global conversations
// by passing a different actor, unlike the project-scoped listing which is
// visible to the whole project team.
func (s *Service) ListGlobalConversations(ctx context.Context, actorUserID uuid.UUID, in agentdom.ListConversationsFilter, limit int) ([]*agentdom.AgentConversation, bool, error) {
	in.GlobalOnly = true
	in.ProjectID = nil
	in.ActorUserID = &actorUserID
	return s.repo.ListConversations(ctx, in, limit)
}

// GetGlobalConversation returns a single conversation after verifying it is
// both a global-chat conversation (ProjectID == uuid.Nil) AND owned by
// actorUserID — the global-chat equivalent of GetConversation's projectID
// ownership check. Without the actor check, any authenticated user could
// read, control, or inject messages into another user's global-chat
// conversation simply by knowing its ID, since global conversations have no
// project-team membership to gate access the way project conversations do.
func (s *Service) GetGlobalConversation(ctx context.Context, conversationID, actorUserID uuid.UUID) (*agentdom.AgentConversation, error) {
	c, err := s.repo.FindConversationByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if c.ProjectID != uuid.Nil || c.ActorUserID == nil || *c.ActorUserID != actorUserID {
		return nil, agentdom.ErrConversationNotFound
	}
	return c, nil
}

// StopGlobalConversation stops a global conversation that is not already finished.
func (s *Service) StopGlobalConversation(ctx context.Context, conversationID, actorUserID uuid.UUID) error {
	c, err := s.GetGlobalConversation(ctx, conversationID, actorUserID)
	if err != nil {
		return err
	}
	if agentdom.ConversationStatus(c.Status).IsTerminal() {
		return agentdom.ErrConversationAlreadyStopped
	}
	if err := s.repo.UpdateConversationStatus(ctx, conversationID, string(agentdom.ConversationStatusStopped)); err != nil {
		return err
	}
	return s.publishTrigger(ctx, events.TopicAgentStop, map[string]any{
		"conversation_id": conversationID.String(),
	})
}

// PauseGlobalConversation interrupts a global conversation's in-flight turn.
func (s *Service) PauseGlobalConversation(ctx context.Context, conversationID, actorUserID uuid.UUID) error {
	c, err := s.GetGlobalConversation(ctx, conversationID, actorUserID)
	if err != nil {
		return err
	}
	if agentdom.ConversationStatus(c.Status) != agentdom.ConversationStatusRunning {
		return agentdom.ErrConversationNotRunning
	}
	return s.publishTrigger(ctx, events.TopicAgentPause, map[string]any{
		"conversation_id": conversationID.String(),
	})
}

// GlobalHeartbeat refreshes a global conversation's idle timer.
func (s *Service) GlobalHeartbeat(ctx context.Context, conversationID, actorUserID uuid.UUID) error {
	if _, err := s.GetGlobalConversation(ctx, conversationID, actorUserID); err != nil {
		return err
	}
	return s.publishTrigger(ctx, events.TopicAgentHeartbeat, map[string]any{
		"conversation_id": conversationID.String(),
	})
}

// SendGlobalConversationMessage publishes a chat message to an active global conversation.
func (s *Service) SendGlobalConversationMessage(ctx context.Context, conversationID uuid.UUID, message string, actorUserID uuid.UUID) error {
	c, err := s.GetGlobalConversation(ctx, conversationID, actorUserID)
	if err != nil {
		return err
	}

	agent, err := s.repo.FindAgentByID(ctx, c.AgentID)
	if err != nil {
		return err
	}
	if agent.AgentType == agentdom.AgentTypeACP {
		return s.sendACPGlobalConversationMessage(ctx, c, message, actorUserID)
	}

	if agentdom.ConversationStatus(c.Status) != agentdom.ConversationStatusRunning {
		return agentdom.ErrConversationNotRunning
	}
	return s.publishTrigger(ctx, events.TopicAgentChatMessage, map[string]any{
		"conversation_id": conversationID.String(),
		"agent_id":        c.AgentID.String(),
		"message":         message,
		"actor_user_id":   actorUserID.String(),
	})
}

// sendACPGlobalConversationMessage is sendACPConversationMessage's
// global-chat sibling — see its doc comment for why ACP conversations can
// always be resumed regardless of trigger type or terminal status.
func (s *Service) sendACPGlobalConversationMessage(ctx context.Context, c *agentdom.AgentConversation, message string, actorUserID uuid.UUID) error {
	status := agentdom.ConversationStatus(c.Status)
	if status == agentdom.ConversationStatusRunning || status == agentdom.ConversationStatusQueued {
		return agentdom.ErrConversationBusy
	}
	claimed, err := s.repo.ClaimConversationStatus(ctx, c.ID, string(status), string(agentdom.ConversationStatusRunning))
	if err != nil {
		return err
	}
	if !claimed {
		return agentdom.ErrConversationBusy
	}
	return s.publishTrigger(ctx, events.TopicAgentChatMessage, map[string]any{
		"conversation_id": c.ID.String(),
		"agent_id":        c.AgentID.String(),
		"trigger_type":    c.TriggerType,
		"actor_user_id":   actorUserID.String(),
		"message":         message,
	})
}

// -------------------------------------------------------------------------
// Chat Sessions
// -------------------------------------------------------------------------

// ListChatSessions returns all chat sessions for the given agent and member.
func (s *Service) ListChatSessions(ctx context.Context, _, agentID, memberID uuid.UUID) ([]*agentdom.AgentChatSession, error) {
	return s.repo.ListChatSessions(ctx, agentID, memberID)
}

// StartChatSession creates a new chat session and publishes the initial message trigger.
func (s *Service) StartChatSession(ctx context.Context, projectID, agentID, memberID uuid.UUID, message string) (*agentdom.AgentChatSession, *agentdom.AgentConversation, error) {
	now := time.Now()

	session := &agentdom.AgentChatSession{
		ID:            uuid.New(),
		AgentID:       agentID,
		ProjectID:     projectID,
		MemberID:      memberID,
		LastMessageAt: &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.CreateChatSession(ctx, session); err != nil {
		return nil, nil, err
	}

	conv, err := s.createConversation(ctx, projectID, agentID, &memberID, agentdom.AgentConversation{
		TriggerType:   "chat_message",
		ChatSessionID: &session.ID,
	})
	if err != nil {
		return nil, nil, err
	}

	if err := s.publishChatTrigger(ctx, agentID, conv.ID, session.ID, projectID, memberID, message, s.gatherRepoPluginIDs(ctx)); err != nil {
		return nil, nil, err
	}

	return session, conv, nil
}

// SendChatMessage sends a message to an existing chat session and publishes the trigger.
//
// The ai-agent service keeps a chat session's sandbox alive between replies
// instead of tearing it down after every turn (see docs/ai-agent's
// pause/resume design) — a conversation that reaches a natural per-turn
// finish is left with status "paused" rather than "finished". A reply while
// paused resumes that same conversation (same conversation_id, so the agent
// keeps the sandbox/history) instead of cold-starting a new one.
//
// ACP-type agents get the same treatment even once a conversation goes
// terminal (finished/failed/stopped): unlike an LLM agent's cloud sandbox,
// which is gone for good once its chat conversation ends, an ACP agent's
// local bridge daemon (apps/acp-bridge) keeps the underlying Conversation
// object alive in memory for as long as the daemon keeps running. So a reply
// can always continue the *same* conversation_id, no matter how long ago it
// went terminal — see runner.ConversationRunner.start_turn's resume branch.
func (s *Service) SendChatMessage(ctx context.Context, projectID, sessionID, memberID uuid.UUID, message string) (*agentdom.AgentConversation, error) {
	session, err := s.repo.FindChatSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.ProjectID != projectID {
		return nil, agentdom.ErrChatSessionNotFound
	}

	latest, err := s.repo.FindLatestConversationByChatSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	conv := latest
	if latest != nil {
		switch agentdom.ConversationStatus(latest.Status) {
		case agentdom.ConversationStatusRunning, agentdom.ConversationStatusQueued:
			// Still mid-turn (or not yet picked up by the worker) — reject
			// instead of racing a second conversation/sandbox into existence
			// for the same chat session.
			return nil, agentdom.ErrConversationBusy
		case agentdom.ConversationStatusPaused:
			// Resume — claim the conversation atomically so two concurrent
			// replies can't both win and double-publish a resume trigger for
			// the same conversation_id. The loser is told to retry as busy
			// rather than silently racing ai-agent's sandbox reattachment.
			claimed, err := s.repo.ClaimConversationStatus(ctx, latest.ID,
				string(agentdom.ConversationStatusPaused), string(agentdom.ConversationStatusRunning))
			if err != nil {
				return nil, err
			}
			if !claimed {
				return nil, agentdom.ErrConversationBusy
			}
		case agentdom.ConversationStatusFinished, agentdom.ConversationStatusFailed, agentdom.ConversationStatusStopped:
			agent, err := s.repo.FindAgentByID(ctx, session.AgentID)
			if err != nil {
				return nil, err
			}
			if agent.AgentType == agentdom.AgentTypeACP {
				// Resume — same atomic-claim treatment as the paused case
				// above, just starting from a terminal status instead of
				// "paused" (ACP conversations never reach "paused" — see the
				// doc comment above).
				claimed, err := s.repo.ClaimConversationStatus(ctx, latest.ID,
					latest.Status, string(agentdom.ConversationStatusRunning))
				if err != nil {
					return nil, err
				}
				if !claimed {
					return nil, agentdom.ErrConversationBusy
				}
			} else {
				// Terminal status — fall through to create a new conversation.
				conv = nil
			}
		}
	}

	if conv == nil {
		conv, err = s.createConversation(ctx, projectID, session.AgentID, &memberID, agentdom.AgentConversation{
			TriggerType:   "chat_message",
			ChatSessionID: &sessionID,
		})
		if err != nil {
			return nil, err
		}
	}
	// else: resume — reuse the same conversation_id so ai-agent reattaches
	// to the sandbox it kept alive rather than cold-starting a new one.

	if err := s.publishChatTrigger(ctx, session.AgentID, conv.ID, sessionID, projectID, memberID, message, s.gatherRepoPluginIDs(ctx)); err != nil {
		return nil, err
	}

	// Update last_message_at
	now := time.Now()
	session.LastMessageAt = &now
	_ = s.repo.UpdateChatSession(ctx, session)

	return conv, nil
}

// -------------------------------------------------------------------------
// Global Chat Sessions — siblings of the Chat Sessions methods above,
// keyed by actor_user_id instead of a project's memberID.
// -------------------------------------------------------------------------

// ListGlobalChatSessions returns all global chat sessions for the given
// agent and human actor.
func (s *Service) ListGlobalChatSessions(ctx context.Context, agentID, actorUserID uuid.UUID) ([]*agentdom.AgentChatSession, error) {
	return s.repo.ListGlobalChatSessions(ctx, agentID, actorUserID)
}

// StartGlobalChatSession creates a new global chat session and publishes
// the initial message trigger.
func (s *Service) StartGlobalChatSession(ctx context.Context, agentID, actorUserID uuid.UUID, message string) (*agentdom.AgentChatSession, *agentdom.AgentConversation, error) {
	now := time.Now()

	session := &agentdom.AgentChatSession{
		ID:            uuid.New(),
		AgentID:       agentID,
		ActorUserID:   &actorUserID,
		LastMessageAt: &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.CreateChatSession(ctx, session); err != nil {
		return nil, nil, err
	}

	conv, err := s.createGlobalConversation(ctx, agentID, actorUserID, agentdom.AgentConversation{
		TriggerType:   "chat_message",
		ChatSessionID: &session.ID,
	})
	if err != nil {
		return nil, nil, err
	}

	if err := s.publishGlobalChatTrigger(ctx, agentID, conv.ID, session.ID, actorUserID, message); err != nil {
		return nil, nil, err
	}

	return session, conv, nil
}

// SendGlobalChatMessage sends a message to an existing global chat session
// and publishes the trigger. Mirrors SendChatMessage's resume/terminal
// handling — see its doc comment for the pause/resume rationale.
func (s *Service) SendGlobalChatMessage(ctx context.Context, sessionID, actorUserID uuid.UUID, message string) (*agentdom.AgentConversation, error) {
	session, err := s.repo.FindChatSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.ProjectID != uuid.Nil || session.ActorUserID == nil || *session.ActorUserID != actorUserID {
		return nil, agentdom.ErrChatSessionNotFound
	}

	latest, err := s.repo.FindLatestConversationByChatSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	conv := latest
	if latest != nil {
		switch agentdom.ConversationStatus(latest.Status) {
		case agentdom.ConversationStatusRunning, agentdom.ConversationStatusQueued:
			return nil, agentdom.ErrConversationBusy
		case agentdom.ConversationStatusPaused:
			claimed, err := s.repo.ClaimConversationStatus(ctx, latest.ID,
				string(agentdom.ConversationStatusPaused), string(agentdom.ConversationStatusRunning))
			if err != nil {
				return nil, err
			}
			if !claimed {
				return nil, agentdom.ErrConversationBusy
			}
		case agentdom.ConversationStatusFinished, agentdom.ConversationStatusFailed, agentdom.ConversationStatusStopped:
			agent, err := s.repo.FindAgentByID(ctx, session.AgentID)
			if err != nil {
				return nil, err
			}
			if agent.AgentType == agentdom.AgentTypeACP {
				claimed, err := s.repo.ClaimConversationStatus(ctx, latest.ID,
					latest.Status, string(agentdom.ConversationStatusRunning))
				if err != nil {
					return nil, err
				}
				if !claimed {
					return nil, agentdom.ErrConversationBusy
				}
			} else {
				conv = nil
			}
		}
	}

	if conv == nil {
		conv, err = s.createGlobalConversation(ctx, session.AgentID, actorUserID, agentdom.AgentConversation{
			TriggerType:   "chat_message",
			ChatSessionID: &sessionID,
		})
		if err != nil {
			return nil, err
		}
	}
	// else: resume — reuse the same conversation_id so ai-agent reattaches
	// to the sandbox it kept alive rather than cold-starting a new one.

	if err := s.publishGlobalChatTrigger(ctx, session.AgentID, conv.ID, sessionID, actorUserID, message); err != nil {
		return nil, err
	}

	now := time.Now()
	session.LastMessageAt = &now
	_ = s.repo.UpdateChatSession(ctx, session)

	return conv, nil
}

// ListChatMessages returns conversation events for a chat session.
func (s *Service) ListChatMessages(ctx context.Context, sessionID uuid.UUID, offset, limit int) ([]*agentdom.AgentConversationEvent, int64, error) {
	// TODO: We'd need to aggregate events from all conversations in this session.
	// For now, return events from the most recent conversation with this session_id.
	filter := agentdom.ListConversationsFilter{}
	_ = sessionID
	convs, _, err := s.repo.ListConversations(ctx, filter, 1)
	if err != nil {
		return nil, 0, err
	}
	if len(convs) == 0 {
		return []*agentdom.AgentConversationEvent{}, 0, nil
	}
	return s.repo.ListConversationEvents(ctx, convs[0].ID, offset, limit)
}

// -------------------------------------------------------------------------
// Internal helpers
// -------------------------------------------------------------------------

func (s *Service) createConversation(ctx context.Context, projectID, agentID uuid.UUID, memberID *uuid.UUID, template agentdom.AgentConversation) (*agentdom.AgentConversation, error) {
	now := time.Now()
	conv := &agentdom.AgentConversation{
		ID:                  uuid.New(),
		AgentID:             agentID,
		ProjectID:           projectID,
		TriggerType:         template.TriggerType,
		TaskID:              template.TaskID,
		CommentID:           template.CommentID,
		ChatSessionID:       template.ChatSessionID,
		TriggeredByMemberID: memberID,
		Status:              string(agentdom.ConversationStatusQueued),
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.repo.CreateConversation(ctx, conv); err != nil {
		return nil, err
	}
	return conv, nil
}

// createGlobalConversation is createConversation's global-chat sibling:
// there is no project_id and the actor is a user directly rather than a
// resolved project_members.id.
func (s *Service) createGlobalConversation(ctx context.Context, agentID, actorUserID uuid.UUID, template agentdom.AgentConversation) (*agentdom.AgentConversation, error) {
	now := time.Now()
	conv := &agentdom.AgentConversation{
		ID:            uuid.New(),
		AgentID:       agentID,
		TriggerType:   template.TriggerType,
		ChatSessionID: template.ChatSessionID,
		ActorUserID:   &actorUserID,
		Status:        string(agentdom.ConversationStatusQueued),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.CreateConversation(ctx, conv); err != nil {
		return nil, err
	}
	return conv, nil
}

// gatherRepoPlugins returns all installed plugins with the "repository" capability.
func (s *Service) gatherRepoPlugins(ctx context.Context) []*plugindom.Plugin {
	if s.pluginRepo == nil {
		return nil
	}
	plugins, err := s.pluginRepo.FindByCapability(ctx, "repository")
	if err != nil {
		return nil
	}
	return plugins
}

// gatherRepoPluginIDs returns the string Names (e.g. "com.paca.github") of all
// installed plugins with the "repository" capability. These are the identifiers
// used in plugin API paths and published to the agent trigger stream.
func (s *Service) gatherRepoPluginIDs(ctx context.Context) []string {
	names := []string{}
	for _, p := range s.gatherRepoPlugins(ctx) {
		names = append(names, p.Name)
	}
	return names
}

// TriggerTaskAssigned creates a conversation and publishes the trigger event
// when a task is assigned to an agent member. note, when non-empty, is
// prepended to the agent's initial prompt as trigger.message — used by the
// automation-workflow engine to tell the agent which status closes out its
// step (e.g. "set the status to 'Done' when you finish"). triggeredByMemberID
// is nil when the assignment came from the automation-workflow engine rather
// than a human member.
func (s *Service) TriggerTaskAssigned(ctx context.Context, projectID, agentID, taskID uuid.UUID, triggeredByMemberID *uuid.UUID, note string) (*agentdom.AgentConversation, error) {
	repoPlugins := s.gatherRepoPlugins(ctx)
	repoPluginIDs := make([]string, 0, len(repoPlugins))
	for _, p := range repoPlugins {
		repoPluginIDs = append(repoPluginIDs, p.Name)
	}

	var repoPluginID *uuid.UUID
	if len(repoPlugins) > 0 {
		id := repoPlugins[0].ID
		repoPluginID = &id
	}

	conv, err := s.createConversation(ctx, projectID, agentID, triggeredByMemberID, agentdom.AgentConversation{
		TriggerType:  "task_assigned",
		TaskID:       &taskID,
		RepoPluginID: repoPluginID,
	})
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"conversation_id": conv.ID.String(),
		"project_id":      projectID.String(),
		"agent_id":        agentID.String(),
		"task_id":         taskID.String(),
		"trigger_type":    "task_assigned",
		"message":         note,
		"repo_plugin_ids": strings.Join(repoPluginIDs, ","),
	}
	if triggeredByMemberID != nil {
		payload["actor_member_id"] = triggeredByMemberID.String()
	}
	_ = s.publishTrigger(ctx, events.TopicAgentTaskAssigned, payload)
	return conv, nil
}

// TriggerDirectMessage fires message straight at agentID, with no task
// involved at all — used by the automation engine's trigger_ai_agent action
// when its trigger has no target task (cron/api_trigger/predecessor_done
// with no target_task_id configured, so there's nothing to assign, unlike
// TriggerTaskAssigned). triggeredByMemberID is nil, same as
// TriggerTaskAssigned's automation-triggered case — there's no human actor
// behind an automation firing.
func (s *Service) TriggerDirectMessage(ctx context.Context, projectID, agentID uuid.UUID, triggeredByMemberID *uuid.UUID, message string) (*agentdom.AgentConversation, error) {
	repoPlugins := s.gatherRepoPlugins(ctx)
	repoPluginIDs := make([]string, 0, len(repoPlugins))
	for _, p := range repoPlugins {
		repoPluginIDs = append(repoPluginIDs, p.Name)
	}

	var repoPluginID *uuid.UUID
	if len(repoPlugins) > 0 {
		id := repoPlugins[0].ID
		repoPluginID = &id
	}

	conv, err := s.createConversation(ctx, projectID, agentID, triggeredByMemberID, agentdom.AgentConversation{
		TriggerType:  "automation_message",
		RepoPluginID: repoPluginID,
	})
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"conversation_id": conv.ID.String(),
		"project_id":      projectID.String(),
		"agent_id":        agentID.String(),
		"trigger_type":    "automation_message",
		"message":         message,
		"repo_plugin_ids": strings.Join(repoPluginIDs, ","),
	}
	if triggeredByMemberID != nil {
		payload["actor_member_id"] = triggeredByMemberID.String()
	}
	_ = s.publishTrigger(ctx, events.TopicAgentAutomationMessage, payload)
	return conv, nil
}

// TriggerCommentMention creates a conversation and publishes a comment-mention trigger.
// message is the plain-text content of the comment so the agent's initial prompt
// is populated without requiring a separate MCP call.
func (s *Service) TriggerCommentMention(ctx context.Context, projectID, agentID, taskID, commentID, triggeredByMemberID uuid.UUID, message string) (*agentdom.AgentConversation, error) {
	repoPlugins := s.gatherRepoPlugins(ctx)
	repoPluginIDs := make([]string, 0, len(repoPlugins))
	for _, p := range repoPlugins {
		repoPluginIDs = append(repoPluginIDs, p.Name)
	}

	var repoPluginID *uuid.UUID
	if len(repoPlugins) > 0 {
		id := repoPlugins[0].ID
		repoPluginID = &id
	}

	conv, err := s.createConversation(ctx, projectID, agentID, &triggeredByMemberID, agentdom.AgentConversation{
		TriggerType:  "comment_mention",
		TaskID:       &taskID,
		CommentID:    &commentID,
		RepoPluginID: repoPluginID,
	})
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"conversation_id": conv.ID.String(),
		"project_id":      projectID.String(),
		"agent_id":        agentID.String(),
		"task_id":         taskID.String(),
		"comment_id":      commentID.String(),
		"actor_member_id": triggeredByMemberID.String(),
		"trigger_type":    "comment_mention",
		"message":         message,
		"repo_plugin_ids": strings.Join(repoPluginIDs, ","),
	}
	_ = s.publishTrigger(ctx, events.TopicAgentCommentMention, payload)
	return conv, nil
}

// TriggerDescriptionWrite creates a conversation and publishes a trigger for
// the agent to write a description for the given task.
func (s *Service) TriggerDescriptionWrite(ctx context.Context, projectID, agentID, taskID, triggeredByMemberID uuid.UUID) (*agentdom.AgentConversation, error) {
	repoPlugins := s.gatherRepoPlugins(ctx)
	repoPluginIDs := make([]string, 0, len(repoPlugins))
	for _, p := range repoPlugins {
		repoPluginIDs = append(repoPluginIDs, p.Name)
	}

	var repoPluginID *uuid.UUID
	if len(repoPlugins) > 0 {
		id := repoPlugins[0].ID
		repoPluginID = &id
	}

	conv, err := s.createConversation(ctx, projectID, agentID, &triggeredByMemberID, agentdom.AgentConversation{
		TriggerType:  "description_write",
		TaskID:       &taskID,
		RepoPluginID: repoPluginID,
	})
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"conversation_id": conv.ID.String(),
		"project_id":      projectID.String(),
		"agent_id":        agentID.String(),
		"task_id":         taskID.String(),
		"actor_member_id": triggeredByMemberID.String(),
		"trigger_type":    "description_write",
		"message":         "Please write a clear and detailed description for this task.",
		"repo_plugin_ids": strings.Join(repoPluginIDs, ","),
	}
	_ = s.publishTrigger(ctx, events.TopicAgentDescriptionWrite, payload)
	return conv, nil
}

func (s *Service) publishTrigger(ctx context.Context, topic string, payload map[string]any) error {
	if s.publisher == nil {
		return nil
	}
	// Write flat fields so services/ai-agent can read them without JSON decoding.
	// The trigger_type is embedded in the payload; the stream entry type field
	// mirrors it for routing convenience.
	payload["type"] = topic
	return s.publisher.AppendFlat(ctx, events.StreamAgentTriggers, payload)
}

func (s *Service) publishChatTrigger(ctx context.Context, agentID, convID, sessionID, projectID, memberID uuid.UUID, message string, repoPluginIDs []string) error {
	payload := map[string]any{
		"conversation_id": convID.String(),
		"project_id":      projectID.String(),
		"agent_id":        agentID.String(),
		"chat_session_id": sessionID.String(),
		"actor_member_id": memberID.String(),
		"trigger_type":    "chat_message",
		"message":         message,
		"repo_plugin_ids": strings.Join(repoPluginIDs, ","),
	}
	return s.publishTrigger(ctx, events.TopicAgentChatMessage, payload)
}

// publishGlobalChatTrigger is publishChatTrigger's global-chat sibling — no
// project_id, actor identified by actor_user_id, and repo_plugin_ids
// omitted entirely (repo/PR tools are excluded from global-chat
// conversations; see the Global Conversations section's doc comment).
func (s *Service) publishGlobalChatTrigger(ctx context.Context, agentID, convID, sessionID, actorUserID uuid.UUID, message string) error {
	payload := map[string]any{
		"conversation_id": convID.String(),
		"agent_id":        agentID.String(),
		"chat_session_id": sessionID.String(),
		"actor_user_id":   actorUserID.String(),
		"trigger_type":    "chat_message",
		"message":         message,
	}
	return s.publishTrigger(ctx, events.TopicAgentChatMessage, payload)
}
