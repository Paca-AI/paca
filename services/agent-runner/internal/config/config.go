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

	// AgentServerImage is the pinned goose image reference. This should be
	// a digest- or tag-pinned reference a human chose deliberately, not a
	// floating one, so there is no hardcoded default here.
	AgentServerImage string

	PacaAPIKey     string
	PacaAPIURL     string
	PacaGatewayURL string

	// PortPoolStart/PortPoolSize size the local-dev host-port pool (see
	// internal/sandbox/docker.Manager).
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

	// SandboxBackend selects which internal/sandbox.Backend runs each
	// conversation's sandbox: "docker" (default — internal/sandbox/docker.Manager,
	// a Docker container reached via a mounted /var/run/docker.sock) or
	// "kubernetes" (internal/sandbox/k8s.Manager, a Kubernetes Job — see
	// that package's doc comment). Defaults to "docker" so an existing
	// Docker Compose deployment needs no env changes.
	SandboxBackend string

	// SandboxNamespace is where the kubernetes backend creates every
	// sandbox Job/Pod — only read when SandboxBackend is "kubernetes".
	// Falls back to this process's own namespace (the file Kubernetes
	// mounts into every Pod) when unset, so the common case — sandboxes
	// alongside agent-runner itself — needs no explicit configuration.
	SandboxNamespace string

	// SandboxCPULimit/SandboxMemoryLimit set both the request and the
	// limit on the kubernetes backend's sandbox containers, in
	// resource.ParseQuantity syntax (e.g. "2", "4Gi"). Defaults match the
	// docker backend's own hardcoded values — see
	// internal/sandbox/k8s.defaultCPULimit/defaultMemoryLimit.
	SandboxCPULimit    string
	SandboxMemoryLimit string

	// SandboxImagePullSecrets names Secrets already present in
	// SandboxNamespace, attached to every sandbox Pod — needed when
	// AgentServerImage is pulled from a private registry. Only read when
	// SandboxBackend is "kubernetes".
	SandboxImagePullSecrets []string

	// SandboxEnvironmentsStorageClass sets StorageClassName on every
	// PersistentVolumeClaim provisioned for a static environment (see
	// internal/sandbox/k8s's CreateEnvironment) — SANDBOX_ENVIRONMENTS_STORAGE_CLASS,
	// only read when SandboxBackend is "kubernetes". Empty (the default)
	// leaves the PVC's StorageClassName unset, letting Kubernetes
	// provision from the cluster's own default StorageClass rather than
	// this process hardcoding one that may not exist on every cluster.
	SandboxEnvironmentsStorageClass string

	// SSHBastionPortRangeStart/End bound the pool of external ports
	// assigned to a static environment's own sshd, one dedicated port per
	// environment, published directly on the environment's own
	// container/Pod (a native Docker -p binding, or a Kubernetes NodePort
	// Service entry — see sandbox.EnvironmentConfig.PortMappings) rather
	// than relayed through this process — see
	// postgres.EnvironmentRepository.AssignSSHPort and docs/ai-agent/
	// environment-management.md's "Terminal / SSH Access" section. Both
	// SSH_BASTION_PORT_RANGE_START/_END, 0 by default: this whole feature
	// is entirely opt-in — an unconfigured deployment never assigns a
	// port at all, behaving byte-for-byte as it did before this feature
	// existed. See Load's own validatePortRange call (both must be set
	// and End >= Start, or both left unset).
	SSHBastionPortRangeStart int
	SSHBastionPortRangeEnd   int

	// PortForwardRangeStart/End are the exact same pattern as
	// SSHBastionPortRangeStart/End above, for reaching any container port
	// a user has explicitly exposed via the user-managed
	// environment_port_forwards table instead (docs/ai-agent/
	// environment-management.md's "Port Forwarding" section), one
	// dedicated host port per port-forward row a user adds, published the
	// same native way SSH is. Replaces what used to be Caddy Admin
	// API-driven subdomain routing (CADDY_ADMIN_URL/ENV_SUBDOMAIN_BASE,
	// removed) — see that doc section for why: some self-hosted
	// deployments can only forward one port through their router/
	// firewall at all, which rules out a shared-port-plus-wildcard-DNS
	// model.
	PortForwardRangeStart int
	PortForwardRangeEnd   int

	// PortForwardHost is the descriptive external hostname a user-added
	// port forward is reachable at (PORT_FORWARD_HOST) — the exact same
	// env var services/api's own config.Settings.PortForwardHost reads,
	// sourced independently here because agent-runner, unlike
	// services/api, needs it for more than an API response: it's how
	// executor.buildEnvironmentContext tells an environment-attached
	// conversation's agent a real `host:port` address instead of just a
	// bare container port. Purely descriptive — agent-runner never
	// publishes a port itself (see PortForwardRangeStart/End above).
	// Empty by default; a forward is still listed without an address in
	// that case rather than omitted, since a self-hosted deployment may
	// not have a reachable hostname configured at all.
	PortForwardHost string

	LogLevel string
}

// Load reads Settings from the environment, applying defaults where the
// environment doesn't set a value.
func Load() (Settings, error) {
	s := Settings{
		DatabaseURL:                     os.Getenv("DATABASE_URL"),
		ValkeyURL:                       os.Getenv("VALKEY_URL"),
		EncryptionKey:                   os.Getenv("ENCRYPTION_KEY"),
		AgentServerImage:                os.Getenv("AGENT_SERVER_IMAGE"),
		PacaAPIKey:                      os.Getenv("PACA_API_KEY"),
		PacaAPIURL:                      os.Getenv("PACA_API_URL"),
		PacaGatewayURL:                  os.Getenv("PACA_GATEWAY_URL"),
		PortPoolStart:                   envInt("PORT_POOL_START", 10000),
		PortPoolSize:                    envInt("PORT_POOL_SIZE", 100),
		WorkerConcurrency:               envInt("WORKER_CONCURRENCY", 5),
		ChatSandboxIdleTimeout:          time.Duration(envInt("CHAT_SANDBOX_IDLE_TIMEOUT_MINUTES", 3)) * time.Minute,
		HTTPAddr:                        envOr("HTTP_ADDR", ":8080"),
		InternalAPIKey:                  os.Getenv("INTERNAL_API_KEY"),
		LLMModelsPath:                   envOr("LLM_MODELS_PATH", "./data/llm_models.json"),
		MCPDevSourceDir:                 os.Getenv("PACA_MCP_DEV_SOURCE_DIR"),
		SandboxBackend:                  envOr("SANDBOX_BACKEND", "docker"),
		SandboxNamespace:                envOr("SANDBOX_NAMESPACE", inClusterNamespace()),
		SandboxCPULimit:                 os.Getenv("SANDBOX_CPU_LIMIT"),
		SandboxMemoryLimit:              os.Getenv("SANDBOX_MEMORY_LIMIT"),
		SandboxEnvironmentsStorageClass: os.Getenv("SANDBOX_ENVIRONMENTS_STORAGE_CLASS"),
		SSHBastionPortRangeStart:        envInt("SSH_BASTION_PORT_RANGE_START", 0),
		SSHBastionPortRangeEnd:          envInt("SSH_BASTION_PORT_RANGE_END", 0),
		PortForwardRangeStart:           envInt("PORT_FORWARD_RANGE_START", 0),
		PortForwardRangeEnd:             envInt("PORT_FORWARD_RANGE_END", 0),
		PortForwardHost:                 os.Getenv("PORT_FORWARD_HOST"),
		LogLevel:                        envOr("LOG_LEVEL", "INFO"),
	}

	if raw := os.Getenv("AGENT_RUNNER_ALLOWED_AGENT_IDS"); raw != "" {
		for _, id := range strings.Split(raw, ",") {
			if id = strings.TrimSpace(id); id != "" {
				s.AllowedAgentIDs = append(s.AllowedAgentIDs, id)
			}
		}
	}

	if raw := os.Getenv("SANDBOX_IMAGE_PULL_SECRETS"); raw != "" {
		for _, name := range strings.Split(raw, ",") {
			if name = strings.TrimSpace(name); name != "" {
				s.SandboxImagePullSecrets = append(s.SandboxImagePullSecrets, name)
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

	switch s.SandboxBackend {
	case "docker":
		// PortPoolStart/PortPoolSize above already default when unset —
		// nothing further to validate for this backend.
	case "kubernetes":
		if s.SandboxNamespace == "" {
			return Settings{}, fmt.Errorf(
				"config: SANDBOX_NAMESPACE is required when SANDBOX_BACKEND=kubernetes " +
					"and this process isn't running in a pod (no in-cluster namespace to fall back to)")
		}
	default:
		return Settings{}, fmt.Errorf(`config: SANDBOX_BACKEND must be "docker" or "kubernetes", got %q`, s.SandboxBackend)
	}

	if err := validatePortRange("SSH_BASTION_PORT_RANGE", s.SSHBastionPortRangeStart, s.SSHBastionPortRangeEnd); err != nil {
		return Settings{}, err
	}
	if err := validatePortRange("PORT_FORWARD_RANGE", s.PortForwardRangeStart, s.PortForwardRangeEnd); err != nil {
		return Settings{}, err
	}

	return s, nil
}

// validatePortRange rejects a misconfigured start/end pair for one of the
// SSH_BASTION_PORT_RANGE_*/PORT_FORWARD_RANGE_* env var pairs — both 0 (the
// shared default) means the feature is off and is always valid; anything
// else needs both set and End >= Start. Catching this at startup, not only
// per-request (the acpbridge handlers that actually assign a port already
// treat start==0 as "not configured" and simply never assign one), turns a
// silent "SSH access never works on this deployment" into an immediate,
// loud failure to boot.
func validatePortRange(envPrefix string, start, end int) error {
	if start == 0 && end == 0 {
		return nil
	}
	if start == 0 || end == 0 {
		return fmt.Errorf("config: %s_START and %s_END must both be set (or both left unset to disable this feature)", envPrefix, envPrefix)
	}
	if end < start {
		return fmt.Errorf("config: %s_END (%d) must be >= %s_START (%d)", envPrefix, end, envPrefix, start)
	}
	return nil
}

// inClusterNamespace reads the namespace Kubernetes mounts into every Pod
// via its ServiceAccount projection — the same file client-go's own
// rest.InClusterConfig-adjacent tooling (e.g. clientcmd's
// InClusterNamespace) reads. Returns "" when absent (not running in a pod,
// or SANDBOX_BACKEND=docker where this file was never expected to exist)
// so SandboxNamespace's SANDBOX_NAMESPACE env var can still be set
// explicitly either way.
func inClusterNamespace() string {
	b, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
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
