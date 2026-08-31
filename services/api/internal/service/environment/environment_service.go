// Package environmentsvc implements the Environment application service —
// the use-case layer for static environments (see
// docs/ai-agent/environment-management.md). Talks to agent-runner over two
// transports: an HTTP client for its fast, bounded calls (stop/delete/
// folders/browse/ssh-keys-sync/port-forwards-assign), and
// StreamAgentEnvironmentCommands (see callEnvironmentCommand) for the 3
// that wait on a Pod/container becoming ready (create/start/restart-ports)
// — see aiAgentProvisionHTTPTimeout's doc comment for why those don't fit
// a synchronous HTTP call well.
package environmentsvc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/secret"
)

// Default resource limits, mirroring migration 000042_add_environments.sql's
// column defaults — CreateEnvironmentInput's CPULimit/MemoryLimit/DiskLimitGB
// are optional overrides, but every column is written explicitly on insert
// (see that input type's doc comment), so the service — not the DB default —
// is what actually applies these when the caller doesn't override them.
const (
	defaultCPULimit           = "2"
	defaultMemoryLimit        = "4Gi"
	defaultDiskLimitGB        = 20
	defaultIdleTimeoutMinutes = 60
)

// aiAgentHTTPTimeout bounds every fast call this service makes into
// agent-runner so a slow or wedged agent-runner instance can't hang the
// calling request indefinitely — mirrors handler.aiAgentHTTPTimeout's
// rationale for the existing agent-runner calls in agent_handler.go.
const aiAgentHTTPTimeout = 30 * time.Second

// aiAgentProvisionHTTPTimeout bounds the three agent-runner calls that can
// bring up a Pod/container from a cold (0-replica/never-started) state:
// create (ExecuteCreate), plain start (ExecuteStart), and restart-ports
// (restartEnvironmentPorts, reached both from ExecuteStart's
// PortsPendingRestart branch and from RestartEnvironment). The kubernetes
// backend's own documented worst case for that work is podReadyTimeout
// (60s) + readyTimeout (120s) + dindReadyTimeout (90s, DockerEnabled
// only) — see internal/sandbox/k8s/manager.go, sandbox/sandbox.go, and
// internal/sandbox/k8s/dind.go — well past aiAgentHTTPTimeout's budget,
// which is sized for this service's other, fast calls (stop/delete/
// folders/browse/ssh-keys/port-forwards) instead. That budget mismatch is
// exactly why these three don't go through callInternal/s.httpClient at
// all — see callEnvironmentCommand — this constant now bounds a BRPop
// wait on agent-runner's own reply, not an http.Client.Timeout. Also
// used, slightly enlarged, as agent-runner's own per-command context
// timeout (see messaging.EnvironmentCommandConsumer), so agent-runner
// never self-cancels an in-flight SandboxMgr call at the exact moment
// this side's own wait would already have given up.
//
// ExecuteCreate and ExecuteStart run only from
// worker.EnvironmentCommandConsumer, never on a live request path;
// RestartEnvironment does sit on one, but in the common case (kubernetes,
// environment already running) agent-runner's own
// RestartEnvironmentPorts never touches the Pod and returns almost
// immediately — this ceiling only matters as a worst-case bound.
const aiAgentProvisionHTTPTimeout = 5 * time.Minute

// maxSlugAttempts bounds the collision-retry loop below so a pathological
// run of collisions can't spin forever.
const maxSlugAttempts = 50

// slugNonAlnum matches any run of characters that isn't a lowercase letter
// or digit — collapsed to a single '-' by slugify.
var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// Service is the concrete Environment service.
type Service struct {
	repo environmentdom.Repository
	// encryptor encrypts/decrypts each environment's secret_key_encrypted —
	// the same AES-256-GCM Encryptor instance agentsvc.Service uses for
	// agents.llm_api_key_secret (see WithEncryptor).
	encryptor *secret.Encryptor
	// aiAgentURL/aiAgentInternalKey are the same AI_AGENT_URL/
	// AI_AGENT_INTERNAL_KEY values agent_handler.go already uses to call
	// agent-runner's other internal-only routes — reused, not duplicated
	// (see this package's doc comment).
	aiAgentURL         string
	aiAgentInternalKey string
	httpClient         *http.Client
	// redisClient backs callEnvironmentCommand's BRPop wait on
	// agent-runner's reply — see that method's own doc comment. Required
	// only for the 3 calls that use it (create/start/restart-ports); every
	// other method works fine with this left nil (tests that never reach
	// those 3 have no need to set it, mirroring encryptor's identical
	// "nil is fine unless you need it" contract).
	redisClient *redis.Client
	// publisher queues StartEnvironment's own actual agent-runner call onto
	// StreamEnvironmentCommands instead of making it on the request path —
	// see WithPublisher and StartEnvironment's own doc comment. Also
	// enqueues onto StreamAgentEnvironmentCommands (see
	// callEnvironmentCommand) and publishes environment.status_changed for
	// services/realtime — see publishStatusChanged.
	publisher environmentPublisher
}

// environmentPublisher is the minimal messaging.Publisher surface this
// service needs, narrowed to keep it mockable in tests without a real
// Valkey connection — *messaging.Publisher satisfies this directly, no
// adapter needed. Append queues a slow agent-runner call onto
// StreamEnvironmentCommands (see WithPublisher); AppendFlat enqueues a
// command onto StreamAgentEnvironmentCommands (see callEnvironmentCommand);
// Publish sends the lightweight environment.status_changed notification to
// ChannelRealtime (see publishStatusChanged).
type environmentPublisher interface {
	Append(ctx context.Context, stream, eventType string, payload any) error
	AppendFlat(ctx context.Context, stream string, fields map[string]any) error
	Publish(ctx context.Context, channel string, payload any) error
}

// New returns a configured Environment service.
func New(repo environmentdom.Repository, aiAgentURL, aiAgentInternalKey string) *Service {
	return &Service{
		repo:               repo,
		aiAgentURL:         aiAgentURL,
		aiAgentInternalKey: aiAgentInternalKey,
		httpClient:         &http.Client{Timeout: aiAgentHTTPTimeout},
	}
}

// WithEncryptor configures AES-256-GCM encryption for each environment's
// persisted secret key, mirroring agentsvc.Service.WithEncryptor.
func (s *Service) WithEncryptor(enc *secret.Encryptor) *Service {
	s.encryptor = enc
	return s
}

// WithRedisClient wires the raw Valkey client callEnvironmentCommand
// BRPops agent-runner's reply from — see that method's own doc comment.
// Distinct from WithPublisher's Publisher (which only ever appends/
// publishes, never blocks waiting on a reply) because BRPop has no
// equivalent on that narrower interface.
func (s *Service) WithRedisClient(c *redis.Client) *Service {
	s.redisClient = c
	return s
}

// WithPublisher wires the shared Valkey Streams publisher StartEnvironment
// uses to queue its own execution — see StreamEnvironmentCommands' own doc
// comment. Required for StartEnvironment to work; every other method on
// Service functions without it.
func (s *Service) WithPublisher(p environmentPublisher) *Service {
	s.publisher = p
	return s
}

// setStatus persists an environment's status via UpdateEnvironmentStatus and
// publishes environment.status_changed so services/realtime can notify
// connected clients — see publishStatusChanged. Every status transition in
// this file goes through here (or, for ExecuteCreate's success path,
// UpdateEnvironmentProvisioning followed by a direct publishStatusChanged
// call) instead of calling the repo directly, so no transition is silently
// missed.
func (s *Service) setStatus(ctx context.Context, projectID, environmentID uuid.UUID, status string, backendRef, errMsg *string) error {
	if err := s.repo.UpdateEnvironmentStatus(ctx, environmentID, status, backendRef, errMsg); err != nil {
		return err
	}
	s.publishStatusChanged(ctx, projectID, environmentID, status, errMsg)
	return nil
}

// publishStatusChanged sends environment.status_changed to ChannelRealtime
// so services/realtime can fan it out to clients viewing projectID's
// environments — the socket-driven replacement for the frontend's old
// fixed-interval polling of GET .../environments/:id while a transition was
// in flight. Best-effort: a missed publish just means the affected client's
// view goes stale until its next manual action, the same posture as every
// other service's realtime notification in this codebase (e.g.
// sprintsvc.Service.publish).
func (s *Service) publishStatusChanged(ctx context.Context, projectID, environmentID uuid.UUID, status string, errMsg *string) {
	if s.publisher == nil {
		return
	}
	payload := map[string]any{
		"project_id":     projectID.String(),
		"environment_id": environmentID.String(),
		"status":         status,
	}
	if errMsg != nil {
		payload["error_message"] = *errMsg
	}
	_ = s.publisher.Publish(ctx, events.ChannelRealtime, map[string]any{
		"type":    events.TopicEnvironmentStatusChanged,
		"payload": payload,
	})
}

// encryptSecret/decryptSecret fall back to plaintext when no encryptor is
// configured — same posture as agentsvc.Service.encryptKey (see its doc
// comment): not fatal, since some self-hosted deployments have no
// ENCRYPTION_KEY set at all, but the value is never silently lost either
// way.
func (s *Service) encryptSecret(plaintext string) (string, error) {
	if s.encryptor == nil {
		return plaintext, nil
	}
	return s.encryptor.Encrypt(plaintext)
}

func (s *Service) decryptSecret(stored string) (string, error) {
	if s.encryptor == nil {
		return stored, nil
	}
	return s.encryptor.Decrypt(stored)
}

// -------------------------------------------------------------------------
// Environments
// -------------------------------------------------------------------------

// ListEnvironments returns every environment in a project.
func (s *Service) ListEnvironments(ctx context.Context, projectID uuid.UUID) ([]*environmentdom.Environment, error) {
	return s.repo.ListEnvironments(ctx, projectID)
}

// GetEnvironment returns a single environment visible in projectID, with
// its Folders populated. Unlike FindVisibleEnvironmentInProject — used
// internally by every other operation in this file (Start/Stop/AddFolder/
// etc.), none of which need Folders and shouldn't pay for the extra
// query — this is the one path a client actually reads Folders back from
// (see dto.EnvironmentFromEntity), so the fetch-then-attach happens here
// rather than inside FindVisibleEnvironmentInProject itself.
func (s *Service) GetEnvironment(ctx context.Context, projectID, environmentID uuid.UUID) (*environmentdom.Environment, error) {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	folders, err := s.repo.ListFolders(ctx, env.ID)
	if err != nil {
		return nil, err
	}
	env.Folders = folders
	return env, nil
}

// CreateEnvironment writes the environment row (status StatusCreating) and
// queues the actual agent-runner provisioning call onto
// StreamEnvironmentCommands for worker.EnvironmentCommandConsumer to
// execute (via ExecuteCreate below) — same reasoning as StartEnvironment's
// own doc comment: provisioning a real container/Pod and volume can take
// longer than an HTTP request should stay open for, so this method itself
// never calls agent-runner and returns as soon as the row exists and the
// command is durably queued. The row stays StatusCreating until
// ExecuteCreate updates it to StatusRunning (persisting the backend/
// backend_ref/volume_ref agent-runner reports back) or StatusError (with
// ErrorMessage set) — either transition publishes environment.status_changed
// (see publishStatusChanged) so the frontend learns the outcome without
// polling. See environmentdom.EnvironmentService's doc comment.
func (s *Service) CreateEnvironment(ctx context.Context, projectID uuid.UUID, in environmentdom.CreateEnvironmentInput) (*environmentdom.Environment, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, environmentdom.ErrEnvironmentNameInvalid
	}

	slug, err := s.generateUniqueSlug(ctx, projectID, name)
	if err != nil {
		return nil, fmt.Errorf("generate environment slug: %w", err)
	}

	secretKeyPlain, err := randomHex(32)
	if err != nil {
		return nil, fmt.Errorf("generate environment secret key: %w", err)
	}
	secretKeyEncrypted, err := s.encryptSecret(secretKeyPlain)
	if err != nil {
		return nil, fmt.Errorf("encrypt environment secret key: %w", err)
	}

	cpuLimit := defaultCPULimit
	if in.CPULimit != nil && *in.CPULimit != "" {
		if err := validateCPULimit(*in.CPULimit); err != nil {
			return nil, err
		}
		cpuLimit = *in.CPULimit
	}
	memoryLimit := defaultMemoryLimit
	if in.MemoryLimit != nil && *in.MemoryLimit != "" {
		if err := validateMemoryLimit(*in.MemoryLimit); err != nil {
			return nil, err
		}
		memoryLimit = *in.MemoryLimit
	}
	diskLimitGB := defaultDiskLimitGB
	if in.DiskLimitGB != nil && *in.DiskLimitGB > 0 {
		diskLimitGB = *in.DiskLimitGB
	}

	now := time.Now()
	env := &environmentdom.Environment{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      name,
		Slug:      slug,
		Status:    environmentdom.StatusCreating,
		// Backend is a provisional placeholder: the environments.backend
		// column is NOT NULL/CHECK(backend IN ('docker','kubernetes')), but
		// the real value is chosen by agent-runner (whichever backend it's
		// configured for — see Environment.Backend's doc comment) and only
		// known once its response to POST /internal/environments comes
		// back. Corrected below via UpdateEnvironmentProvisioning the
		// moment that response arrives; a failure before then leaves this
		// placeholder on an already-StatusError row, which is harmless.
		Backend:            environmentdom.BackendDocker,
		Image:              in.Image,
		CPULimit:           cpuLimit,
		MemoryLimit:        memoryLimit,
		DiskLimitGB:        diskLimitGB,
		DockerEnabled:      in.DockerEnabled,
		SecretKeyEncrypted: secretKeyEncrypted,
		IdleTimeoutMinutes: defaultIdleTimeoutMinutes,
		LastActiveAt:       now,
		CreatedBy:          in.CreatedBy,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.repo.CreateEnvironment(ctx, env); err != nil {
		return nil, fmt.Errorf("create environment row: %w", err)
	}

	if err := s.publisher.Append(ctx, events.StreamEnvironmentCommands, events.TopicEnvironmentCreate, map[string]any{
		"environment_id": env.ID.String(),
	}); err != nil {
		// The row already exists but nothing will ever provision it now —
		// leaving it StatusCreating would strand it there forever instead
		// of surfacing the failure, the same reasoning ExecuteStart/
		// ExecuteStop apply to their own "can't proceed" cases.
		errMsg := fmt.Sprintf("queue environment create: %s", err)
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
		return nil, fmt.Errorf("queue environment create: %w", err)
	}
	return env, nil
}

// ExecuteCreate performs the actual (potentially slow) work CreateEnvironment
// used to do inline: ask agent-runner to provision environmentID's backing
// container/Pod and volume, then persist the resulting backend/backend_ref/
// volume_ref. Called only by worker.EnvironmentCommandConsumer once it
// reads the command CreateEnvironment queued — see ExecuteStart's own doc
// comment for why this isn't part of environmentdom.Service.
//
// Re-reads environmentID fresh rather than threading its input through the
// queued command: every field CreateEnvironment resolved (name, limits,
// image, the encrypted secret key) is already persisted on the row by the
// time this runs, the same "the row is the source of truth, not the
// message" approach ExecuteStart/ExecuteStop already take — notably this
// also means the plaintext secret key never has to pass through Valkey at
// all, only its encrypted form (already on the row) and the decryption
// key this process already holds.
func (s *Service) ExecuteCreate(ctx context.Context, environmentID uuid.UUID) error {
	env, err := s.repo.FindEnvironmentByID(ctx, environmentID)
	if err != nil {
		return err
	}
	if env.Status != environmentdom.StatusCreating {
		// A stale replay of an already-superseded command — e.g. this
		// message's ack failed and worker.EnvironmentCommandConsumer's
		// processPending redelivered it from the PEL after a later action
		// already moved the row past StatusCreating. Nothing to do: acting
		// on it now would send agent-runner a create command a second time
		// against a row it (or a retry of CreateEnvironment) has already
		// resolved one way or the other.
		return nil
	}

	secretKeyPlain, err := s.decryptSecret(env.SecretKeyEncrypted)
	if err != nil {
		errMsg := fmt.Sprintf("decrypt environment secret key: %s", err)
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
		return fmt.Errorf("decrypt environment secret key: %w", err)
	}

	reqBody := internalCreateEnvironmentRequest{
		EnvironmentID: env.ID.String(),
		ProjectID:     env.ProjectID.String(),
		CPULimit:      env.CPULimit,
		MemoryLimit:   env.MemoryLimit,
		DiskLimitGB:   env.DiskLimitGB,
		DockerEnabled: env.DockerEnabled,
		SecretKey:     secretKeyPlain,
	}
	if env.Image != nil {
		reqBody.Image = *env.Image
	}

	var respBody internalCreateEnvironmentResponse
	if err := s.callEnvironmentCommand(ctx, events.EnvironmentCommandCreate, reqBody, &respBody); err != nil {
		errMsg := err.Error()
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
		return fmt.Errorf("agent-runner: create environment: %w", err)
	}

	if err := s.repo.UpdateEnvironmentProvisioning(ctx, env.ID,
		environmentdom.StatusRunning, respBody.Backend, respBody.BackendRef, respBody.VolumeRef); err != nil {
		// agent-runner has already provisioned the container/Pod and volume
		// by this point — leaving the row at StatusCreating here would
		// strand it forever: StartEnvironment/StopEnvironment both reject
		// anything that isn't Stopped/Suspended/Error/Running, and the idle
		// reaper only ever looks at StatusRunning rows, so nothing would
		// notice or recover it. Mark StatusError instead so it's at least
		// visible: BackendRef/VolumeRef are still lost (this same write is
		// what would have persisted them), so the freshly-provisioned
		// container/Pod and volume are orphaned on agent-runner's side —
		// a known gap, not solved here — but the row itself no longer sits
		// invisibly stuck.
		errMsg := fmt.Sprintf("persist environment provisioning: %s", err)
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
		return fmt.Errorf("persist environment provisioning: %w", err)
	}
	s.publishStatusChanged(ctx, env.ProjectID, env.ID, environmentdom.StatusRunning, nil)
	return nil
}

// UpdateEnvironment patches mutable environment fields (name,
// idle_timeout_minutes).
func (s *Service) UpdateEnvironment(ctx context.Context, projectID, environmentID uuid.UUID, in environmentdom.UpdateEnvironmentInput) (*environmentdom.Environment, error) {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, environmentdom.ErrEnvironmentNameInvalid
		}
		env.Name = name
	}
	if in.IdleTimeoutMinutes != nil {
		v := *in.IdleTimeoutMinutes
		if v <= 0 {
			v = defaultIdleTimeoutMinutes
		}
		env.IdleTimeoutMinutes = v
	}
	env.UpdatedAt = time.Now()
	if err := s.repo.UpdateEnvironment(ctx, env); err != nil {
		return nil, err
	}
	return env, nil
}

// StartEnvironment validates that environmentID is eligible to start and
// queues the actual agent-runner call onto StreamEnvironmentCommands for
// worker.EnvironmentCommandConsumer to execute (via ExecuteStart below) —
// this method itself never calls agent-runner, and returns as soon as the
// command is durably queued. That work used to happen synchronously here,
// which meant this call's latency was however long the real container/Pod
// took to come up; a legitimately slow start could run past the HTTP
// server's own WriteTimeout, which the gateway then reported to the
// caller as a 502 for an operation that was actually still succeeding
// server-side a few seconds later. No-op if already running.
func (s *Service) StartEnvironment(ctx context.Context, projectID, environmentID uuid.UUID) (*environmentdom.Environment, error) {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	if env.Status == environmentdom.StatusRunning {
		return env, nil
	}
	if env.Status != environmentdom.StatusStopped && env.Status != environmentdom.StatusSuspended && env.Status != environmentdom.StatusError {
		return nil, environmentdom.ErrEnvironmentBusy
	}
	if env.BackendRef == nil {
		return nil, fmt.Errorf("environment %s has never been provisioned (no backend_ref)", env.ID)
	}
	// Cheap, already-loaded-row check — kept synchronous for immediate
	// feedback since it costs no extra I/O, unlike the agent-runner call
	// this same precondition used to gate inline (restartEnvironmentPorts
	// is now only reached from ExecuteStart, which re-checks this too:
	// see its own doc comment for why).
	if env.PortsPendingRestart && env.VolumeRef == nil {
		return nil, fmt.Errorf("environment %s has never been provisioned (no volume_ref)", env.ID)
	}

	// Status set BEFORE queuing, not after: worker.EnvironmentCommandConsumer
	// can pick up and finish the command as soon as it's queued — if the
	// order were reversed, a fast enough worker could persist StatusRunning
	// before this request's own StatusStarting write ran, and that write
	// would then silently overwrite it back to StatusStarting, permanently
	// (nothing else would ever move it off StatusStarting again). Queuing
	// second means a failure there is reported as StatusError instead —
	// see the fallback below — rather than corrupting a state a concurrent
	// worker has already legitimately advanced past.
	if err := s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusStarting, nil, nil); err != nil {
		return nil, err
	}
	if err := s.publisher.Append(ctx, events.StreamEnvironmentCommands, events.TopicEnvironmentStart, map[string]any{
		"environment_id": env.ID.String(),
	}); err != nil {
		// Nothing will ever execute this start now — leaving the row at
		// StatusStarting (just persisted above) would strand it there
		// forever the same way an unhandled queue failure would elsewhere
		// in this file (see CreateEnvironment's identical fallback).
		errMsg := fmt.Sprintf("queue environment start: %s", err)
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
		return nil, fmt.Errorf("queue environment start: %w", err)
	}
	env.Status = environmentdom.StatusStarting
	env.ErrorMessage = nil
	return env, nil
}

// ExecuteStart performs the actual (potentially slow) work StartEnvironment
// used to do inline: ask agent-runner to start environmentID's backing
// container/Pod, or — if a port forward changed while it was stopped —
// recreate it via restartEnvironmentPorts instead, the same branch
// StartEnvironment used to take synchronously. Called only by
// worker.EnvironmentCommandConsumer once it reads the command
// StartEnvironment queued. Deliberately not part of environmentdom.Service:
// it has no projectID scoping of its own (environmentID alone identifies a
// globally unique row) and isn't a REST operation, just this service's own
// deferred continuation of StartEnvironment.
//
// Re-reads environmentID fresh rather than trusting any state captured
// when the command was queued, since an environment can be stopped,
// deleted, or have its ports changed again in the time between queuing
// and execution — and re-validates BackendRef/VolumeRef even though
// StartEnvironment already checked them, since a nil pointer here would
// otherwise panic this goroutine rather than fail the one environment
// being started.
func (s *Service) ExecuteStart(ctx context.Context, environmentID uuid.UUID) error {
	env, err := s.repo.FindEnvironmentByID(ctx, environmentID)
	if err != nil {
		return err
	}
	if env.Status != environmentdom.StatusStarting {
		// A stale replay of an already-superseded command (its ack failed
		// and processPending redelivered it from the PEL after a later
		// action already moved the row past StatusStarting) — most
		// importantly, the user may have since stopped this environment on
		// purpose; blindly sending agent-runner a start command here would
		// start it back up out from under them. Whatever the current status is,
		// it reflects a command that ran after this one queued, so this one
		// has nothing left to do.
		return nil
	}
	if env.BackendRef == nil {
		errMsg := fmt.Sprintf("environment %s has never been provisioned (no backend_ref)", env.ID)
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
		return errors.New(errMsg)
	}

	if env.PortsPendingRestart {
		// See StartEnvironment's identical comment on this branch: applying
		// the environment's current full port-mapping set is safe here
		// rather than just plain-starting, since nothing is serving live
		// traffic yet.
		if env.VolumeRef == nil {
			errMsg := fmt.Sprintf("environment %s has never been provisioned (no volume_ref)", env.ID)
			_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
			return errors.New(errMsg)
		}
		_, err := s.restartEnvironmentPorts(ctx, env)
		return err
	}

	secretKeyPlain, err := s.decryptSecret(env.SecretKeyEncrypted)
	if err != nil {
		errMsg := fmt.Sprintf("decrypt environment secret key: %s", err)
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
		return fmt.Errorf("decrypt environment secret key: %w", err)
	}
	reqBody := internalStartEnvironmentRequest{
		EnvironmentID: env.ID.String(),
		BackendRef:    *env.BackendRef,
		CPULimit:      env.CPULimit,
		MemoryLimit:   env.MemoryLimit,
		DiskLimitGB:   env.DiskLimitGB,
		DockerEnabled: env.DockerEnabled,
		SecretKey:     secretKeyPlain,
	}
	if env.Image != nil {
		reqBody.Image = *env.Image
	}

	var respBody internalStartEnvironmentResponse
	if err := s.callEnvironmentCommand(ctx, events.EnvironmentCommandStart, reqBody, &respBody); err != nil {
		errMsg := err.Error()
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
		return fmt.Errorf("agent-runner: start environment: %w", err)
	}

	// respBody.BackendRef is only non-empty when agent-runner had to
	// self-heal a container removed outside of Paca — see
	// internalStartEnvironmentResponse.BackendRef's own doc comment. Same
	// "persist only if it actually changed" handling restartEnvironmentPorts
	// already uses for its own (differently triggered) backend_ref change.
	var newBackendRef *string
	if respBody.BackendRef != "" && respBody.BackendRef != *env.BackendRef {
		newBackendRef = &respBody.BackendRef
	}
	if err := s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusRunning, newBackendRef, nil); err != nil {
		// agent-runner has already started the backend by this point —
		// leaving the row at StatusStarting here would strand it forever
		// (see StartEnvironment's identical reasoning for setting status
		// before queuing). Mark StatusError instead: Error is one of
		// StartEnvironment's accepted states, so the user can retry, and
		// retrying is safe here specifically because agent-runner's start
		// command against an already-running backend is a no-op, not a
		// second start.
		errMsg := fmt.Sprintf("persist running status: %s", err)
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, newBackendRef, &errMsg)
		return err
	}
	// Best-effort like the repo's other bookkeeping-only calls (e.g.
	// InvalidateMembersCache) — see StartEnvironment's superseded version
	// of this same comment for the idle-reaper reasoning. The backend is
	// already started and status already persisted as running by this
	// point, so a touch failure here must not be reported as this
	// execution having failed.
	_ = s.repo.TouchEnvironment(ctx, env.ID)
	return nil
}

// RestartEnvironment applies any pending port-forward changes to a
// currently-running environment's backing container/Pod — see
// environmentdom.EnvironmentService's own doc comment for why this is a
// separate action from StartEnvironment.
func (s *Service) RestartEnvironment(ctx context.Context, projectID, environmentID uuid.UUID) (*environmentdom.Environment, error) {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	if env.Status != environmentdom.StatusRunning {
		return nil, environmentdom.ErrEnvironmentBusy
	}
	if env.BackendRef == nil || env.VolumeRef == nil {
		return nil, fmt.Errorf("environment %s has never been provisioned (no backend_ref/volume_ref)", env.ID)
	}
	return s.restartEnvironmentPorts(ctx, env)
}

// restartEnvironmentPorts sends agent-runner a restart-ports command to
// apply env's current full port-mapping set (SSH port plus every
// environment_port_forwards row) to its backing container/Pod, persists
// the resulting backend_ref (docker recreates the container, so it may
// have changed; kubernetes never touches the Pod, so it won't) and clears
// PortsPendingRestart, and returns the updated environment. Shared by
// StartEnvironment (when env.PortsPendingRestart is true) and
// RestartEnvironment — both env.BackendRef and env.VolumeRef must already
// be non-nil by the time either caller reaches this.
func (s *Service) restartEnvironmentPorts(ctx context.Context, env *environmentdom.Environment) (*environmentdom.Environment, error) {
	secretKeyPlain, err := s.decryptSecret(env.SecretKeyEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt environment secret key: %w", err)
	}
	reqBody := internalRestartPortsRequest{
		EnvironmentID: env.ID.String(),
		BackendRef:    *env.BackendRef,
		VolumeRef:     *env.VolumeRef,
		CPULimit:      env.CPULimit,
		MemoryLimit:   env.MemoryLimit,
		DiskLimitGB:   env.DiskLimitGB,
		DockerEnabled: env.DockerEnabled,
		SecretKey:     secretKeyPlain,
	}
	if env.Image != nil {
		reqBody.Image = *env.Image
	}

	var respBody internalRestartPortsResponse
	if err := s.callEnvironmentCommand(ctx, events.EnvironmentCommandRestartPorts, reqBody, &respBody); err != nil {
		errMsg := err.Error()
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
		return nil, fmt.Errorf("agent-runner: restart environment ports: %w", err)
	}

	var newBackendRef *string
	if respBody.BackendRef != "" && respBody.BackendRef != *env.BackendRef {
		newBackendRef = &respBody.BackendRef
	}
	if err := s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusRunning, newBackendRef, nil); err != nil {
		// agent-runner has already recreated the container/Pod with the new
		// port bindings by this point — see ExecuteStart's identical
		// reasoning for why a failure to persist that must not leave the
		// row stuck on whatever transitional status it entered this
		// function with (StatusStarting, when called from ExecuteStart's
		// pending-restart branch).
		errMsg := fmt.Sprintf("persist running status: %s", err)
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, newBackendRef, &errMsg)
		return nil, err
	}
	// Same reasoning as StartEnvironment's own call to this: best-effort,
	// must not stop this function from clearing ports_pending_restart below
	// (the new port bindings are already live on the backend at this
	// point regardless of whether this bookkeeping write succeeds).
	_ = s.repo.TouchEnvironment(ctx, env.ID)
	if err := s.repo.SetPortsPendingRestart(ctx, env.ID, false); err != nil {
		return nil, err
	}

	env.Status = environmentdom.StatusRunning
	env.ErrorMessage = nil
	env.PortsPendingRestart = false
	if newBackendRef != nil {
		env.BackendRef = newBackendRef
	}
	if respBody.SSHPort != 0 {
		env.SSHPort = &respBody.SSHPort
	}
	return env, nil
}

// StopEnvironment asks agent-runner to stop the environment's container/Pod
// without deleting it or its volume. No-op if already stopped/suspended.
// StopEnvironment validates that environmentID is eligible to stop and
// queues the actual agent-runner call onto StreamEnvironmentCommands for
// worker.EnvironmentCommandConsumer to execute (via ExecuteStop below) —
// same reasoning as StartEnvironment's own doc comment: this method never
// calls agent-runner itself, and returns as soon as the command is
// durably queued, rather than holding the request open for however long
// the backing container/Pod actually takes to stop. No-op if already
// stopped/suspended.
func (s *Service) StopEnvironment(ctx context.Context, projectID, environmentID uuid.UUID) (*environmentdom.Environment, error) {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	if env.Status == environmentdom.StatusStopped || env.Status == environmentdom.StatusSuspended {
		return env, nil
	}
	if env.Status != environmentdom.StatusRunning {
		return nil, environmentdom.ErrEnvironmentBusy
	}
	if env.BackendRef == nil {
		return nil, fmt.Errorf("environment %s has never been provisioned (no backend_ref)", env.ID)
	}

	// Status set BEFORE queuing — see StartEnvironment's identical reasoning
	// for why: it closes the race where a fast worker finishes the command
	// and persists StatusStopped before this request's own status write
	// runs, which would otherwise silently regress it back to
	// StatusStopping forever.
	if err := s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusStopping, nil, nil); err != nil {
		return nil, err
	}
	if err := s.publisher.Append(ctx, events.StreamEnvironmentCommands, events.TopicEnvironmentStop, map[string]any{
		"environment_id": env.ID.String(),
	}); err != nil {
		// Nothing will ever execute this stop now — see StartEnvironment's
		// identical fallback for why this must not leave the row stranded
		// at StatusStopping.
		errMsg := fmt.Sprintf("queue environment stop: %s", err)
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
		return nil, fmt.Errorf("queue environment stop: %w", err)
	}
	env.Status = environmentdom.StatusStopping
	env.ErrorMessage = nil
	return env, nil
}

// ExecuteStop performs the actual (potentially slow) work StopEnvironment
// used to do inline: ask agent-runner to stop environmentID's backing
// container/Pod. Called only by worker.EnvironmentCommandConsumer once it
// reads the command StopEnvironment queued — see ExecuteStart's own doc
// comment for why this isn't part of environmentdom.Service, and why it
// re-reads environmentID fresh and re-validates BackendRef rather than
// trusting state captured when the command was queued.
func (s *Service) ExecuteStop(ctx context.Context, environmentID uuid.UUID) error {
	env, err := s.repo.FindEnvironmentByID(ctx, environmentID)
	if err != nil {
		return err
	}
	if env.Status != environmentdom.StatusStopping {
		// A stale replay of an already-superseded command — see
		// ExecuteStart's identical guard. Just as important here: the user
		// may have since started this environment back up on purpose, and
		// blindly calling agent-runner's /stop would kill it out from under
		// them.
		return nil
	}
	if env.BackendRef == nil {
		errMsg := fmt.Sprintf("environment %s has never been provisioned (no backend_ref)", env.ID)
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
		return errors.New(errMsg)
	}

	if err := s.callInternal(ctx, s.httpClient, http.MethodPost, "/internal/environments/"+env.ID.String()+"/stop",
		internalBackendRefRequest{BackendRef: *env.BackendRef}, nil); err != nil {
		errMsg := err.Error()
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
		return fmt.Errorf("agent-runner: stop environment: %w", err)
	}
	if err := s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusStopped, nil, nil); err != nil {
		// agent-runner has already stopped the backend by this point — see
		// ExecuteStart's identical reasoning for why a failure to persist
		// that must not leave the row stuck at StatusStopping forever.
		// StopEnvironment itself only accepts a StatusRunning row, so a
		// stranded StatusStopping row couldn't be retried via Stop anyway
		// (the backend isn't running to stop) — but StartEnvironment does
		// accept StatusError, and starting an already-stopped backend is
		// exactly what that action does, so marking Error still leaves the
		// user a working recovery path.
		errMsg := fmt.Sprintf("persist stopped status: %s", err)
		_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
		return err
	}
	return nil
}

// DeleteEnvironment asks agent-runner to permanently remove the
// environment's container/Pod and volume, then soft-deletes the row. If the
// environment was never successfully provisioned (no backend_ref — e.g. it
// landed in StatusError straight out of CreateEnvironment), there is
// nothing for agent-runner to tear down, so this skips straight to the
// soft-delete.
func (s *Service) DeleteEnvironment(ctx context.Context, projectID, environmentID uuid.UUID) error {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return err
	}
	if env.BackendRef != nil {
		volumeRef := ""
		if env.VolumeRef != nil {
			volumeRef = *env.VolumeRef
		}
		if err := s.callInternal(ctx, s.httpClient, http.MethodDelete, "/internal/environments/"+env.ID.String(),
			internalDeleteEnvironmentRequest{BackendRef: *env.BackendRef, VolumeRef: volumeRef}, nil); err != nil {
			errMsg := err.Error()
			_ = s.setStatus(ctx, env.ProjectID, env.ID, environmentdom.StatusError, nil, &errMsg)
			return fmt.Errorf("agent-runner: delete environment: %w", err)
		}
	}
	return s.repo.SoftDeleteEnvironment(ctx, env.ID)
}

// Heartbeat bumps last_active_at — called periodically by the browser
// terminal while open, and by agent-runner on every conversation attach/
// turn-end, so the idle reaper never stops an environment something is
// actively using.
func (s *Service) Heartbeat(ctx context.Context, projectID, environmentID uuid.UUID) error {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return err
	}
	return s.repo.TouchEnvironment(ctx, env.ID)
}

// ResolveConversationWorkdir validates and resolves the environment+folder
// a new (or resumed) conversation should work in. See
// environmentdom.EnvironmentService's doc comment for the exact contract —
// this is also reused, unchanged, to re-resolve a resumed conversation's
// already-known environment_id/environment_folder_id back into a live
// Environment/EnvironmentFolder pair (folderID non-nil in that case, so the
// deterministic FindFolderByID branch runs, not the ambiguous
// auto-select-if-one branch).
func (s *Service) ResolveConversationWorkdir(ctx context.Context, projectID uuid.UUID, environmentID, folderID *uuid.UUID) (*environmentdom.Environment, *environmentdom.EnvironmentFolder, error) {
	if environmentID == nil {
		return nil, nil, nil
	}
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, *environmentID)
	if err != nil {
		return nil, nil, err
	}

	if folderID != nil {
		folder, err := s.repo.FindFolderByID(ctx, *folderID)
		if err != nil {
			return nil, nil, err
		}
		if folder.EnvironmentID != env.ID {
			return nil, nil, environmentdom.ErrFolderNotFound
		}
		return env, folder, nil
	}

	folders, err := s.repo.ListFolders(ctx, env.ID)
	if err != nil {
		return nil, nil, err
	}
	if len(folders) != 1 {
		// Zero folders: nothing to auto-select. More than one: ambiguous —
		// the caller must ask the user to pick.
		return nil, nil, environmentdom.ErrFolderNotFound
	}
	return env, folders[0], nil
}

// -------------------------------------------------------------------------
// Folders
// -------------------------------------------------------------------------

// ListFolders returns every folder in an environment, verifying the
// environment itself is visible in projectID first.
func (s *Service) ListFolders(ctx context.Context, projectID, environmentID uuid.UUID) ([]*environmentdom.EnvironmentFolder, error) {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListFolders(ctx, env.ID)
}

// AddFolder creates the folder row and asks agent-runner to mkdir it into
// place inside the environment's container. Folders are path-only — no
// name/repo-clone/branch fields (dropped before ever shipping; see
// EnvironmentFolder's doc comment).
func (s *Service) AddFolder(ctx context.Context, projectID, environmentID uuid.UUID, in environmentdom.AddFolderInput) (*environmentdom.EnvironmentFolder, error) {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}

	path := strings.TrimSpace(in.Path)
	if !strings.HasPrefix(path, "/") {
		return nil, environmentdom.ErrFolderPathInvalid
	}

	now := time.Now()
	folder := &environmentdom.EnvironmentFolder{
		ID:            uuid.New(),
		EnvironmentID: env.ID,
		Path:          path,
		CreatedBy:     in.CreatedBy,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.CreateFolder(ctx, folder); err != nil {
		return nil, err
	}

	// Only ask agent-runner to actually create the directory when the
	// environment has something running to exec into — an environment
	// that's StatusCreating/StatusError has no live backend_ref yet, and
	// StatusStopped/StatusSuspended has no live process to exec a command
	// in. The folder row still exists either way; the mkdir happens lazily
	// the next time the environment starts if needed — a known
	// simplification, not attempted here.
	if env.BackendRef != nil && env.Status == environmentdom.StatusRunning {
		reqBody := internalAddFolderRequest{
			BackendRef: *env.BackendRef,
			Path:       path,
		}
		if err := s.callInternal(ctx, s.httpClient, http.MethodPost, "/internal/environments/"+env.ID.String()+"/folders", reqBody, nil); err != nil {
			return folder, fmt.Errorf("agent-runner: add folder: %w", err)
		}
	}
	return folder, nil
}

// Browse lists the immediate children of path inside environmentID's
// running container/Pod — the folder-creation UI's "browse instead of
// typing blind" affordance. Requires the environment to be StatusRunning;
// agent-runner itself additionally scopes path to its own fixed folders
// root (see internal/acpbridge/environment_handlers.go's
// environmentHomeRoot), rejecting anything outside it.
func (s *Service) Browse(ctx context.Context, projectID, environmentID uuid.UUID, path string) (string, []environmentdom.BrowseEntry, error) {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return "", nil, err
	}
	if env.BackendRef == nil || env.Status != environmentdom.StatusRunning {
		return "", nil, environmentdom.ErrEnvironmentNotRunning
	}

	q := url.Values{}
	q.Set("backend_ref", *env.BackendRef)
	if path != "" {
		q.Set("path", path)
	}
	var resp internalBrowseResponse
	if err := s.callInternal(ctx, s.httpClient, http.MethodGet, "/internal/environments/"+env.ID.String()+"/browse?"+q.Encode(), nil, &resp); err != nil {
		return "", nil, fmt.Errorf("agent-runner: browse folder: %w", err)
	}
	entries := make([]environmentdom.BrowseEntry, 0, len(resp.Entries))
	for _, e := range resp.Entries {
		entries = append(entries, environmentdom.BrowseEntry{Name: e.Name, IsDir: e.IsDir})
	}
	return resp.Path, entries, nil
}

// DeleteFolder unregisters a folder — a folder row is only ever a pointer
// to a working directory inside the environment's filesystem, not
// something Paca owns the contents of, so deleting it never touches the
// container: no agent-runner round-trip, no filesystem operation, just the
// row. A user who wants the directory itself gone can do that from a
// terminal/SSH session like they would for anything else in the
// environment.
func (s *Service) DeleteFolder(ctx context.Context, projectID, environmentID, folderID uuid.UUID) error {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return err
	}
	folder, err := s.repo.FindFolderByID(ctx, folderID)
	if err != nil {
		return err
	}
	if folder.EnvironmentID != env.ID {
		return environmentdom.ErrFolderNotFound
	}
	return s.repo.DeleteFolder(ctx, folder.ID)
}

// -------------------------------------------------------------------------
// SSH keys — pure CRUD, no agent-runner round-trip (see
// environmentdom.SSHKeyService's doc comment).
// -------------------------------------------------------------------------

// ListSSHKeys returns every SSH key registered on an environment.
func (s *Service) ListSSHKeys(ctx context.Context, projectID, environmentID uuid.UUID) ([]*environmentdom.EnvironmentSSHKey, error) {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListSSHKeys(ctx, env.ID)
}

// AddSSHKey parses PublicKey, derives its fingerprint, and registers it —
// rejecting an unparseable key or a fingerprint already registered on this
// environment.
func (s *Service) AddSSHKey(ctx context.Context, projectID, environmentID uuid.UUID, in environmentdom.AddSSHKeyInput) (*environmentdom.EnvironmentSSHKey, error) {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}

	fingerprint, err := sshFingerprint(in.PublicKey)
	if err != nil {
		return nil, environmentdom.ErrSSHKeyInvalid
	}

	if _, err := s.repo.FindSSHKeyByFingerprint(ctx, env.ID, fingerprint); err == nil {
		return nil, environmentdom.ErrSSHKeyFingerprintTaken
	} else if err != environmentdom.ErrSSHKeyNotFound {
		return nil, err
	}

	key := &environmentdom.EnvironmentSSHKey{
		ID:            uuid.New(),
		EnvironmentID: env.ID,
		Label:         strings.TrimSpace(in.Label),
		PublicKey:     strings.TrimSpace(in.PublicKey),
		Fingerprint:   fingerprint,
		CreatedBy:     in.CreatedBy,
		CreatedAt:     time.Now(),
	}
	if err := s.repo.CreateSSHKey(ctx, key); err != nil {
		return nil, err
	}
	s.syncSSHKeys(ctx, env)
	return key, nil
}

// DeleteSSHKey removes a registered SSH key, verifying it belongs to the
// given environment first.
func (s *Service) DeleteSSHKey(ctx context.Context, projectID, environmentID, keyID uuid.UUID) error {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return err
	}
	key, err := s.repo.FindSSHKeyByID(ctx, keyID)
	if err != nil {
		return err
	}
	if key.EnvironmentID != env.ID {
		return environmentdom.ErrSSHKeyNotFound
	}
	if err := s.repo.DeleteSSHKey(ctx, key.ID); err != nil {
		return err
	}
	s.syncSSHKeys(ctx, env)
	return nil
}

// syncSSHKeys asks agent-runner to re-render and re-push env's
// authorized_keys immediately, so a key just added/removed takes effect
// on an already-running environment without waiting for its next Start
// (see internal/acpbridge/environment_handlers.go's
// handleSyncEnvironmentSSHKeys). Best-effort and silent on failure —
// mirrors this service's own agent-runner calls elsewhere in spirit, but
// deliberately never surfaces an error to AddSSHKey/DeleteSSHKey's caller:
// the row write (the actual source of truth for what's authorized) has
// already succeeded by the time this runs, and an environment that's
// stopped or an agent-runner that's briefly unreachable both self-heal on
// the environment's next Start regardless.
func (s *Service) syncSSHKeys(ctx context.Context, env *environmentdom.Environment) {
	if env.BackendRef == nil || *env.BackendRef == "" {
		return
	}
	_ = s.callInternal(ctx, s.httpClient, http.MethodPost, "/internal/environments/"+env.ID.String()+"/ssh-keys/sync",
		internalBackendRefRequest{BackendRef: *env.BackendRef}, nil)
}

// -------------------------------------------------------------------------
// Port forwards — user-managed, one row per container port they want
// reachable from outside (see environmentdom.PortForwardService's doc
// comment for how this differs from the environment's own auto-created
// SSHPort).
// -------------------------------------------------------------------------

// ListPortForwards returns every port forward on an environment, verifying
// the environment itself is visible in projectID first.
func (s *Service) ListPortForwards(ctx context.Context, projectID, environmentID uuid.UUID) ([]*environmentdom.EnvironmentPortForward, error) {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListPortForwards(ctx, env.ID)
}

// AddPortForward creates the port-forward row and, if the environment is
// currently running, asks agent-runner to assign it a host port
// immediately rather than waiting for the environment's next Start —
// purely so the frontend can show the assigned port right away. Either
// way, marks the environment's ports as pending a restart: this only ever
// decided which host port to use, it never touches the backing
// container/Pod itself — see environmentdom.EnvironmentService's own doc
// comment on why applying it requires a separate, explicit action.
func (s *Service) AddPortForward(ctx context.Context, projectID, environmentID uuid.UUID, in environmentdom.AddPortForwardInput) (*environmentdom.EnvironmentPortForward, error) {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	if in.ContainerPort < 1 || in.ContainerPort > 65535 {
		return nil, environmentdom.ErrPortForwardContainerPortInvalid
	}
	label := strings.TrimSpace(in.Label)
	if label == "" {
		label = fmt.Sprintf("port %d", in.ContainerPort)
	}

	pf := &environmentdom.EnvironmentPortForward{
		ID:            uuid.New(),
		EnvironmentID: env.ID,
		Label:         label,
		ContainerPort: in.ContainerPort,
		CreatedBy:     in.CreatedBy,
		CreatedAt:     time.Now(),
	}
	if err := s.repo.CreatePortForward(ctx, pf); err != nil {
		return nil, err
	}

	if env.BackendRef != nil && env.Status == environmentdom.StatusRunning {
		if err := s.callInternal(ctx, s.httpClient, http.MethodPost, "/internal/environments/"+env.ID.String()+"/port-forwards/assign",
			internalBackendRefRequest{BackendRef: *env.BackendRef}, nil); err == nil {
			// Re-read so the caller sees the host_port agent-runner just
			// assigned, rather than the nil value pf still holds locally.
			// Best-effort on failure, like syncSSHKeys above: the row
			// (source of truth) is already written, and the next
			// Start/RestartEnvironment assigns one regardless.
			if refreshed, err := s.repo.FindPortForwardByID(ctx, pf.ID); err == nil {
				pf = refreshed
			}
		}
	}

	if err := s.repo.SetPortsPendingRestart(ctx, env.ID, true); err != nil {
		return nil, err
	}
	return pf, nil
}

// DeletePortForward removes the row and marks the environment's ports as
// pending a restart — unlike the old relay design, there is nothing to
// tell agent-runner right now: the stale binding (if the environment is
// currently running and this row had a host_port) stays published until
// the next Start/RestartEnvironment actually recreates the container/Pod
// with the updated set, matching the exact same "changing a port requires
// a restart" constraint AddPortForward is subject to.
func (s *Service) DeletePortForward(ctx context.Context, projectID, environmentID, portForwardID uuid.UUID) error {
	env, err := s.repo.FindVisibleEnvironmentInProject(ctx, projectID, environmentID)
	if err != nil {
		return err
	}
	pf, err := s.repo.FindPortForwardByID(ctx, portForwardID)
	if err != nil {
		return err
	}
	if pf.EnvironmentID != env.ID {
		return environmentdom.ErrPortForwardNotFound
	}

	if err := s.repo.DeletePortForward(ctx, pf.ID); err != nil {
		return err
	}
	return s.repo.SetPortsPendingRestart(ctx, env.ID, true)
}

// -------------------------------------------------------------------------
// Slug generation
// -------------------------------------------------------------------------

// slugify lowercases name, replaces every run of non-alphanumeric
// characters with a single '-', and trims leading/trailing dashes —
// mirrors the description in this package's CreateEnvironment doc comment.
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugNonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "environment"
	}
	return s
}

// generateUniqueSlug derives a slug from name and appends "-2", "-3", ... on
// collision within projectID (uq_environments_project_slug) — no existing
// "agents.handle"-style auto-suffix helper was found elsewhere in this
// codebase to mirror (CreateAgent instead just rejects a taken handle
// outright), so this is a new, minimal implementation of the algorithm
// described in this feature's own spec.
func (s *Service) generateUniqueSlug(ctx context.Context, projectID uuid.UUID, name string) (string, error) {
	base := slugify(name)
	candidate := base
	for i := 1; i <= maxSlugAttempts; i++ {
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		taken, err := s.repo.SlugTaken(ctx, projectID, candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not generate a unique slug for %q after %d attempts", name, maxSlugAttempts)
}

// randomHex returns n random bytes, hex-encoded — used for each
// environment's secret_key_encrypted plaintext (see that column's doc
// comment in migration 000042 for why it's generated once at creation and
// reused across stop/start, unlike an ephemeral conversation sandbox's
// per-Start sandbox.RandomHex(32)).
func randomHex(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// -------------------------------------------------------------------------
// agent-runner internal HTTP client
// -------------------------------------------------------------------------

type internalCreateEnvironmentRequest struct {
	EnvironmentID string `json:"environment_id"`
	ProjectID     string `json:"project_id"`
	Image         string `json:"image,omitempty"`
	CPULimit      string `json:"cpu_limit"`
	MemoryLimit   string `json:"memory_limit"`
	DiskLimitGB   int    `json:"disk_limit_gb"`
	DockerEnabled bool   `json:"docker_enabled"`
	SecretKey     string `json:"secret_key"`
}

type internalCreateEnvironmentResponse struct {
	Backend    string `json:"backend"`
	BackendRef string `json:"backend_ref"`
	VolumeRef  string `json:"volume_ref"`
	BaseURL    string `json:"base_url"`
	// SSHPort is 0 when agent-runner's own SSH-routing feature isn't
	// configured — see environmentdom.Environment.SSHPort's doc comment.
	// Never persisted by this service (agent-runner already wrote it
	// directly to this same row via its own Postgres connection); only
	// mirrored onto the in-memory Environment this call returns, so the
	// immediate API response reflects it without a redundant re-fetch.
	SSHPort int `json:"ssh_port"`
}

type internalStartEnvironmentRequest struct {
	// EnvironmentID identifies the target now that this travels over
	// StreamAgentEnvironmentCommands instead of a URL path segment — see
	// callEnvironmentCommand.
	EnvironmentID string `json:"environment_id"`
	BackendRef    string `json:"backend_ref"`
	Image         string `json:"image,omitempty"`
	CPULimit      string `json:"cpu_limit"`
	MemoryLimit   string `json:"memory_limit"`
	DiskLimitGB   int    `json:"disk_limit_gb"`
	DockerEnabled bool   `json:"docker_enabled"`
	SecretKey     string `json:"secret_key"`
}

type internalStartEnvironmentResponse struct {
	BaseURL string `json:"base_url"`
	// SSHPort — see internalCreateEnvironmentResponse.SSHPort's doc comment.
	SSHPort int `json:"ssh_port"`
	// BackendRef is set only when the docker backend had to recreate the
	// container from scratch because it was removed outside of Paca (see
	// agent-runner's docker.Manager.recreateGoneEnvironmentContainer) —
	// empty on an ordinary restart of a still-existing container. Same
	// "may differ from the request's own" contract as
	// internalRestartPortsResponse.BackendRef below, just triggered by a
	// missing container instead of a changed port-forward set.
	BackendRef string `json:"backend_ref,omitempty"`
}

// internalRestartPortsRequest is the request body for POST
// /internal/environments/{id}/restart-ports — mirrors
// internalStartEnvironmentRequest plus VolumeRef, needed to reattach the
// same volume/PVC if the docker backend has to recreate the container
// (see restartEnvironmentPorts's own doc comment for when this is called
// instead of a plain /start).
type internalRestartPortsRequest struct {
	// EnvironmentID identifies the target now that this travels over
	// StreamAgentEnvironmentCommands instead of a URL path segment — see
	// callEnvironmentCommand.
	EnvironmentID string `json:"environment_id"`
	BackendRef    string `json:"backend_ref"`
	VolumeRef     string `json:"volume_ref"`
	Image         string `json:"image,omitempty"`
	CPULimit      string `json:"cpu_limit"`
	MemoryLimit   string `json:"memory_limit"`
	DiskLimitGB   int    `json:"disk_limit_gb"`
	DockerEnabled bool   `json:"docker_enabled"`
	SecretKey     string `json:"secret_key"`
}

type internalRestartPortsResponse struct {
	// BackendRef may differ from the request's own (docker recreates the
	// container; kubernetes never touches the Pod, so it won't) — see
	// restartEnvironmentPorts's own doc comment.
	BackendRef string `json:"backend_ref"`
	BaseURL    string `json:"base_url"`
	SSHPort    int    `json:"ssh_port"`
}

// internalBackendRefRequest is the request body for POST
// /internal/environments/{id}/stop — the only lifecycle call needing
// nothing but backend_ref.
type internalBackendRefRequest struct {
	BackendRef string `json:"backend_ref"`
}

type internalDeleteEnvironmentRequest struct {
	BackendRef string `json:"backend_ref"`
	VolumeRef  string `json:"volume_ref"`
}

type internalAddFolderRequest struct {
	BackendRef string `json:"backend_ref"`
	Path       string `json:"path"`
}

// internalBrowseResponse mirrors agent-runner's GET
// /internal/environments/{id}/browse response body exactly.
type internalBrowseResponse struct {
	Path    string `json:"path"`
	Entries []struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
	} `json:"entries"`
}

// environmentCommandReply is the JSON envelope agent-runner's own
// messaging.EnvironmentCommandConsumer RPushes onto a command's reply_key
// — must stay byte-identical to that consumer's
// messaging.EnvironmentCommandReply.
type environmentCommandReply struct {
	OK      bool            `json:"ok"`
	Error   string          `json:"error,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// callEnvironmentCommand is callInternal's counterpart for the 3 calls
// that wait on a Pod/container becoming ready (create/start/
// restart-ports — see aiAgentProvisionHTTPTimeout's doc comment for why).
// Same blocking-call shape as callInternal (blocks until it has a
// response or times out, decodes respBody, returns an error otherwise),
// transport swapped from HTTP to a Valkey stream + list round trip:
// publish {type, request_id, reply_key, payload} onto
// StreamAgentEnvironmentCommands, then BRPop reply_key with
// aiAgentProvisionHTTPTimeout as the wait budget — this *is* the timeout
// enforcement point now, not an http.Client.Timeout.
//
// List+BRPop, not Pub/Sub: a list value persists until popped, so there's
// no "subscriber must already be listening" race — whether agent-runner's
// RPush lands before or after this call's BRPop starts, it's seen either
// way. Requires both s.publisher and s.redisClient to be set (see
// WithPublisher/WithRedisClient) — every production caller (bootstrap/app.go)
// sets both; a test that never reaches create/start/restart-ports has no
// need to.
func (s *Service) callEnvironmentCommand(ctx context.Context, cmdType string, reqBody, respBody any) error {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal environment command payload: %w", err)
	}
	requestID := uuid.New().String()
	replyKey := events.EnvironmentReplyKey(requestID)

	if err := s.publisher.AppendFlat(ctx, events.StreamAgentEnvironmentCommands, map[string]any{
		"type":       cmdType,
		"request_id": requestID,
		"reply_key":  replyKey,
		"payload":    string(payload),
	}); err != nil {
		return fmt.Errorf("queue agent-runner command: %w", err)
	}

	result, err := s.redisClient.BRPop(ctx, aiAgentProvisionHTTPTimeout, replyKey).Result()
	if err != nil {
		if err == redis.Nil {
			return fmt.Errorf("agent-runner did not reply to %s within %s", cmdType, aiAgentProvisionHTTPTimeout)
		}
		return fmt.Errorf("wait for agent-runner reply: %w", err)
	}
	// BRPop's result is [key, value] — result[0] is always replyKey here
	// since only one key was passed in.
	var reply environmentCommandReply
	if err := json.Unmarshal([]byte(result[1]), &reply); err != nil {
		return fmt.Errorf("decode agent-runner reply: %w", err)
	}
	if !reply.OK {
		return fmt.Errorf("agent-runner: %s", reply.Error)
	}
	if respBody != nil && len(reply.Payload) > 0 {
		if err := json.Unmarshal(reply.Payload, respBody); err != nil {
			return fmt.Errorf("decode agent-runner reply payload: %w", err)
		}
	}
	return nil
}

// callInternal calls one of agent-runner's internal/environments endpoints,
// authenticated the same way agent_handler.go's existing agent-runner calls
// are (X-Internal-Token: AI_AGENT_INTERNAL_KEY). respBody may be nil for
// endpoints whose 200 response body is just "{}".
func (s *Service) callInternal(ctx context.Context, client *http.Client, method, path string, reqBody, respBody any) error {
	if s.aiAgentURL == "" {
		return fmt.Errorf("agent-runner service URL not configured")
	}

	var bodyReader io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, s.aiAgentURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Internal-Token", s.aiAgentInternalKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call agent-runner: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read agent-runner response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("agent-runner returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if respBody != nil && len(data) > 0 {
		if err := json.Unmarshal(data, respBody); err != nil {
			return fmt.Errorf("decode agent-runner response: %w", err)
		}
	}
	return nil
}
