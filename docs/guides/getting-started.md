# Getting Started

## Option 1 — Install Script (recommended)

Runs on any Linux server with Docker. Downloads the release assets, walks you through configuration interactively, and starts the full stack.

```bash
curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/install.sh | bash
```

Open `http://your-server-ip` when it finishes.

**Non-interactive (CI, scripts, AI coding agents):** set `PACA_YES=1` — this is
required for unattended use, since without it the script can block on a prompt
nobody is there to answer. Every other setting is also steerable via environment
variable instead of accepting its default. AI agents installing Paca on someone's
behalf should prefer this over hand-writing `docker-compose.yml` / `.env` from
Option 2 below — it stays in sync with what each release actually needs.

```bash
PACA_YES=1 bash <(curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/install.sh)
```

See [../../deploy/README.md](../../deploy/README.md#non-interactive-install-ci-scripts-ai-coding-agents)
for the full environment variable reference.

---

## Option 2 — Docker Compose (manual)

Pulls pre-built images. No repository clone required.

```bash
# Download compose file and Caddyfile
mkdir paca && cd paca
curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/docker-compose.yml -o docker-compose.yml
mkdir -p caddy
curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/Caddyfile -o caddy/Caddyfile

# Create an environment file
cat > .env <<'EOF'
JWT_SECRET=<run: openssl rand -hex 32>
ADMIN_PASSWORD=<your-admin-password>
POSTGRES_PASSWORD=<run: openssl rand -hex 32>
AGENT_API_KEY=<run: openssl rand -hex 32>
INTERNAL_API_KEY=<run: openssl rand -hex 32>
ENCRYPTION_KEY=<run: openssl rand -hex 32>
PUBLIC_URL=http://localhost
EOF

# Start the stack
docker compose --env-file .env up -d
```

Open `http://localhost` — log in with `admin` and the password you set.

Want HTTPS? Set `SITE_ADDRESS` to a domain or IP address, with `PUBLIC_URL=https://…`
and `COOKIE_SECURE=true` to match. A real domain with DNS pointed here gets a trusted
Let's Encrypt certificate; an IP address or `localhost` gets one from Caddy's own local
CA instead (browsers will show a trust warning, but the connection is still encrypted).
The [install script](#option-1--install-script-recommended) enables this by default and
prompts for the address. See
[../../deploy/README.md](../../deploy/README.md#production-deployment) for details.

---

## Option 3 — Local Development

For contributors. Clone the repo, then start everything with one command:

```bash
git clone https://github.com/Paca-AI/paca.git && cd paca
docker compose -f deploy/docker-compose.dev.yml up -d
```

All services start with hot-reload — the API, web app, and realtime service all watch your local source files and rebuild automatically. Open `http://localhost:3000` when the stack is healthy.

See [local-development.md](local-development.md) for details on the dev stack and running services on the host.

---

## Upgrading to a new version

Run the upgrade script from the directory where your `docker-compose.yml` and `.env`
live. It refreshes `docker-compose.yml` and the Caddyfile (backing up the old ones
first), then pulls and restarts the stack:

```bash
curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/upgrade.sh -o upgrade.sh
bash upgrade.sh
```

Database migrations run automatically on API startup — no manual steps are required.
Non-interactive (CI, AI agents): set `PACA_YES=1`, same as `install.sh`. See
[../../deploy/README.md](../../deploy/README.md#upgrading-to-a-new-version) for the full
env var reference, pinning a specific version, passing through `--scale` flags, or
upgrading manually.

---

## Connect an AI Agent via MCP

After Paca is running:

1. Generate an API key: **Settings → API Keys → New Key**
2. Add the Paca MCP server to your agent config (Claude Desktop example):

```json
{
  "mcpServers": {
    "paca": {
      "command": "npx",
      "args": ["-y", "@paca-ai/paca-mcp"],
      "env": {
        "PACA_API_KEY": "your-api-key-here",
        "PACA_API_URL": "http://localhost:8080"
      }
    }
  }
}
```

See [mcp-server-setup.md](mcp-server-setup.md) for platform-specific instructions and advanced configuration.

---

## What to Read Next

| Document | When to read it |
|---|---|
| [local-development.md](local-development.md) | Setting up a contributor environment |
| [mcp-server-setup.md](mcp-server-setup.md) | Connecting AI agents via MCP |
| [../architecture/overview.md](../architecture/overview.md) | Understanding the system architecture |
| [../plugins/overview.md](../plugins/overview.md) | Writing or installing plugins |
| [../../deploy/README.md](../../deploy/README.md) | Production deployment reference |
| [../../CHANGELOG.md](../../CHANGELOG.md) | Release history |
