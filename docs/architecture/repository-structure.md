# Repository Structure

Paca is a monorepo with clearly separated runtime surfaces, tooling, and documentation.

```text
paca/
├── README.md
├── CONTRIBUTING.md
├── CODE_OF_CONDUCT.md
├── SECURITY.md
├── ROADMAP.md
├── LICENSE
├── .github/                    # CI workflows and issue templates
├── docs/                       # Architecture, guides, API, and plugin documentation
├── apps/
│   ├── web/                    # React + TanStack Start + shadcn/ui frontend
│   ├── mcp/                    # @paca-ai/paca-mcp MCP server (npm package)
│   └── e2e/                    # Playwright end-to-end test suite
├── services/
│   ├── api/                    # Go + Gin application backend
│   ├── realtime/               # Node.js + Socket.IO real-time fan-out
│   ├── agent-runner/            # Go agent execution service (Goose over ACP)
│   └── agent-server/            # Goose sandbox image (services/agent-server/Dockerfile)
├── plugins/
│   └── local/                  # Local plugin store (WASM binaries + frontend bundles)
├── scripts/                    # Install and plugin management scripts
└── deploy/
    ├── docker-compose.dev.yml
    ├── docker-compose.prod.yml
    ├── docker-compose.e2e.yml
    └── caddy/                  # Gateway configuration mounted into the Caddy container
```

## Why This Shape

- `docs` keeps durable technical writing out of the root.
- `apps` holds user-facing surfaces, the MCP integration, and their test counterparts.
- `apps/e2e` lives under `apps` because it directly exercises `apps/web` and is versioned alongside it; it is not deployed.
- `services` holds backend runtimes with different language stacks.
- `services/realtime` is split out so Socket.IO delivery can scale and evolve independently from the transactional API.
- `plugins/local` is the on-disk plugin store — WASM modules and frontend bundles land here after installation.
- `scripts` holds the install script and plugin management helpers used by the CLI and the Marketplace UI.
- `deploy` keeps all environment and infrastructure assets in one place.
- `deploy/caddy` holds gateway configuration that is mounted read-only into the Caddy container at runtime, making it easy to review and modify without rebuilding images.
