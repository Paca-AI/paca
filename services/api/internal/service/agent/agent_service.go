// Package agentsvc implements the AI Agent application service.
package agentsvc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
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
	avatarSvc  attachmentdom.AvatarService
	// environmentSvc resolves/validates static environments — used by
	// CreateAgent/UpdateAgent to validate default_environment_id (see
	// validateDefaultEnvironment) and by StartChatSession/
	// StartGlobalChatSession to resolve which environment+folder a new
	// conversation attaches to (see ResolveConversationWorkdir). Nil is a
	// valid, supported configuration (mirrors avatarSvc/encryptor above) —
	// every call site guards against it and behaves as if environments
	// don't exist yet, rather than panicking.
	environmentSvc environmentdom.Service
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

// WithAvatarService configures avatar upload support.
func (s *Service) WithAvatarService(svc attachmentdom.AvatarService) *Service {
	s.avatarSvc = svc
	return s
}

// WithEnvironmentService wires in the static-environment service — see the
// environmentSvc field's doc comment for what it's used for.
func (s *Service) WithEnvironmentService(svc environmentdom.Service) *Service {
	s.environmentSvc = svc
	return s
}

// validateDefaultEnvironment resolves and validates a candidate
// default_environment_id for an agent being created/updated in projectID:
// uuid.Nil clears it (returns nil, nil — see UpdateAgentInput.
// DefaultEnvironmentID's doc comment for why uuid.Nil, not a bare nil
// pointer, means "clear"); any other value must resolve to a real
// environment belonging to projectID, and the agent must be project-scoped
// (a global agent has no single project's environments to default to —
// see Agent.DefaultEnvironmentID's doc comment).
func (s *Service) validateDefaultEnvironment(ctx context.Context, projectID uuid.UUID, candidate uuid.UUID, scope agentdom.AgentScope) (*uuid.UUID, error) {
	if candidate == uuid.Nil {
		return nil, nil
	}
	if scope == agentdom.AgentScopeGlobal {
		return nil, agentdom.ErrDefaultEnvironmentInvalid
	}
	if s.environmentSvc == nil {
		return nil, agentdom.ErrDefaultEnvironmentInvalid
	}
	if _, err := s.environmentSvc.GetEnvironment(ctx, projectID, candidate); err != nil {
		return nil, agentdom.ErrDefaultEnvironmentInvalid
	}
	id := candidate
	return &id, nil
}

// validateDefaultFolder resolves and validates a candidate
// default_folder_id for an agent being created/updated in projectID:
// uuid.Nil clears it (same convention validateDefaultEnvironment uses);
// any other value must belong to resolvedEnvID — the agent's own
// (already-resolved, by validateDefaultEnvironment, earlier in the same
// call) default environment, since a default folder is meaningless without
// one to scope it to (see Agent.DefaultFolderID's doc comment) — and the
// agent must be project-scoped. GetEnvironment already returns Folders
// populated (see environmentdom.EnvironmentService.GetEnvironment's doc
// comment), so this needs no separate folder lookup.
func (s *Service) validateDefaultFolder(ctx context.Context, projectID uuid.UUID, candidate uuid.UUID, resolvedEnvID *uuid.UUID, scope agentdom.AgentScope) (*uuid.UUID, error) {
	if candidate == uuid.Nil {
		return nil, nil
	}
	if scope == agentdom.AgentScopeGlobal {
		return nil, agentdom.ErrDefaultFolderInvalid
	}
	if resolvedEnvID == nil {
		return nil, agentdom.ErrDefaultFolderInvalid
	}
	if s.environmentSvc == nil {
		return nil, agentdom.ErrDefaultFolderInvalid
	}
	env, err := s.environmentSvc.GetEnvironment(ctx, projectID, *resolvedEnvID)
	if err != nil {
		return nil, agentdom.ErrDefaultFolderInvalid
	}
	for _, f := range env.Folders {
		if f.ID == candidate {
			id := candidate
			return &id, nil
		}
	}
	return nil, agentdom.ErrDefaultFolderInvalid
}

// uuidPtrEqual reports whether a and b are both nil, or both non-nil and
// equal — UpdateAgent's own way of detecting whether
// validateDefaultEnvironment actually changed a.DefaultEnvironmentID
// (rather than re-resolving to the same value it already had) without
// duplicating a nil-then-dereference check inline at each call site.
func uuidPtrEqual(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
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
	if agentType != agentdom.AgentTypeLLM && agentType != agentdom.AgentTypeACP && agentType != agentdom.AgentTypeProviderCLI {
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

	switch agentType {
	case agentdom.AgentTypeACP:
		if !agentdom.ValidACPProviders[in.ACPProvider] {
			return nil, agentdom.ErrACPProviderInvalid
		}
		if in.ACPProvider == agentdom.ACPProviderCustom && len(in.ACPCommand) == 0 {
			return nil, agentdom.ErrACPCommandRequired
		}
		provider := in.ACPProvider
		a.ACPProvider = &provider
		a.ACPCommand = in.ACPCommand
	case agentdom.AgentTypeProviderCLI:
		if !agentdom.ValidCLIProviders[in.CLIProvider] {
			return nil, agentdom.ErrCLIProviderInvalid
		}
		authMode := in.CLIAuthMode
		if authMode == "" {
			authMode = agentdom.CLIAuthModeLogin
		}
		if authMode != agentdom.CLIAuthModeAPIKey && authMode != agentdom.CLIAuthModeLogin {
			return nil, agentdom.ErrCLIAuthModeInvalid
		}
		if authMode == agentdom.CLIAuthModeAPIKey && !agentdom.CLIProvidersWithAPIKeyAuth[in.CLIProvider] {
			return nil, agentdom.ErrCLIProviderNoAPIKeyAuth
		}
		provider := in.CLIProvider
		a.CLIProvider = &provider
		a.CLIModel = in.CLIModel
		a.CLIAuthMode = authMode
		if in.CLIAPIKey != "" {
			encryptedKey, err := s.encryptKey(in.CLIAPIKey)
			if err != nil {
				return nil, fmt.Errorf("encrypt CLI API key: %w", err)
			}
			a.CLIAPIKeySecret = encryptedKey
		}
		// System prompt and git committer identity are meaningless here too
		// (same reasoning as the ACP case below) — the underlying CLI owns
		// its own persona/system-prompt mechanism and its own git identity.
	default:
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
		a.DockerEnabled = in.DockerEnabled
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

	if in.DefaultEnvironmentID != nil {
		envID, err := s.validateDefaultEnvironment(ctx, projectID, *in.DefaultEnvironmentID, agentdom.AgentScopeProject)
		if err != nil {
			return nil, err
		}
		a.DefaultEnvironmentID = envID
	}
	if in.DefaultFolderID != nil {
		folderID, err := s.validateDefaultFolder(ctx, projectID, *in.DefaultFolderID, a.DefaultEnvironmentID, agentdom.AgentScopeProject)
		if err != nil {
			return nil, err
		}
		a.DefaultFolderID = folderID
	}
	// provider_cli agents never fall back to an ephemeral sandbox — their
	// CLI's login state must persist across conversations, which only a
	// static environment's volume provides (see Agent.DefaultEnvironmentID's
	// doc comment). Checked after resolution above so an *invalid*
	// environment ID still surfaces the more specific ErrDefaultEnvironmentInvalid.
	if agentType == agentdom.AgentTypeProviderCLI && a.DefaultEnvironmentID == nil {
		return nil, agentdom.ErrDefaultEnvironmentRequiredForCLIProvider
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
	// LLM/ACP/provider_cli fields are guarded by the agent's existing
	// (immutable) type — agent_type can't be changed through this API, so
	// applying another shape's fields would only ever leave stale/wrong
	// data on the agent (e.g. an encrypted LLM API key sitting unused on an
	// ACP agent). A request that happens to include more than one type's
	// fields (e.g. a generic client payload) silently has the irrelevant
	// ones ignored rather than erroring, matching CreateAgent's per-type
	// field selection. Anything other than the explicit ACP/provider_cli
	// types is treated as LLM (its default, as in CreateAgent) so an agent
	// loaded with an unset AgentType isn't silently locked out of updating
	// its LLM fields. SystemPrompt and the git committer identity fields
	// ride along in the LLM block — like the LLM fields, they're
	// meaningless on an ACP or provider_cli agent (see the doc comment on
	// Agent.SystemPrompt), so a request that sets them on one is silently
	// ignored too.
	if a.AgentType == agentdom.AgentTypeLLM || a.AgentType == "" {
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
		if in.DockerEnabled != nil {
			a.DockerEnabled = *in.DockerEnabled
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
	if a.AgentType == agentdom.AgentTypeProviderCLI {
		if in.CLIProvider != nil {
			if !agentdom.ValidCLIProviders[*in.CLIProvider] {
				return nil, agentdom.ErrCLIProviderInvalid
			}
			a.CLIProvider = in.CLIProvider
		}
		if in.CLIModel != nil {
			a.CLIModel = *in.CLIModel
		}
		if in.CLIAuthMode != nil {
			if *in.CLIAuthMode != agentdom.CLIAuthModeAPIKey && *in.CLIAuthMode != agentdom.CLIAuthModeLogin {
				return nil, agentdom.ErrCLIAuthModeInvalid
			}
			a.CLIAuthMode = *in.CLIAuthMode
		}
		if a.CLIAuthMode == agentdom.CLIAuthModeAPIKey && a.CLIProvider != nil && !agentdom.CLIProvidersWithAPIKeyAuth[*a.CLIProvider] {
			return nil, agentdom.ErrCLIProviderNoAPIKeyAuth
		}
		if in.CLIAPIKey != nil {
			encryptedKey, err := s.encryptKey(*in.CLIAPIKey)
			if err != nil {
				return nil, fmt.Errorf("encrypt CLI API key: %w", err)
			}
			a.CLIAPIKeySecret = encryptedKey
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
	if in.DefaultEnvironmentID != nil {
		envID, err := s.validateDefaultEnvironment(ctx, projectID, *in.DefaultEnvironmentID, a.AgentScope)
		if err != nil {
			return nil, err
		}
		envChanged := !uuidPtrEqual(a.DefaultEnvironmentID, envID)
		a.DefaultEnvironmentID = envID
		// The agent's existing default folder belongs to the OLD
		// environment — if the environment just changed and this same
		// request didn't also specify a new default_folder_id (handled
		// below, which would overwrite this), the stale folder reference
		// can never be valid again, so it's cleared here rather than left
		// dangling for validateDefaultFolder to reject on every future
		// update until someone notices.
		if envChanged && in.DefaultFolderID == nil {
			a.DefaultFolderID = nil
		}
	}
	if in.DefaultFolderID != nil {
		folderID, err := s.validateDefaultFolder(ctx, projectID, *in.DefaultFolderID, a.DefaultEnvironmentID, a.AgentScope)
		if err != nil {
			return nil, err
		}
		a.DefaultFolderID = folderID
	}
	// Same "never falls back to ephemeral" guarantee as CreateAgent — also
	// catches an update that tries to CLEAR default_environment_id (via
	// DefaultEnvironmentID: &uuid.Nil) on an existing provider_cli agent.
	if a.AgentType == agentdom.AgentTypeProviderCLI && a.DefaultEnvironmentID == nil {
		return nil, agentdom.ErrDefaultEnvironmentRequiredForCLIProvider
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
	// provider_cli is rejected explicitly (a clearer error than falling
	// through to the generic type-invalid one) — a global agent has no
	// single project's environments to default to, and provider_cli
	// requires one (see Agent.DefaultEnvironmentID's doc comment).
	if agentType == agentdom.AgentTypeProviderCLI {
		return nil, agentdom.ErrCLIProviderNotSupportedForGlobalAgents
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
		a.DockerEnabled = in.DockerEnabled
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
		if in.DockerEnabled != nil {
			a.DockerEnabled = *in.DockerEnabled
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

// VerifyCLILogin probes whether a provider_cli agent's underlying CLI is
// currently authenticated inside its default environment (each CLI's own
// real status subcommand where one is confirmed to exist, a file-existence
// guess only as a last resort — see environmentdom.Service.VerifyCLIAuth's
// doc comment), and, on success, persists the verification timestamp via
// SetCLILoginVerifiedAt. Returns ErrAgentNotProviderCLI for any other
// agent_type, and ErrDefaultEnvironmentRequiredForCLIProvider if somehow
// called on a provider_cli agent with no default environment (shouldn't
// happen — CreateAgent/UpdateAgent both enforce one — but checked
// defensively rather than assumed).
func (s *Service) VerifyCLILogin(ctx context.Context, projectID, agentID uuid.UUID) (bool, error) {
	a, err := s.GetAgent(ctx, projectID, agentID)
	if err != nil {
		return false, err
	}
	if a.AgentType != agentdom.AgentTypeProviderCLI {
		return false, agentdom.ErrAgentNotProviderCLI
	}
	if a.DefaultEnvironmentID == nil || a.CLIProvider == nil {
		return false, agentdom.ErrDefaultEnvironmentRequiredForCLIProvider
	}
	if s.environmentSvc == nil {
		return false, fmt.Errorf("environment service not configured")
	}
	authenticated, err := s.environmentSvc.VerifyCLIAuth(ctx, projectID, *a.DefaultEnvironmentID, *a.CLIProvider)
	if err != nil {
		return false, err
	}
	if authenticated {
		if err := s.repo.SetCLILoginVerifiedAt(ctx, agentID, time.Now()); err != nil {
			return false, err
		}
	}
	return authenticated, nil
}

// ErrAvatarServiceRequired indicates a missing AvatarService dependency when
// an avatar-upload path is invoked.
var ErrAvatarServiceRequired = errors.New("agent svc: avatar service required")

// InitiateAvatarUpload starts an avatar upload for a project-scoped agent.
func (s *Service) InitiateAvatarUpload(ctx context.Context, projectID, agentID uuid.UUID, fileName, contentType string, fileSize int64, uploadedBy uuid.UUID) (*attachmentdom.UploadSession, error) {
	if s.avatarSvc == nil {
		return nil, ErrAvatarServiceRequired
	}
	if _, err := s.GetAgent(ctx, projectID, agentID); err != nil {
		return nil, err
	}
	return s.avatarSvc.InitiateAvatarUpload(ctx, attachmentdom.AvatarUploadInput{
		OwnerKind:   attachmentdom.AvatarOwnerAgent,
		OwnerID:     agentID,
		FileName:    fileName,
		ContentType: contentType,
		FileSize:    fileSize,
		UploadedBy:  uploadedBy,
	})
}

// CompleteAvatarUpload finishes an avatar upload for a project-scoped agent.
func (s *Service) CompleteAvatarUpload(ctx context.Context, projectID, agentID, fileID uuid.UUID) (*agentdom.Agent, error) {
	a, err := s.GetAgent(ctx, projectID, agentID)
	if err != nil {
		return nil, err
	}
	return s.completeAvatarUpload(ctx, a, fileID)
}

// RemoveAvatar clears a project-scoped agent's avatar.
func (s *Service) RemoveAvatar(ctx context.Context, projectID, agentID uuid.UUID) (*agentdom.Agent, error) {
	a, err := s.GetAgent(ctx, projectID, agentID)
	if err != nil {
		return nil, err
	}
	return s.removeAvatar(ctx, a)
}

// InitiateGlobalAvatarUpload is InitiateAvatarUpload's global-agent sibling.
func (s *Service) InitiateGlobalAvatarUpload(ctx context.Context, agentID uuid.UUID, fileName, contentType string, fileSize int64, uploadedBy uuid.UUID) (*attachmentdom.UploadSession, error) {
	if s.avatarSvc == nil {
		return nil, ErrAvatarServiceRequired
	}
	if _, err := s.GetGlobalAgent(ctx, agentID); err != nil {
		return nil, err
	}
	return s.avatarSvc.InitiateAvatarUpload(ctx, attachmentdom.AvatarUploadInput{
		OwnerKind:   attachmentdom.AvatarOwnerAgent,
		OwnerID:     agentID,
		FileName:    fileName,
		ContentType: contentType,
		FileSize:    fileSize,
		UploadedBy:  uploadedBy,
	})
}

// CompleteGlobalAvatarUpload is CompleteAvatarUpload's global-agent sibling.
func (s *Service) CompleteGlobalAvatarUpload(ctx context.Context, agentID, fileID uuid.UUID) (*agentdom.Agent, error) {
	a, err := s.GetGlobalAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return s.completeAvatarUpload(ctx, a, fileID)
}

// RemoveGlobalAvatar is RemoveAvatar's global-agent sibling.
func (s *Service) RemoveGlobalAvatar(ctx context.Context, agentID uuid.UUID) (*agentdom.Agent, error) {
	a, err := s.GetGlobalAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return s.removeAvatar(ctx, a)
}

// completeAvatarUpload is the shared tail of CompleteAvatarUpload and
// CompleteGlobalAvatarUpload once the agent has been loaded and its scope
// verified by the caller.
func (s *Service) completeAvatarUpload(ctx context.Context, a *agentdom.Agent, fileID uuid.UUID) (*agentdom.Agent, error) {
	if s.avatarSvc == nil {
		return nil, ErrAvatarServiceRequired
	}
	keys, err := s.avatarSvc.CompleteAvatarUpload(ctx, attachmentdom.AvatarCompleteInput{
		OwnerKind: attachmentdom.AvatarOwnerAgent,
		OwnerID:   a.ID,
		FileID:    fileID,
	})
	if err != nil {
		return nil, err
	}

	oldKey, oldThumbKey := a.AvatarKey, a.AvatarThumbKey
	a.AvatarKey = &keys.Key
	a.AvatarThumbKey = &keys.ThumbKey
	if err := s.repo.UpdateAgent(ctx, a); err != nil {
		return nil, err
	}

	s.avatarSvc.DeleteAvatarObjects(ctx, oldKey, oldThumbKey)
	return a, nil
}

// removeAvatar is the shared tail of RemoveAvatar and RemoveGlobalAvatar
// once the agent has been loaded and its scope verified by the caller.
func (s *Service) removeAvatar(ctx context.Context, a *agentdom.Agent) (*agentdom.Agent, error) {
	if s.avatarSvc == nil {
		return nil, ErrAvatarServiceRequired
	}
	oldKey, oldThumbKey := a.AvatarKey, a.AvatarThumbKey
	if oldKey == nil && oldThumbKey == nil {
		return a, nil
	}
	a.AvatarKey = nil
	a.AvatarThumbKey = nil
	if err := s.repo.UpdateAgent(ctx, a); err != nil {
		return nil, err
	}

	s.avatarSvc.DeleteAvatarObjects(ctx, oldKey, oldThumbKey)
	return a, nil
}

// requireGooseManagedAgent rejects MCP server / skill / environment
// variable mutations targeting an ACP-type agent (renamed from
// requireNonACPAgent — the name now reflects what it actually permits, not
// just what it excludes). ACP agents run entirely in the user's own local
// CLI via paca-acp-bridge; services/ai-agent's acp_dispatch.py never reads
// any of these tables when dispatching an ACP turn, so accepting the write
// here would silently no-op rather than have any effect — better to reject
// it outright.
//
// llm and provider_cli agents both pass this check, deliberately: an llm
// agent's skills/MCP servers are read by Goose's own native discovery;
// a provider_cli agent's are instead synced into the underlying CLI's own
// config files on every conversation attach (see
// docs/ai-agent/overview.md's provider_cli section) — Paca-side storage and
// the create/update/delete API are identical for both types, only the
// *consumer* of that configuration differs at execution time.
//
// Read (List*) operations are left permissive for every type since
// returning an empty list is harmless.
func (s *Service) requireGooseManagedAgent(ctx context.Context, agentID uuid.UUID) error {
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
	if err := s.requireGooseManagedAgent(ctx, agentID); err != nil {
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
	if err := s.requireGooseManagedAgent(ctx, agentID); err != nil {
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
	if err := s.requireGooseManagedAgent(ctx, agentID); err != nil {
		return err
	}
	return s.repo.DeleteMCPServer(ctx, serverID)
}

// -------------------------------------------------------------------------
// Skills
// -------------------------------------------------------------------------

// validateSkillName rejects a skill name that would let the on-disk
// SKILL.md path built from it — executor/skills.go's buildSkillsTar
// (skillsRelDir + "/" + name + "/SKILL.md") on the agent-runner side, and
// providercli's claude_code.go SyncFiles (.claude/skills/<name>/SKILL.md)
// for a provider_cli agent — escape the skills directory it's meant to
// land in. Neither writer sanitizes or validates name itself (see their
// own doc comments), so this is the one place in the stack that does.
func validateSkillName(name string) error {
	if agentdom.IsReservedSkillName(name) {
		return agentdom.ErrSkillNameReserved
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return agentdom.ErrSkillNameInvalid
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
	if err := s.requireGooseManagedAgent(ctx, agentID); err != nil {
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
	if err := s.requireGooseManagedAgent(ctx, agentID); err != nil {
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
	if err := s.requireGooseManagedAgent(ctx, agentID); err != nil {
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
	if err := s.requireGooseManagedAgent(ctx, agentID); err != nil {
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
	if err := s.requireGooseManagedAgent(ctx, agentID); err != nil {
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
	if err := s.requireGooseManagedAgent(ctx, agentID); err != nil {
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

// GetConversation returns a single conversation after verifying project
// ownership and, for owner-private conversations, chat-session ownership.
func (s *Service) GetConversation(ctx context.Context, projectID, conversationID, memberID uuid.UUID) (*agentdom.AgentConversation, error) {
	c, err := s.repo.FindConversationByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if c.ProjectID != projectID {
		return nil, agentdom.ErrConversationNotFound
	}
	if err := s.authorizeConversationAccess(ctx, c, memberID); err != nil {
		return nil, err
	}
	return c, nil
}

// GetConversationForAgent implements agentdom.Service.GetConversationForAgent
// — see its doc comment for the full authorization rule and why bare agent-
// identity matching isn't sufficient on its own.
func (s *Service) GetConversationForAgent(ctx context.Context, conversationID, callerAgentID, currentConversationID uuid.UUID) (*agentdom.AgentConversation, error) {
	target, err := s.repo.FindConversationByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if target.AgentID != callerAgentID {
		return nil, agentdom.ErrConversationNotFound
	}
	// Always allowed: an agent may read the conversation it's currently
	// running as part of. Also short-circuits the common case (no other
	// conversation was attached) without a second lookup.
	if target.ID == currentConversationID {
		return target, nil
	}

	// Anything else must be authorized against whichever human is driving
	// currentConversationID, not against callerAgentID alone — see
	// authorizeAgentConversationRead.
	current, err := s.repo.FindConversationByID(ctx, currentConversationID)
	if err != nil || current.AgentID != callerAgentID {
		// currentConversationID is missing, unverifiable, or (if ever
		// spoofed) doesn't even belong to this agent — there is no trusted
		// context to check the target against, so fail closed rather than
		// falling back to bare agent-identity matching.
		return nil, agentdom.ErrConversationNotFound
	}
	if err := s.authorizeAgentConversationRead(ctx, current, target); err != nil {
		return nil, err
	}
	return target, nil
}

// authorizeAgentConversationRead lets an agent read `target` on behalf of
// `current` — the conversation actually driving this MCP call — only when
// the human associated with `current` could already reach `target` by
// asking for it directly, mirroring authorizeConversationAccess (project-
// scoped) and GetGlobalConversation (global) exactly rather than
// re-deriving a separate, easier-to-get-wrong rule:
//   - global (current.ProjectID is nil): target must also be global and
//     share the same actor_user_id — GetGlobalConversation's own rule.
//   - project-scoped: target must be in the same project, and either
//     project_shared (visible to any project member already) or
//     owner-private to the same chat-session member current belongs to —
//     authorizeConversationAccess's rule, reused via current's own chat
//     session so a human never needs to be threaded through explicitly.
func (s *Service) authorizeAgentConversationRead(ctx context.Context, current, target *agentdom.AgentConversation) error {
	if current.ProjectID == uuid.Nil {
		if target.ProjectID != uuid.Nil ||
			current.ActorUserID == nil || target.ActorUserID == nil ||
			*target.ActorUserID != *current.ActorUserID {
			return agentdom.ErrConversationNotFound
		}
		return nil
	}

	if target.ProjectID != current.ProjectID {
		return agentdom.ErrConversationNotFound
	}
	if current.ChatSessionID == nil {
		// current isn't chat-session-backed (e.g. a task-assigned or
		// automation-triggered run) — there is no "human currently
		// chatting" to authorize target's owner-private audience against,
		// so only its already-project-wide-visible audience is reachable.
		return s.authorizeConversationAccess(ctx, target, uuid.Nil)
	}
	session, err := s.repo.FindChatSessionByID(ctx, *current.ChatSessionID)
	if err != nil {
		return agentdom.ErrConversationNotFound
	}
	return s.authorizeConversationAccess(ctx, target, session.MemberID)
}

// authorizeConversationAccess fails closed (ErrConversationNotFound) when a
// project-scoped owner-private conversation is not owned by memberID.
// project-shared conversations are readable by any project member, whose
// membership is already enforced by the router's project-scope middleware.
func (s *Service) authorizeConversationAccess(ctx context.Context, c *agentdom.AgentConversation, memberID uuid.UUID) error {
	if c.Audience != agentdom.AudienceOwnerPrivate {
		return nil
	}
	// A project-scoped owner-private conversation is always session-backed
	// (global chat has project_id IS NULL and never reaches this path), so the
	// owner is the chat session's member, not triggered_by_member_id (which a
	// pre-fix cross-member send could have pointed at a different member).
	if c.ChatSessionID == nil {
		return agentdom.ErrConversationNotFound
	}
	session, err := s.repo.FindChatSessionByID(ctx, *c.ChatSessionID)
	if err != nil || session.MemberID != memberID {
		return agentdom.ErrConversationNotFound
	}
	return nil
}

// ListConversationEvents returns one keyset-paginated window of events for a
// conversation (see agentdom.ConversationEventWindow), plus its total count.
func (s *Service) ListConversationEvents(ctx context.Context, conversationID uuid.UUID, window agentdom.ConversationEventWindow) ([]*agentdom.AgentConversationEvent, int64, error) {
	return s.repo.ListConversationEvents(ctx, conversationID, window)
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
func (s *Service) StopConversation(ctx context.Context, projectID, conversationID, memberID uuid.UUID) error {
	c, err := s.GetConversation(ctx, projectID, conversationID, memberID)
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
func (s *Service) PauseConversation(ctx context.Context, projectID, conversationID, memberID uuid.UUID) error {
	c, err := s.GetConversation(ctx, projectID, conversationID, memberID)
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
func (s *Service) Heartbeat(ctx context.Context, projectID, conversationID, memberID uuid.UUID) error {
	if _, err := s.GetConversation(ctx, projectID, conversationID, memberID); err != nil {
		return err
	}
	return s.publishTrigger(ctx, events.TopicAgentHeartbeat, map[string]any{
		"conversation_id": conversationID.String(),
		"project_id":      projectID.String(),
	})
}

// SendConversationMessage publishes a chat message to an active conversation.
//
// ACP-type agents, and any conversation attached to a static environment,
// route through resumeConversationMessage instead: unlike an ordinary LLM
// agent's ephemeral sandbox (where a follow-up message only ever makes
// sense while a turn is actually running — its sandbox is gone for good
// once the turn ends), an ACP agent's local bridge daemon keeps a
// conversation alive by conversation_id regardless of which trigger type
// started it (task_assigned, comment_mention, description_write,
// automation_message — not just chat_message), and a static environment's
// container/Pod likewise outlives any one conversation's status (see
// docker.Manager.StopEnvironment's doc comment) — so either can always be
// resumed here too, from any status, not just chat_message ones —
// mirroring SendChatMessage's own terminal-status resume carve-out for
// chat sessions.
func (s *Service) SendConversationMessage(ctx context.Context, projectID, conversationID uuid.UUID, message string, memberID uuid.UUID, contextItems []agentdom.ContextItemRef) error {
	c, err := s.GetConversation(ctx, projectID, conversationID, memberID)
	if err != nil {
		return err
	}

	agent, err := s.repo.FindAgentByID(ctx, c.AgentID)
	if err != nil {
		return err
	}
	if agent.AgentType == agentdom.AgentTypeACP || c.EnvironmentID != nil {
		return s.resumeConversationMessage(ctx, projectID, c, message, memberID, contextItems)
	}

	if agentdom.ConversationStatus(c.Status) != agentdom.ConversationStatusRunning {
		return agentdom.ErrConversationNotRunning
	}
	payload := map[string]any{
		"conversation_id": conversationID.String(),
		"project_id":      projectID.String(),
		"message":         message,
		"member_id":       memberID.String(),
	}
	if len(contextItems) > 0 {
		b, _ := json.Marshal(contextItems)
		payload["context_items"] = string(b)
	}
	return s.publishTrigger(ctx, events.TopicAgentChatMessage, payload)
}

// resumeConversationMessage resumes a conversation of any trigger type from
// any status other than running/queued (busy), so it can be continued from
// the chat box instead of being stuck once its first turn ends — used for
// ACP-type agents (see SendConversationMessage's own doc comment) and for
// any conversation attached to a static environment.
func (s *Service) resumeConversationMessage(ctx context.Context, projectID uuid.UUID, c *agentdom.AgentConversation, message string, memberID uuid.UUID, contextItems []agentdom.ContextItemRef) error {
	status := agentdom.ConversationStatus(c.Status)
	if status == agentdom.ConversationStatusRunning || status == agentdom.ConversationStatusQueued {
		// Still mid-turn (or not yet picked up by the worker) — reject
		// instead of dispatching a second start_turn/attach on top of one
		// that hasn't finished: for ACP, ConversationRunner.start_turn's
		// own "still running" guard would report the *in-flight* turn as
		// failed, not queue this message behind it; for an
		// environment-backed conversation, a concurrent turn is already
		// attached to the same goose serve session.
		return agentdom.ErrConversationBusy
	}

	// Validate the environment/folder still resolves *before* the
	// ClaimConversationStatus call below moves status to "running" — a
	// claim that then failed validation would otherwise be stuck there
	// with no rollback (mirrors SendChatMessage's own early-validate-
	// before-claim comment; the later resolveWorkdirForConversation call
	// below, which builds the actual trigger payload, is a cheap, harmless
	// duplicate read on this now-validated path).
	if c.EnvironmentID != nil {
		if _, _, err := s.resolveWorkdirForConversation(ctx, projectID, c); err != nil {
			return err
		}
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

	// Re-resolve into a live (environmentID, workdir) pair for the trigger
	// payload — needed on every resume, not just the first (see
	// resolveWorkdirForConversation's doc comment). nil for an ACP
	// conversation (c.EnvironmentID is always nil there — ACP sandboxing is
	// owned by the user's own local client, not agent-runner).
	envID, workdir, err := s.resolveWorkdirForConversation(ctx, projectID, c)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"conversation_id": c.ID.String(),
		"project_id":      c.ProjectID.String(),
		"agent_id":        c.AgentID.String(),
		"trigger_type":    c.TriggerType,
		"actor_member_id": memberID.String(),
		"message":         message,
		"repo_plugin_ids": strings.Join(s.gatherRepoPluginIDs(ctx), ","),
	}
	if envID != nil {
		payload["environment_id"] = envID.String()
		payload["workdir"] = workdir
	}
	if len(contextItems) > 0 {
		b, _ := json.Marshal(contextItems)
		payload["context_items"] = string(b)
	}
	return s.publishTrigger(ctx, events.TopicAgentChatMessage, payload)
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
func (s *Service) SendGlobalConversationMessage(ctx context.Context, conversationID uuid.UUID, message string, actorUserID uuid.UUID, contextItems []agentdom.ContextItemRef) error {
	c, err := s.GetGlobalConversation(ctx, conversationID, actorUserID)
	if err != nil {
		return err
	}

	agent, err := s.repo.FindAgentByID(ctx, c.AgentID)
	if err != nil {
		return err
	}
	if agent.AgentType == agentdom.AgentTypeACP {
		return s.sendACPGlobalConversationMessage(ctx, c, message, actorUserID, contextItems)
	}

	if agentdom.ConversationStatus(c.Status) != agentdom.ConversationStatusRunning {
		return agentdom.ErrConversationNotRunning
	}
	payload := map[string]any{
		"conversation_id": conversationID.String(),
		"agent_id":        c.AgentID.String(),
		"message":         message,
		"actor_user_id":   actorUserID.String(),
	}
	if len(contextItems) > 0 {
		b, _ := json.Marshal(contextItems)
		payload["context_items"] = string(b)
	}
	return s.publishTrigger(ctx, events.TopicAgentChatMessage, payload)
}

// sendACPGlobalConversationMessage is resumeConversationMessage's
// global-chat, ACP-only sibling — see that function's doc comment for why
// ACP conversations can always be resumed regardless of trigger type or
// terminal status. No environment carve-out here, unlike
// resumeConversationMessage: a global-scope agent can never have a default
// environment (see agentdom.Agent.DefaultEnvironmentID's doc comment), so
// a global conversation's EnvironmentID is always nil.
func (s *Service) sendACPGlobalConversationMessage(ctx context.Context, c *agentdom.AgentConversation, message string, actorUserID uuid.UUID, contextItems []agentdom.ContextItemRef) error {
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
	payload := map[string]any{
		"conversation_id": c.ID.String(),
		"agent_id":        c.AgentID.String(),
		"trigger_type":    c.TriggerType,
		"actor_user_id":   actorUserID.String(),
		"message":         message,
	}
	if len(contextItems) > 0 {
		b, _ := json.Marshal(contextItems)
		payload["context_items"] = string(b)
	}
	return s.publishTrigger(ctx, events.TopicAgentChatMessage, payload)
}

// -------------------------------------------------------------------------
// Chat Sessions
// -------------------------------------------------------------------------

// ListChatSessions returns all chat sessions for the given agent and member.
func (s *Service) ListChatSessions(ctx context.Context, projectID, agentID, memberID uuid.UUID) ([]*agentdom.AgentChatSession, error) {
	if _, err := s.GetAgent(ctx, projectID, agentID); err != nil {
		return nil, err
	}
	return s.repo.ListChatSessions(ctx, agentID, memberID)
}

// StartChatSession creates a new chat session and publishes the initial message trigger.
// environmentID/folderID come from the request and are optional:
// environmentID nil falls back to the agent's own DefaultEnvironmentID (see
// resolveChatEnvironment); folderID nil auto-selects the environment's sole
// folder, or fails with ErrFolderNotFound if that's ambiguous — the caller
// must ask the user to pick.
func (s *Service) StartChatSession(ctx context.Context, projectID, agentID, memberID uuid.UUID, message string, environmentID, folderID *uuid.UUID, contextItems []agentdom.ContextItemRef) (*agentdom.AgentChatSession, *agentdom.AgentConversation, error) {
	if _, err := s.GetAgent(ctx, projectID, agentID); err != nil {
		return nil, nil, err
	}

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

	envID, resolvedFolderID, workdir, err := s.resolveConversationEnvironment(ctx, projectID, agentID, environmentID, folderID)
	if err != nil {
		return nil, nil, err
	}

	conv, err := s.createConversation(ctx, projectID, agentID, &memberID, agentdom.AgentConversation{
		TriggerType:         "chat_message",
		ChatSessionID:       &session.ID,
		EnvironmentID:       envID,
		EnvironmentFolderID: resolvedFolderID,
	})
	if err != nil {
		return nil, nil, err
	}

	if err := s.publishChatTrigger(ctx, agentID, conv.ID, session.ID, projectID, memberID, message, s.gatherRepoPluginIDs(ctx), envID, workdir, contextItems); err != nil {
		return nil, nil, err
	}

	return session, conv, nil
}

// resolveConversationEnvironment resolves which static environment+folder
// (if any) a new conversation should attach to, regardless of what
// triggered it — chat message, task assignment, comment mention,
// description write, or automation message all share this one resolution
// path. environmentID, when nil, falls back to the agent's own
// DefaultEnvironmentID (agentdom.Agent.DefaultEnvironmentID) — the only
// trigger that ever passes a non-nil environmentID/folderID of its own
// (an explicit per-conversation override) is StartChatSession; every other
// caller (TriggerTaskAssigned et al.) passes nil for both, deferring
// entirely to the agent's default. Returns (nil, nil, "", nil) when
// neither the caller nor the agent names an environment and the agent is
// NOT provider_cli — the conversation then gets an ephemeral
// per-conversation sandbox as it always has, unchanged.
//
// For a provider_cli agent deferring to its own default (environmentID ==
// nil on entry — the common case, per the doc above), a still-unresolved
// environment returns ErrDefaultEnvironmentRequiredForCLIProvider instead
// of the usual silent (nil, nil, "", nil): that type's CLI login state must
// persist across conversations, which only a static environment's volume
// provides, so it must never silently fall through to an ephemeral
// sandbox. The agent is only fetched when environmentID == nil, same
// condition as before this check existed — a provider_cli agent can never
// exist at all when s.environmentSvc == nil (CreateAgent's
// validateDefaultEnvironment already requires environmentSvc to resolve a
// default_environment_id, and provider_cli agents require one), so no
// fetch is needed on that branch either. The narrow gap this leaves — a
// caller-supplied explicit environmentID/folderID (StartChatSession only)
// that fails to resolve for a provider_cli agent — falls through to the
// ordinary silent-ephemeral path rather than erroring; ResolveConversationWorkdir
// already returns a real error for an explicit environmentID it can't
// resolve (see its own doc comment: only environmentID == nil resolves to
// (nil, nil, nil)), so this gap should be unreachable in practice.
func (s *Service) resolveConversationEnvironment(ctx context.Context, projectID, agentID uuid.UUID, environmentID, folderID *uuid.UUID) (envID, resolvedFolderID *uuid.UUID, workdir string, err error) {
	if s.environmentSvc == nil {
		return nil, nil, "", nil
	}
	var agent *agentdom.Agent
	if environmentID == nil {
		agent, err = s.repo.FindAgentByID(ctx, agentID)
		if err != nil {
			return nil, nil, "", err
		}
		environmentID = agent.DefaultEnvironmentID
		// agent.DefaultFolderID only ever belongs to agent.DefaultEnvironmentID
		// — only consulted here, in the branch that just defaulted
		// environmentID itself from that same environment, and only when
		// the caller didn't already pick a folder of its own. A caller
		// that passed an explicit environmentID (possibly a different one)
		// must never inherit this agent's default folder, since it could
		// belong to the wrong environment entirely.
		if environmentID != nil && folderID == nil {
			folderID = agent.DefaultFolderID
		}
	}
	if environmentID == nil {
		if agent != nil && agent.AgentType == agentdom.AgentTypeProviderCLI {
			return nil, nil, "", agentdom.ErrDefaultEnvironmentRequiredForCLIProvider
		}
		return nil, nil, "", nil
	}
	env, folder, err := s.environmentSvc.ResolveConversationWorkdir(ctx, projectID, environmentID, folderID)
	if err != nil {
		return nil, nil, "", err
	}
	if env == nil || folder == nil {
		// e.g. the agent's default environment/folder was since deleted.
		if agent != nil && agent.AgentType == agentdom.AgentTypeProviderCLI {
			return nil, nil, "", agentdom.ErrDefaultEnvironmentRequiredForCLIProvider
		}
		return nil, nil, "", nil
	}
	return &env.ID, &folder.ID, folder.Path, nil
}

// resolveWorkdirForConversation re-resolves an already-created
// conversation's environment_id/environment_folder_id back into a live
// (environmentID, workdir) pair for a trigger payload. Needed on every
// trigger a conversation publishes, not just its first — agent-runner's
// goose serve process runs continuously per environment (see
// docs/ai-agent/environment-management.md's "no new in-memory registry"
// design), so a resumed conversation's later turns need to keep telling
// agent-runner which environment+folder to run NewSession against just as
// much as the very first turn did.
func (s *Service) resolveWorkdirForConversation(ctx context.Context, projectID uuid.UUID, c *agentdom.AgentConversation) (envID *uuid.UUID, workdir string, err error) {
	if s.environmentSvc == nil || c.EnvironmentID == nil {
		return nil, "", nil
	}
	_, folder, err := s.environmentSvc.ResolveConversationWorkdir(ctx, projectID, c.EnvironmentID, c.EnvironmentFolderID)
	if err != nil {
		return nil, "", err
	}
	if folder == nil {
		return nil, "", nil
	}
	return c.EnvironmentID, folder.Path, nil
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
//
// An LLM-type conversation attached to a static environment
// (environmentdom.Environment) gets the same terminal-status resume too, for
// the analogous reason: the environment's container outlives the
// conversation's own status (it isn't torn down when a conversation ends —
// see docker.Manager.StopEnvironment's doc comment on the server), so
// "stopped"/"failed" here means "no turn is currently in flight," not "there
// is nothing left to attach to." Only an ordinary (non-environment) LLM
// conversation going terminal still falls through to a brand-new
// conversation_id below — its ephemeral sandbox really is gone for good.
func (s *Service) SendChatMessage(ctx context.Context, projectID, sessionID, memberID uuid.UUID, message string, contextItems []agentdom.ContextItemRef) (*agentdom.AgentConversation, error) {
	session, err := s.repo.FindChatSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.ProjectID != projectID {
		return nil, agentdom.ErrChatSessionNotFound
	}
	// A chat session is owner-private: only its owning member may post to it
	// (previously any project member could, which let a non-owner both read
	// and inject into someone else's session).
	if session.MemberID != memberID {
		return nil, agentdom.ErrChatSessionNotFound
	}

	latest, err := s.repo.FindLatestConversationByChatSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	conv := latest
	if latest != nil {
		// Validate a resumed conversation's environment/folder still
		// resolves *before* any ClaimConversationStatus call below moves
		// it to "running" — a claim that then failed validation would
		// otherwise be stuck there with no rollback (see
		// resolveWorkdirForConversation's own doc comment; the later call
		// at the bottom of this function, which builds the actual trigger
		// payload, is a cheap, harmless duplicate read on this now-
		// validated path).
		if latest.EnvironmentID != nil {
			if _, _, err := s.resolveWorkdirForConversation(ctx, projectID, latest); err != nil {
				return nil, err
			}
		}
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
			if agent.AgentType == agentdom.AgentTypeACP || latest.EnvironmentID != nil {
				// Resume — same atomic-claim treatment as the paused case
				// above, just starting from a terminal status instead of
				// "paused". Two different reasons land on the same
				// behavior: an ACP conversation never reaches "paused" at
				// all (see the doc comment above), while an
				// environment-backed LLM conversation can reach "paused"
				// but still go terminal from there (an explicit Stop, or a
				// genuine turn failure) — either way there's a live
				// container to reattach to, not an ephemeral sandbox
				// that's already gone.
				claimed, err := s.repo.ClaimConversationStatus(ctx, latest.ID,
					latest.Status, string(agentdom.ConversationStatusRunning))
				if err != nil {
					return nil, err
				}
				if !claimed {
					return nil, agentdom.ErrConversationBusy
				}
			} else {
				// Terminal status, no persistent backing (an ordinary
				// ephemeral sandbox, already torn down) — fall through to
				// create a new conversation.
				conv = nil
			}
		}
	}

	if conv == nil {
		// A fresh conversation row: either this chat session's very first
		// message, or a non-environment LLM conversation whose ephemeral
		// sandbox is gone for good now that it's terminal — see the switch
		// above. No environment/folder to carry over in either case: an
		// environment-backed LLM conversation resumes in place instead (same
		// switch), so whenever this runs with latest non-nil,
		// latest.EnvironmentID is already guaranteed nil.
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

	// Re-resolve conv's environment/folder into a live (environmentID,
	// workdir) pair for the trigger payload — needed on every turn, not
	// just the first (see resolveWorkdirForConversation's doc comment).
	envID, workdir, err := s.resolveWorkdirForConversation(ctx, projectID, conv)
	if err != nil {
		return nil, err
	}
	if err := s.publishChatTrigger(ctx, session.AgentID, conv.ID, sessionID, projectID, memberID, message, s.gatherRepoPluginIDs(ctx), envID, workdir, contextItems); err != nil {
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
func (s *Service) StartGlobalChatSession(ctx context.Context, agentID, actorUserID uuid.UUID, message string, contextItems []agentdom.ContextItemRef) (*agentdom.AgentChatSession, *agentdom.AgentConversation, error) {
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

	if err := s.publishGlobalChatTrigger(ctx, agentID, conv.ID, session.ID, actorUserID, message, contextItems); err != nil {
		return nil, nil, err
	}

	return session, conv, nil
}

// SendGlobalChatMessage sends a message to an existing global chat session
// and publishes the trigger. Mirrors SendChatMessage's resume/terminal
// handling — see its doc comment for the pause/resume rationale.
func (s *Service) SendGlobalChatMessage(ctx context.Context, sessionID, actorUserID uuid.UUID, message string, contextItems []agentdom.ContextItemRef) (*agentdom.AgentConversation, error) {
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

	if err := s.publishGlobalChatTrigger(ctx, session.AgentID, conv.ID, sessionID, actorUserID, message, contextItems); err != nil {
		return nil, err
	}

	now := time.Now()
	session.LastMessageAt = &now
	_ = s.repo.UpdateChatSession(ctx, session)

	return conv, nil
}

// ListChatMessages returns conversation events for a chat session. Unreached
// by any route (see agentdom.ChatSessionService) — kept only to satisfy the
// interface until it grows a real caller. memberID is the caller's
// project_members.id and gates ownership so the eventual caller cannot read
// another member's private session.
func (s *Service) ListChatMessages(ctx context.Context, sessionID, memberID uuid.UUID, offset, limit int) ([]*agentdom.AgentConversationEvent, int64, error) {
	session, err := s.repo.FindChatSessionByID(ctx, sessionID)
	if err != nil {
		return nil, 0, err
	}
	if session.MemberID != memberID {
		return nil, 0, agentdom.ErrChatSessionNotFound
	}
	// TODO: We'd need to aggregate events from all conversations in this session.
	// For now, return events from the most recent conversation with this session_id.
	latest, err := s.repo.FindLatestConversationByChatSession(ctx, sessionID)
	if err != nil {
		return nil, 0, err
	}
	if latest == nil {
		return []*agentdom.AgentConversationEvent{}, 0, nil
	}
	// offset has no cursor equivalent (see agentdom.ConversationEventWindow):
	// fail loudly rather than silently ignoring it, in case this method ever
	// grows a real caller that expects offset-based paging to work.
	if offset != 0 {
		return nil, 0, fmt.Errorf("ListChatMessages: non-zero offset %d is not supported by cursor-based ListConversationEvents", offset)
	}
	return s.repo.ListConversationEvents(ctx, latest.ID, agentdom.ConversationEventWindow{Limit: limit})
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
		// EnvironmentID/EnvironmentFolderID: resolved by every trigger
		// constructor via resolveConversationEnvironment before calling
		// this — see that method's own doc comment for how each one
		// resolves to the agent's DefaultEnvironmentID/DefaultFolderID
		// when it has no per-conversation override of its own. nil on the
		// template here just means resolution turned up nothing (no
		// default set, or no environmentSvc wired up), not that this
		// trigger type opted out.
		EnvironmentID:       template.EnvironmentID,
		EnvironmentFolderID: template.EnvironmentFolderID,
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

	envID, resolvedFolderID, workdir, err := s.resolveConversationEnvironment(ctx, projectID, agentID, nil, nil)
	if err != nil {
		return nil, err
	}

	conv, err := s.createConversation(ctx, projectID, agentID, triggeredByMemberID, agentdom.AgentConversation{
		TriggerType:         "task_assigned",
		TaskID:              &taskID,
		RepoPluginID:        repoPluginID,
		EnvironmentID:       envID,
		EnvironmentFolderID: resolvedFolderID,
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
	if envID != nil {
		payload["environment_id"] = envID.String()
		payload["workdir"] = workdir
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

	envID, resolvedFolderID, workdir, err := s.resolveConversationEnvironment(ctx, projectID, agentID, nil, nil)
	if err != nil {
		return nil, err
	}

	conv, err := s.createConversation(ctx, projectID, agentID, triggeredByMemberID, agentdom.AgentConversation{
		TriggerType:         "automation_message",
		RepoPluginID:        repoPluginID,
		EnvironmentID:       envID,
		EnvironmentFolderID: resolvedFolderID,
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
	if envID != nil {
		payload["environment_id"] = envID.String()
		payload["workdir"] = workdir
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

	envID, resolvedFolderID, workdir, err := s.resolveConversationEnvironment(ctx, projectID, agentID, nil, nil)
	if err != nil {
		return nil, err
	}

	conv, err := s.createConversation(ctx, projectID, agentID, &triggeredByMemberID, agentdom.AgentConversation{
		TriggerType:         "comment_mention",
		TaskID:              &taskID,
		CommentID:           &commentID,
		RepoPluginID:        repoPluginID,
		EnvironmentID:       envID,
		EnvironmentFolderID: resolvedFolderID,
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
	if envID != nil {
		payload["environment_id"] = envID.String()
		payload["workdir"] = workdir
	}
	_ = s.publishTrigger(ctx, events.TopicAgentCommentMention, payload)
	return conv, nil
}

// TriggerDescriptionWrite creates a conversation and publishes a trigger for
// the agent to write a description for the given task. Verifies agentID
// belongs to projectID; the caller is responsible for verifying taskID
// belongs to projectID (this service has no task-repository dependency).
func (s *Service) TriggerDescriptionWrite(ctx context.Context, projectID, agentID, taskID, triggeredByMemberID uuid.UUID) (*agentdom.AgentConversation, error) {
	if _, err := s.GetAgent(ctx, projectID, agentID); err != nil {
		return nil, err
	}

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

	envID, resolvedFolderID, workdir, err := s.resolveConversationEnvironment(ctx, projectID, agentID, nil, nil)
	if err != nil {
		return nil, err
	}

	conv, err := s.createConversation(ctx, projectID, agentID, &triggeredByMemberID, agentdom.AgentConversation{
		TriggerType:         "description_write",
		TaskID:              &taskID,
		RepoPluginID:        repoPluginID,
		EnvironmentID:       envID,
		EnvironmentFolderID: resolvedFolderID,
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
	if envID != nil {
		payload["environment_id"] = envID.String()
		payload["workdir"] = workdir
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

// environmentID/workdir, when non-nil/non-empty, tell agent-runner which
// static environment (and folder within it) this conversation is attached
// to — see resolveChatEnvironment/resolveWorkdirForConversation's doc
// comments for how callers resolve them, and
// docs/ai-agent/environment-management.md's "Conversation attach path"
// section for how agent-runner's decode.go/coldStartEnvironment consume
// them.
func (s *Service) publishChatTrigger(ctx context.Context, agentID, convID, sessionID, projectID, memberID uuid.UUID, message string, repoPluginIDs []string, environmentID *uuid.UUID, workdir string, contextItems []agentdom.ContextItemRef) error {
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
	if environmentID != nil {
		payload["environment_id"] = environmentID.String()
		payload["workdir"] = workdir
	}
	if len(contextItems) > 0 {
		b, _ := json.Marshal(contextItems)
		payload["context_items"] = string(b)
	}
	return s.publishTrigger(ctx, events.TopicAgentChatMessage, payload)
}

// publishGlobalChatTrigger is publishChatTrigger's global-chat sibling — no
// project_id, actor identified by actor_user_id, and repo_plugin_ids
// omitted entirely (repo/PR tools are excluded from global-chat
// conversations; see the Global Conversations section's doc comment).
func (s *Service) publishGlobalChatTrigger(ctx context.Context, agentID, convID, sessionID, actorUserID uuid.UUID, message string, contextItems []agentdom.ContextItemRef) error {
	payload := map[string]any{
		"conversation_id": convID.String(),
		"agent_id":        agentID.String(),
		"chat_session_id": sessionID.String(),
		"actor_user_id":   actorUserID.String(),
		"trigger_type":    "chat_message",
		"message":         message,
	}
	if len(contextItems) > 0 {
		b, _ := json.Marshal(contextItems)
		payload["context_items"] = string(b)
	}
	return s.publishTrigger(ctx, events.TopicAgentChatMessage, payload)
}
