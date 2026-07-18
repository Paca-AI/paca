# Paca Roadmap

This document outlines the planned development trajectory for Paca. It is updated as priorities shift and milestones are reached.

> **Legend:** ✅ Done &nbsp;·&nbsp; 🚧 In progress &nbsp;·&nbsp; 📋 Planned &nbsp;·&nbsp; 💡 Exploring

---

## Phase 1 — Foundation (Alpha)

_Goal: a working, self-hostable core that a small team can actually use._

### Infrastructure & Deployment
- ✅ Docker Compose single-command setup
- ✅ Interactive install script for Linux servers
- ✅ PostgreSQL + Valkey bundled by default
- ✅ Caddy gateway with service routing
- ✅ Environment-based configuration (`.env`)

### Core Platform
- ✅ User authentication (JWT)
- ✅ Project and workspace management
- ✅ Task CRUD with custom fields and task types
- ✅ Backlog management
- ✅ Sprint creation and lifecycle (start, complete)
- ✅ Scrumban board with drag-and-drop
- ✅ Real-time board updates via Socket.IO
- ✅ Task comments and activity feed
- ✅ File attachments
- ✅ Board view customization (swimlanes, grouping)

### Documentation
- ✅ Living document editor per project
- ✅ AI agent contributions to documents (diagram suggestions, component descriptions)
- ✅ Document version history and diff view
- ✅ Link document sections to tasks and epics

### Plugin System
- ✅ WASM sandbox for backend plugins
- ✅ Frontend module bundles
- ✅ Capability-based permission declaration
- ✅ Plugin Marketplace UI (Settings → Plugins → Marketplace)
- ✅ Local plugin install script
- ✅ Plugin SDK and developer documentation
- ✅ GitHub plugin (PR status on task cards, branch linking)
- ✅ BDD plugin (Gherkin scenario editor, AI-assisted scenario generation)
- ✅ Checklist plugin (sub-task checklists on any task card)

### AI Agent Integration
- ✅ Agent membership in projects and sprints
- ✅ Agent task assignment and status updates
- ✅ OpenHands SDK integration (isolated sandbox containers)
- ✅ Agent activity feed and progress reporting on task cards

### MCP Server
- ✅ `@paca-ai/paca-mcp` npm package
- ✅ Core tool set: projects, tasks, sprints, docs, members
- ✅ Claude Desktop quick-setup guide
- ✅ Agent-mode: scoped identity, project-bound context
- ✅ Plugin tools registered at runtime by installed plugins

---

## Phase 2 — Beta

_Goal: deliver the features that make Paca meaningfully different from standard project tools._

### Infrastructure & Deployment
- 📋 ARM64 Docker image support
- 📋 Helm chart for Kubernetes

### Core Platform
- ✅ Keyboard shortcuts and command palette
- ✅ i18n foundation for the web UI

### Planning & Task Management
- ✅ Task linking — link related tasks (blocked by, blocks, related, duplicate, parent/child)

### AI Agent Collaboration
- ✅ In-app chat with agents — send messages directly to an agent on a task, get replies in the activity feed
- ✅ Agent-to-human handoff workflow (agent flags tasks it cannot complete, notifies assignee with context)

### Official Plugins
- ✅ Webhook plugin (outgoing webhooks for task and sprint events, configurable per project)
- 📋 GitLab plugin (MR status on task cards, branch linking)
- ✅ Time logging plugin (track time spent per task, per sprint)
- 📋 Burndown chart plugin (sprint burndown and velocity tracking)
- 📋 One-click AI-generated sprint retrospective

### Sprint Intelligence
- 📋 Agent vs. human task completion metrics
- 📋 Sprint health indicators (scope creep, blocked tasks)

---

## Phase 3 — v1.0 General Availability

_Goal: production-grade stability, security, and observability for real teams._

### Security & Access Control
- 📋 Role-based access control (RBAC) — project-level roles
- 📋 API key scoping (read-only, project-scoped)
- 📋 Audit log for all board and admin actions
- 📋 SSO / OIDC support (connect to your IdP)
- 📋 Security hardening review and responsible disclosure process

### Observability & Operations
- 📋 Health check endpoints for all services
- 📋 Prometheus metrics export
- 📋 Structured JSON logging
- ✅ Backup and restore tooling for PostgreSQL data
- 📋 Upgrade migration guide and tooling

### Performance & Scale
- 📋 Pagination and virtual scrolling for large backlogs
- 📋 Database index audit and query optimization
- 📋 Horizontal scaling guide for the API and realtime services
- 📋 Load testing suite

### Developer Experience
- 📋 OpenAPI / Swagger documentation for the REST API
- 📋 End-to-end test coverage for all core workflows (Playwright)
- 📋 Contributor plugin development guide
- 📋 One-command local dev environment (`make dev`)

---

## Beyond v1.0 — Exploring

_These are ideas we find compelling but have not yet committed to._

- 💡 Mobile-friendly progressive web app (PWA)
- 💡 Multi-agent orchestration — agents that delegate sub-tasks to other agents
- 💡 Git repository integration as a first-class feature (branch ↔ task linking, PR status on board)
- 💡 Multi-workspace / organization support
- 💡 Hosted cloud option (opt-in, for teams that don't want to self-host)

---

## How to Influence the Roadmap

This is an open-source project — the roadmap is shaped by the community.

- **Vote on issues** — 👍 existing GitHub issues to signal priority
- **Open a discussion** — propose a feature or share how you use Paca in [GitHub Discussions](https://github.com/Paca-AI/paca/discussions)
- **Contribute** — see [CONTRIBUTING.md](CONTRIBUTING.md) to get started

Items marked 📋 are not in any fixed release order. If something here matters to your team, open an issue or pull request — that moves it forward.
