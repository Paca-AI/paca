# Documentation

This directory is the main documentation home for Paca.

## Sections

- [architecture/overview.md](architecture/overview.md): high-level system view.
- [architecture/repository-structure.md](architecture/repository-structure.md): planned repository layout.
- [architecture/service-boundaries.md](architecture/service-boundaries.md): responsibilities for each service.
- [architecture/database-schema.md](architecture/database-schema.md): database schema (DBML) and interactive diagram.
- [architecture/interaction-views.md](architecture/interaction-views.md): how sprint, backlog, and timeline views share one model and one task-list API.
- [architecture/authorization.md](architecture/authorization.md): the IAM-style policy model (statements, actions, resources, conditions, role attachments), route gates, list scoping, and how to add a route safely.
- [guides/getting-started.md](guides/getting-started.md): what a new contributor should read first.
- [guides/mcp-server-setup.md](guides/mcp-server-setup.md): setup guide for integrating AI agents (Claude, custom agents) with Paca via MCP server.
- [guides/local-development.md](guides/local-development.md): local development intent and future setup direction.
- [guides/design-system.md](guides/design-system.md): visual language, component patterns, and interaction conventions for the web UI.
- [guides/iam-authorization.md](guides/iam-authorization.md): start here for authorization: principals, roles, attachments, how a request is decided, the full actions/resources/conditions reference, built-in roles, guard rails, the web UI and troubleshooting.
- [guides/roles-and-policies.md](guides/roles-and-policies.md): writing roles and policies, the Simple and Advanced editors, and worked examples (read-only, per-project developer, restricting an agent, sprint-limited access).
- [guides/sso-oidc.md](guides/sso-oidc.md): letting users sign in with an OpenID Connect identity provider (Google, Entra ID, Okta, Keycloak, …).
- [api/README.md](api/README.md): API and event contract documentation index.
- [api/http-design.md](api/http-design.md): HTTP API paths, endpoint responsibilities, and future resource design.
- [api/roles-and-policies.md](api/roles-and-policies.md): roles, role assignment, validate/simulate and "my permissions" endpoints.
- [releases/](releases/README.md): release notes with upgrade and migration instructions.
- [deployment/README.md](deployment/README.md): deployment and environment documentation index.
- [product/overview.md](product/overview.md): product concepts and workflow direction.
- [plugins/overview.md](plugins/overview.md): plugin system — architecture, extension points, and security model.
- [plugins/frontend-plugin-system.md](plugins/frontend-plugin-system.md): module federation, extension point registry, and frontend SDK API.
- [plugins/backend-plugin-system.md](plugins/backend-plugin-system.md): WASM runtime, host function bridge, and route registration.
- [plugins/marketplace.md](plugins/marketplace.md): public GitHub marketplace catalog schema and install flow.
- [plugins/sdk-reference.md](plugins/sdk-reference.md): full API reference for `@paca-ai/plugin-sdk-react` (TypeScript) and `github.com/Paca-AI/plugin-sdk` (Go).
- [plugins/developer-guide.md](plugins/developer-guide.md): step-by-step guide to building and publishing a Paca plugin.

## Principles

- Keep documents short and navigable.
- Document decisions before implementation details.
- Prefer stable concepts over framework-level churn.
- Keep the root README product-focused and use this directory for technical detail.
