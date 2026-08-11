// Command agent-runner consumes paca:agent:triggers for llm-type agents,
// runs each conversation in a dedicated Goose sandbox container over ACP,
// and publishes events back to paca:agent:events — the Go replacement for
// services/ai-agent's llm-type execution path. See
// docs/ai-agent/goose-migration.md.
//
// This file is deliberately thin — it only constructs dependencies and
// wires them into a handler.Handler. The actual per-message behavior lives
// in internal/handler so it can be driven directly by tests and livecheck
// programs without going through the full binary.
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
	"github.com/Paca-AI/agent-runner/internal/chatsandbox"
	"github.com/Paca-AI/agent-runner/internal/config"
	"github.com/Paca-AI/agent-runner/internal/executor"
	"github.com/Paca-AI/agent-runner/internal/handler"
	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/registry"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
	"github.com/Paca-AI/agent-runner/internal/sandbox"
	"github.com/Paca-AI/agent-runner/internal/secret"
)

// idleReaperInterval is how often the chat-sandbox idle reaper wakes up to
// check for conversations past ChatSandboxIdleTimeout — mirrors
// reap_idle_chat_sandboxes's asyncio.sleep(20).
const idleReaperInterval = 20 * time.Second

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

	sandboxMgr, err := sandbox.NewManager(settings.PortPoolStart, settings.PortPoolSize)
	if err != nil {
		return fmt.Errorf("main: %w", err)
	}

	exec := executor.New(sandboxMgr, encryptor, executor.Options{
		Image:          settings.AgentServerImage,
		PacaAPIKey:     settings.PacaAPIKey,
		PacaAPIURL:     settings.PacaAPIURL,
		PacaGatewayURL: settings.PacaGatewayURL,
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

	h := &handler.Handler{
		Gate:          config.NewGate(settings.AllowedAgentIDs),
		AgentRepo:     agentRepo,
		ConvRepo:      convRepo,
		Publisher:     publisher,
		Executor:      exec,
		InFlight:      inFlight,
		ChatSandboxes: chatSandboxes,
		ACPDispatcher: acpDispatcher,
		ACPRegistry:   acpRegistry,
		Log:           log,
	}

	consumer := messaging.NewConsumer(redisClient, settings.WorkerConcurrency, h.Handle, h.HandleControl, log)

	acpServer := &acpbridge.Server{
		Registry:      acpRegistry,
		AgentRepo:     agentRepo,
		ConvRepo:      convRepo,
		Publisher:     publisher,
		InternalToken: settings.InternalAPIKey,
		LLMModelsPath: settings.LLMModelsPath,
		Log:           log,
	}
	httpServer := &http.Server{Addr: settings.HTTPAddr, Handler: acpServer.Routes()}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("agent-runner: starting",
		"image", settings.AgentServerImage,
		"allowed_agent_ids", settings.AllowedAgentIDs,
		"chat_sandbox_idle_timeout", settings.ChatSandboxIdleTimeout,
		"http_addr", settings.HTTPAddr,
	)
	go reapIdleChatSandboxes(ctx, h, chatSandboxes, inFlight, settings.ChatSandboxIdleTimeout, log)
	go runHTTPServer(ctx, httpServer, log)
	consumer.Run(ctx)
	return nil
}

// runHTTPServer serves internal/acpbridge.Server's routes until ctx is
// done, then shuts down gracefully. Mirrors services/ai-agent's uvicorn
// process — this is the same-process HTTP server design note in
// docs/ai-agent/goose-migration.md: one Go binary now covers both the
// trigger-consuming role consumer.Run drives and the ACP bridge role this
// covers, the same way services/ai-agent's single asyncio event loop did.
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
