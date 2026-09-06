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
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/authz"
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

// defaultParallelismLimit/parallelismLimitCap clamp Agent.ParallelismLimit
// the same way CreateAgent/UpdateAgent already clamp MaxIterations —
// applied by CreateAgent/UpdateAgent/CreateGlobalAgent/UpdateGlobalAgent at
// write time, and again defensively by checkParallelismCapacity in case a
// row (or a directly-constructed Agent, e.g. in a test) predates this field.
const (
	defaultParallelismLimit = 1
	parallelismLimitCap     = 10
)

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
	// authorizer backs authorizeConversationsReadForConversation's
	// conversations.read check in GetConversationForAgent — see that
	// method's doc comment.
	// Nil is a valid, supported configuration (same convention as
	// environmentSvc/encryptor above): the check is skipped rather than
	// failing closed, since every existing GetConversationForAgent test
	// constructs a bare Service with no authorizer. Production wiring
	// (bootstrap/app.go) always configures one via WithAuthorizer.
	authorizer *authz.Authorizer
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

// WithAuthorizer wires in the permission authorizer — see the authorizer
// field's doc comment for what it's used for.
func (s *Service) WithAuthorizer(a *authz.Authorizer) *Service {
	s.authorizer = a
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
		ID:               uuid.New(),
		ProjectID:        projectID,
		Name:             name,
		Handle:           handle,
		AgentType:        agentType,
		MaxIterations:    in.MaxIterations,
		TimeoutMinutes:   in.TimeoutMinutes,
		ParallelismLimit: in.ParallelismLimit,
		CreatedBy:        in.CreatedBy,
		CreatedAt:        now,
		UpdatedAt:        now,
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
	if a.ParallelismLimit <= 0 {
		a.ParallelismLimit = defaultParallelismLimit
	} else if a.ParallelismLimit > parallelismLimitCap {
		a.ParallelismLimit = parallelismLimitCap
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
	if err := validateParallelismLimit(a); err != nil {
		return nil, err
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
	oldParallelismLimit := a.ParallelismLimit
	if oldParallelismLimit <= 0 {
		oldParallelismLimit = defaultParallelismLimit
	}
	if in.ParallelismLimit != nil {
		v := *in.ParallelismLimit
		if v <= 0 {
			v = defaultParallelismLimit
		} else if v > parallelismLimitCap {
			v = parallelismLimitCap
		}
		a.ParallelismLimit = v
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
	if err := validateParallelismLimit(a); err != nil {
		return nil, err
	}
	a.UpdatedAt = time.Now()

	if err := s.repo.UpdateAgent(ctx, a); err != nil {
		return nil, err
	}
	// Raising the limit frees slots with no terminal-status event of their
	// own to react to — see AdvanceQueue's doc comment. Best-effort: a
	// missed catch-up here just leaves the newly-freed slot(s) idle until the
	// next conversation of this agent's actually finishes (which advances
	// the queue anyway), not a correctness problem.
	if a.ParallelismLimit > oldParallelismLimit {
		_, _ = s.AdvanceQueue(ctx, a.ID, a.ParallelismLimit-oldParallelismLimit)
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
		ID:               uuid.New(),
		AgentScope:       agentdom.AgentScopeGlobal,
		GlobalRoleID:     in.GlobalRoleID,
		Name:             name,
		Handle:           handle,
		AgentType:        agentType,
		MaxIterations:    in.MaxIterations,
		TimeoutMinutes:   in.TimeoutMinutes,
		ParallelismLimit: in.ParallelismLimit,
		CreatedBy:        in.CreatedBy,
		CreatedAt:        now,
		UpdatedAt:        now,
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
	if a.ParallelismLimit <= 0 {
		a.ParallelismLimit = defaultParallelismLimit
	} else if a.ParallelismLimit > parallelismLimitCap {
		a.ParallelismLimit = parallelismLimitCap
	}
	if err := validateParallelismLimit(a); err != nil {
		return nil, err
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
	oldParallelismLimit := a.ParallelismLimit
	if oldParallelismLimit <= 0 {
		oldParallelismLimit = defaultParallelismLimit
	}
	if in.ParallelismLimit != nil {
		v := *in.ParallelismLimit
		if v <= 0 {
			v = defaultParallelismLimit
		} else if v > parallelismLimitCap {
			v = parallelismLimitCap
		}
		a.ParallelismLimit = v
	}
	if in.GlobalRoleID != nil {
		if *in.GlobalRoleID == uuid.Nil {
			a.GlobalRoleID = nil
		} else {
			a.GlobalRoleID = in.GlobalRoleID
		}
	}
	if err := validateParallelismLimit(a); err != nil {
		return nil, err
	}
	a.UpdatedAt = time.Now()

	if err := s.repo.UpdateAgent(ctx, a); err != nil {
		return nil, err
	}
	// See UpdateAgent's identical catch-up call for why.
	if a.ParallelismLimit > oldParallelismLimit {
		_, _ = s.AdvanceQueue(ctx, a.ID, a.ParallelismLimit-oldParallelismLimit)
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
// identity matching isn't sufficient on its own. Also requires the calling
// agent to hold conversations.read (see
// authorizeConversationsReadForConversation) to read any conversation other
// than the one it's currently running as part of — see the same-conversation
// shortcut below for why that one case is exempt.
func (s *Service) GetConversationForAgent(ctx context.Context, conversationID, callerAgentID, currentConversationID uuid.UUID) (*agentdom.AgentConversation, error) {
	target, err := s.repo.FindConversationByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if target.AgentID != callerAgentID {
		return nil, agentdom.ErrConversationNotFound
	}
	// Always allowed, regardless of conversations.read: an agent may read
	// the conversation it's currently running as part of — it already has
	// this data as that conversation's own active participant, so gating it
	// on a permission grant adds no protection while breaking the common
	// case of a global-scope agent with no global role (conversations.read
	// is backfilled onto project_roles, not global_roles — see
	// 000051_add_conversation_permissions.sql — and a global agent's own
	// global role is optional, commonly left unset). Also short-circuits
	// the common case (no other conversation was attached) without a
	// second lookup.
	if target.ID == currentConversationID {
		return target, nil
	}
	if err := s.authorizeConversationsReadForConversation(ctx, callerAgentID, target); err != nil {
		return nil, err
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

// authorizeConversationsReadForConversation reports whether callerAgentID
// holds conversations.read for the scope conv belongs to: its own global
// role, or (for a project-scoped conversation) its role in that specific
// project — an OR, mirroring the MCP server's own isToolVisible check for
// read_conversation (apps/mcp/src/permissions.ts's requiresProject: true) so
// tool-list visibility and backend enforcement agree. Skipped (always
// allowed) when s.authorizer is nil — see that field's doc comment.
func (s *Service) authorizeConversationsReadForConversation(ctx context.Context, callerAgentID uuid.UUID, conv *agentdom.AgentConversation) error {
	if s.authorizer == nil {
		return nil
	}
	globalOK, err := s.authorizer.HasGlobalPermissionsForAgent(ctx, callerAgentID, authz.PermissionConversationsRead)
	if err != nil {
		return fmt.Errorf("authz: check agent global conversations.read: %w", err)
	}
	if globalOK {
		return nil
	}
	if conv.ProjectID != uuid.Nil {
		projectOK, err := s.authorizer.HasPermissionsForAgent(ctx, callerAgentID, conv.ProjectID, authz.PermissionConversationsRead)
		if err != nil {
			return fmt.Errorf("authz: check agent project conversations.read: %w", err)
		}
		if projectOK {
			return nil
		}
	}
	return agentdom.ErrConversationNotFound
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
	// If this conversation was still sitting in the parallelism backlog
	// (agent_pending_triggers — see PendingTrigger's doc comment), remove
	// its row so AdvanceQueue can never dequeue and dispatch it after it's
	// already been marked stopped. wasQueued is false when it had already
	// been dispatched (no pending-trigger row to begin with) — agent-runner
	// was never told about a conversation this never reached, so there's
	// nothing there to interrupt.
	wasQueued, _ := s.repo.DeletePendingTriggerByConversationID(ctx, conversationID)
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
	if wasQueued {
		return nil
	}
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
	// See StopConversation's identical cleanup for why: a global-chat
	// conversation can be queued behind a busy global agent too, and
	// AdvanceQueue needs the StreamAgentConversationStatus publish below to
	// ever learn this agent's slot just freed.
	wasQueued, _ := s.repo.DeletePendingTriggerByConversationID(ctx, conversationID)
	_ = s.publisher.AppendFlat(ctx, events.StreamAgentConversationStatus, map[string]any{
		"conversation_id": conversationID.String(),
		"status":          string(agentdom.ConversationStatusStopped),
	})
	if wasQueued {
		return nil
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
func (s *Service) StartChatSession(ctx context.Context, projectID, agentID, memberID uuid.UUID, message string, environmentID, folderID *uuid.UUID, contextItems []agentdom.ContextItemRef, onBusy string) (*agentdom.AgentChatSession, *agentdom.AgentConversation, error) {
	if _, err := s.GetAgent(ctx, projectID, agentID); err != nil {
		return nil, nil, err
	}

	// Resolved before the capacity check below (which needs it to also
	// enforce checkFolderCapacity), not just before createConversation —
	// resolveConversationEnvironment has no side effects, so reordering it
	// earlier is free, and keeps "an ask rejection leaves nothing behind"
	// true for the folder constraint too, not just the agent one.
	envID, resolvedFolderID, workdir, err := s.resolveConversationEnvironment(ctx, projectID, agentID, environmentID, folderID)
	if err != nil {
		return nil, nil, err
	}

	// Starting a session always creates a brand new conversation (there is
	// no existing one yet to be "busy"), so the capacity check applies
	// unconditionally here — decided once, before anything is persisted, so
	// an "ask" rejection leaves no chat session or conversation behind.
	dispatchNow, err := s.checkDispatchCapacity(ctx, agentID, envID, resolvedFolderID, onBusy)
	if err != nil {
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

	conv, err := s.createConversation(ctx, projectID, agentID, &memberID, agentdom.AgentConversation{
		TriggerType:         "chat_message",
		ChatSessionID:       &session.ID,
		EnvironmentID:       envID,
		EnvironmentFolderID: resolvedFolderID,
	})
	if err != nil {
		return nil, nil, err
	}

	// needsClaim=true: this conversation was just created fresh above, still
	// "queued", never claimed by anything else.
	if err := s.publishChatTrigger(ctx, agentID, conv.ID, session.ID, projectID, memberID, message, s.gatherRepoPluginIDs(ctx), envID, resolvedFolderID, workdir, contextItems, dispatchNow, true); err != nil {
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
func (s *Service) SendChatMessage(ctx context.Context, projectID, sessionID, memberID uuid.UUID, message string, contextItems []agentdom.ContextItemRef, onBusy string) (*agentdom.AgentConversation, error) {
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

	// dispatchNow is decided by whichever branch below actually goes on to
	// resume/create a conversation — see checkParallelismCapacity's doc
	// comment. It stays false only if that never happens (the function
	// returns early as busy first). freshConversation is true only for the
	// conv==nil branch below (a brand-new "queued" row, never claimed by
	// anything else yet) — see publishChatTrigger's needsClaim doc comment.
	var dispatchNow, freshConversation bool
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
			// This session's own conversation is idle (not itself occupying
			// a running slot), so the capacity check runs fresh here —
			// before the claim below, so an "ask" rejection leaves the
			// conversation untouched at "paused" rather than stuck
			// mid-claim. latest.EnvironmentID/EnvironmentFolderID (already
			// validated above) feed checkDispatchCapacity's folder check —
			// resuming in place is still "starting a turn in this folder"
			// as far as another conversation sharing it is concerned.
			dispatchNow, err = s.checkDispatchCapacity(ctx, session.AgentID, latest.EnvironmentID, latest.EnvironmentFolderID, onBusy)
			if err != nil {
				return nil, err
			}
			targetStatus := string(agentdom.ConversationStatusRunning)
			if !dispatchNow {
				targetStatus = string(agentdom.ConversationStatusQueued)
			}
			// Resume — claim the conversation atomically so two concurrent
			// replies can't both win and double-publish a resume trigger for
			// the same conversation_id. The loser is told to retry as busy
			// rather than silently racing ai-agent's sandbox reattachment.
			claimed, err := s.repo.ClaimConversationStatus(ctx, latest.ID,
				string(agentdom.ConversationStatusPaused), targetStatus)
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
				// See the paused case's identical checkDispatchCapacity call
				// above for why latest's own environment/folder feed in here
				// too (nil/nil for the ACP branch, which never has one).
				dispatchNow, err = s.checkDispatchCapacity(ctx, session.AgentID, latest.EnvironmentID, latest.EnvironmentFolderID, onBusy)
				if err != nil {
					return nil, err
				}
				targetStatus := string(agentdom.ConversationStatusRunning)
				if !dispatchNow {
					targetStatus = string(agentdom.ConversationStatusQueued)
				}
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
					latest.Status, targetStatus)
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
		dispatchNow, err = s.checkParallelismCapacity(ctx, session.AgentID, onBusy)
		if err != nil {
			return nil, err
		}
		freshConversation = true
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
	if err := s.publishChatTrigger(ctx, session.AgentID, conv.ID, sessionID, projectID, memberID, message, s.gatherRepoPluginIDs(ctx), envID, conv.EnvironmentFolderID, workdir, contextItems, dispatchNow, freshConversation); err != nil {
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
func (s *Service) StartGlobalChatSession(ctx context.Context, agentID, actorUserID uuid.UUID, message string, contextItems []agentdom.ContextItemRef, onBusy string) (*agentdom.AgentChatSession, *agentdom.AgentConversation, error) {
	// See StartChatSession's identical check for why this runs unconditionally
	// and before anything is persisted.
	dispatchNow, err := s.checkParallelismCapacity(ctx, agentID, onBusy)
	if err != nil {
		return nil, nil, err
	}

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

	// needsClaim=true: this conversation was just created fresh above, still
	// "queued", never claimed by anything else.
	if err := s.publishGlobalChatTrigger(ctx, agentID, conv.ID, session.ID, actorUserID, message, contextItems, dispatchNow, true); err != nil {
		return nil, nil, err
	}

	return session, conv, nil
}

// SendGlobalChatMessage sends a message to an existing global chat session
// and publishes the trigger. Mirrors SendChatMessage's resume/terminal
// handling — see its doc comment for the pause/resume rationale.
func (s *Service) SendGlobalChatMessage(ctx context.Context, sessionID, actorUserID uuid.UUID, message string, contextItems []agentdom.ContextItemRef, onBusy string) (*agentdom.AgentConversation, error) {
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

	// freshConversation: see SendChatMessage's identical variable doc comment.
	var dispatchNow, freshConversation bool
	conv := latest
	if latest != nil {
		switch agentdom.ConversationStatus(latest.Status) {
		case agentdom.ConversationStatusRunning, agentdom.ConversationStatusQueued:
			return nil, agentdom.ErrConversationBusy
		case agentdom.ConversationStatusPaused:
			dispatchNow, err = s.checkParallelismCapacity(ctx, session.AgentID, onBusy)
			if err != nil {
				return nil, err
			}
			targetStatus := string(agentdom.ConversationStatusRunning)
			if !dispatchNow {
				targetStatus = string(agentdom.ConversationStatusQueued)
			}
			claimed, err := s.repo.ClaimConversationStatus(ctx, latest.ID,
				string(agentdom.ConversationStatusPaused), targetStatus)
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
				dispatchNow, err = s.checkParallelismCapacity(ctx, session.AgentID, onBusy)
				if err != nil {
					return nil, err
				}
				targetStatus := string(agentdom.ConversationStatusRunning)
				if !dispatchNow {
					targetStatus = string(agentdom.ConversationStatusQueued)
				}
				claimed, err := s.repo.ClaimConversationStatus(ctx, latest.ID,
					latest.Status, targetStatus)
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
		dispatchNow, err = s.checkParallelismCapacity(ctx, session.AgentID, onBusy)
		if err != nil {
			return nil, err
		}
		freshConversation = true
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

	if err := s.publishGlobalChatTrigger(ctx, session.AgentID, conv.ID, sessionID, actorUserID, message, contextItems, dispatchNow, freshConversation); err != nil {
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
	_ = s.dispatchOrEnqueue(ctx, agentID, conv.ID, events.TopicAgentTaskAssigned, payload, envID, resolvedFolderID)
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
	_ = s.dispatchOrEnqueue(ctx, agentID, conv.ID, events.TopicAgentAutomationMessage, payload, envID, resolvedFolderID)
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
	_ = s.dispatchOrEnqueue(ctx, agentID, conv.ID, events.TopicAgentCommentMention, payload, envID, resolvedFolderID)
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
	_ = s.dispatchOrEnqueue(ctx, agentID, conv.ID, events.TopicAgentDescriptionWrite, payload, envID, resolvedFolderID)
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

// requiresSerialDispatch reports whether agent's conversations must never
// run more than one at a time, regardless of its configured
// ParallelismLimit, because a second one running concurrently would mean
// two turns writing into the very same shared working directory:
//
//   - ACP-type agents always resolve to the user's own local checkout via
//     apps/acp-bridge — and independently of that, apps/acp-bridge's own
//     Runner session model (keyed by task_id or agent_id depending on its
//     configured scope, never by conversation_id — see runner.go's
//     sessionKeyFor) rejects a second concurrent turn sharing its session
//     key rather than queueing it. Even an agent/task pairing that happens
//     not to collide would just be relying on happenstance the bridge
//     itself gives no guarantee about, so this applies to every ACP agent
//     unconditionally.
//   - Any agent (LLM or provider_cli) attached to a static
//     DefaultEnvironmentID: unlike the default ephemeral sandbox (a fresh,
//     isolated checkout per conversation), a static environment's
//     filesystem is shared across every conversation attached to it, so two
//     running at once would be exactly the same-directory race this whole
//     feature exists to prevent (see https://github.com/Paca-AI/paca/issues/462).
//     provider_cli agents always fall into this case, since they require a
//     DefaultEnvironmentID unconditionally.
//
// Enforced both at write time (see validateParallelismLimit, called from
// CreateAgent/UpdateAgent/CreateGlobalAgent/UpdateGlobalAgent) and
// defensively here at dispatch time (effectiveParallelismLimit) — the
// latter also covers an agent updated to attach a DefaultEnvironmentID
// after its ParallelismLimit was already set above 1, and any row that
// predates this validation.
func requiresSerialDispatch(agent *agentdom.Agent) bool {
	return agent.AgentType == agentdom.AgentTypeACP || agent.DefaultEnvironmentID != nil
}

// effectiveParallelismLimit resolves agent's real dispatch limit: its
// configured ParallelismLimit, defaulted and capped the same way
// CreateAgent/UpdateAgent already do at write time (defends against a
// directly-constructed Agent, e.g. an older row predating this column, or a
// test fixture, whose zero value would otherwise read as "never dispatch"),
// then forced down to 1 if requiresSerialDispatch — see that function's doc
// comment for why this override can never be configured away.
func effectiveParallelismLimit(agent *agentdom.Agent) int {
	limit := agent.ParallelismLimit
	if limit <= 0 {
		limit = defaultParallelismLimit
	} else if limit > parallelismLimitCap {
		limit = parallelismLimitCap
	}
	if requiresSerialDispatch(agent) && limit > 1 {
		limit = 1
	}
	return limit
}

// validateParallelismLimit rejects a ParallelismLimit above 1 on an agent
// that requiresSerialDispatch — called from CreateAgent/UpdateAgent/
// CreateGlobalAgent/UpdateGlobalAgent after every other field (in
// particular AgentType and DefaultEnvironmentID) has already been resolved
// to its final value, so this sees exactly the combination that would be
// persisted.
func validateParallelismLimit(a *agentdom.Agent) error {
	if a.ParallelismLimit > 1 && requiresSerialDispatch(a) {
		return agentdom.ErrParallelismLimitRequiresIsolatedSandbox
	}
	return nil
}

// checkParallelismCapacity decides whether a new turn for agentID may
// dispatch right now, given onBusy ("" | agentdom.OnBusyQueue |
// agentdom.OnBusyForce — see those constants' doc comments).
//
//   - OnBusyForce always returns (true, nil): skip the check entirely.
//   - Otherwise, dispatchNow is true when agentID currently has fewer than
//     its effective ParallelismLimit (see effectiveParallelismLimit)
//     conversations in status "running".
//   - When there's no free slot: OnBusyQueue returns (false, nil) — the
//     caller must hold the trigger in agent_pending_triggers instead of
//     publishing it. "" (ask, the default) instead returns a non-nil
//     *apierr.Error (CodeAgentParallelismLimitReached) carrying the
//     running/limit counts, so an interactive caller can surface it to a
//     human — with nothing created or mutated yet — instead of silently
//     picking a side.
func (s *Service) checkParallelismCapacity(ctx context.Context, agentID uuid.UUID, onBusy string) (dispatchNow bool, err error) {
	if onBusy == agentdom.OnBusyForce {
		return true, nil
	}
	agent, err := s.repo.FindAgentByID(ctx, agentID)
	if err != nil {
		return false, err
	}
	limit := effectiveParallelismLimit(agent)
	running, err := s.repo.CountRunningConversations(ctx, agentID)
	if err != nil {
		return false, err
	}
	if running < limit {
		return true, nil
	}
	if onBusy == agentdom.OnBusyQueue {
		return false, nil
	}
	return false, apierr.NewWithDetails(apierr.CodeAgentParallelismLimitReached,
		fmt.Sprintf("agent is already running %d/%d task(s)", running, limit),
		map[string]string{"running": strconv.Itoa(running), "limit": strconv.Itoa(limit)})
}

// checkFolderCapacity reports whether environmentID/folderID is free for a
// new conversation to start working in, independently of which agent it
// belongs to.
//
// This exists as its own constraint, separate from checkParallelismCapacity,
// because the per-agent limit alone doesn't protect a shared folder from
// every way it can actually be shared:
//   - Two different agents can have the same DefaultEnvironmentID (nothing
//     stops a project from pointing two agents at one persistent checkout).
//   - StartChatSession accepts an explicit environment_id/folder_id
//     override, so even one agent's own conversations can be aimed at a
//     folder that isn't its configured default — one this exact check has
//     never seen before and has no per-agent counter for.
//
// "Free" is not just an exact folderID match: the repository query behind
// CountRunningConversationsInFolder also counts a conversation running in
// any ANCESTOR or DESCENDANT of folderID within the same environment.
// environment_folders.path is an absolute filesystem path inside the
// environment's shared container — a parent folder and anything nested
// inside it are the same directory tree on disk, so a conversation running
// in the parent is already touching whatever a conversation about to start
// in the child would touch, and vice versa; treating them as unrelated
// slots would just let the exact-match version of this same problem back
// in one level up (or down) the tree. A conversation with no specific
// folder set (environment_folder_id NULL) is treated as spanning the whole
// environment — see folderOverlapPredicate on the repository side for the
// exact predicate.
//
// Both agent capacity and folder capacity must hold for a dispatch to
// proceed — see checkDispatchCapacity, which combines them. onBusy mirrors
// checkParallelismCapacity's contract exactly: OnBusyForce always returns
// (true, nil); OnBusyQueue returns (false, nil) instead of erroring so the
// caller holds the trigger in agent_pending_triggers rather than publishing
// it; "" (ask, the default) returns a non-nil *apierr.Error
// (CodeAgentEnvironmentFolderBusy) instead, with nothing created or
// mutated yet.
func (s *Service) checkFolderCapacity(ctx context.Context, environmentID uuid.UUID, folderID *uuid.UUID, onBusy string) (dispatchNow bool, err error) {
	if onBusy == agentdom.OnBusyForce {
		return true, nil
	}
	occupied, err := s.repo.CountRunningConversationsInFolder(ctx, environmentID, folderID)
	if err != nil {
		return false, err
	}
	if occupied == 0 {
		return true, nil
	}
	if onBusy == agentdom.OnBusyQueue {
		return false, nil
	}
	return false, apierr.NewWithDetails(apierr.CodeAgentEnvironmentFolderBusy,
		"another conversation is already running in this environment folder",
		map[string]string{"environment_id": environmentID.String()})
}

// checkDispatchCapacity is checkParallelismCapacity extended with the
// independent folder constraint checkFolderCapacity enforces — the single
// entry point every dispatch decision in this file should call instead of
// checkParallelismCapacity directly, so neither constraint can be
// accidentally skipped. envID nil (the default ephemeral per-conversation
// sandbox, or a global-chat conversation, which never has one at all) skips
// the folder check entirely — there's no shared folder to protect.
//
// The agent check runs first and short-circuits: if it already fails (or
// already produced the "ask" apierr.Error), that's returned as-is without
// ever touching the folder — same posture as checkFolderCapacity itself,
// just composed. This does mean a caller blocked by both constraints at
// once sees only the agent-limit message, never both; that's an acceptable
// simplification; either message correctly directs the human to the same
// on_busy=queue|force retry.
func (s *Service) checkDispatchCapacity(ctx context.Context, agentID uuid.UUID, envID, folderID *uuid.UUID, onBusy string) (dispatchNow bool, err error) {
	dispatchNow, err = s.checkParallelismCapacity(ctx, agentID, onBusy)
	if err != nil || !dispatchNow || envID == nil {
		return dispatchNow, err
	}
	return s.checkFolderCapacity(ctx, *envID, folderID, onBusy)
}

// flattenPayload narrows a publishTrigger-shaped payload to the flat string
// map agentdom.PendingTrigger.Payload stores — every value ever placed in
// one of these payloads is already a string (see publishChatTrigger et al.),
// so this never actually drops anything; the map[string]any typing exists
// only because AppendFlat's signature predates PendingTrigger.
func flattenPayload(payload map[string]any) map[string]string {
	out := make(map[string]string, len(payload))
	for k, v := range payload {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// claimQueuedForDispatch atomically flips convID from "queued" to "running"
// immediately before its trigger is actually published to
// StreamAgentTriggers — the last step before a conversation's trigger is
// handed to agent-runner, for every path that hasn't already claimed it via
// some other CAS (SendChatMessage/SendGlobalChatMessage's own
// ClaimConversationStatus calls in their paused/terminal resume branches
// already do this, so they pass needsClaim=false to deliverTrigger instead
// of calling this twice).
//
// This exists to close two races, both only possible because a "queued"
// conversation's status doesn't become "running" until agent-runner itself
// picks the trigger off the stream — an inherently asynchronous, unbounded
// delay relative to the moment services/api decides to publish:
//
//  1. worker.AgentQueueConsumer reads StreamAgentConversationStatus with
//     at-least-once delivery (a Valkey Streams consumer group). If it
//     crashes after AdvanceQueue successfully dispatches a pending trigger
//     but before acking that message, the same terminal-status event is
//     redelivered and AdvanceQueue runs again. Without this claim,
//     CountRunningConversations would still read the just-dispatched
//     conversation as "queued" (agent-runner hasn't reached it yet) and
//     the redelivery would dispatch a second one for the same single freed
//     slot. With it, the first dispatch's claim has already flipped that
//     conversation to "running" by the time any redelivery re-measures
//     capacity, so the redelivered call correctly sees no room.
//  2. StopConversation can run concurrently with AdvanceQueue dequeuing the
//     very conversation being stopped (DeletePendingTriggerByConversationID
//     blocks on the same row AdvanceQueue's SELECT ... FOR UPDATE SKIP
//     LOCKED already holds). Without a conditional claim, AdvanceQueue would
//     unconditionally publish the trigger regardless of what StopConversation
//     just did to the row. With it, whichever of the two actually reaches
//     the conversation's status column first wins: if StopConversation's
//     write to "stopped" lands first, this claim fails (current status
//     isn't "queued" anymore) and the trigger is correctly never published.
//
// A false return means exactly that — something else already moved convID
// out of "queued" between the capacity decision and now — so the caller
// must not publish.
func (s *Service) claimQueuedForDispatch(ctx context.Context, convID uuid.UUID) (bool, error) {
	return s.repo.ClaimConversationStatus(ctx, convID,
		string(agentdom.ConversationStatusQueued), string(agentdom.ConversationStatusRunning))
}

// deliverTrigger publishes topic/payload immediately if dispatchNow (the
// verdict a prior checkParallelismCapacity call already reached), otherwise
// persists it as a PendingTrigger for AdvanceQueue to replay once a running
// slot frees up — see PendingTrigger's doc comment.
//
// needsClaim must be true for a conversation still sitting at its
// just-created "queued" status (every fresh-create path: StartChatSession,
// StartGlobalChatSession, dispatchOrEnqueue's non-interactive triggers, and
// SendChatMessage/SendGlobalChatMessage's own conv==nil branch) — see
// claimQueuedForDispatch's doc comment for why. false for a conversation
// that has already been atomically claimed by the caller's own
// ClaimConversationStatus call (SendChatMessage/SendGlobalChatMessage's
// paused/terminal resume branches, which claim straight to "running" or
// "queued" depending on dispatchNow before ever reaching here) — claiming
// again here would simply fail (current status is already "running", not
// "queued") and wrongly suppress a publish that was already correctly
// authorized.
// envID/folderID are recorded on the PendingTrigger when dispatchNow is
// false so DequeueOldestPendingTriggerForFolder can later find this trigger
// by its target folder, not just by agent_id — see checkFolderCapacity's
// doc comment for why a trigger can be blocked by folder occupancy even
// when its own agent has room. nil for a trigger with no environment (the
// default ephemeral sandbox, or global chat).
func (s *Service) deliverTrigger(ctx context.Context, agentID, convID uuid.UUID, dispatchNow, needsClaim bool, topic string, payload map[string]any, envID, folderID *uuid.UUID) error {
	if dispatchNow {
		if needsClaim {
			claimed, err := s.claimQueuedForDispatch(ctx, convID)
			if err != nil {
				return err
			}
			if !claimed {
				// Something else (StopConversation, most likely) already
				// moved this conversation out of "queued" — never publish a
				// trigger for a conversation that's no longer waiting to
				// start.
				return nil
			}
		}
		return s.publishTrigger(ctx, topic, payload)
	}
	return s.repo.CreatePendingTrigger(ctx, &agentdom.PendingTrigger{
		ID:                  uuid.New(),
		AgentID:             agentID,
		ConversationID:      convID,
		Topic:               topic,
		Payload:             flattenPayload(payload),
		EnvironmentID:       envID,
		EnvironmentFolderID: folderID,
		CreatedAt:           time.Now(),
	})
}

// dispatchPendingTrigger resolves pending's conversation fresh, atomically
// claims it, and publishes its trigger — the common tail of AdvanceQueue and
// AdvanceFolderQueue, once each has separately confirmed pending is
// actually dispatchable (both its agent's ParallelismLimit and its target
// folder's occupancy, whichever axis that particular caller owns — see
// checkDispatchCapacity's doc comment on why the two are independent).
// Returns (true, nil) once genuinely dispatched, (false, nil) if the claim
// lost a race (see claimQueuedForDispatch) — the caller should treat that
// as "this item is gone for good, try something else," never as an error
// and never by putting it back.
func (s *Service) dispatchPendingTrigger(ctx context.Context, pending *agentdom.PendingTrigger) (bool, error) {
	conv, err := s.repo.FindConversationByID(ctx, pending.ConversationID)
	if err != nil {
		return false, err
	}
	// Re-resolve the conversation's environment/folder fresh rather than
	// trust the environment_id/workdir snapshot captured in pending.Payload
	// back when it was first queued — mirrors resolveWorkdirForConversation's
	// own doc comment ("needed on every trigger a conversation publishes,
	// not just the first"): the environment or folder it named could have
	// been deleted, or its workdir path changed, in however long this sat
	// in the backlog. Done before claiming below, same reasoning
	// SendChatMessage's own doc comment gives for validating workdir
	// resolution before its own ClaimConversationStatus call — a claim that
	// then failed resolution would otherwise be stuck at "running" with
	// nothing ever published to move it along.
	envID, workdir, err := s.resolveWorkdirForConversation(ctx, conv.ProjectID, conv)
	if err != nil {
		return false, err
	}
	claimed, err := s.claimQueuedForDispatch(ctx, pending.ConversationID)
	if err != nil {
		return false, err
	}
	if !claimed {
		return false, nil
	}
	payload := make(map[string]any, len(pending.Payload))
	for k, v := range pending.Payload {
		payload[k] = v
	}
	if envID != nil {
		payload["environment_id"] = envID.String()
		payload["workdir"] = workdir
	} else {
		delete(payload, "environment_id")
		delete(payload, "workdir")
	}
	return true, s.publishTrigger(ctx, pending.Topic, payload)
}

// requeueSkipped re-persists every PendingTrigger AdvanceQueue/
// AdvanceFolderQueue dequeued but decided NOT to dispatch this call (the
// other axis's constraint — folder occupancy for AdvanceQueue, agent
// capacity for AdvanceFolderQueue — wasn't satisfied). Each is reinserted
// with its original ID/CreatedAt/Payload unchanged, so it lands back at
// exactly its original FIFO position rather than losing its place in line.
//
// Why dequeue-then-maybe-reinsert instead of a non-destructive peek: a
// dequeued-but-not-yet-reinserted item is temporarily invisible to the next
// DequeueOldestPendingTrigger[ForFolder] call within the SAME loop — which
// is exactly what lets that next call reach a *different* item behind it
// instead of re-dequeuing the same stuck one forever (an oldest-first
// query would otherwise always return the same head-of-queue item again
// immediately after a plain "skip and continue"). Called from a defer so
// every return path — including an error return — still puts skipped items
// back rather than losing them.
func (s *Service) requeueSkipped(ctx context.Context, skipped []*agentdom.PendingTrigger, errp *error) {
	for _, p := range skipped {
		if err := s.repo.CreatePendingTrigger(ctx, p); err != nil && *errp == nil {
			*errp = err
		}
	}
}

// AdvanceQueue dispatches up to maxDispatch of agentID's queued conversations
// (agent_pending_triggers, oldest first), never exceeding its free
// running-slot count, and reports how many it actually dispatched.
//
// maxDispatch is the caller's own bound on how many slots just became free,
// since running (CountRunningConversations) never reflects a conversation
// this call just dispatched — agent-runner flips it to "running"
// asynchronously, only once it actually reads the trigger off the stream —
// so re-deriving free capacity from a fresh count on every loop iteration
// would understate how many are already spoken for and over-dispatch. Two
// callers: worker.AgentQueueConsumer passes 1 for every conversation of
// agentID's that reaches a terminal status (exactly one slot freed per
// event); UpdateAgent/UpdateGlobalAgent pass the exact size of a
// ParallelismLimit increase (freeing that many slots at once, with no
// per-slot event to react to individually). Safe to call speculatively —
// returns (0, nil) if nothing is queued or there's no free slot at all.
//
// This only advances agentID's OWN queue, gated by its own ParallelismLimit
// — a dequeued item whose target folder is occupied by a conversation
// belonging to some OTHER agent is set aside (see requeueSkipped) rather
// than dispatched; AdvanceFolderQueue is what re-tries those once that
// folder actually frees up, from whichever agent's queue they're sitting in.
func (s *Service) AdvanceQueue(ctx context.Context, agentID uuid.UUID, maxDispatch int) (dispatched int, err error) {
	if maxDispatch <= 0 {
		return 0, nil
	}
	agent, err := s.repo.FindAgentByID(ctx, agentID)
	if err != nil {
		return 0, err
	}
	limit := effectiveParallelismLimit(agent)
	running, err := s.repo.CountRunningConversations(ctx, agentID)
	if err != nil {
		return 0, err
	}

	var skipped []*agentdom.PendingTrigger
	// Wrapped in a closure, not `defer s.requeueSkipped(ctx, skipped, &err)`
	// directly: a bare defer call evaluates its arguments immediately, which
	// would capture skipped's value right here (still empty) rather than
	// whatever the loop below eventually appends to it. The closure defers
	// reading skipped until the function actually returns.
	defer func() { s.requeueSkipped(ctx, skipped, &err) }()

	for dispatched < maxDispatch && running+dispatched < limit {
		pending, dequeueErr := s.repo.DequeueOldestPendingTrigger(ctx, agentID)
		if dequeueErr != nil {
			return dispatched, dequeueErr
		}
		if pending == nil {
			return dispatched, nil
		}

		if pending.EnvironmentID != nil {
			folderFree, capErr := s.checkFolderCapacity(ctx, *pending.EnvironmentID, pending.EnvironmentFolderID, agentdom.OnBusyQueue)
			if capErr != nil {
				return dispatched, capErr
			}
			if !folderFree {
				// Occupied by some other agent's conversation right now —
				// not this agent's queue to solve; AdvanceFolderQueue will
				// retry this exact item once that folder frees up.
				skipped = append(skipped, pending)
				continue
			}
		}

		ok, dispatchErr := s.dispatchPendingTrigger(ctx, pending)
		if dispatchErr != nil {
			return dispatched, dispatchErr
		}
		if !ok {
			continue
		}
		dispatched++
	}
	return dispatched, nil
}

// AdvanceFolderQueue dispatches at most one conversation waiting on
// environmentID/folderID once it becomes free — the folder-occupancy
// counterpart to AdvanceQueue's per-agent counter, called whenever a
// conversation attached to a static environment reaches a terminal status
// (alongside that conversation's own AdvanceQueue call — see
// worker.AgentQueueConsumer.handle), since whichever agent is queued
// waiting on the now-free folder might not be the same agent whose
// conversation just vacated it.
//
// Unlike AdvanceQueue there's no maxDispatch/limit parameter: a folder can
// only ever host one running conversation at a time (checkFolderCapacity's
// whole point), so at most one dispatch out of this call is ever
// meaningful regardless of how many terminal events arrive. Returns whether
// it actually dispatched something.
func (s *Service) AdvanceFolderQueue(ctx context.Context, environmentID uuid.UUID, folderID *uuid.UUID) (dispatchedOne bool, err error) {
	var skipped []*agentdom.PendingTrigger
	// Wrapped in a closure, not `defer s.requeueSkipped(ctx, skipped, &err)`
	// directly: a bare defer call evaluates its arguments immediately, which
	// would capture skipped's value right here (still empty) rather than
	// whatever the loop below eventually appends to it. The closure defers
	// reading skipped until the function actually returns.
	defer func() { s.requeueSkipped(ctx, skipped, &err) }()

	for {
		// DequeueOldestPendingTriggerForFolder matches by path overlap (see
		// folderOverlapPredicate on the repository side), so a candidate it
		// returns can legitimately name a *different* folder than
		// folderID — a parent, a child, or the same one. That means its own
		// occupancy can't be assumed free just because folderID itself is:
		// two unrelated siblings both nested under folderID neither overlap
		// each other nor need to wait on one another, so this re-checks
		// each candidate's own folder fresh instead of gating the whole
		// call on folderID's — a single upfront check here would wrongly
		// skip a genuinely dispatchable sibling whenever folderID's parent
		// scope also happens to overlap some unrelated still-running
		// conversation.
		pending, dequeueErr := s.repo.DequeueOldestPendingTriggerForFolder(ctx, environmentID, folderID)
		if dequeueErr != nil {
			return dispatchedOne, dequeueErr
		}
		if pending == nil {
			return dispatchedOne, nil
		}

		folderFree, capErr := s.checkFolderCapacity(ctx, environmentID, pending.EnvironmentFolderID, agentdom.OnBusyQueue)
		if capErr != nil {
			return dispatchedOne, capErr
		}
		if !folderFree {
			// Something else still overlaps THIS candidate's own folder
			// (not necessarily folderID) — leave it queued.
			skipped = append(skipped, pending)
			continue
		}

		agentOK, agentErr := s.checkParallelismCapacity(ctx, pending.AgentID, agentdom.OnBusyQueue)
		if agentErr != nil {
			return dispatchedOne, agentErr
		}
		if !agentOK {
			// Its folder is free, but the item's own agent isn't right
			// now — not this call's constraint to solve; that agent's own
			// AdvanceQueue call will retry this exact item once ITS slot
			// frees up.
			skipped = append(skipped, pending)
			continue
		}

		ok, dispatchErr := s.dispatchPendingTrigger(ctx, pending)
		if dispatchErr != nil {
			return dispatchedOne, dispatchErr
		}
		if !ok {
			continue
		}
		return true, nil
	}
}

// dispatchOrEnqueue combines checkParallelismCapacity and deliverTrigger for
// every non-interactive trigger (task_assigned, comment_mention,
// description_write, automation_message) — there's no human synchronously
// waiting for a reply on any of these, so it's always fine to silently queue
// (agentdom.OnBusyQueue) rather than ask.
// envID/folderID are the conversation's already-resolved environment/folder
// (nil for the default ephemeral sandbox) — passed through to
// checkDispatchCapacity so a shared folder blocks dispatch the same way an
// agent-at-capacity does, and recorded on the PendingTrigger if this ends
// up queued (see deliverTrigger's doc comment).
func (s *Service) dispatchOrEnqueue(ctx context.Context, agentID, convID uuid.UUID, topic string, payload map[string]any, envID, folderID *uuid.UUID) error {
	dispatchNow, err := s.checkDispatchCapacity(ctx, agentID, envID, folderID, agentdom.OnBusyQueue)
	if err != nil {
		return err
	}
	// needsClaim=true: every caller of dispatchOrEnqueue just created convID
	// fresh (still "queued"), never claimed by anything else yet.
	return s.deliverTrigger(ctx, agentID, convID, dispatchNow, true, topic, payload, envID, folderID)
}

// environmentID/workdir, when non-nil/non-empty, tell agent-runner which
// static environment (and folder within it) this conversation is attached
// to — see resolveChatEnvironment/resolveWorkdirForConversation's doc
// comments for how callers resolve them, and
// docs/ai-agent/environment-management.md's "Conversation attach path"
// section for how agent-runner's decode.go/coldStartEnvironment consume
// them.
// needsClaim: see deliverTrigger's doc comment — true from StartChatSession
// and SendChatMessage's own conv==nil branch (a freshly-created, never
// claimed conversation), false from SendChatMessage's paused/terminal
// resume branches (already atomically claimed by their own
// ClaimConversationStatus call before reaching here).
// folderID is environmentID's resolved folder — nil whenever environmentID
// is, and otherwise threaded through to deliverTrigger purely so a queued
// trigger records which folder it's waiting on (see that method's doc
// comment); it plays no role in the payload itself, which only ever named
// the resolved workdir path.
func (s *Service) publishChatTrigger(ctx context.Context, agentID, convID, sessionID, projectID, memberID uuid.UUID, message string, repoPluginIDs []string, environmentID, folderID *uuid.UUID, workdir string, contextItems []agentdom.ContextItemRef, dispatchNow, needsClaim bool) error {
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
	return s.deliverTrigger(ctx, agentID, convID, dispatchNow, needsClaim, events.TopicAgentChatMessage, payload, environmentID, folderID)
}

// publishGlobalChatTrigger is publishChatTrigger's global-chat sibling — no
// project_id, actor identified by actor_user_id, and repo_plugin_ids
// omitted entirely (repo/PR tools are excluded from global-chat
// conversations; see the Global Conversations section's doc comment).
// needsClaim: see publishChatTrigger's identical doc comment.
func (s *Service) publishGlobalChatTrigger(ctx context.Context, agentID, convID, sessionID, actorUserID uuid.UUID, message string, contextItems []agentdom.ContextItemRef, dispatchNow, needsClaim bool) error {
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
	// envID/folderID both nil: global chat never attaches to a static
	// environment (a global-scope agent can't have DefaultEnvironmentID —
	// see Agent.DefaultEnvironmentID's doc comment — and StartGlobalChatSession
	// has no per-conversation override for it either).
	return s.deliverTrigger(ctx, agentID, convID, dispatchNow, needsClaim, events.TopicAgentChatMessage, payload, nil, nil)
}
