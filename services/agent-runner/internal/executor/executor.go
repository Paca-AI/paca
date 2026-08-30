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

// environmentGooseDataDir is where coldStartEnvironment points GOOSE_PATH_ROOT
// for every static-environment container — a dot-prefixed subdirectory of
// the same persisted workspace mount internal/sandbox/environmentssh.go's
// environmentWorkspaceRoot names (mirrored, not imported, the same way the
// docker/k8s sandbox packages each keep their own copy of that literal), so
// it survives recreateGoneEnvironmentContainer/recreateEnvironmentContainer
// exactly like environmentSSHHostKeyDir already does.
//
// Without this, goose serve's default session store lives under
// /home/goose/.local/share/goose (or /root/... — see conversation
// 8b325e33-567f-46d5-8224-8df899429036: verified directly against a live
// environment container's filesystem, not assumed), which is NOT under the
// persisted volume — only /home/paca/workspaces itself is. Every
// environment container is disposable (idle-reaped, then recreated fresh
// against the same volume on the next attach — see
// docker.Manager.recreateGoneEnvironmentContainer's doc comment), so a
// session store outside that volume is wiped every time that happens,
// leaving attachEnvironmentSession's LoadSession call with nothing to
// resume: it fails "Session not found", and the fallback it documents (a
// brand-new, context-free session) silently takes over — exactly what
// happened in that conversation. Relocating the store onto the volume via
// GOOSE_PATH_ROOT (confirmed empirically: it relocates goose's config/data/
// state dirs, sessions.db included, wholesale) makes LoadSession actually
// have something to find after a recreate.
const environmentGooseDataDir = "/home/paca/workspaces/.goose"

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
	// PortForwardHost is config.Settings.PortForwardHost (PORT_FORWARD_HOST)
	// — the descriptive external hostname a static environment's port
	// forwards are reachable at. Used by coldStartEnvironment to build the
	// agent-facing address list buildEnvironmentContext returns; empty on a
	// deployment that hasn't configured one, in which case a forward is
	// still described, just without an address (see that function's own
	// doc comment).
	PortForwardHost string
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
	// portForwardRepo lets coldStartEnvironment read an environment's
	// already-assigned port forwards, fed into environmentPortMappings
	// (alongside env's own SSHPort field, so a mid-attach container
	// recreate — recreateEnvironmentIfMissingEnv — never silently drops
	// them, see that method's own doc comment) and into
	// buildEnvironmentContext (the agent-facing "## Static Environment"
	// note). Same nil-safety convention as envRepo/convRepo.
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
	// timeoutFor mints a fresh timeoutMinutes-bounded context from ctx (the
	// caller's own, not turnCtx) — used below to give
	// coldStartEnvironment's attach phase its own deadline, separate from
	// turnCtx's. Every acp.Client call must still be bounded by something
	// (see defaultTimeoutMinutes's own doc comment on why — a caller-
	// supplied deadline is the only thing that ever stops a hung one); this
	// just avoids one phase's cost silently eating into another's.
	timeoutFor := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(ctx, time.Duration(timeoutMinutes)*time.Minute)
	}

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
		// Its own attachCtx, not turnCtx: session/load's full-history
		// replay (see attachEnvironmentSession's own doc comment) scales
		// with this conversation's length, unlike coldStart's cheap,
		// roughly-constant-time NewSession below — sharing turnCtx's single
		// deadline across both this attach phase and the client.Prompt call
		// further down would let a long replay eat into the LLM call's own
		// budget, making a long-lived environment conversation
		// progressively more likely to time out as its history grows.
		// attachCtx gets the same nominal timeoutMinutes window, just
		// starting fresh from now instead of sharing turnCtx's clock —
		// turnCtx itself is then reset to a fresh window of its own for
		// Prompt below, once this phase is done.
		attachCtx, cancelAttach := timeoutFor()
		defer cancelAttach()
		var err error
		var envNote string
		client, sessionID, envNote, err = e.coldStartEnvironment(ctx, attachCtx, cfg, trigger)
		if err != nil {
			return Result{}, err
		}
		var cancelPrompt context.CancelFunc
		turnCtx, cancelPrompt = timeoutFor()
		defer cancelPrompt()
		// envNote (buildEnvironmentContext's output — see
		// coldStartEnvironment) comes first: it's environment/infra state
		// from outside trigger, not something buildInitialMessage's own
		// plain agent.Trigger input can build on its own, and it already
		// ends with its own "\n\n" separator (see that function's doc
		// comment) so it flows straight into buildInitialMessage's own
		// leading "## ..." section.
		message = envNote + buildInitialMessage(trigger)
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
//
// Takes forwards pre-fetched by the caller (coldStartEnvironment), rather
// than querying portForwardRepo itself as an earlier version of this
// function did: coldStartEnvironment needs that same forward list a second
// time, to build the agent-facing note buildEnvironmentContext returns, so
// fetching it once and passing it to both avoids a redundant query every
// turn. Pure and receiver-free as a result — confirmed via a repo-wide
// grep that coldStartEnvironment's own StartEnvironment call is this
// function's only caller.
func environmentPortMappings(env *postgres.Environment, forwards []*postgres.PortForward) []sandbox.PortMapping {
	var mappings []sandbox.PortMapping
	if env.SSHPort != nil {
		mappings = append(mappings, sandbox.PortMapping{ContainerPort: sandbox.EnvironmentSSHPort, HostPort: *env.SSHPort})
	}
	for _, pf := range forwards {
		if pf.HostPort == nil {
			continue
		}
		mappings = append(mappings, sandbox.PortMapping{ContainerPort: pf.ContainerPort, HostPort: *pf.HostPort})
	}
	return mappings
}

// environmentReadyToStart reports whether env's current row state allows
// starting/attaching to it — see coldStartEnvironment's own comment below
// (its first check of this) for why each excluded status is excluded.
// Factored out so coldStartEnvironment can run the exact same check twice
// without the two copies drifting apart: once immediately (a clearly-
// broken environment fails fast, before decrypting secrets or listing port
// forwards for nothing), and again right after LockEnvironmentForStart is
// acquired, against a freshly-re-read env — see that second call site's
// own comment for why the first check alone isn't enough.
func environmentReadyToStart(env *postgres.Environment) bool {
	return env.BackendRef != nil && *env.BackendRef != "" &&
		env.Status != "error" && env.Status != "deleting" && env.Status != "stopping"
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
// File-skills ARE delivered here, unlike an earlier version of this
// function — but targeted at sandboxWorkdir (the container's actual
// $HOME), not trigger.Workdir: Goose's skill loader has a home-rooted
// global lookup (~/.agents/skills) alongside its cwd-relative one (see
// skills.go's skillsRelDir doc comment), and trigger.Workdir is inside a
// folder's own git checkout for an environment — writing agent-runner-
// managed files there risks an accidental commit or a collision with the
// repo's own content, which sandboxWorkdir sidesteps entirely since it's
// never part of any checkout.
//
// .goosehints is NOT delivered here, and can't be via the same trick:
// unlike skills, Goose reads .goosehints from cwd only, with no home-rooted
// fallback (see hints.go's own doc comment) — so writing it would mean
// writing into trigger.Workdir, the exact git-checkout collision problem
// above. That still means an environment-backed conversation's agent never
// gets its own system prompt or the bootstrapInstruction nudge to call
// load_skill(paca) first — a real, still-open gap, not an oversight (see
// docs/ai-agent/environment-management.md's Phase 1 scope). What it DOES
// get now, via a completely different channel: buildEnvironmentContext's
// "## Static Environment" note (persistent-environment framing plus the
// current port-forward list), prepended onto buildInitialMessage's own
// per-turn message in Run (see that function's trigger.EnvironmentID != nil
// case) rather than written to any file — that content is never part of
// the sandbox's filesystem at all, so it never runs into the
// cwd/git-checkout problem above.
func (e *Executor) coldStartEnvironment(ctx, turnCtx context.Context, cfg agent.Config, trigger agent.Trigger) (*acp.Client, string, string, error) {
	if e.envRepo == nil {
		return nil, "", "", fmt.Errorf("executor: no environment repository configured")
	}
	if trigger.EnvironmentID == nil {
		return nil, "", "", fmt.Errorf("executor: coldStartEnvironment called without an environment_id")
	}
	if trigger.Workdir == nil || *trigger.Workdir == "" {
		return nil, "", "", fmt.Errorf("executor: environment %s attach requires a workdir", *trigger.EnvironmentID)
	}
	environmentID := *trigger.EnvironmentID

	env, err := e.envRepo.FindEnvironmentByID(ctx, environmentID)
	if err != nil {
		return nil, "", "", fmt.Errorf("executor: find environment %s: %w", environmentID, err)
	}
	// No lazy creation here — see the doc comment above. A never-created
	// (BackendRef nil) or explicitly broken (error/deleting) environment
	// fails this turn clearly rather than silently doing nothing useful.
	// "stopping" is included too, distinctly from those: it means the idle
	// reaper (cmd/agent-runner/main.go's reapOneIdleEnvironment) has
	// already claimed this environment and is actively stopping its
	// container/Pod right now — starting it back up concurrently would
	// race that stop rather than cleanly losing to it. Failing this turn
	// is safe and retryable: the reaper's own stop finishes in well under
	// a turn's timeout, and the next attach attempt (this turn's own retry,
	// or simply the user's next message) finds status "stopped" instead
	// and proceeds normally.
	if !environmentReadyToStart(env) {
		return nil, "", "", fmt.Errorf("executor: environment is not ready (environment_id=%s, status=%s)", environmentID, env.Status)
	}

	// Fetched once, here, and reused below for both StartEnvironment's own
	// PortMappings and the agent-facing "## Static Environment" note
	// buildEnvironmentContext builds from this same snapshot — nothing
	// between here and this turn's Prompt call mutates port forward rows
	// or ports_pending_restart (only an explicit user action via
	// services/api does), so one fetch safely serves both instead of the
	// two separate queries an earlier version of this code ran. Nil-safe
	// and logged-not-fatal on error, same as environmentPortMappings
	// handled this same query itself before this existed: a port-forward
	// listing failure shouldn't fail the whole turn, only degrade
	// PortMappings to SSH-only and the note to its own honest
	// forwardsUnknown case rather than the "no forwards configured" one
	// (see buildEnvironmentContext's own doc comment for why those two
	// must stay distinguishable).
	var forwards []*postgres.PortForward
	forwardsUnknown := false
	if e.portForwardRepo != nil {
		forwards, err = e.portForwardRepo.ListForEnvironment(ctx, environmentID)
		if err != nil {
			e.log.Warn("executor: failed to list port forwards for environment attach",
				"environment_id", environmentID, "error", err)
			forwardsUnknown = true
		}
	}
	envNote := buildEnvironmentContext(forwards, e.opts.PortForwardHost, env.PortsPendingRestart, forwardsUnknown)

	secretKey, err := e.encryptor.Decrypt(env.SecretKeyEncrypted)
	if err != nil {
		return nil, "", "", fmt.Errorf("executor: decrypt environment %s secret key: %w", environmentID, err)
	}

	containerEnv, err := e.buildAgentContainerEnv(cfg)
	if err != nil {
		return nil, "", "", err
	}

	// See environmentGooseDataDir's own doc comment. Set unconditionally
	// (not just on first create) so ensureEnvironmentInfraEnv's
	// pacaInfraEnvKeys staleness check (docker/k8s sandbox packages) can
	// detect a container created before this existed and recreate it once
	// to backfill this, the same way it already does for
	// PACA_API_KEY/PACA_WORKDIR/etc.
	containerEnv["GOOSE_PATH_ROOT"] = environmentGooseDataDir

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

	// Held across the StartEnvironment call below — blocking, not
	// TryLockEnvironmentForStart's non-blocking sibling, since a
	// conversation attach has no "skip it, a later pass will retry" option
	// the way cmd/agent-runner/main.go's boot reconciler does: this turn
	// needs an actual client/session back. This is what keeps a concurrent
	// boot-time reconcile pass (or a concurrent HTTP-triggered Start, which
	// acquires this exact same lock — see handleStartEnvironment's own doc
	// comment) from racing this attach's own self-heal attempt: both
	// backends give a self-heal's ContainerCreate/Deployment-Create a
	// deterministic, environment-derived name, so two callers racing to
	// recreate the same gone container/Deployment would otherwise have the
	// loser fail on a name conflict — which, unhandled below, would
	// needlessly fail this turn and flip an environment the winner was
	// simultaneously fixing to a stuck "error" status.
	release, err := e.envRepo.LockEnvironmentForStart(ctx, environmentID)
	if err != nil {
		return nil, "", "", fmt.Errorf("executor: acquire start lock for environment %s: %w", environmentID, err)
	}
	defer release()

	// Re-read env now that the lock is actually held: the snapshot taken
	// above, before the lock, can be stale by the time we get here — a
	// concurrent self-heal (the boot reconciler, or a sibling attach/
	// HTTP-start that won this same lock first) can have already persisted
	// a new BackendRef via ClaimEnvironmentRunning while this call was
	// waiting to acquire it. On docker specifically, BackendRef is a
	// container ID that changes on every self-heal recreate (unlike k8s,
	// where it's the deterministic Deployment name and so never goes
	// stale this way) — using the stale ID below would make
	// StartEnvironment's own ContainerStart fail not-found, walk into the
	// gone-container self-heal branch a second time, and collide on the
	// exact deterministic container name the winner already claimed (see
	// docker.Manager.recreateGoneEnvironmentContainer's own doc comment),
	// failing this turn and writing the environment to a stuck "error"
	// status — precisely the outcome this lock exists to prevent. Also
	// re-runs the not-ready gate: env.Status can equally have moved to
	// "error"/"deleting"/"stopping" in that same window (an idle-reaper
	// stop racing this attach, say), and proceeding on a stale "it looked
	// fine a moment ago" would be just as wrong for status as for
	// BackendRef.
	env, err = e.envRepo.FindEnvironmentByID(ctx, environmentID)
	if err != nil {
		return nil, "", "", fmt.Errorf("executor: re-check environment %s after acquiring start lock: %w", environmentID, err)
	}
	if !environmentReadyToStart(env) {
		return nil, "", "", fmt.Errorf("executor: environment is not ready (environment_id=%s, status=%s)", environmentID, env.Status)
	}

	// Always calls StartEnvironment, even if env.Status already says
	// "running" — deliberate, not a bug: it's the only way to get a
	// guaranteed-fresh, reachable BaseURL back (never persisted — see
	// sandbox.EnvironmentHandle's doc comment), and it self-heals a stale
	// "running" DB status if the container died externally. Both backend
	// implementations tolerate this (idempotent start) — see
	// docs/ai-agent/environment-management.md's "Conversation attach path".
	handle, err := e.sandboxMgr.StartEnvironment(ctx, *env.BackendRef, sandbox.EnvironmentConfig{
		EnvironmentID:   environmentID.String(),
		Image:           image,
		Env:             containerEnv,
		CPULimit:        env.CPULimit,
		MemoryLimit:     env.MemoryLimit,
		DiskLimitGB:     env.DiskLimitGB,
		DockerEnabled:   env.DockerEnabled,
		SecretKey:       secretKey,
		PortMappings:    environmentPortMappings(env, forwards),
		MCPDevSourceDir: e.opts.MCPDevSourceDir,
	})
	if err != nil {
		errMsg := err.Error()
		if updErr := e.envRepo.UpdateEnvironmentStatus(context.WithoutCancel(ctx), environmentID, "error", nil, &errMsg); updErr != nil {
			e.log.Warn("executor: failed to record environment error status",
				"environment_id", environmentID, "error", updErr)
		}
		return nil, "", "", fmt.Errorf("executor: start environment %s: %w", environmentID, err)
	}

	// handle.BackendRef only differs from *env.BackendRef when the docker
	// backend had to recreate a container removed outside of Paca (see
	// docker.Manager.recreateGoneEnvironmentContainer's doc comment) — pass
	// it through non-nil so the COALESCE below persists the new value; nil
	// (leave backend_ref as-is) on the ordinary path where nothing changed.
	var newBackendRef *string
	if handle.BackendRef != "" && handle.BackendRef != *env.BackendRef {
		newBackendRef = &handle.BackendRef
	}
	// ClaimEnvironmentRunning, not a plain UpdateEnvironmentStatus: the
	// initial status check above already closed the common case, but the
	// reaper could still have claimed this environment "stopping" (or an
	// explicit delete could have started) in the gap between that check
	// and StartEnvironment returning just above — an unconditional write
	// here would silently stomp that back to "running" even though the
	// container may already be stopped. A lost race just leaves status as
	// whatever the reaper/delete set it to; ACP itself already succeeded
	// above regardless, so this turn still completes normally.
	if won, err := e.envRepo.ClaimEnvironmentRunning(ctx, environmentID, newBackendRef); err != nil {
		e.log.Warn("executor: failed to record environment running status",
			"environment_id", environmentID, "error", err)
	} else if !won {
		e.log.Warn("executor: environment was claimed stopping/deleting concurrently with this attach — leaving its status as-is",
			"environment_id", environmentID)
	}
	if err := e.envRepo.TouchEnvironment(ctx, environmentID); err != nil {
		e.log.Warn("executor: failed to touch environment", "environment_id", environmentID, "error", err)
	}

	// Written before Initialize/NewSession, same ordering and reasoning
	// coldStart uses for its own skills tar (see that function's comment):
	// Goose's skills platform extension discovers SKILL.md files from disk,
	// so they must exist before anything that might read them. Targets
	// sandboxWorkdir, not trigger.Workdir — see this function's own doc
	// comment for why.
	fileSkills := prepareFileSkills(cfg.Skills)
	skillsTar, err := buildSkillsTar(fileSkills)
	if err != nil {
		return nil, "", "", fmt.Errorf("executor: build skills tar: %w", err)
	}
	if skillsTar != nil {
		if err := e.sandboxMgr.CopyToEnvironment(ctx, handle.BackendRef, sandboxWorkdir, skillsTar); err != nil {
			return nil, "", "", fmt.Errorf("executor: write skills to environment %s: %w", environmentID, err)
		}
	}

	client := acp.NewClient(handle.BaseURL, secretKey, nil)
	if err := client.Initialize(turnCtx); err != nil {
		return nil, "", "", fmt.Errorf("executor: acp initialize (environment %s): %w", environmentID, err)
	}

	sessionID, err := e.attachEnvironmentSession(turnCtx, client, trigger.ConversationID, *trigger.Workdir, mcpServers)
	if err != nil {
		// Same reasoning as coldStart's matching branch: Initialize above
		// already started client's connection-scoped SSE reader goroutine,
		// so it must be closed here rather than dropped.
		client.Close()
		return nil, "", "", fmt.Errorf("executor: acp session attach (environment %s): %w", environmentID, err)
	}

	return client, sessionID, envNote, nil
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
// stale (failed-to-load) session is treated the same as no session at
// all — logged, not fatal — since a fresh session is always a safe,
// working fallback; the only thing lost is history a container recreation
// had already made unrecoverable. A failure to even *look up* the stored
// id is different and NOT treated this way — see the error branch below —
// since silently minting a fresh session there would overwrite a real,
// still-resumable one.
func (e *Executor) attachEnvironmentSession(ctx context.Context, client *acp.Client, conversationID uuid.UUID, workdir string, mcpServers []acp.MCPServerConfig) (string, error) {
	if e.convRepo != nil {
		stored, err := e.convRepo.GetACPSessionID(ctx, conversationID)
		if err != nil {
			// A real error here (a DB blip, not "no session yet" — see
			// GetACPSessionID's own doc comment on that distinction) must
			// not fall through to NewSession: the fallback below persists
			// whatever session id it mints over the one already stored,
			// so treating a transient read failure as "no session" would
			// silently and permanently destroy this conversation's real,
			// resumable history the moment it happened to race a DB
			// hiccup. Fail the turn instead — it's retryable, and nothing
			// has been overwritten yet.
			return "", fmt.Errorf("executor: look up stored acp session id: %w", err)
		}
		if stored != "" {
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
	if trigger.Workdir != nil {
		// Only set for an environment-attached conversation (trigger.Workdir
		// is nil for an ephemeral one) — tells apps/mcp's clone_repository
		// where THIS conversation's actual working directory is, so its
		// default target dir (otherwise hardcoded to the ephemeral
		// sandbox's own /home/goose/repo — see DEFAULT_REPO_DIR's doc
		// comment there) resolves to the environment folder the agent was
		// actually attached to instead. Without this, an agent that calls
		// clone_repository without an explicit targetDir clones into the
		// wrong place even though session/new's own cwd was set correctly.
		env["PACA_WORKDIR"] = *trigger.Workdir
	}
	// Dev override: run the Paca MCP server from a locally-mounted apps/mcp
	// checkout (see sandbox.Config.MCPDevSourceDir /
	// sandbox.EnvironmentConfig.MCPDevSourceDir) instead of the image's
	// globally npm-installed @paca-ai/paca-mcp, so a local source change is
	// live on the next conversation without an npm publish + image rebuild.
	// /usr/bin/node is the same absolute-path requirement pacaMCPBinPath's
	// doc comment explains — ACP rejects a bare command name resolved via
	// PATH lookup.
	//
	// Applies to both ephemeral and environment-attached conversations:
	// both sandbox.Config (docker.Manager.Start) and
	// sandbox.EnvironmentConfig (docker.Manager.createAndStartEnvironment
	// Container, shared by CreateEnvironment/StartEnvironment's self-heal
	// recreate) bind-mount MCPDevSourceDir at sandbox.MCPDevMountPath on
	// the docker backend; the kubernetes backend rejects a non-empty
	// MCPDevSourceDir outright in both Manager.Start and
	// Manager.CreateEnvironment, so this path is never reachable there.
	// This used to be ephemeral-only — using the dev command path for an
	// environment-attached conversation while only the ephemeral path had
	// the bind mount pointed /usr/bin/node at a file that was never
	// mounted into that container, so the "paca" MCP subprocess failed to
	// start at all, silently zeroing out its tools with no protocol-level
	// error (same "Tool 'X' not found" symptom
	// recreateEnvironmentIfMissingEnv's PACA_API_KEY sibling bug produces,
	// for an unrelated reason) — fixed by adding the missing mount instead
	// of leaving the gate in place, so local apps/mcp iteration works the
	// same way for both conversation kinds. The npm-installed
	// pacaMCPBinPath, by contrast, is baked into every image regardless of
	// backend, so it always exists.
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
