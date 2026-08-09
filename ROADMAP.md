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
- ✅ Host version compatibility checks for plugin install and upgrade

### AI Agent Integration
- ✅ Agent membership in projects and sprints
- ✅ Agent task assignment and status updates
- ✅ OpenHands SDK integration (isolated sandbox containers)
- ✅ Agent activity feed and progress reporting on task cards
- 📋 Reduce AI agent sandbox container image size and improve startup/runtime performance

### MCP Server
- ✅ `@paca-ai/paca-mcp` npm package
- ✅ Core tool set: projects, tasks, sprints, docs, members
- ✅ Claude Desktop quick-setup guide
- ✅ Agent-mode: scoped identity, project-bound context
- ✅ Plugin tools registered at runtime by installed plugins

### Agent Skills
- ✅ `/paca` Agent Skills for Claude Code, Gemini CLI, Cursor, and AGENTS.md-reading tools — served live from the running instance's own API, always in sync with the installed version
- ✅ One-command install script that detects and installs to every supported client on the machine
- ✅ Full command set: `/paca`, `/paca-epic`, `/paca-clarify`, `/paca-breakdown`, `/paca-sprint`, `/paca-estimate`, `/paca-prioritize`, `/paca-do`, `/paca-test`, `/paca-doc`, `/paca-setup`
- ✅ Plugin-contributed skills installed alongside core skills

---

## Phase 2 — Beta

_Goal: deliver the features that make Paca meaningfully different from standard project tools._

### Infrastructure & Deployment
- ✅ ARM64 Docker image support
- 📋 Helm chart for Kubernetes

### Core Platform
- ✅ Keyboard shortcuts and command palette
- ✅ i18n foundation for the web UI
- ✅ In-app notifications — notification bell with read/unread state
- ✅ Activity diff & revert — visual before/after diff for every field change in the activity pane, one-click revert (tasks and docs)
- ✅ Custom branding — admin-configurable brand name, logo, favicon, and accent color (light/dark presets), applied across the app shell and login screen

### Planning & Task Management
- ✅ Task linking — link related tasks (blocked by, blocks, related, duplicate, parent/child)
- ✅ Timeline / Roadmap view — Gantt-style view of epics by start → due date, selectable alongside Board/Table on any interaction
- ✅ Advanced view settings — per-view column grouping, swimlanes, sort, field visibility/ordering, slicing, and field aggregation across built-in and custom fields
- ✅ Interactions sidebar — quick nav to Timeline, Product Backlog, and Sprints, with drag-and-drop to reassign a task's sprint

### Automation
- ✅ Event-driven automation engine — visual, n8n-style graph builder for Trigger → Condition → Action flows, with multi-branch switch logic and an `Else` fallback path
- ✅ Nine-plus built-in trigger types, including UTC cron schedules, due-date offsets, task-dependency gates, and inbound webhooks with secret-token auth
- ✅ Actions that retarget linked tasks (parent, children, blockers, or explicit picks) with automatic fan-out, dispatch AI agents with custom prompts, or call external APIs (SSRF-safe outbound networking)
- ✅ Run History panel — step-by-step trace of every automation run
- ✅ Project-wide Dependency Map visualizing cross-task automation relationships
- ✅ Plugin-contributed automation node types — plugins can add custom trigger, condition, and action nodes via WASM

### AI Agent Collaboration
- ✅ In-app chat with agents — send messages directly to an agent on a task, get replies in the activity feed
- ✅ Agent-to-human handoff workflow (agent flags tasks it cannot complete, notifies assignee with context)
- ✅ ACP agent support — connect Claude Code, Codex, Gemini CLI, or any custom Agent Client Protocol server as a Paca agent via a local bridge (`paca-acp-bridge`), streaming over an authenticated WebSocket using your own local auth and git/`gh` credentials
- ✅ Global agents — agents not bound to a single project, managed from Admin → Agents
- ✅ Agent-scoped MCP API keys and identity verification for programmatic agent authentication
- ✅ Unified Conversations — direct, task-triggered, and automation-triggered agent conversations, with project-scoped and global views

### Official Plugins
- ✅ Webhook plugin (outgoing webhooks for task and sprint events, configurable per project)
- 📋 GitLab plugin (MR status on task cards, branch linking)
- ✅ Time logging plugin (track time spent per task, per sprint)
- ✅ Dashboard plugin

### Security & Access Control
- ✅ Global RBAC — admin-managed global roles with granular, per-domain permission control
- ✅ User administration — admin-created accounts, forced password change on first login, password reset, account deletion
- ✅ Personal API keys — Settings → API Keys for human users
- 📋 SSO / OIDC support (connect to your IdP)

### Observability & Operations
- ✅ Backup and restore tooling for PostgreSQL data

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
