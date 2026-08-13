// Package config reads services/agent-runner's process settings from
// environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Settings holds agent-runner's process configuration, loaded once from
// the environment at startup.
type Settings struct {
	DatabaseURL string
	ValkeyURL   string

	// EncryptionKey is the 64-char hex ENCRYPTION_KEY shared with
	// services/api — see internal/secret's package doc comment on why the
	// two must byte-for-byte agree.
	EncryptionKey string

	// AgentServerImage is the pinned goose image reference. See
	// docs/ai-agent/goose-migration.md's "no current pinned Goose image
	// tag" open risk — this should be a digest- or tag-pinned reference a
	// human chose deliberately, not a floating one, so there is no
	// hardcoded default here.
	AgentServerImage string

	PacaAPIKey     string
	PacaAPIURL     string
	PacaGatewayURL string

	// PortPoolStart/PortPoolSize size the local-dev host-port pool (see
	// sandbox.Manager).
	PortPoolStart int
	PortPoolSize  int

	// AllowedAgentIDs restricts execution to a specific set of agent UUIDs
	// (or every agent, if the literal value "*") — see gate.go's doc
	// comment for what this does and doesn't provide.
	AllowedAgentIDs []string

	// WorkerConcurrency bounds how many trigger goroutines (each running one
	// conversation's sandbox container) may be in flight at once — see
	// messaging.Consumer's doc comment on why control messages are
	// deliberately NOT subject to this same bound.
	WorkerConcurrency int

	// ChatSandboxIdleTimeout bounds how long a paused chat conversation's
	// sandbox stays alive with no activity before the idle reaper tears it
	// down — see chatsandbox.Registry.FindIdle and the reaper goroutine in
	// cmd/agent-runner/main.go.
	ChatSandboxIdleTimeout time.Duration

	// HTTPAddr is the address internal/acpbridge.Server listens on, serving
	// /agent-bridge/ws, /agent-bridge/status/*, /agent-bridge/disconnect/*,
	// and /llm/models. Defaults to :8080 to match services/api's
	// AI_AGENT_URL default port (see services/api's config/load.go).
	HTTPAddr string

	// InternalAPIKey authenticates services/api's calls into
	// /agent-bridge/status/* and /agent-bridge/disconnect/* (sent as
	// X-Internal-Token) — must equal services/api's AI_AGENT_INTERNAL_KEY.
	InternalAPIKey string

	// LLMModelsPath is the static provider/model catalog served at
	// /llm/models — see internal/acpbridge.Server.LLMModelsPath's doc
	// comment.
	LLMModelsPath string

	// MCPDevSourceDir, when set, is a HOST filesystem path (not a path
	// inside this container — see sandbox.Config.MCPDevSourceDir's doc
	// comment on why) to a locally-built apps/mcp checkout. When empty
	// (the production/default case), every sandbox runs the Paca MCP server
	// from the image's globally npm-installed @paca-ai/paca-mcp — see
	// pacaMCPBinPath. Dev-only: lets a change to apps/mcp source show up in
	// the next conversation without publishing a new npm version and
	// rebuilding the sandbox image first.
	MCPDevSourceDir string

	LogLevel string
}

// Load reads Settings from the environment, applying defaults where the
// environment doesn't set a value.
func Load() (Settings, error) {
	s := Settings{
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		ValkeyURL:              os.Getenv("VALKEY_URL"),
		EncryptionKey:          os.Getenv("ENCRYPTION_KEY"),
		AgentServerImage:       os.Getenv("AGENT_SERVER_IMAGE"),
		PacaAPIKey:             os.Getenv("PACA_API_KEY"),
		PacaAPIURL:             os.Getenv("PACA_API_URL"),
		PacaGatewayURL:         os.Getenv("PACA_GATEWAY_URL"),
		PortPoolStart:          envInt("PORT_POOL_START", 10000),
		PortPoolSize:           envInt("PORT_POOL_SIZE", 100),
		WorkerConcurrency:      envInt("WORKER_CONCURRENCY", 5),
		ChatSandboxIdleTimeout: time.Duration(envInt("CHAT_SANDBOX_IDLE_TIMEOUT_MINUTES", 3)) * time.Minute,
		HTTPAddr:               envOr("HTTP_ADDR", ":8080"),
		InternalAPIKey:         os.Getenv("INTERNAL_API_KEY"),
		LLMModelsPath:          envOr("LLM_MODELS_PATH", "./data/llm_models.json"),
		MCPDevSourceDir:        os.Getenv("PACA_MCP_DEV_SOURCE_DIR"),
		LogLevel:               envOr("LOG_LEVEL", "INFO"),
	}

	if raw := os.Getenv("AGENT_RUNNER_ALLOWED_AGENT_IDS"); raw != "" {
		for _, id := range strings.Split(raw, ",") {
			if id = strings.TrimSpace(id); id != "" {
				s.AllowedAgentIDs = append(s.AllowedAgentIDs, id)
			}
		}
	}

	for name, val := range map[string]string{
		"DATABASE_URL":       s.DatabaseURL,
		"VALKEY_URL":         s.ValkeyURL,
		"ENCRYPTION_KEY":     s.EncryptionKey,
		"AGENT_SERVER_IMAGE": s.AgentServerImage,
		// Required even though llm-type gating doesn't need it: an empty
		// InternalAPIKey would make internal/acpbridge.Server's internal
		// endpoints accept any request with no X-Internal-Token header at
		// all (an empty configured token trivially "matches" a missing
		// one) — this HTTP server always starts alongside the trigger
		// consumer (see cmd/agent-runner/main.go), so there's no
		// ACP-specific opt-out that would make this genuinely optional.
		"INTERNAL_API_KEY": s.InternalAPIKey,
	} {
		if val == "" {
			return Settings{}, fmt.Errorf("config: %s is required", name)
		}
	}
	if len(s.AllowedAgentIDs) == 0 {
		return Settings{}, fmt.Errorf(
			"config: AGENT_RUNNER_ALLOWED_AGENT_IDS is required — set to a " +
				"comma-separated list of agent UUIDs, or \"*\" to allow every agent " +
				"(local dev / a fully cut-over deployment only)")
	}

	return s, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
