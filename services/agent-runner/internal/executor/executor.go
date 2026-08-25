// Package executor runs one llm-type agent conversation end to end: spawn
// a sandbox, drive it over ACP, tear it down. Go analog of
// services/ai-agent/src/agent/executor.py's run_conversation.
package executor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/acp"
	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/chatsandbox"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
	"github.com/Paca-AI/agent-runner/internal/sandbox"
	"github.com/Paca-AI/agent-runner/internal/secret"
)

// defaultMaxToolCalls caps a turn when an agent has no MaxIterations
// configured (<= 0). Goose enforces no turn cap of its own — confirmed in
// the protocol spike, where a non-converging reply produced 600+ tool-call
// cycles in 20 seconds with no backoff — so "unconfigured" must fail safe
// to a bounded default, not to unlimited.
const defaultMaxToolCalls = 50

// defaultTimeoutMinutes bounds a turn when an agent has no TimeoutMinutes
// configured (<= 0) — matches the agents table's own DB default
// (`timeout_minutes INTEGER NOT NULL DEFAULT 30`), so in practice this is
// only a safety net for malformed data, not the normal path.
//
// This exists because of a real, live bug found while building the sandbox
// image: a wrong mcpServers wire format made a real goose serve's
// session/new hang forever rather than return an ACP error. acp.Client's
// calls have no timeout of their own — only whatever context.Context the
// caller supplies — so
// without this, a *different* future downstream failure (a stuck MCP
// subprocess, a wedged provider) could hang a real conversation, and the
// container and goroutine driving it, indefinitely.
const defaultTimeoutMinutes = 30

// sandboxWorkdir is the ACP session's cwd. Must be a directory the
// container's own user can write to and spawn subprocesses from — /root is
// a trap on the stock goose image (uid 1000, user "goose"), confirmed in
// the same spike. /home/goose is that user's home directory.
const sandboxWorkdir = "/home/goose"

// Options holds service-level settings shared across every conversation
// this process runs — the Go analog of services/ai-agent's config.Settings,
// scoped to just the fields the executor itself needs.
type Options struct {
	// Image is the pinned goose image reference; callers should pass a
	// digest- or tag-pinned reference, not a floating one.
	Image string
	// PacaAPIKey/PacaAPIURL/PacaGatewayURL configure the built-in Paca MCP
	// server appended to every conversation's MCP server list — mirrors
	// build_mcp_config's "paca" entry in builder.py. Leave PacaAPIKey empty
	// to omit the Paca MCP server entirely (matches builder.py's
	// `if settings.paca_api_key:` gate).
	PacaAPIKey     string
	PacaAPIURL     string
	PacaGatewayURL string
	// MCPDevSourceDir, when set, is forwarded to every sandbox.Config so the
	// Paca MCP server runs from a local apps/mcp checkout instead of the
	// image's globally npm-installed @paca-ai/paca-mcp — see
	// config.Settings.MCPDevSourceDir and buildMCPServers.
	MCPDevSourceDir string
}

// Executor runs conversations for one process — holds the shared sandbox
// backend and secret decryptor, not any per-conversation state (mirrors
// every sandbox.Backend implementation's own contract: safe to call Run
// concurrently for different conversations, since each gets its own
// sandbox and acp.Client).
type Executor struct {
	// sandboxMgr is a sandbox.FullBackend, not a concrete implementation,
	// so the same Executor code drives either the Docker backend
	// (internal/sandbox/docker.Manager) or the Kubernetes backend
	// (internal/sandbox/k8s.Manager) depending on which one main.go wires
	// up. Widened from sandbox.Backend to sandbox.FullBackend so
	// coldStartEnvironment can call StartEnvironment directly — backward
	// compatible, since FullBackend embeds Backend (see that interface's
	// own doc comment).
	sandboxMgr sandbox.FullBackend
	// envRepo resolves a static environment's row for coldStartEnvironment
	// — nil in tests/tooling that never attach a conversation to an
	// environment (trigger.EnvironmentID stays nil in that case, so
	// coldStartEnvironment, the only reader of this field, is never
	// reached).
	envRepo *postgres.EnvironmentRepository
	// convRepo persists/reads a conversation's goose ACP sessionId — see
	// attachEnvironmentSession's own doc comment. nil in the same
	// tests/tooling envRepo's own doc comment describes; attachEnvironmentSession
	// falls back to a fresh session/new every turn when nil, exactly the
	// old (pre-LoadSession) behavior, rather than panicking on a nil repo.
	convRepo *postgres.ConversationRepository
	// portForwardRepo lets environmentPortMappings read an environment's
	// already-assigned port forwards, alongside its own SSHPort field, so a
	// mid-attach container recreate (recreateEnvironmentIfMissingEnv) never
	// silently drops them — see that method's own doc comment. Same
	// nil-safety convention as envRepo/convRepo.
	portForwardRepo *postgres.PortForwardRepository
	encryptor       *secret.Encryptor
	opts            Options
	log             *slog.Logger
}

// New builds an Executor sharing the given sandbox backend, environment,
// conversation, and port-forward repositories, and decryptor across every
// conversation it runs.
func New(sandboxMgr sandbox.FullBackend, envRepo *postgres.EnvironmentRepository, convRepo *postgres.ConversationRepository, portForwardRepo *postgres.PortForwardRepository, encryptor *secret.Encryptor, opts Options, log *slog.Logger) *Executor {
	return &Executor{sandboxMgr: sandboxMgr, envRepo: envRepo, convRepo: convRepo, portForwardRepo: portForwardRepo, encryptor: encryptor, opts: opts, log: log}
}

// Result is what one turn produced. Handle/Client/SessionID are always
// populated whenever a sandbox was actually reached (even on a later
// error) — see Run's doc comment on why Run itself no longer decides
// whether to tear the sandbox down.
type Result struct {
	StopReason string
	// Usage is this turn's token accounting, when goose reported one — see
	// acp.Usage's doc comment. Nil on any error return from client.Prompt.
	Usage     *acp.Usage
	Handle    *sandbox.Handle
	Client    *acp.Client
	SessionID string
}

// Run drives one ACP turn — a cold-started sandbox by default, or an
// existing one reattached via resume — mirrors executor.py's
// run_conversation's overall shape (build LLM/skills/MCP config, spin up or
// reattach to a workspace, send a message, run), adapted for Goose's
// ACP-over-HTTP transport instead of the OpenHands SDK.
//
// Unlike earlier versions of this method, Run does NOT tear the sandbox
// down itself, on success or on error — ownership of that decision moved to
// the caller (handler.Handle) once chat conversation continuity required
// it: whether to keep a sandbox alive for the conversation's next reply
// depends on whether the trigger is chat-type and, if it was interrupted,
// whether that was a pause or a full stop — information Run itself doesn't
// have. The caller must call StopSandbox once it decides teardown is
// appropriate.
//
// resume, when non-nil, mirrors run_conversation's resume_state branch:
// reattach to resume.Handle/resume.Client/resume.SessionID instead of
// starting a new container and ACP session, and send only trigger.Message
// as the turn's prompt (the agent already has full context from earlier
// turns) instead of the skills/trigger-context message buildInitialMessage
// assembles for a cold start. The agent's own system prompt isn't part of
// that either way any more (see hints.go's buildGooseHints) — it's written
// to the container's filesystem once at cold start as a .goosehints file,
// and Goose's own default system.md template re-renders it into every
// turn's system-role message from there for as long as that file exists,
// resumed turns included (same container, same filesystem), so there's
// nothing for this branch to resend.
//
// onEvent is called for every session/update notification during the turn
// — the caller is responsible for translating each into an
// AgentConversationEvent and publishing/persisting it, the same role
// make_event_callback plays in executor.py. Not done inside this package so
// Run stays testable without a live Valkey/Postgres connection.
func (e *Executor) Run(ctx context.Context, cfg agent.Config, trigger agent.Trigger, resume *chatsandbox.State, onEvent func(acp.Event), onReady func()) (Result, error) {
	timeoutMinutes := cfg.TimeoutMinutes
	if timeoutMinutes <= 0 {
		timeoutMinutes = defaultTimeoutMinutes
	}
	// Bounds Initialize/NewSession/Prompt specifically, not sandboxMgr.Start
	// below (which has its own internal ready-timeout, see sandbox.go's
	// readyTimeout) — this is deliberately scoped to the ACP protocol calls,
	// which is where the hang defaultTimeoutMinutes's doc comment describes
	// actually happened.
	turnCtx, cancelTurn := context.WithTimeout(ctx, time.Duration(timeoutMinutes)*time.Minute)
	defer cancelTurn()

	var handle *sandbox.Handle
	var client *acp.Client
	var sessionID string
	var message string

	switch {
	case resume != nil:
		handle, client, sessionID = resume.Handle, resume.Client, resume.SessionID
		message = trigger.Message
	case trigger.EnvironmentID != nil:
		// A static environment's conversations never populate ChatSandboxes
		// in the first place (see handler.Handler.keepSandboxAlive's own
		// EnvironmentID guard), so resume above is always nil here — every
		// turn re-attaches fresh via coldStartEnvironment against the
		// already-running container instead, exactly as cheap as an
		// ordinary cold start (see
		// docs/ai-agent/environment-management.md's "no new in-memory
		// registry" design choice). No sandbox.Handle exists for this
		// path — handle stays nil, so Result.Handle stays nil too; there's
		// nothing for the caller's teardown path to Stop.
		var err error
		client, sessionID, err = e.coldStartEnvironment(ctx, turnCtx, cfg, trigger)
		if err != nil {
			return Result{}, err
		}
		message = buildInitialMessage(trigger)
	default:
		fileSkills := prepareFileSkills(cfg.Skills)

		var err error
		handle, client, sessionID, err = e.coldStart(ctx, turnCtx, cfg, trigger, fileSkills)
		if err != nil {
			return Result{Handle: handle}, err
		}
		message = buildInitialMessage(trigger)
	}

	// Fires once the sandbox/session is ready to receive session/prompt,
	// whether it was just cold-started or resumed from a paused chat
	// sandbox — the UI's cue to stop saying "setting up your environment"
	// and start saying "thinking", since from here on any further wait is
	// the LLM itself, not container/ACP-handshake setup. Called on every
	// turn (not just cold starts) rather than relying on an earlier turn's
	// marker still being loaded — a long conversation's event window is
	// paginated, so an old turn's readiness event can page out.
	if onReady != nil {
		onReady()
	}

	maxToolCalls := cfg.MaxIterations
	if maxToolCalls <= 0 {
		maxToolCalls = defaultMaxToolCalls
	}

	stopReason, usage, err := client.Prompt(turnCtx, sessionID, []acp.ContentBlock{acp.TextBlock(message)}, maxToolCalls, e.attachDiffs(turnCtx, handle, onEvent))
	result := Result{StopReason: stopReason, Usage: usage, Handle: handle, Client: client, SessionID: sessionID}
	if err != nil {
		return result, fmt.Errorf("executor: acp session/prompt: %w", err)
	}
	return result, nil
}

// StopSandbox tears down a sandbox previously returned in a Result's Handle
// — a thin pass-through to the sandbox.Backend this Executor was built
// with, so callers (handler.Handle) don't need their own reference to it
// just to finish what Run used to do unconditionally.
func (e *Executor) StopSandbox(ctx context.Context, h *sandbox.Handle) error {
	return e.sandboxMgr.Stop(ctx, h)
}

// coldStart spins up a fresh sandbox and completes the ACP handshake for a
// non-resumed turn — everything Run used to do inline before chat
// continuity split "start" from "decide whether to keep alive or stop".
// ctx bounds sandboxMgr.Start (unaffected by the turn timeout); turnCtx
// bounds Initialize/NewSession, same as it bounds Prompt in Run.
func (e *Executor) coldStart(ctx, turnCtx context.Context, cfg agent.Config, trigger agent.Trigger, fileSkills []agent.Skill) (*sandbox.Handle, *acp.Client, string, error) {
	containerEnv, err := e.buildAgentContainerEnv(cfg)
	if err != nil {
		return nil, nil, "", err
	}

	// Built before Start, not inline in the NewSession call below, so its
	// stdio servers' env (e.g. the built-in Paca MCP server's PACA_API_KEY)
	// can also be folded into containerEnv here — session/new's
	// _meta.enabledExtensions mechanism (see acp.GooseExtension's doc
	// comment) only accepts a stdio mcp server's env as *names* referencing
	// values already present in the container's own OS environment, not
	// inline values, so those values have to land in containerEnv too, not
	// only in the MCPServerConfig entries themselves.
	mcpServers := e.buildMCPServers(trigger, cfg)
	for _, s := range mcpServers {
		if s.Type != acp.McpServerStdio || s.Env == nil {
			continue
		}
		for _, ev := range *s.Env {
			containerEnv[ev.Name] = ev.Value
		}
	}

	gitName := cfg.GitCommitterName
	if gitName == "" {
		gitName = "paca-agent"
	}
	gitEmail := cfg.GitCommitterEmail
	if gitEmail == "" {
		gitEmail = "280579135+paca-agent@users.noreply.github.com"
	}

	handle, err := e.sandboxMgr.Start(ctx, sandbox.Config{
		ConversationID:    trigger.ConversationID.String(),
		Image:             e.opts.Image,
		Env:               containerEnv,
		GitCommitterName:  gitName,
		GitCommitterEmail: gitEmail,
		MCPDevSourceDir:   e.opts.MCPDevSourceDir,
		DockerEnabled:     cfg.DockerEnabled,
	})
	if err != nil {
		return nil, nil, "", fmt.Errorf("executor: start sandbox: %w", err)
	}

	// Written before Initialize/NewSession, not after — Goose's own skills
	// platform extension discovers SKILL.md files from disk (see skills.go's
	// package doc comment), so they must already be there before anything
	// that might read them, not just before the first Prompt call.
	skillsTar, err := buildSkillsTar(fileSkills)
	if err != nil {
		return handle, nil, "", fmt.Errorf("executor: build skills tar: %w", err)
	}
	if skillsTar != nil {
		if err := e.sandboxMgr.CopyToContainer(ctx, handle.ContainerID, sandboxWorkdir, skillsTar); err != nil {
			return handle, nil, "", fmt.Errorf("executor: write skills to sandbox: %w", err)
		}
	}

	// Same "before Initialize" timing as the skills tar above, and for the
	// same reason: goose's default system.md template renders .goosehints
	// unconditionally into the real system-role message on every turn (see
	// hints.go's buildGooseHints doc comment for how this was verified),
	// so it has to exist before anything that might trigger a render.
	hints := buildGooseHints(cfg, fileSkills)
	hintsTar, err := buildHintsTar(hints)
	if err != nil {
		return handle, nil, "", fmt.Errorf("executor: build goosehints tar: %w", err)
	}
	if hintsTar != nil {
		if err := e.sandboxMgr.CopyToContainer(ctx, handle.ContainerID, sandboxWorkdir, hintsTar); err != nil {
			return handle, nil, "", fmt.Errorf("executor: write goosehints to sandbox: %w", err)
		}
	}

	client := acp.NewClient(handle.BaseURL, handle.SecretKey, nil)
	if err := client.Initialize(turnCtx); err != nil {
		return handle, nil, "", fmt.Errorf("executor: acp initialize: %w", err)
	}

	sessionID, err := client.NewSession(turnCtx, sandboxWorkdir, mcpServers)
	if err != nil {
		// Initialize above already started client's connection-scoped SSE
		// reader goroutine (see client.go's Close doc comment) — dropping
		// client here by returning nil instead of it would otherwise leak
		// that goroutine until whatever eventually stops this sandbox
		// closes the underlying connection out from under it, rather than
		// being torn down cleanly the moment this turn actually fails.
		client.Close()
		return handle, nil, "", fmt.Errorf("executor: acp session/new: %w", err)
	}

	return handle, client, sessionID, nil
}

// buildAgentContainerEnv resolves the container-level environment
// variables derived from cfg's LLM/env-var configuration —
// GOOSE_PROVIDER/GOOSE_MODEL/the provider's own API key env var,
// OPENAI_HOST for a custom base URL, and every one of cfg.EnvVars,
// decrypted. Shared between coldStart (a fresh disposable sandbox, applied
// once) and coldStartEnvironment (re-applied on every StartEnvironment
// call against a static environment's already-running container — see
// sandbox.EnvironmentConfig's doc comment on why every field it carries,
// Env included, must be safe to re-apply on every start, not just the
// first).
func (e *Executor) buildAgentContainerEnv(cfg agent.Config) (map[string]string, error) {
	apiKey, err := e.encryptor.Decrypt(cfg.LLMAPIKeySecret)
	if err != nil {
		return nil, fmt.Errorf("executor: decrypt llm api key: %w", err)
	}

	gooseProvider, apiKeyEnvVar := resolveProviderEnv(cfg.LLMProvider)
	containerEnv := map[string]string{
		"GOOSE_PROVIDER": gooseProvider,
		"GOOSE_MODEL":    cfg.LLMModel,
		apiKeyEnvVar:     apiKey,
	}
	if cfg.LLMBaseURL != "" {
		// Only meaningful for the openai-routed fallback path (see
		// resolveProviderEnv) — OPENAI_HOST is the env var confirmed
		// against a real goose serve in the protocol spike. A custom base
		// URL for a *named* provider (e.g. a private Anthropic-compatible
		// gateway) isn't wired here; extend this once that case is real.
		containerEnv["OPENAI_HOST"] = cfg.LLMBaseURL
	}

	for _, ev := range cfg.EnvVars {
		val, err := e.encryptor.Decrypt(ev.EncryptedValue)
		if err != nil {
			return nil, fmt.Errorf("executor: decrypt env var %q: %w", ev.Key, err)
		}
		containerEnv[ev.Key] = val
	}
	return containerEnv, nil
}

// environmentPortMappings reassembles env's currently-published port set —
// its own SSHPort, if assigned, plus every already-assigned port forward —
// so coldStartEnvironment's StartEnvironment call can pass it straight
// through as sandbox.EnvironmentConfig.PortMappings.
//
// This exists because of a real gap: docker.Manager.StartEnvironment's
// recreateEnvironmentIfMissingEnv can recreate the environment's container
// mid-attach (backfilling GOOSE_PROVIDER onto one handleCreateEnvironment
// created, which never bakes in agent-specific env — see that method's own
// doc comment), and Docker fixes a container's port bindings at create
// time. Before this existed, that call passed no PortMappings at all, so
// the very first conversation to attach to a freshly-created environment
// would silently strip its SSH (and any port-forward) binding the moment
// this backfill fired — the container kept running and the environment
// stayed reachable over ACP, so nothing about it looked broken, only SSH
// silently stopped working. handleStartEnvironment's own HTTP path
// (internal/acpbridge/environment_handlers.go) already computes this same
// set for the identical reason on its own recreate path
// (recreateGoneEnvironmentContainer's self-heal); this is coldStartEnvironment's
// side of the same fix.
//
// Deliberately read-only: unlike acpbridge.Server.buildPortMappings (which
// this mirrors), a port forward with no host_port yet is left alone here
// rather than assigned one — auto-assigning a new port is an explicit
// user-facing action (adding a port forward), not something a routine
// conversation-attach turn should ever decide on its own.
func (e *Executor) environmentPortMappings(ctx context.Context, env *postgres.Environment) []sandbox.PortMapping {
	var mappings []sandbox.PortMapping
	if env.SSHPort != nil {
		mappings = append(mappings, sandbox.PortMapping{ContainerPort: sandbox.EnvironmentSSHPort, HostPort: *env.SSHPort})
	}
	if e.portForwardRepo == nil {
		return mappings
	}
	forwards, err := e.portForwardRepo.ListForEnvironment(ctx, env.ID)
	if err != nil {
		e.log.Warn("executor: failed to list port forwards while reapplying environment port bindings",
			"environment_id", env.ID, "error", err)
		return mappings
	}
	for _, pf := range forwards {
		if pf.HostPort == nil {
			continue
		}
		mappings = append(mappings, sandbox.PortMapping{ContainerPort: pf.ContainerPort, HostPort: *pf.HostPort})
	}
	return mappings
}

// coldStartEnvironment attaches trigger to trigger.EnvironmentID's already-
// created static environment instead of spinning up a fresh disposable
// sandbox — the environment counterpart to coldStart. ctx bounds
// StartEnvironment (unaffected by the turn timeout); turnCtx bounds
// Initialize/NewSession, same as coldStart.
//
// Unlike coldStart, this never creates anything — CreateEnvironment runs
// exactly once per environment, ever, driven explicitly by a user action
// via services/api's internal HTTP call into
// internal/acpbridge/environment_handlers.go, never lazily from a
// conversation attach. This only starts (idempotently) an environment that
// already exists, and attaches a fresh ACP session at trigger.Workdir
// instead of the ephemeral sandboxWorkdir constant.
//
// No file-skills/.goosehints delivery happens here, unlike coldStart —
// both are written relative to the ACP session's own cwd (see skills.go's
// skillsRelDir and hints.go's own doc comment on why .goosehints must live
// in the sandbox's cwd to be picked up), which for an environment is
// trigger.Workdir, i.e. inside a folder's own git checkout — dropping
// agent-runner-managed files straight into a user's repo working tree is a
// real design question (accidental commits, collision with the repo's own
// content) that docs/ai-agent/environment-management.md's Phase 1 scope
// doesn't resolve, so it's deliberately left undone here rather than
// guessed at. Flagged as an open follow-up, not an oversight.
func (e *Executor) coldStartEnvironment(ctx, turnCtx context.Context, cfg agent.Config, trigger agent.Trigger) (*acp.Client, string, error) {
	if e.envRepo == nil {
		return nil, "", fmt.Errorf("executor: no environment repository configured")
	}
	if trigger.EnvironmentID == nil {
		return nil, "", fmt.Errorf("executor: coldStartEnvironment called without an environment_id")
	}
	if trigger.Workdir == nil || *trigger.Workdir == "" {
		return nil, "", fmt.Errorf("executor: environment %s attach requires a workdir", *trigger.EnvironmentID)
	}
	environmentID := *trigger.EnvironmentID

	env, err := e.envRepo.FindEnvironmentByID(ctx, environmentID)
	if err != nil {
		return nil, "", fmt.Errorf("executor: find environment %s: %w", environmentID, err)
	}
	// No lazy creation here — see the doc comment above. A never-created
	// (BackendRef nil) or explicitly broken (error/deleting) environment
	// fails this turn clearly rather than silently doing nothing useful.
	if env.BackendRef == nil || *env.BackendRef == "" || env.Status == "error" || env.Status == "deleting" {
		return nil, "", fmt.Errorf("executor: environment is not ready (environment_id=%s, status=%s)", environmentID, env.Status)
	}

	secretKey, err := e.encryptor.Decrypt(env.SecretKeyEncrypted)
	if err != nil {
		return nil, "", fmt.Errorf("executor: decrypt environment %s secret key: %w", environmentID, err)
	}

	containerEnv, err := e.buildAgentContainerEnv(cfg)
	if err != nil {
		return nil, "", err
	}

	// Git identity has no dedicated EnvironmentConfig field (unlike
	// sandbox.Config.GitCommitterName/Email) — folded into the generic Env
	// map instead, under the exact same GIT_AUTHOR_*/GIT_COMMITTER_* names
	// both docker.Manager and k8s.Manager already set from those dedicated
	// fields for an ephemeral sandbox (see internal/sandbox/docker/manager.go),
	// so a static environment's container ends up with the same git
	// identity an ephemeral one would have gotten.
	gitName := cfg.GitCommitterName
	if gitName == "" {
		gitName = "paca-agent"
	}
	gitEmail := cfg.GitCommitterEmail
	if gitEmail == "" {
		gitEmail = "280579135+paca-agent@users.noreply.github.com"
	}
	containerEnv["GIT_AUTHOR_NAME"] = gitName
	containerEnv["GIT_AUTHOR_EMAIL"] = gitEmail
	containerEnv["GIT_COMMITTER_NAME"] = gitName
	containerEnv["GIT_COMMITTER_EMAIL"] = gitEmail

	mcpServers := e.buildMCPServers(trigger, cfg)
	for _, s := range mcpServers {
		if s.Type != acp.McpServerStdio || s.Env == nil {
			continue
		}
		for _, ev := range *s.Env {
			containerEnv[ev.Name] = ev.Value
		}
	}

	image := ""
	if env.Image != nil {
		image = *env.Image
	}

	// Always calls StartEnvironment, even if env.Status already says
	// "running" — deliberate, not a bug: it's the only way to get a
	// guaranteed-fresh, reachable BaseURL back (never persisted — see
	// sandbox.EnvironmentHandle's doc comment), and it self-heals a stale
	// "running" DB status if the container died externally. Both backend
	// implementations tolerate this (idempotent start) — see
	// docs/ai-agent/environment-management.md's "Conversation attach path".
	handle, err := e.sandboxMgr.StartEnvironment(ctx, *env.BackendRef, sandbox.EnvironmentConfig{
		EnvironmentID: environmentID.String(),
		Image:         image,
		Env:           containerEnv,
		CPULimit:      env.CPULimit,
		MemoryLimit:   env.MemoryLimit,
		DiskLimitGB:   env.DiskLimitGB,
		SecretKey:     secretKey,
		PortMappings:  e.environmentPortMappings(ctx, env),
	})
	if err != nil {
		errMsg := err.Error()
		if updErr := e.envRepo.UpdateEnvironmentStatus(context.WithoutCancel(ctx), environmentID, "error", nil, &errMsg); updErr != nil {
			e.log.Warn("executor: failed to record environment error status",
				"environment_id", environmentID, "error", updErr)
		}
		return nil, "", fmt.Errorf("executor: start environment %s: %w", environmentID, err)
	}

	// handle.BackendRef only differs from *env.BackendRef when the docker
	// backend had to recreate a container removed outside of Paca (see
	// docker.Manager.recreateGoneEnvironmentContainer's doc comment) — pass
	// it through non-nil so UpdateEnvironmentStatus's COALESCE persists the
	// new value; nil (leave backend_ref as-is) on the ordinary path where
	// nothing changed.
	var newBackendRef *string
	if handle.BackendRef != "" && handle.BackendRef != *env.BackendRef {
		newBackendRef = &handle.BackendRef
	}
	if err := e.envRepo.UpdateEnvironmentStatus(ctx, environmentID, "running", newBackendRef, nil); err != nil {
		e.log.Warn("executor: failed to record environment running status",
			"environment_id", environmentID, "error", err)
	}
	if err := e.envRepo.TouchEnvironment(ctx, environmentID); err != nil {
		e.log.Warn("executor: failed to touch environment", "environment_id", environmentID, "error", err)
	}

	client := acp.NewClient(handle.BaseURL, secretKey, nil)
	if err := client.Initialize(turnCtx); err != nil {
		return nil, "", fmt.Errorf("executor: acp initialize (environment %s): %w", environmentID, err)
	}

	sessionID, err := e.attachEnvironmentSession(turnCtx, client, trigger.ConversationID, *trigger.Workdir, mcpServers)
	if err != nil {
		// Same reasoning as coldStart's matching branch: Initialize above
		// already started client's connection-scoped SSE reader goroutine,
		// so it must be closed here rather than dropped.
		client.Close()
		return nil, "", fmt.Errorf("executor: acp session attach (environment %s): %w", environmentID, err)
	}

	return client, sessionID, nil
}

// attachEnvironmentSession gives an environment-backed conversation goose's
// own conversation memory back across turns, instead of the blank-context
// session/new call every one of its turns used before this existed: if a
// prior turn already recorded an acp_session_id for conversationID (see
// convRepo.SetACPSessionID below), this resumes it via session/load — per
// the ACP spec (and verified directly against a real goose serve instance,
// not assumed — see acp.Client.LoadSession's own doc comment), the Agent
// replays the session's entire prior history into its own context before
// this returns, so goose actually remembers everything this conversation
// has ever said, not just the single message the current turn is sending.
//
// Falls back to a fresh session/new (persisting the new id for next time)
// whenever there's no stored session yet — this conversation's very first
// environment turn, or convRepo is nil (see Executor's own doc comment on
// that field) — or the stored one no longer resolves. That second case is
// expected to happen sometimes, not a bug of its own: docker.Manager.
// recreateGoneEnvironmentContainer's self-heal path shares the environment's
// persisted workspace volume with whatever container preceded it, but
// starts goose's own on-disk session store (outside that volume) empty, so
// a session recorded before a container recreation is gone for good. A
// stale session is treated the same as no session at all — logged, not
// fatal — since a fresh session is always a safe, working fallback; the
// only thing lost is history a container recreation had already made
// unrecoverable, never a reason to fail the turn outright.
func (e *Executor) attachEnvironmentSession(ctx context.Context, client *acp.Client, conversationID uuid.UUID, workdir string, mcpServers []acp.MCPServerConfig) (string, error) {
	if e.convRepo != nil {
		stored, err := e.convRepo.GetACPSessionID(ctx, conversationID)
		if err != nil {
			e.log.Warn("executor: failed to look up stored acp session id, starting a fresh session",
				"conversation_id", conversationID, "error", err)
		} else if stored != "" {
			if loadErr := client.LoadSession(ctx, stored, workdir, mcpServers); loadErr == nil {
				return stored, nil
			} else {
				e.log.Warn("executor: failed to resume stored acp session, starting a fresh session",
					"conversation_id", conversationID, "session_id", stored, "error", loadErr)
			}
		}
	}

	sessionID, err := client.NewSession(ctx, workdir, mcpServers)
	if err != nil {
		return "", err
	}
	if e.convRepo != nil {
		if err := e.convRepo.SetACPSessionID(ctx, conversationID, sessionID); err != nil {
			e.log.Warn("executor: failed to persist new acp session id",
				"conversation_id", conversationID, "session_id", sessionID, "error", err)
		}
	}
	return sessionID, nil
}

// pacaMCPBinPath is the absolute path to the Paca MCP server's own
// executable inside the pinned Goose sandbox image
// (services/agent-server/Dockerfile) — `npm install -g @paca-ai/paca-mcp`
// creates this symlink automatically from the package's own `bin` field
// ({"paca": "./build/index.js"}), since that Dockerfile's npm prefix is
// /usr. ACP's schema requires McpServerStdio.command to be an absolute
// path ("Absolute path to the MCP server executable"), not a bare name
// resolved via PATH lookup. Confirmed by inspecting that exact image; if
// the image's Node install or the package's bin name ever moves, this
// must move with it.
//
// Deliberately NOT invoked via `npx -y @paca-ai/paca-mcp` (an earlier
// version of this constant pointed at /usr/bin/npx with those args) —
// measured directly inside a real sandbox container: npx added ~1.2s of
// its own overhead on top of this binary's own ~1.4s Node/import startup
// cost, entirely from npx's registry version-check, even though the
// package is already installed globally and npx never needed to install
// anything. That's real latency on every single conversation's cold
// start, for a check that can never find anything to do here — the image
// is rebuilt to update this package's version, not resolved freshly per
// session.
const pacaMCPBinPath = "/usr/bin/paca"

// buildMCPServers mirrors builder.py's build_mcp_config: the agent's own
// configured servers first, with the built-in Paca MCP server always
// appended last so it can't be overridden by a same-named user entry.
//
// The wire shape here is verified against ACP's real schema.json
// ($defs.McpServer — a Rust internally-tagged enum requiring a "type"
// discriminator, with env as an array of {name,value} pairs, not a JSON
// object) — an earlier, hand-guessed version of this function that omitted
// both made goose serve's session/new hang forever rather than return an
// error.
//
// User-configured "oauth"-transport servers (agent.MCPServer.Transport ==
// "oauth", the fourth value the agents_mcp_servers DB CHECK allows) have no
// direct equivalent in ACP's three-way stdio/http/sse enum and are skipped
// entirely for now — mapping OAuth to an http entry's bearer-token header
// wasn't attempted this pass.
func (e *Executor) buildMCPServers(trigger agent.Trigger, cfg agent.Config) []acp.MCPServerConfig {
	servers := make([]acp.MCPServerConfig, 0, len(cfg.MCPServers)+1)
	for _, s := range cfg.MCPServers {
		if !s.IsEnabled {
			continue
		}
		switch s.Transport {
		case "stdio":
			args := s.Args
			if args == nil {
				args = []string{}
			}
			userEnv := envMapToList(s.Env)
			servers = append(servers, acp.MCPServerConfig{
				Type:    acp.McpServerStdio,
				Name:    s.ServerName,
				Command: s.Command,
				Args:    &args,
				Env:     &userEnv,
			})
		case "http":
			servers = append(servers, acp.MCPServerConfig{
				Type:    acp.McpServerHTTP,
				Name:    s.ServerName,
				URL:     s.URL,
				Headers: &[]acp.HTTPHeader{},
			})
		case "sse":
			servers = append(servers, acp.MCPServerConfig{
				Type:    acp.McpServerSSE,
				Name:    s.ServerName,
				URL:     s.URL,
				Headers: &[]acp.HTTPHeader{},
			})
			// "oauth": skipped — see doc comment above.
		}
	}

	if e.opts.PacaAPIKey == "" {
		return servers
	}
	env := map[string]string{
		"PACA_API_KEY":     e.opts.PacaAPIKey,
		"PACA_API_URL":     e.opts.PacaAPIURL,
		"PACA_GATEWAY_URL": e.opts.PacaGatewayURL,
		"PACA_AGENT_ID":    cfg.ID.String(),
	}
	if trigger.ProjectID != uuid.Nil {
		env["PACA_PROJECT_ID"] = trigger.ProjectID.String()
	}
	if trigger.ActorUserID != nil {
		env["PACA_ACTOR_USER_ID"] = trigger.ActorUserID.String()
	}
	if len(trigger.RepoPluginIDs) > 0 {
		// Read by apps/mcp's repo-tools.ts to scope list_repositories and
		// gate whether the repo tool set is shown to the agent at all — see
		// PacaConfig.repoPluginIds's doc comment there.
		env["PACA_REPO_PLUGIN_IDS"] = strings.Join(trigger.RepoPluginIDs, ",")
	}
	// Dev override: run the Paca MCP server from a locally-mounted apps/mcp
	// checkout (see sandbox.Config.MCPDevSourceDir) instead of the image's
	// globally npm-installed @paca-ai/paca-mcp, so a local source change is
	// live on the next conversation without an npm publish + image rebuild.
	// /usr/bin/node is the same absolute-path requirement pacaMCPBinPath's
	// doc comment explains — ACP rejects a bare command name resolved via
	// PATH lookup.
	command, args := pacaMCPBinPath, []string{}
	if e.opts.MCPDevSourceDir != "" {
		command = "/usr/bin/node"
		args = []string{sandbox.MCPDevMountPath + "/build/index.js"}
	}
	pacaEnv := envMapToList(env)
	servers = append(servers, acp.MCPServerConfig{
		Type:    acp.McpServerStdio,
		Name:    "paca",
		Command: command,
		Args:    &args,
		Env:     &pacaEnv,
	})
	return servers
}

func envMapToList(env map[string]string) []acp.EnvVariable {
	out := make([]acp.EnvVariable, 0, len(env))
	for k, v := range env {
		out = append(out, acp.EnvVariable{Name: k, Value: v})
	}
	return out
}
