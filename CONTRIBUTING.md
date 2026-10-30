# Contributing to Paca

Thanks for contributing to Paca.

## Repository Shape

- `apps/web` — React + TanStack Start + shadcn/ui frontend.
- `apps/mcp` — MCP server (`@paca-ai/paca-mcp` npm package).
- `apps/e2e` — Playwright end-to-end test suite.
- `services/api` — Go + Gin application backend.
- `services/realtime` — Node.js + Socket.IO real-time event fan-out.
- `services/agent-runner` — Go AI agent execution service (Goose over ACP; also brokers `acp`-type dispatch to `apps/acp-bridge`).
- `services/agent-server` — Docker image for the Goose sandbox agent-runner spawns per conversation.
- `docs` — architecture, guides, API, deployment, and plugin documentation.
- `deploy` — Docker Compose files and environment templates.
- `plugins` — local plugin store (WASM + frontend bundles).
- `scripts` — install and plugin management scripts.

## Our Promise to Contributors

Paca is free and non-profit, and it belongs to the community. There will never be paid tiers or paywalled features: every feature is free for everyone, and contributions will not be relicensed into a closed or commercial edition. See [Our Promise](README.md#our-promise-free-forever-owned-by-the-community) in the README.

## Development Setup

The only prerequisite is [Docker](https://docs.docker.com/get-docker/) with the Compose plugin. `deploy/docker-compose.dev.yml` sets up the entire development environment — PostgreSQL, Valkey, RustFS, the API, web app, realtime service, agent-runner, MCP server, and the gateway. You do not need to install Go or Bun, or run anything else by hand.

From the repository root:

```bash
docker compose -f deploy/docker-compose.dev.yml up -d
```

Then open <http://localhost:3000> and log in with `admin` / `adminpassword`. Every service watches your local source files and reloads automatically, so edits show up without rebuilding.

To stop the stack:

```bash
docker compose -f deploy/docker-compose.dev.yml down
```

Add `-v` to also remove the data volumes.

Running individual services on the host is optional, for IDE debugging or faster feedback. See [docs/guides/local-development.md](docs/guides/local-development.md) for that, plus migrations and default dev credentials.

## Plugin Development

If you are building a plugin, read the [plugin developer guide](docs/plugins/developer-guide.md) first. To try a plugin against your dev stack, use the install script, which builds the plugin and installs it through the API:

```bash
./scripts/install-local-plugin.sh <path-to-plugin> \
  --api-url http://localhost:3000 \
  --api-key <your-api-key>
```

- `<path-to-plugin>` is a directory containing a `plugin.json`.
- `--api-url` must point at the dev gateway (`http://localhost:3000`). The script's default is `http://localhost`, which is the production port.
- The API key can also be set with the `API_KEY` environment variable.
- Backend plugins build with TinyGo by default. Pass `--go-toolchain go` only if your plugin needs a standard-library feature TinyGo lacks (for example `html/template`).
- Use `--skip-build` or `--skip-install` to run only half of the process.

To remove a plugin, run `./scripts/remove-plugin.sh <plugin-id> --api-key <your-api-key>`.

See [scripts/README.md](scripts/README.md) for all options. You can also install published plugins from **Settings → Plugins → Marketplace** in the UI.

## Contribution Guidelines

- Keep pull requests focused on one concern.
- Explain the reasoning behind non-obvious changes.
- Update relevant docs when changing behavior or interfaces.
- Avoid premature abstraction — add it when reuse is proven.

## Pull Request Checklist

- The change is scoped to one concern.
- Tests are added or updated for changed behaviour.
- Related documentation is updated.
- New decisions or non-obvious tradeoffs are explained clearly.

## Discussion Areas

- Product workflow and user experience.
- Service responsibilities and system boundaries.
- Plugin system design and extension points.
- AI agent behaviour and collaboration model.
- Open-source governance and contributor experience.