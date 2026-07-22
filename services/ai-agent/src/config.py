from pydantic import Field
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
    # point to the gateway's internal address. Must use the internal :8080 port,
    # not the public port — see the "Internal MCP/Skills bundle port" block in
    # deploy/caddy/Caddyfile for why (auto-HTTPS redirect breaks internal fetches
    # on the public port when SITE_ADDRESS is a real domain).
    gateway_base_url: str = "http://gateway:8080"

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

    # Docker sandbox
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

    # Chat sandboxes are kept alive between turns instead of being torn down
    # after each reply (so the agent has memory across a chat session). The
    # frontend pings an "agent.heartbeat" control message every ~30s while a
    # conversation is loaded in a browser tab, refreshing last_active_at —
    # this timeout is the disconnect-detection window: once heartbeats stop
    # (tab closed, crash, network loss) for longer than this, the reaper
    # tears the sandbox down.
    chat_sandbox_idle_timeout_minutes: int = 3


settings = Settings()
