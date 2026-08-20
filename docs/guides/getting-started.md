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
behalf should prefer this over hand-writing `docker-compose.yml` / `.env`
themselves — it stays in sync with what each release actually needs.

```bash
PACA_YES=1 bash <(curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/install.sh)
```

See [../../deploy/README.md](../../deploy/README.md#non-interactive-install-ci-scripts-ai-coding-agents)
for the full environment variable reference.

Want HTTPS? Set `SITE_ADDRESS` to a domain or IP address, with `PUBLIC_URL=https://…`
and `COOKIE_SECURE=true` to match. A real domain with DNS pointed here gets a trusted
Let's Encrypt certificate; an IP address or `localhost` gets one from Caddy's own local
CA instead (browsers will show a trust warning, but the connection is still encrypted).
The install script enables this by default and prompts for the address. See
[../../deploy/README.md](../../deploy/README.md#production-deployment) for details.

Prefer a manual Docker Compose setup, or a local dev environment instead? See
[../../deploy/README.md](../../deploy/README.md#manual-setup) and
[local-development.md](local-development.md).

---

## Option 2 — Kubernetes (Helm chart)

For an existing Kubernetes cluster instead of a single Docker host. No repository clone required — the chart is published as an OCI artifact alongside every other release image.

```bash
kubectl create namespace paca
helm install paca oci://ghcr.io/paca-ai/charts/paca --version <release-version> -n paca -f my-values.yaml
```

`<release-version>` is a [release](https://github.com/Paca-AI/paca/releases) tag without its leading `v` (e.g. `0.13.1` for `v0.13.1`); omit `--version` to install the newest chart published. `my-values.yaml` needs `publicUrl` plus the required secrets (`jwtSecret`, `adminPassword`, `encryptionKey`, and others) — there are no guessable defaults.

See [Artifact Hub](https://artifacthub.io/packages/helm/paca/paca) for the full values reference, Ingress/TLS setup, what's bundled vs. external, and troubleshooting.

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
| [../../deploy/helm/README.md](../../deploy/helm/README.md) | Kubernetes / Helm chart deployment reference |
| [../../CHANGELOG.md](../../CHANGELOG.md) | Release history |
