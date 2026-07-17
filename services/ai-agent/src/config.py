import sys

from pydantic import Field, ValidationError
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    # Service
    port: int = 8080
    log_level: str = "INFO"

    # Valkey / Redis
    valkey_url: str = "redis://valkey:6379/0"

    # Database
    database_url: str

    # Service-to-service
    internal_api_key: str = Field(min_length=1)
    api_base_url: str = "http://api:8080"
    # Gateway base URL — used by the MCP server to resolve plugin MCP bundle URLs.
    # The gateway (Caddy) serves /plugins-mcp/, not the API service, so this must
    # point to the gateway's internal address.
    gateway_base_url: str = "http://gateway"

    # Built-in Paca MCP — the API key used by the AI agent's hardcoded paca MCP
    # server.  Set this to the same value as AGENT_API_KEY on the api service.
    # When empty the built-in paca MCP server is not injected.
    paca_api_key: str = ""
    # Development override — absolute path to the local MCP build entry point
    # (e.g. /workspace/apps/mcp/build/index.js).  When set, the agent runs
    # the local build instead of the published @paca-ai/paca-mcp npm package.
    dev_mcp_path: str = ""

    # AES-256 encryption key (hex-encoded, 64 chars) shared with the API service.
    # Set via ENCRYPTION_KEY (same variable used by the api service).
    # When set, llm_api_key_secret values read from the DB are decrypted before use.
    encryption_key: str = ""

    # LLM routing overrides (ADR-038).  When set they take precedence over the
    # per-agent llm_base_url / API key stored in the DB, so production can
    # force ALL LLM traffic through the platform proxy regardless of what
    # individual agents are configured with.
    llm_base_url_override: str = ""
    llm_api_key_override: str = ""

    # ── Galaxy platform AI role routing (ADR-038 T3) ─────────────────────────
    # When GALAXY_AI_ROLE is set (e.g. "paca-ai"), ALL LLM traffic goes through
    # the Vortex identity AI proxy under that role: the proxy resolves the role
    # to whatever model the platform admin bound in /nexus/admin → AI Models
    # (ai_role_assignments), so rebinding the model needs no Paca redeploy.
    # Auth is a per-conversation RS256 act_as token minted from identity's
    # /internal/mint-service-token (attribution contract — see
    # src/agent/galaxy_llm.py). Takes precedence over LLM_BASE_URL_OVERRIDE.
    galaxy_ai_role: str = ""
    # Internal (galaxy_network) address of the proxy — split-horizon like OIDC.
    galaxy_ai_proxy_url: str = "http://nexus-identity:8086/ai/v1"
    # Identity base URL for the token mint endpoint. Empty = derived from
    # galaxy_ai_proxy_url by stripping the /ai/v1 suffix.
    galaxy_identity_url: str = ""
    # The platform's INTERNAL_SERVICE_SECRET (NOT Paca's INTERNAL_API_KEY and
    # NOT the fleet JWT_SECRET). Required whenever GALAXY_AI_ROLE is set —
    # without it no act_as token can be minted and conversations fail closed.
    galaxy_internal_service_secret: str = ""
    # Signed subject for the minted service token (house convention:
    # <app>-service@galaxy.internal.nexus).
    galaxy_service_subject: str = "paca-service@galaxy.internal.nexus"
    # Extra Docker networks the sandbox containers join in addition to their
    # primary stack network (comma-separated). In galaxy mode the agent-server
    # containers make the LLM calls themselves (the OpenHands SDK sends the
    # agent spec — LLM config included — into the sandbox), so they must join
    # galaxy_network to reach nexus-identity. These networks are EXCLUDED when
    # picking the sandbox's primary network, keeping `api`/`gateway` hostname
    # resolution on the stack network deterministic.
    sandbox_extra_networks: str = ""

    # Docker sandbox.
    # DOCKER_HOST, when set, is the full daemon URL (e.g. tcp://socket-proxy:2375
    # for a mediating socket proxy per ADR-038) and takes precedence over
    # DOCKER_SOCKET, which remains the legacy bare-path form.
    docker_host: str = ""
    docker_socket: str = "/var/run/docker.sock"
    agent_server_image: str = "ghcr.io/paca-ai/paca-agent-server:latest"
    # Port the agent-server process listens on *inside* its container.
    # ghcr.io/openhands/agent-server binds on 8000 by default.
    agent_server_container_port: int = 8000
    # Host-side port pool — only used when running outside Docker (local dev).
    port_pool_start: int = 10000
    port_pool_size: int = 100

    # Worker
    worker_concurrency: int = 10

    # Upper bound on a single conversation turn (the executor's polling loop
    # gives up after this long).  Also feeds the Paca MCP bearer token TTL
    # request (conversation timeout + buffer — see galaxy_llm), though the
    # identity mint endpoint clamps TTLs to its own platform maximum.
    conversation_timeout_seconds: int = 3600

    # Chat sandboxes are kept alive between turns instead of being torn down
    # after each reply (so the agent has memory across a chat session). The
    # frontend pings an "agent.heartbeat" control message every ~30s while a
    # conversation is loaded in a browser tab, refreshing last_active_at —
    # this timeout is the disconnect-detection window: once heartbeats stop
    # (tab closed, crash, network loss) for longer than this, the reaper
    # tears the sandbox down.
    chat_sandbox_idle_timeout_minutes: int = 3


# Fail fast on invalid configuration (ADR-038): a clear message and a non-zero
# exit beat a fail-open service.  Only field locations and messages are
# printed — never configured values.
try:
    settings = Settings()
except ValidationError as exc:
    details = "; ".join(
        f"{'.'.join(str(p) for p in err['loc']).upper()}: {err['msg']}" for err in exc.errors()
    )
    print(f"FATAL: invalid ai-agent configuration — {details}", file=sys.stderr)
    raise SystemExit(1) from None

if not settings.internal_api_key.strip():
    print(
        "FATAL: INTERNAL_API_KEY must be a non-empty secret — an empty key would "
        "leave the conversations API unauthenticated (ADR-038).",
        file=sys.stderr,
    )
    raise SystemExit(1)

if settings.galaxy_ai_role.strip() and not settings.galaxy_internal_service_secret.strip():
    print(
        "FATAL: GALAXY_AI_ROLE is set but GALAXY_INTERNAL_SERVICE_SECRET is empty — "
        "no act_as token can be minted for the platform AI proxy, so every "
        "conversation would fail. Set the platform INTERNAL_SERVICE_SECRET "
        "(host-side env reference, never committed) or unset GALAXY_AI_ROLE.",
        file=sys.stderr,
    )
    raise SystemExit(1)
