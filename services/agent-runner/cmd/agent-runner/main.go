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

	"github.com/google/uuid"
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
	"github.com/Paca-AI/agent-runner/internal/turnruntime"
)

// idleReaperInterval is how often the chat-sandbox idle reaper wakes up to
// check for conversations past ChatSandboxIdleTimeout — mirrors
// reap_idle_chat_sandboxes's asyncio.sleep(20).
const (
	idleReaperInterval          = 20 * time.Second
	authoritativeReaperInterval = 30 * time.Second
)

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
	sandboxReaper, ok := sandboxBackend.(sandbox.AuthoritativeOrphanReaper)
	if !ok {
		return fmt.Errorf("main: sandbox backend %q does not implement authoritative orphan cleanup", settings.SandboxBackend)
	}

	exec := executor.New(sandboxBackend, encryptor, executor.Options{
		Image:           settings.AgentServerImage,
		PacaAPIKey:      settings.PacaAPIKey,
		PacaAPIURL:      settings.PacaAPIURL,
		PacaGatewayURL:  settings.PacaGatewayURL,
		MCPDevSourceDir: settings.MCPDevSourceDir,
	}, log)

	chatSandboxes := chatsandbox.New()
	inFlight := registry.New()
	publisher := messaging.NewPublisher(redisClient)
	agentRepo := postgres.NewAgentRepository(db)
	convRepo := postgres.NewConversationRepository(db)

	acpRegistry := acpbridge.New(redisClient, publisher, log)
	acpDispatcher := &acpbridge.Dispatcher{
		Registry:  acpRegistry,
		ConvRepo:  convRepo,
		Publisher: publisher,
		Log:       log,
	}

	turnClient := turnruntime.NewClient(settings.PacaAPIURL, settings.InternalAPIKey)
	h := &handler.Handler{
		Gate:          config.NewGate(settings.AllowedAgentIDs),
		AgentRepo:     agentRepo,
		ConvRepo:      convRepo,
		BundledSkills: bundledskills.NewClient(settings.PacaAPIURL),
		Publisher:     publisher,
		Executor:      exec,
		InFlight:      inFlight,
		ChatSandboxes: chatSandboxes,
		ACPDispatcher: acpDispatcher,
		ACPRegistry:   acpRegistry,
		TurnRuntime:   turnClient,
		WorkerID:      "agent-runner:" + uuid.NewString(),
		Log:           log,
	}

	consumer := messaging.NewConsumer(redisClient, settings.WorkerConcurrency, h.Handle, h.HandleControl, log)
	turnConsumer := messaging.NewTurnConsumer(redisClient, settings.WorkerConcurrency, h.HandleTurn, log)
	turnControlConsumer := messaging.NewTurnControlConsumer(redisClient, h.HandleTurnControl, log)

	acpServer := &acpbridge.Server{
		Registry:      acpRegistry,
		AgentRepo:     agentRepo,
		ConvRepo:      convRepo,
		Publisher:     publisher,
		TurnRuntime:   turnClient,
		InternalToken: settings.InternalAPIKey,
		LLMModelsPath: settings.LLMModelsPath,
		Log:           log,
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
	go reapIdleChatSandboxes(ctx, h, chatSandboxes, inFlight, settings.ChatSandboxIdleTimeout, log)
	go reapAuthoritativeSandboxes(ctx, sandboxReaper, turnClient, log)
	go runHTTPServer(ctx, httpServer, log)
	go turnConsumer.Run(ctx)
	go turnControlConsumer.Run(ctx)
	consumer.Run(ctx)
	return nil
}

// newSandboxBackend builds the sandbox.Backend settings.SandboxBackend
// selects — dockersandbox.Manager (one Docker container per conversation,
// reached via a mounted /var/run/docker.sock) for "docker", or
// k8ssandbox.Manager (one Kubernetes Job per conversation) for
// "kubernetes". config.Load already rejects any other value, so the
// switch's default case here is unreachable in practice — kept as an
// explicit error rather than a silent fallback so a future third backend
// value can't slip through unwired.
func newSandboxBackend(settings config.Settings) (sandbox.Backend, error) {
	switch settings.SandboxBackend {
	case "kubernetes":
		return k8ssandbox.NewManager(k8ssandbox.Options{
			Namespace:        settings.SandboxNamespace,
			CPULimit:         settings.SandboxCPULimit,
			MemoryLimit:      settings.SandboxMemoryLimit,
			ImagePullSecrets: settings.SandboxImagePullSecrets,
		})
	case "docker":
		return dockersandbox.NewManager(settings.PortPoolStart, settings.PortPoolSize)
	default:
		return nil, fmt.Errorf("main: unknown SANDBOX_BACKEND %q", settings.SandboxBackend)
	}
}

func reapAuthoritativeSandboxes(ctx context.Context, manager sandbox.AuthoritativeOrphanReaper, runtimeClient *turnruntime.Client, log *slog.Logger) {
	reap := func() {
		reapCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		count, err := manager.ReapAuthoritativeOrphans(reapCtx,
			func(checkCtx context.Context, turnID, runID uuid.UUID) (bool, error) {
				envelope, err := runtimeClient.Get(checkCtx, turnID)
				if err != nil {
					var apiErr *turnruntime.APIError
					if errors.As(err, &apiErr) && apiErr.Code == "TURN_NOT_FOUND" {
						return false, nil
					}
					return false, err
				}
				if envelope.RunID != runID || envelope.TerminalStatus != nil || envelope.LeaseExpiresAt == nil {
					return false, nil
				}
				return envelope.LeaseExpiresAt.After(time.Now().UTC()), nil
			})
		if err != nil && reapCtx.Err() == nil {
			log.Warn("agent-runner: authoritative sandbox reaper incomplete", "error", err)
		}
		if count > 0 {
			log.Info("agent-runner: reaped authoritative sandbox resources", "count", count)
		}
	}
	reap()
	ticker := time.NewTicker(authoritativeReaperInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reap()
		}
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
