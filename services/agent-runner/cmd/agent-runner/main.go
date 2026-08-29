// Command agent-runner consumes paca:agent:triggers for llm-type agents,
// runs each conversation in a dedicated Goose sandbox container over ACP,
// and publishes events back to paca:agent:events — the Go replacement for
// services/ai-agent's llm-type execution path.
//
// This file is deliberately thin — it only constructs dependencies and
// wires them into a handler.Handler. The actual per-message behavior lives
// in internal/handler so it can be driven directly by tests (including
// test/e2e's real-infra suite) without going through the full binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/acpbridge"
	"github.com/Paca-AI/agent-runner/internal/bundledskills"
	"github.com/Paca-AI/agent-runner/internal/chatsandbox"
	"github.com/Paca-AI/agent-runner/internal/config"
	"github.com/Paca-AI/agent-runner/internal/executor"
	"github.com/Paca-AI/agent-runner/internal/handler"
	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/registry"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
	"github.com/Paca-AI/agent-runner/internal/sandbox"
	dockersandbox "github.com/Paca-AI/agent-runner/internal/sandbox/docker"
	k8ssandbox "github.com/Paca-AI/agent-runner/internal/sandbox/k8s"
	"github.com/Paca-AI/agent-runner/internal/secret"
)

// idleReaperInterval is how often the chat-sandbox idle reaper wakes up to
// check for conversations past ChatSandboxIdleTimeout — mirrors
// reap_idle_chat_sandboxes's asyncio.sleep(20).
const idleReaperInterval = 20 * time.Second

// environmentReaperInterval is how often the static-environment idle
// reaper wakes up to check for environments past their own per-row
// idle_timeout_minutes (see EnvironmentRepository.ListIdleRunningEnvironments).
// A static environment's idle timeout is measured in minutes (environments.
// idle_timeout_minutes, DB default 60) — much coarser than a chat
// sandbox's (idleReaperInterval above polls every 20s for a timeout
// usually measured in a few minutes), so polling once a minute is plenty
// responsive at that grain without adding meaningful extra DB load.
const environmentReaperInterval = 1 * time.Minute

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := run(log); err != nil {
		log.Error("agent-runner: fatal", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	settings, err := config.Load()
	if err != nil {
		return err
	}

	db, err := postgres.Open(settings.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	redisOpts, err := redis.ParseURL(settings.ValkeyURL)
	if err != nil {
		return fmt.Errorf("main: parse VALKEY_URL: %w", err)
	}
	redisClient := redis.NewClient(redisOpts)
	defer func() { _ = redisClient.Close() }()

	keyBytes, err := secret.DecodeHexKey(settings.EncryptionKey)
	if err != nil {
		return fmt.Errorf("main: %w", err)
	}
	encryptor, err := secret.NewEncryptor(keyBytes)
	if err != nil {
		return fmt.Errorf("main: %w", err)
	}

	sandboxBackend, err := newSandboxBackend(settings)
	if err != nil {
		return fmt.Errorf("main: %w", err)
	}

	envRepo := postgres.NewEnvironmentRepository(db)
	sshKeyRepo := postgres.NewSSHKeyRepository(db)
	portForwardRepo := postgres.NewPortForwardRepository(db)
	convRepo := postgres.NewConversationRepository(db)

	exec := executor.New(sandboxBackend, envRepo, convRepo, portForwardRepo, encryptor, executor.Options{
		Image:           settings.AgentServerImage,
		PacaAPIKey:      settings.PacaAPIKey,
		PacaAPIURL:      settings.PacaAPIURL,
		PacaGatewayURL:  settings.PacaGatewayURL,
		MCPDevSourceDir: settings.MCPDevSourceDir,
		PortForwardHost: settings.PortForwardHost,
	}, log)

	chatSandboxes := chatsandbox.New()
	inFlight := registry.New()
	publisher := messaging.NewPublisher(redisClient)
	agentRepo := postgres.NewAgentRepository(db)

	acpRegistry := acpbridge.New(redisClient, publisher, log)
	acpDispatcher := &acpbridge.Dispatcher{
		Registry:  acpRegistry,
		ConvRepo:  convRepo,
		Publisher: publisher,
		Log:       log,
	}

	h := &handler.Handler{
		Gate:            config.NewGate(settings.AllowedAgentIDs),
		AgentRepo:       agentRepo,
		ConvRepo:        convRepo,
		BundledSkills:   bundledskills.NewClient(settings.PacaAPIURL),
		Publisher:       publisher,
		Executor:        exec,
		InFlight:        inFlight,
		ChatSandboxes:   chatSandboxes,
		ACPDispatcher:   acpDispatcher,
		ACPRegistry:     acpRegistry,
		EnvironmentRepo: envRepo,
		Log:             log,
	}

	consumer := messaging.NewConsumer(redisClient, settings.WorkerConcurrency, h.Handle, h.HandleControl, log)

	acpServer := &acpbridge.Server{
		Registry:              acpRegistry,
		AgentRepo:             agentRepo,
		ConvRepo:              convRepo,
		Publisher:             publisher,
		InternalToken:         settings.InternalAPIKey,
		LLMModelsPath:         settings.LLMModelsPath,
		SandboxMgr:            sandboxBackend,
		EnvironmentRepo:       envRepo,
		SSHKeyRepo:            sshKeyRepo,
		SSHPortRangeStart:     settings.SSHBastionPortRangeStart,
		SSHPortRangeEnd:       settings.SSHBastionPortRangeEnd,
		PortForwardRepo:       portForwardRepo,
		PortForwardRangeStart: settings.PortForwardRangeStart,
		PortForwardRangeEnd:   settings.PortForwardRangeEnd,
		Backend:               settings.SandboxBackend,
		MCPDevSourceDir:       settings.MCPDevSourceDir,
		Log:                   log,
	}
	httpServer := &http.Server{Addr: settings.HTTPAddr, Handler: acpServer.Routes()}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("agent-runner: starting",
		"image", settings.AgentServerImage,
		"sandbox_backend", settings.SandboxBackend,
		"allowed_agent_ids", settings.AllowedAgentIDs,
		"chat_sandbox_idle_timeout", settings.ChatSandboxIdleTimeout,
		"http_addr", settings.HTTPAddr,
		"mcp_dev_source_dir", settings.MCPDevSourceDir,
	)
	reconcileEnvironmentsOnStartup(ctx, envRepo, encryptor, acpServer, settings.MCPDevSourceDir, log)
	go reapIdleChatSandboxes(ctx, h, chatSandboxes, inFlight, settings.ChatSandboxIdleTimeout, log)
	go reapIdleEnvironments(ctx, envRepo, sandboxBackend, log)
	go runHTTPServer(ctx, httpServer, log)
	consumer.Run(ctx)
	return nil
}

// newSandboxBackend builds the sandbox.FullBackend settings.SandboxBackend
// selects — dockersandbox.Manager (one Docker container per conversation,
// reached via a mounted /var/run/docker.sock) for "docker", or
// k8ssandbox.Manager (one Kubernetes Job per conversation) for
// "kubernetes". config.Load already rejects any other value, so the
// switch's default case here is unreachable in practice — kept as an
// explicit error rather than a silent fallback so a future third backend
// value can't slip through unwired.
//
// Returns sandbox.FullBackend, not sandbox.Backend — widened so the same
// value can drive both the disposable-per-conversation path (executor.
// Executor) and the static-environment path (internal/acpbridge's
// environment endpoints/terminal WS, and the idle reaper below).
// Backward compatible: FullBackend embeds Backend, so every existing call
// site that only needs Backend's four methods keeps compiling unchanged.
func newSandboxBackend(settings config.Settings) (sandbox.FullBackend, error) {
	switch settings.SandboxBackend {
	case "kubernetes":
		return k8ssandbox.NewManager(k8ssandbox.Options{
			Namespace:        settings.SandboxNamespace,
			CPULimit:         settings.SandboxCPULimit,
			MemoryLimit:      settings.SandboxMemoryLimit,
			ImagePullSecrets: settings.SandboxImagePullSecrets,
			// Read only by CreateEnvironment/StartEnvironment as the
			// fallback when sandbox.EnvironmentConfig.Image is empty — see
			// that field's own doc comment. Every ephemeral-sandbox call
			// still goes through executor.Options.Image (Start's cfg.Image
			// is always the caller-supplied one), unaffected by this.
			AgentServerImage: settings.AgentServerImage,
			// StorageClassName for every static environment's
			// PersistentVolumeClaim — see EnvironmentsStorageClassName's own
			// doc comment on why leaving this empty (the default) is
			// meaningfully different from hardcoding one.
			EnvironmentsStorageClassName: settings.SandboxEnvironmentsStorageClass,
		})
	case "docker":
		mgr, err := dockersandbox.NewManager(settings.PortPoolStart, settings.PortPoolSize)
		if err != nil {
			return nil, err
		}
		// Same fallback role as k8ssandbox.Options.AgentServerImage above —
		// dockersandbox.NewManager takes no Options struct to set this at
		// construction time, so it's set directly on the returned *Manager
		// instead.
		mgr.AgentServerImage = settings.AgentServerImage
		return mgr, nil
	default:
		return nil, fmt.Errorf("main: unknown SANDBOX_BACKEND %q", settings.SandboxBackend)
	}
}

// runHTTPServer serves internal/acpbridge.Server's routes until ctx is
// done, then shuts down gracefully. Mirrors services/ai-agent's uvicorn
// process — one Go binary now covers both the trigger-consuming role
// consumer.Run drives and the ACP bridge role this covers, the same way
// services/ai-agent's single asyncio event loop did.
func runHTTPServer(ctx context.Context, srv *http.Server, log *slog.Logger) {
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("agent-runner: acp bridge http server failed", "error", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Warn("agent-runner: acp bridge http server shutdown error", "error", err)
		}
	}
}

// reapIdleChatSandboxes periodically tears down chat sandboxes idle longer
// than idleTimeout — mirrors executor.py's reap_idle_chat_sandboxes: the
// disconnect-detection mechanism for a chat conversation whose frontend tab
// stopped sending "agent.heartbeat" control messages (closed, crashed, lost
// network), since nothing else would ever notice and stop that sandbox.
// Runs until ctx is done. Reuses h.TeardownPausedChatSandbox — the same
// stop/status-write/publish path HandleControl's stop case uses — rather
// than a second hand-copied version of that logic.
func reapIdleChatSandboxes(
	ctx context.Context,
	h *handler.Handler,
	chatSandboxes *chatsandbox.Registry,
	inFlight *registry.Conversations,
	idleTimeout time.Duration,
	log *slog.Logger,
) {
	ticker := time.NewTicker(idleReaperInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, convID := range chatSandboxes.FindIdle(time.Now(), idleTimeout, inFlight.IsRegistered) {
				log.Info("agent-runner: reaping idle chat sandbox", "conversation_id", convID)
				h.TeardownPausedChatSandbox(ctx, convID)
			}
		}
	}
}

// reapIdleEnvironments periodically stops static environments that have
// been "running" longer than their own idle_timeout_minutes with no
// activity — the DB-backed counterpart to reapIdleChatSandboxes above (see
// docs/ai-agent/environment-management.md's "Idle-suspend" section).
// DB-backed rather than in-memory specifically so this is safe if
// agent-runner ever runs more than one replica: ClaimEnvironmentStatus's
// atomic status-guarded UPDATE means at most one replica's reaper actually
// stops any given environment, even if more than one observes it as idle
// in the same tick. Runs until ctx is done.
func reapIdleEnvironments(
	ctx context.Context,
	envRepo *postgres.EnvironmentRepository,
	sandboxMgr sandbox.FullBackend,
	log *slog.Logger,
) {
	ticker := time.NewTicker(environmentReaperInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			envs, err := envRepo.ListIdleRunningEnvironments(ctx)
			if err != nil {
				log.Warn("agent-runner: failed to list idle environments", "error", err)
				continue
			}
			for _, env := range envs {
				reapOneIdleEnvironment(ctx, envRepo, sandboxMgr, env, log)
			}
		}
	}
}

// reapOneIdleEnvironment claims env for stopping (skipping it if another
// replica already won the race), stops its backing container/Pod, and
// records the outcome — "stopped" on success, "error" (with the failure
// message, never left stuck in "stopping") otherwise. Nothing further to
// clean up: stopping the container/Pod already stops whatever was
// publishing its ports (see handleStopEnvironment's own doc comment in
// environment_handlers.go — this reaper stops environments directly via
// sandboxMgr, bypassing that HTTP handler entirely, but there's no
// handler-side cleanup step left to duplicate here anymore).
//
// Checks for a live SSH session before actually stopping anything — see
// sandbox.EnvironmentHasActiveSSHSession's own doc comment for why
// last_active_at alone can't be trusted here: real SSH access never
// touches it at all, so an environment can look idle by that column while
// a user is, at this exact moment, typing in a real terminal over it. If
// one is found, this un-claims back to "running" and touches
// last_active_at (so the next tick's ListIdleRunningEnvironments query
// won't immediately re-flag it) instead of stopping — the session itself
// is what keeps the environment alive for as long as it stays open, the
// same way a conversation turn or the browser terminal already do.
func reapOneIdleEnvironment(
	ctx context.Context,
	envRepo *postgres.EnvironmentRepository,
	sandboxMgr sandbox.FullBackend,
	env *postgres.Environment,
	log *slog.Logger,
) {
	won, err := envRepo.ClaimEnvironmentStatus(ctx, env.ID, "running", "stopping")
	if err != nil {
		log.Warn("agent-runner: failed to claim idle environment for stop", "environment_id", env.ID, "error", err)
		return
	}
	if !won {
		// Another agent-runner replica's reaper claimed it first this tick
		// — nothing further to do here.
		return
	}
	if env.BackendRef == nil || *env.BackendRef == "" {
		log.Warn("agent-runner: idle environment claimed for stop has no backend_ref", "environment_id", env.ID)
		errMsg := "idle reaper: environment claimed for stop but has no backend_ref"
		if updErr := envRepo.UpdateEnvironmentStatus(ctx, env.ID, "error", nil, &errMsg); updErr != nil {
			log.Warn("agent-runner: failed to record environment error status", "environment_id", env.ID, "error", updErr)
		}
		return
	}

	if active, err := sandbox.EnvironmentHasActiveSSHSession(ctx, sandboxMgr, *env.BackendRef); err != nil {
		// Best-effort: can't tell either way (e.g. the container briefly
		// unreachable) — fall through and reap as already decided, rather
		// than let a transient check failure pin an environment "running"
		// forever.
		log.Warn("agent-runner: failed to check for active ssh sessions before reaping", "environment_id", env.ID, "error", err)
	} else if active {
		log.Info("agent-runner: skipping idle reap — an ssh session is still open", "environment_id", env.ID)
		if err := envRepo.UpdateEnvironmentStatus(ctx, env.ID, "running", nil, nil); err != nil {
			log.Warn("agent-runner: failed to un-claim environment kept alive by an ssh session", "environment_id", env.ID, "error", err)
		}
		if err := envRepo.TouchEnvironment(ctx, env.ID); err != nil {
			log.Warn("agent-runner: failed to touch environment kept alive by an ssh session", "environment_id", env.ID, "error", err)
		}
		return
	}

	log.Info("agent-runner: reaping idle environment", "environment_id", env.ID)
	if err := sandboxMgr.StopEnvironment(ctx, *env.BackendRef); err != nil {
		log.Warn("agent-runner: failed to stop idle environment", "environment_id", env.ID, "error", err)
		errMsg := err.Error()
		if updErr := envRepo.UpdateEnvironmentStatus(ctx, env.ID, "error", nil, &errMsg); updErr != nil {
			log.Warn("agent-runner: failed to record environment stop error", "environment_id", env.ID, "error", updErr)
		}
		return
	}
	if err := envRepo.UpdateEnvironmentStatus(ctx, env.ID, "stopped", nil, nil); err != nil {
		log.Warn("agent-runner: failed to record stopped status for idle environment", "environment_id", env.ID, "error", err)
	}
}

// reconcileEnvironmentsOnStartup verifies every environment the database
// believes is "running" still actually has a live backing container/Pod —
// the fix for a gap only agent-runner's own downtime can create. While
// it's up, Start/Stop/Delete are the only ways an environment's
// container/Pod goes away, and each already keeps status in sync (see
// docker.Manager.StopEnvironment/DeleteEnvironment's own not-found
// tolerance). But a user running `docker rm`/`kubectl delete` directly, a
// host reboot that clears containers, or `docker system prune` while
// agent-runner itself was stopped, leaves a "running" row with no way to
// notice until this runs — and until it does, the row's lie means
// services/api's own StartEnvironment short-circuits on "already running"
// without ever asking agent-runner, so the self-heal below never gets a
// chance to fire on its own.
//
// Run once, synchronously, from run() before the HTTP server/consumer
// start accepting anything — see that call site's own comment for why
// synchronous, not a background goroutine. Reuses
// acpbridge.Server.StartEnvironmentByID (the same method
// handleStartEnvironment's HTTP path calls), so a self-heal here gets the
// exact same port-mapping/SSH-key re-bootstrap treatment a normal
// user-triggered Start does, and it's safe to call unconditionally for the
// same reason a plain StartEnvironment call always is (see that method's
// own doc comment on both backends): an environment that's still
// genuinely fine comes back with no meaningful change, just a slightly
// slower boot.
func reconcileEnvironmentsOnStartup(
	ctx context.Context,
	envRepo *postgres.EnvironmentRepository,
	encryptor *secret.Encryptor,
	acpServer *acpbridge.Server,
	mcpDevSourceDir string,
	log *slog.Logger,
) {
	envs, err := envRepo.ListRunningEnvironments(ctx)
	if err != nil {
		log.Warn("agent-runner: failed to list running environments for startup reconciliation", "error", err)
		return
	}
	if len(envs) == 0 {
		return
	}
	log.Info("agent-runner: reconciling running environments against their backing containers/Pods", "count", len(envs))
	for _, env := range envs {
		reconcileOneEnvironment(ctx, envRepo, encryptor, acpServer, mcpDevSourceDir, env, log)
	}
}

// reconcileOneEnvironment is reconcileEnvironmentsOnStartup's per-row
// worker — see that function's own doc comment for why this runs at all.
// Builds the same "context-free caller" EnvironmentConfig
// handleStartEnvironment's plain restart already uses (no cfg.Env): this
// reconciler has no attaching conversation/agent to source one from, and
// both backends' ensureEnvironmentInfraEnv already no-op on an empty
// cfg.Env rather than treat it as "clear every key" (see that method's own
// doc comment on each backend) — exactly the behavior wanted here.
func reconcileOneEnvironment(
	ctx context.Context,
	envRepo *postgres.EnvironmentRepository,
	encryptor *secret.Encryptor,
	acpServer *acpbridge.Server,
	mcpDevSourceDir string,
	env *postgres.Environment,
	log *slog.Logger,
) {
	if env.BackendRef == nil || *env.BackendRef == "" {
		log.Warn("agent-runner: running environment has no backend_ref, skipping startup reconciliation", "environment_id", env.ID)
		return
	}

	secretKey, err := encryptor.Decrypt(env.SecretKeyEncrypted)
	if err != nil {
		log.Warn("agent-runner: failed to decrypt secret key during startup reconciliation", "environment_id", env.ID, "error", err)
		return
	}

	var image string
	if env.Image != nil {
		image = *env.Image
	}

	// self-heals via StartEnvironmentByID -> SandboxMgr.StartEnvironment on
	// both backends when the container/Pod is gone but its volume/PVC
	// survives (docker.Manager.recreateGoneEnvironmentContainer,
	// k8s.Manager.recreateGoneEnvironmentDeployment) — only a genuinely
	// unrecoverable environment (volume/PVC gone too) or an unrelated
	// backend failure reaches the error branch below.
	_, _, recreatedBackendRef, err := acpServer.StartEnvironmentByID(ctx, env.ID, *env.BackendRef, sandbox.EnvironmentConfig{
		EnvironmentID:   env.ID.String(),
		Image:           image,
		CPULimit:        env.CPULimit,
		MemoryLimit:     env.MemoryLimit,
		DiskLimitGB:     env.DiskLimitGB,
		DockerEnabled:   env.DockerEnabled,
		SecretKey:       secretKey,
		MCPDevSourceDir: mcpDevSourceDir,
	})
	if err != nil {
		// Uniform treatment for ErrEnvironmentGone and any other failure
		// alike (a transient backend hiccup included) — mirrors
		// reapOneIdleEnvironment's own "any sandbox-operation failure ->
		// error status" convention just above, rather than inventing a
		// different policy for this reaper.
		log.Warn("agent-runner: environment failed startup reconciliation", "environment_id", env.ID, "error", err)
		errMsg := err.Error()
		if updErr := envRepo.UpdateEnvironmentStatus(ctx, env.ID, "error", nil, &errMsg); updErr != nil {
			log.Warn("agent-runner: failed to record environment error status", "environment_id", env.ID, "error", updErr)
		}
		return
	}

	var backendRefPtr *string
	if recreatedBackendRef != "" {
		backendRefPtr = &recreatedBackendRef
		log.Info("agent-runner: environment container/Pod was recreated from its existing volume during startup reconciliation",
			"environment_id", env.ID, "backend_ref", recreatedBackendRef)
	}
	if won, err := envRepo.ClaimEnvironmentRunning(ctx, env.ID, backendRefPtr); err != nil {
		log.Warn("agent-runner: failed to persist environment status after startup reconciliation", "environment_id", env.ID, "error", err)
	} else if !won {
		log.Info("agent-runner: environment status changed concurrently during startup reconciliation, leaving it as-is", "environment_id", env.ID)
	}
}
