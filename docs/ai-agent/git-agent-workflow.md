# Git Agent Workflow

How Paca agents work with source-control platforms: an identity is connected to a repository, a person mentions the bot or hands it work on a pull/merge request or issue, and the agent replies, pushes, reviews and fixes CI — every action attributed to a bot identity and bounded by least-privilege credentials.

**Paca is for teams on any Git platform.** GitHub is the first provider, not the model. Everything that is *not* platform-specific lives in **Paca core** (triggers, routing, policy, runs, audit, MCP tools, workspace protocol, UI), and a small **Git Provider SPI** is what a plugin implements for one platform. Adding GitLab, Bitbucket, Gitea or Azure DevOps means writing an adapter against the SPI and passing its conformance suite, not re-implementing the workflow. The author's guide for providers is [../plugins/git-provider-spi.md](../plugins/git-provider-spi.md).

Related: [overview.md](overview.md), [environment-management.md](environment-management.md), [../plugins/overview.md](../plugins/overview.md).

## Contents

1. [Principles](#1-principles)
2. [Architecture](#2-architecture)
3. [Vocabulary and events](#3-vocabulary-and-events)
4. [Git Provider SPI](#4-git-provider-spi)
5. [Identity and credentials](#5-identity-and-credentials)
6. [Event ingestion, triggers and routing](#6-event-ingestion-triggers-and-routing)
7. [Agent runs](#7-agent-runs)
8. [MCP tools](#8-mcp-tools)
9. [Workspace and static environments](#9-workspace-and-static-environments)
10. [HTTP API and permissions](#10-http-api-and-permissions)
11. [UI](#11-ui)
12. [Security model](#12-security-model)
13. [Data model](#13-data-model)
14. [GitHub provider](#14-github-provider)
15. [Limitations](#15-limitations)

---

## 1. Principles

| # | Principle |
|---|---|
| 1 | **Platform-neutral core.** A core "Git integration" domain owns repositories, change requests, events, triggers, routing, runs, policy and audit. Provider plugins only adapt one platform to the SPI (§4). |
| 2 | **Connections: any number per host, owned by the workspace or by one project.** A *connection* is the credential Paca reaches a platform host with (`github.com`, `gitlab.acme.com`, …): a GitHub App, a GitLab bot user, an access token. A workspace connection is available to every project; a project connection only to its own. A project can have several, from different organizations or platforms, and participates by **linking repositories** through them. |
| 3 | **Linked repositories are the event boundary.** For every repository a project links, Paca receives the change-request, branch, comment, review and check events through the provider and routes them to that project. Events for unlinked repositories are ignored. |
| 4 | **Agents without their own identity act through the connection their repository was linked with.** Attribution to the specific agent is carried by a footer and a core-side comment index (§7.3). |
| 5 | **An agent may have its own identity** (own bot name, avatar and handle), created from the agent's page. |
| 6 | **Triggers behave like bot products:** mention, label, review request or assignment where the platform allows it, plus opt-in rules (auto-review new change requests; address review feedback and fix CI on the agent's own). Which are available is declared by provider *capabilities* (§4.3), never assumed. |
| 7 | **Git and platform actions are core MCP tools with neutral names** (`change_request_*`, `review_*`, `workspace_*`, …). The same prompt works on any platform; a provider never contributes agent tools. Policy is enforced in core, once. |
| 8 | **The agent decides about the workspace.** Paca does not clone for it: the agent inspects the working folder, decides whether the repository is there, and fetches, checks out and pulls the branch it needs (§9). |
| 9 | **Credentials never reach the sandbox.** They are short-lived and minted per use, and handed to git per command through a credential helper; remotes never contain a token (§5.3). |
| 10 | **Every connection is a provider identity.** Personal access tokens are not a connection method. |

Non-goals: replacing the Paca task model; running agents inside the platform's CI runners; supporting platforms without webhooks or an HTTP API.

---

## 2. Architecture

```
                    GitHub / GitLab / Bitbucket / Gitea / …
                         │ webhooks                 ▲ REST / git over HTTPS
                         ▼                          │
 ┌──────────────────────────────────────┐   ┌──────────────────────────────┐
 │ services/api — Git integration (core)│   │ Provider plugin (WASM)       │
 │  • public webhook endpoint           │──►│  VerifyAndParseWebhook       │
 │  • inbox, dedupe, replay protection  │   │  MintCredential              │
 │  • Router: event → agent run         │◄──│  GetChangeRequest, Diff, …   │
 │  • Policy, caps, audit (IAM actions) │   │  OpenChangeRequest, Review…  │
 │  • repositories, change requests,    │   │  identity setup, access grant│
 │    task links, runs, comment index   │   └──────────────────────────────┘
 └──────┬───────────────────────────────┘
        │ start run (existing dispatch)            ▲
        ▼                                          │ provider operations (internal calls)
 agent-runner ── sandbox ── Paca MCP (core tools: change_request_*, review_*, workspace_*)
```

**Core owns:** the webhook endpoint and inbox; the event normalization *contract*; trigger configuration and routing; actor-permission policy; run lifecycle and run status; the change-request and branch cache and task linking; the MCP tools; the workspace and credential-helper protocol; audit; every screen.

**A provider owns:** signature verification and payload parsing; the platform API calls; how an identity is created and how credentials are minted; how a repository is addressed; the browser steps of identity setup and access grants.

Provider operations are **internal-only plugin routes** (`/_git/<op>`): mounted by the host, unreachable from a browser, and called only by core. The plugin proxy refuses them on the public plugin path.

---

## 3. Vocabulary and events

| Neutral term | GitHub | GitLab | Bitbucket Cloud |
|---|---|---|---|
| Repository | repository | project | repository |
| Change request (CR) | pull request | merge request | pull request |
| Review | review | approval + discussion notes | approval + comments |
| Thread | review thread | discussion | comment thread |
| Check | check run / status | pipeline / job / status | build status / pipeline |
| Subject | issue \| CR | issue \| MR | issue \| PR |
| Identity | GitHub App | bot user (access token) | access token / app |

**Repository reference** (stable across platforms): `{host, full_path, external_id, default_branch, clone_url, provider_ref}` — `full_path` is `owner/name` or `group/subgroup/project`, compared case-insensitively. `clone_url` is always token-free; `provider_ref` is opaque to core.

**Permission** is a neutral ordered value: `none < read < triage < write < maintain < admin`. Each provider maps its own roles.

### 3.1 Normalized event

```json
{
  "id": "<unique per delivery and per fan-out item>",
  "kind": "comment | change_request | review | check | push | label | assignment | review_request",
  "action": "created | edited | opened | ready | synchronized | closed | merged | submitted | completed | …",
  "repo": {"host": "github.com", "full_path": "acme/web", "external_id": "…", "default_branch": "main"},
  "subject": {"kind": "issue | change_request", "number": "42", "url": "https://…",
              "head": {"ref": "feat/x", "sha": "…", "repo": "acme/web", "from_fork": false},
              "base": "main", "draft": false, "author": "octocat"},
  "actor": {"login": "octocat", "kind": "user | bot", "permission": "write"},
  "text": "…comment, review or issue body — UNTRUSTED…",
  "location": {"path": "src/a.ts", "line": 17, "thread_id": "…", "comment_id": "…"},
  "check": {"name": "ci/test", "conclusion": "failure", "run_id": "…", "sha": "…"},
  "push": {"ref": "main", "sha": "…", "created": false, "deleted": false},
  "label": "paca",
  "addressed": [{"identity_id": "…", "via": "mention | review_request | assignment | label"}],
  "raw_ref": "provider-private id for debugging; never sent to agents"
}
```

Design points that keep the core neutral:

* **"Was a bot addressed?" is decided by the provider** (`addressed[]`), because mention syntax differs per platform (`@slug[bot]`, `@bot-user`, `@{account_id}`). Core only sees *which identity* was addressed and *how*.
* **`actor.permission` is optional.** When the provider does not vouch for it, core asks the provider (`GetActorPermission`) and remembers the answer for five minutes. It is never read from a payload field a user could influence.
* Events the provider cannot map are dropped, not guessed.

---

## 4. Git Provider SPI

### 4.1 Packaging

A provider is a normal plugin that declares the capability `git-provider`. The Go SDK (`plugin-sdk-go/gitprovider`) provides the `Provider` interface, the optional interfaces, the wire layer (`serve.Register` mounts `POST /_git/:op`), a **conformance suite** and a **reference fake provider**. Operation payloads are versioned JSON (`gitprovider.v1`).

Core compares the `spi_version` a provider reports with the version it implements and **refuses a provider whose major version differs**: it is not listed, described or called. The check runs once per installed plugin version.

Core keeps its own copy of the SPI types (`services/api/internal/platform/gitprovider/spi_*.go`, generated from the SDK by `scripts/sync-git-spi.sh`) and asserts it has the same JSON shape as the SDK's, using one shared golden file.

### 4.2 Operations

Required unless marked *(opt)*. Anything unsupported is simply not implemented: the wire layer answers `unsupported` and the matching capability flag is `false`, so core never calls it.

| Group | Operation | Notes |
|---|---|---|
| Describe | `Describe()` | Info and capabilities (static). |
| Setup | `BeginIdentitySetup(scope, params)` → `SetupStep` | The next browser step: `redirect`, `form_post` (GitHub's manifest flow), `token_form`, or `done`. A `token_form` lists its inputs (name, label, help, secret, required). |
| | `CompleteIdentitySetup(params)` → `Identity` | Called from the setup callback page. `Identity.ExpiresAt` reports credential expiry. May return a further step (`Next`), for example installing the App. |
| | `DeleteIdentity(identity)` *(opt)* | Remote cleanup where an API exists. |
| | `BeginAccessGrant` / `CompleteAccessGrant` *(opt)* | Grant an identity access to one more account or organization (§5.4). |
| Repos | `ListRepositories(identity, query)` | For the picker; paged. |
| | `ResolveRepository(url \| path, candidates)` | Normalizes a URL or path to a repository reference and picks which candidate identity can see it. |
| Webhooks | `VerifyAndParseWebhook(request)` → `[]Event` | Signature check and normalization (§3.1). The only place a platform's webhook format lives. |
| | `EnsureWebhook` / `RemoveWebhook` *(opt)* | Only for `webhook_scope: repo \| group` platforms. |
| Credentials | `MintCredential(identity, repo, access, ttl)` → `Credential{username, secret, expires_at}` | `access ∈ read \| write`. Down-scoped to the repository. A requested `ttl` is reported as `expires_at` when shorter than the platform's own lifetime. |
| Actor | `GetActorPermission(repo, login)` → `Permission` | |
| | `GetBranchProtection(repo, branch)` | |
| Branches | `GetBranch`, `CreateBranch` *(opt)* | Look up and create a branch (capability `branches`). |
| Read | `GetChangeRequest`, `ListChangedFiles`, `GetDiff(file?)` | One neutral shape; large diffs are size-capped and say so. |
| | `ListThreads`, `ListComments` | Include bot authors; mark `resolved` and `outdated`. |
| | `GetIssue` | |
| | `GetChecks(ref)`, `GetCheckLogs(check, tail)` | The provider maps its CI model (check runs, pipelines, statuses) to `{id, name, state, required, url}`. |
| | `GetFile(ref, path)`, `SearchCode(query)` *(opt)* | Lets review agents work without a checkout. |
| Write | `OpenChangeRequest`, `UpdateChangeRequest` | Draft, labels, reviewers. |
| | `Comment(subject, body)` → comment id | Core appends the footer; the provider posts as given and returns the id. |
| | `ReplyThread`, `ResolveThread` *(opt)* | |
| | `SubmitReview(cr, verdict, summary, comments[], head_sha)` | `verdict ∈ comment \| approve \| request_changes`. Providers without batch review **emulate** it (post the comments, then a summary) and set `Emulated`. |
| | `SetStatus(subject, state, summary, url)` | The run's state on the platform (check run, commit status or comment, per `status_kind`). |
| | `AddLabels`, `RequestReviewers`, `SetAssignees` *(opt)* | |
| | `RerunChecks`, `MergeChangeRequest(cr, method, expected_head_sha)` *(opt)* | |
| Reaction | `React(comment, emoji)` *(opt)* | Cosmetic acknowledgement. |

Rules for implementers: never log tokens or payload bodies; map platform errors to the SPI error set (`not_found`, `forbidden`, `invalid_signature`, `conflict`, `rate_limited`, `validation`, `unsupported`, `transient`, `internal`) so core can retry and back off uniformly; make writes idempotent where the platform allows (core passes an idempotency key); only call hosts from the provider's configured host list.

### 4.3 Capabilities

Declared in `Describe()`. Core's UI, tools and router read them instead of hard-coding platform names.

| Capability | Values | Effect in core |
|---|---|---|
| `identity_kinds` | `app`, `bot_user`, `token` | Which setup flows exist. |
| `multiple_identities` | bool | Whether more than one identity per host can exist; per-agent identities are offered only then. |
| `credential_model` | `ephemeral`, `static` | Whether credentials are minted per use or are a stored secret that never leaves core (§5.3). |
| `webhook_scope` | `app`, `group`, `repo` | Whether core asks the provider to `EnsureWebhook` per linked repository. |
| `triggers` | subset of `mention, label, review_request, assignment, ci_failure, own_cr_feedback, auto_review` | Which trigger switches are offered on an agent. |
| `status_kind` | `check_run`, `commit_status`, `comment` | How run status is shown. |
| `branches`, `resolve_threads`, `file_read`, `code_search`, `labels`, `reviewer_requests`, `assignees`, `merge`, `rerun_checks`, `reactions`, `webhook_management`, `identity_deletion`, `access_grants` | bool | The matching optional operation exists; tools and screens that need it appear only then. |
| `batch_review`, `idempotent_writes`, `hidden_markers`, `fork_change_requests`, `draft_change_requests`, `nested_paths` | bool | Behaviour of required operations. |
| `ci_config_globs` | paths | Files agents must not modify (§12.3). |

---

## 5. Identity and credentials

### 5.1 Connections (workspace or project)

* A connection is a neutral `git_identities` row (`provider_id`, `host`, `kind`, `handle`, `display_name`, `scope`, `project_id`); the provider keeps the secret material in its own encrypted tables. `scope` is `shared` (a **workspace connection**: every project may use it), `project` (only `project_id`) or `agent` (§5.2). There may be any number of `shared` and `project` connections per provider host: two GitHub Apps for two organizations, or an App and an access token.
* **Kinds.** A provider offers one or more kinds (`app`, `bot_user`, `token`) and says in `capabilities.kinds` what each implies: the credential model, where webhooks are delivered and whether the connection must be granted access per account (§5.4). GitHub offers an **App** (manifest flow, ephemeral installation tokens, one App-level webhook) and a **personal access token** (token form, static credential, a webhook per repository, commit statuses instead of check runs). Platforms without apps (GitLab, Gitea, Bitbucket) offer tokens. A provider lists `capabilities.kinds` only when setup offers a choice (GitHub); the choice is sent as `params.kind`. A provider with one way to connect (GitLab) lists none and the UI shows no picker.
* **Who creates them.** There is no Git page under Administration. A connection is added from a project's **Settings → Git → Connections**, where the user chooses *This project only* (needs `git:write` and the project action `git.connections:manage`) or *All projects* (needs the workspace action `git.identities:manage`).
* **Repository → connection resolution.** The connections a project can use are the workspace's plus its own. When a user links a repository, core offers all of them (or just the one the user chose, `identity_id`) to the provider (`ResolveRepository`), which says which can see it (GitHub App: the installation covering the owner; token: the repository is readable). Core stores that `identity_id` and the provider-private reference on the `git_repositories` row. Credentials are always minted through the connection the repository was linked with: there is **no fallback to another connection** of the host, and removing a connection leaves its repositories linked but without a credential until they are reconnected (`POST …/repositories/{id}/reconnect`, which re-resolves the repository through a connection the project can use and re-registers its webhook; tasks, branches and change requests are kept). The delete dialog lists the project's repositories that use the connection and counts the ones in other projects (`repository_count` on each listed connection), and a repository without a connection is shown with a **Reconnect** action.
* **A workspace connection is visible to every project.** Anyone with `git:write` in any project can link, and then read and write through, every repository a workspace connection can see (a GitHub App installed on an organization exposes all of that organization's repositories). Linking no longer needs `git.identities:manage`; whoever holds it decides what is shared by choosing the scope when adding the connection. Use *This project only* for access that must not reach other projects.
* **Choosing among several.** Core does not pick between connections that can all see a repository: with no `identity_id`, the provider's `ResolveRepository` answers with the first candidate that can. The UI picker sends `identity_id` when several can serve the platform. Only access grants (which act on one connection) answer `GIT_IDENTITY_REQUIRED` when several exist and none was chosen.
* **Cleanup.** Removing a connection (or deleting its project, for a project's own) asks the provider to drop its credentials and first removes the repository webhooks registered through it, since a webhook cannot be removed without the connection's credential.
* **Repository webhooks.** For a kind whose webhook scope is `repo` (a token), core asks the provider to register a webhook on the repository when it is linked (`EnsureWebhook`, pointing at the connection's own endpoint) and to remove it when the last project link through that connection goes. The webhook secret is always generated by the provider (the setup form asks for the token only). If the provider cannot register the hook (the token lacks the right: Maintainer on GitLab, Webhooks on GitHub), the link still succeeds and the repository carries a notice saying so; `POST …/repositories/{id}/webhook` retries once the token has been replaced by one with the right.

### 5.2 Agent identity (optional, per agent)

* An agent may have at most one own identity **per provider**; it can work on repositories of different platforms with a different bot on each.
* It is created from the agent's Git tab through the provider's setup flow, with the agent's name as the default handle. Providers declare whether they can mint additional identities (`multiple_identities`).
* Events delivered through an agent's identity are routed to that agent only.

### 5.3 Resolution and credentials

```
identityFor(call, repo):
   the calling agent's own identity for repo.provider, if it exists   → act as it
   else the identity that resolved the repository                     → act as the shared bot
   else error "no Git identity for this repository"
```

Resolution happens **in core** from the agent's authenticated account; the agent never selects or sees an identity.

**Credentials**

* *Ephemeral platforms* (GitHub App): a credential is minted per tool call (API) or per git command (helper), down-scoped to the repository and to the minimum permission for that use (`access: read | write`). Core may reuse a minted credential for at most 30 seconds, keyed by `(identity, repository, access)`, **in memory only** — never persisted and never returned to an agent.
* *Static platforms* (access tokens): the secret never leaves core's credential service; API calls are performed server-side by the provider, so the agent never receives a reusable token. For `git` itself the helper receives the secret per command and git forgets it.
* **Credential helper** (platform-neutral; replaces URL-embedded tokens). git runs with `-c credential.helper=` (resets inherited helpers), `-c credential.helper=<bundled helper>`, `-c credential.useHttpPath=true` and `GIT_TERMINAL_PROMPT=0`. For each network command the MCP server opens a **broker**: a unix socket in a private (0700) temporary directory, answering only requests for that command's own scheme, host and path, at most eight times, and removing itself when the command ends. The helper asks the broker, the broker asks core for a credential for `(repository, access)` (`git_push` → write, everything else → read), core authorizes the call (§12) and returns it, and the helper emits it on git's credential protocol and exits. Nothing lands in `.git/config`, `remote.<n>.url` stays `https://<host>/<path>.git`, and nothing is in the environment or `ps`.

### 5.4 Access grants

Some platforms only let an identity see the accounts it has been granted access to, and a *user* grants that on the platform: a GitHub App sees only the accounts and repositories it is installed on. Because each project can use repositories of a different owner, it cannot happen once, when the identity is created.

A provider declares `access_grants` and implements `BeginAccessGrant` / `CompleteAccessGrant`. Core drives it:

* Linking a repository the identity cannot see answers `GIT_ACCESS_NOT_GRANTED` (only for providers that declare the capability). The project's Git tab then offers "Install on `<owner>`", the platform's page opens, and the platform returns to `/git/setup/callback`.
* The state is stored against the user and the project that started it (`git_access_sessions`) and only that user can complete it, once.
* A grant targets one connection: one the project can use, chosen when the project has several App connections for the host, or an agent's own identity from the agent's Git tab. Connections of a kind that needs no grant (tokens) never see the offer. Right after an agent's own identity is created the install step opens automatically, because that identity serves one project.

---

## 6. Event ingestion, triggers and routing

### 6.1 Pipeline

```
Platform ──POST──► core: /api/v1/git/webhooks/{endpointKey}
                    1. look up the endpoint → provider and identity (the key is unguessable, one per identity)
                    2. provider.VerifyAndParseWebhook(request)     ← signature + normalization
                    3. per linked repository: replay check (event id), write the inbox row,
                       update the change-request cache and task links
                    4. respond
                    5. worker: Router → (agent, trigger) decisions → authorize the actor → start the run
```

The synchronous part is cheap; routing happens off the request, so a slow agent start never causes a redelivery storm. A delivery whose cache update fails removes its inbox row, so the platform's retry is processed instead of being dropped as a replay. Because core owns the endpoint, every provider gets replay protection and the inbox without writing any of it.

### 6.2 Triggers

| Trigger | Source | Default | Provider requirement |
|---|---|---|---|
| **Mention** | `addressed[].via = mention` on a comment, review body or subject body | on | `triggers ∋ mention` |
| **Own-CR feedback** | a human review or comment on a CR an agent opened through Paca (no mention needed) | on | `own_cr_feedback` |
| **Own-CR CI failure** | a failing check on the head commit of an agent's open CR | on | `ci_failure` |
| **Review requested** | `kind = review_request` addressed to the identity | on | `review_request` |
| **Assigned** | `kind = assignment` addressed to the identity | on | `assignment` |
| **Label** | label `paca` (the project's responder) or `paca:<agent-handle>` | on | `label` |
| **Auto-review** | a CR opened, marked ready or updated | **off** | `auto_review`. Filters: target branches and authors. |

Triggers are configured **per agent** (`git_agent_triggers`); an agent without a row has the trigger's default. The agent's Git tab lists only triggers some installed provider declares.

### 6.3 What the router decides

A worker claims inbox events at least ten seconds old (`FOR UPDATE SKIP LOCKED`; an abandoned claim returns after five minutes), so the events of a burst (a review and its inline comments) are routed together. For each event it works out which agents to start:

* **Mention, review request, assignment:** the *responder* (below).
* **Label:** `paca` → the responder; `paca:<handle>` → that agent.
* **Own-CR feedback:** the agent that opened the CR. Edited comments and reviews with an empty body (their inline comments arrive as separate events) start nothing.
* **CI failure:** the CR's owner, if the CR is open and the failing check ran on its current head. At most three CI-failure runs per CR and agent. The `Paca / <agent>` status Paca itself shows for a run is not CI and never triggers anything.
* **Auto-review:** each agent with the trigger on whose filters match. Drafts, the agent's own CRs and bot authors not listed in the filter are skipped; a CR from a fork needs an actor with enough repository permission.

**Responder** for something addressed to the shared bot, in order: the agent that owns the receiving identity; an `agent:<handle>` hint in the text (handles of that project's agents only); the agent that last spoke on the subject (comment index); the project's default responder. If several projects linked one repository, each routes the event independently under its own responder and settings.

**Guards**, applied to every decision:

* The project's automation can be **paused**.
* **Bots never start runs**, except CI results on an agent's own CR.
* The actor needs the project's minimum repository permission (default `write`). Others are ignored silently, so there is no way to probe the system. CI results and auto-review of a same-repository CR need no actor permission.
* The agent must have the trigger switched on.
* **One unfinished run per agent and subject.** An event received before the running run started is already covered (the run reads the whole thread). A later one waits and then starts its own run. A unique database index backs this up when two workers race.
* An event that waits more than 30 minutes is dropped.

---

## 7. Agent runs

### 7.1 Lifecycle

```
 A person on the platform: "@bot fix the failing test"
   │ webhook
   ▼
 core: VerifyAndParseWebhook → inbox → Router → actor allowed? → responder → one run per subject
   │ 1. create the run, start an agent conversation whose first message is the event prompt
   │ 2. react "eyes" on the comment; show a `Paca / <agent>` check run on the head commit
   ▼
 agent-runner: attach to a static environment (or an ephemeral sandbox)
   ▼
 agent (MCP): change_request_get_context → workspace_inspect / workspace_prepare → edit and test →
              git_commit → git_push → change_request_open / review_reply_thread / comment_post
   ▼
 core (conversation finished | failed | stopped): update the check run in place, finish the run
```

The **event prompt** is a structured statement of what happened: repository, subject, head and base, the actor and their permission, location, the failing check or label, the platform's text inside a fence, what to do for the trigger, and the rules (never push to the default branch, answer where asked, say so when unable). Everything a person wrote is fenced (§12.4); logins, branches, paths and check names that appear outside the fence are reduced to one inert line.

### 7.2 Visible state

An "eyes" reaction on the comment that started the run, and a check run named `Paca / <agent>` on the head commit, updated in place to success, failure or cancelled when the conversation ends and linking the Paca conversation. Issues have no commit, so a run on an issue shows only the reaction. The agent writes its answer **where it was asked**: a reply in the same thread, or a comment on the same subject.

### 7.3 Attribution, thread ownership, loop prevention

* Everything an agent posts through core ends with a footer — "Posted by **agent** via Paca", linking the conversation — which tells apart agents that share a bot.
* **Core keeps a comment index** (`git_comment_index`): every comment, reply and review core posts is recorded with the agent and conversation. It works on every platform, and it is how "the agent that last spoke on this subject" and thread ownership are known.
* A CR opened through `change_request_open` records its agent (`opened_by_agent_id`), which makes feedback and CI failures on it return to that agent.
* Bot-authored events never start runs, which prevents bots answering each other.

### 7.4 Caps

Per CR and agent: three CI-failure runs. Per repository: six check re-runs per hour. Per call: diffs are capped at 400 KB and log tails at 64 KB (CI logs have likely secrets masked), and anything cut says so.

---

## 8. MCP tools

Core-owned (`apps/mcp`), neutral, calling core API routes that resolve the repository's provider, authorize the call, call the SPI and write the audit row. Every tool takes `projectId` and `repoId` (from `list_repositories`) and never a credential. Text written on the platform (titles, bodies, comments, diffs, CI logs, file contents) is returned inside `UNTRUSTED` fences; the markers inside the text are neutralized so it cannot close its own fence (§12.4). The Git tools are listed only when a Git provider is installed.

### 8.1 Read (`git:read`)

| Tool | Purpose |
|---|---|
| `change_request_get_context` | One call: metadata, head and base, draft state, changed files (first page), review threads with their resolved state, latest checks, and whether the calling agent opened it. |
| `change_request_get_diff` | Unified diff, whole or for one file, size-capped. |
| `review_list_threads` | Threads with every comment, path and line, resolved or outdated. |
| `issue_get`, `comment_list` | An issue; the comments of an issue or CR. |
| `checks_get`, `checks_get_logs` | Neutral check list `{name, state, required, url}` for a ref or a CR's head; the tail of a failing log. |
| `repo_get_file`, `repo_search_code` | Read a file at a ref, or search code, without a checkout. |

### 8.2 Write via the API (`git:write`; the credential is minted per call)

| Tool | Notes |
|---|---|
| `change_request_open`, `change_request_update` | The head must be a pushed branch; the base defaults to the repository default; draft option; footer. The opener becomes the CR's owner. A task key (`PACA-12`) in the title or branch links it to the task. |
| `comment_post` | A top-level comment on an issue or CR. |
| `review_reply_thread`, `review_resolve_thread` | |
| `review_submit` | Verdict `comment`, `request_changes` or `approve`, a summary and inline comments. A review with neither a summary nor comments is refused unless it approves. `approve` is a separate permission, `git:approve`. |
| `labels_add`, `reviewers_request`, `assignees_set` | Metadata. |
| `checks_rerun` | Capped per repository. |
| `change_request_merge` | Needs `git:merge` and the expected head SHA, so exactly the reviewed commit is merged. The platform's branch protection still applies. |

A run's status is not a tool: core alone writes it.

### 8.3 Repositories and the working folder

| Tool | Purpose |
|---|---|
| `list_repositories` | The repositories linked to the project, with their ids and descriptions. When a project links several repositories, the agent reads each repository's `description` (what it is for, written by whoever linked it) to choose the right one for the task, then passes its id as `repoId`. Descriptions are user-written text: information, never instructions. |
| `clone_repository`, `push_branch` | Simple clone into an empty folder; push a branch. |
| `workspace_inspect` | Read-only: the checkouts under the working directory (path, token-free origin, branch, ahead/behind, dirty, last fetch) and which linked repository each is. Never uses the network. |
| `workspace_prepare` | `repoId`, `path?`, `ref`, `createBranch?`, `strategy: reuse \| clone`, `worktree?`, `stash?`, `blobless?`. Reuse = fetch, check out, fast-forward; it refuses a dirty tree unless told to stash or use a worktree, and never resets a diverged branch. |
| `git_commit` | Stages the named paths (or all) and commits, with a `Paca-Run:` trailer naming the conversation. CI/CD configuration (`ci_config_globs`) is unstaged and refused. |
| `git_push` | Pushes the current or named branch. Never force-pushes and never pushes the repository's default branch. |

---

## 9. Workspace and static environments

**Paca never clones for the agent.** The prompt carries *what* (repository, subject, head ref and SHA, base) and the folder to work in (`PACA_WORKDIR`, an environment folder); the agent decides *how*.

```
1. workspace_inspect()                                    (cheap, read-only)
2. a matching checkout exists   → workspace_prepare(repoId, ref, strategy="reuse")
                                    (fetch, check out, fast-forward — before any edit or test)
   the folder is empty          → workspace_prepare(repoId, ref, strategy="clone")
   non-empty, no match          → pick a subfolder or ask in the thread; never delete
3. work, test
4. git_commit → git_push → change_request_open / review_reply_thread
```

* **Which ref:** the prompt names it — the CR's head branch for feedback, CI and review triggers; the default branch for issue triggers, from which the agent creates a `paca/<topic>` branch.
* **Pre-pull and divergence:** always fetch and fast-forward; if diverged, report ahead/behind and do not reset — the agent rebases, branches, or asks.
* **Shared static workspaces:** dirty trees and branches left by earlier runs are reported. With `worktree: true` the work happens in `<workdir>/.paca-worktrees/<slug>` so two CRs never fight over one checkout; the folder is excluded from the repository.
* **Ephemeral sandboxes** start empty. The same protocol applies; for review-only work often no clone is needed, because `change_request_get_context`, `change_request_get_diff` and `repo_get_file` are enough.

---

## 10. HTTP API and permissions

All authorization is declared on the routes (router gates); handlers and services contain no permission checks. Actions: `git:read`, `git:write`, `git:approve`, `git:merge`, `git.connections:manage` (project); `git.identities:manage` (workspace, with its own `git_identity/*` resource root).

**Identities and providers**

| Route | Gate |
|---|---|
| `GET /git/providers` | `git.identities:manage` |
| `GET /projects/{id}/git/providers` | `git:read` |
| `GET /git/identities`, `POST /git/identities/setup`, `POST /git/identities/setup/complete`, `DELETE /git/identities/{id}` (workspace connections) | `git.identities:manage` |
| `GET /projects/{id}/git/connections` (the workspace's and the project's own) | `git:read` |
| `POST /projects/{id}/git/connections/setup`, `…/setup/complete`, `DELETE …/connections/{id}` (the project's own) | `git:write` **and** `git.connections:manage` |
| `GET /projects/{id}/agents/{agentId}/git/identity` | `agents:read` on the agent |
| `POST …/identity/setup`, `DELETE …/identity` | `agents:write` on the agent and `git.identities:manage` |
| `POST …/identity/access/begin` | `agents:write` on the agent and `git:write` |
| `POST /projects/{id}/git/access/begin`, `…/complete` | `git:write` |
| `POST /projects/{id}/git/repositories` (link), `POST …/repositories/{id}/webhook`, `POST …/repositories/{id}/reconnect` | `git:write`: the connections linking goes through were made available to the project by whoever holds `git.identities:manage` (workspace) or `git.connections:manage` (project) |

**Repositories and agent operations** (`/projects/{id}/git/repositories…`)

| Route | Gate |
|---|---|
| `GET …/repositories` | `git:read` |
| `POST …/repositories` (optional `description`), `PATCH …/repositories/{repoId}` (`{"description"}`), `DELETE …/repositories/{repoId}` | `git:write` |
| `POST …/{repoId}/credential/read` | `git:read` |
| `POST …/{repoId}/credential/write` | `git:write` |
| `GET …/{repoId}/change-requests/{n}[/files\|/diff\|/threads]`, `…/subjects/{kind}/{n}/comments`, `…/issues/{n}`, `…/checks[/{id}/logs]`, `…/files`, `…/code-search` | `git:read` |
| `POST/PATCH …/change-requests[/{n}]`, `…/reviews`, `…/reviewers`, `…/threads/{id}/replies`, `…/threads/{id}/resolution`, `…/subjects/{kind}/{n}/comments`, `…/labels`, `…/assignees`, `…/checks/rerun` | `git:write` |
| `POST …/change-requests/{n}/approvals` | `git:approve` |
| `POST …/change-requests/{n}/merge` | `git:merge` |

The verdict of a review comes from the route (`reviews` accepts `comment` and `request_changes`; `approvals` approves), so a route gate decides who may approve. `approve` and `merge` are in no role template; the project Admin role's `*` includes them. The Editor template holds `git:read` and `git:write`. An agent needs the actions it uses in its project role. The `X-Paca-Conversation-Id` header attributes a call to a conversation only if the calling agent owns it.

**Workflow configuration and runs**

| Route | Gate |
|---|---|
| `GET /projects/{id}/git/settings` | `git:read` |
| `PUT /projects/{id}/git/settings` (default responder, pause, minimum actor permission) | `git:write` |
| `GET /projects/{id}/git/runs` | `git:read` |
| `GET /projects/{id}/agents/{agentId}/git/triggers` | `agents:read` on the agent |
| `PUT …/triggers/{kind}` | `agents:write` on the agent |

**Tasks** (`/projects/{id}/tasks/{taskId}/git…`)

| Route | Gate |
|---|---|
| `GET …/git` — linked CRs (with CI state) and branches | `tasks:read` on the task |
| `POST …/git/change-requests`, `DELETE …/git/change-requests/{n}`, `POST …/git/branches`, `DELETE …/git/branches` | `tasks:write` on the task and `git:read` |
| `POST …/git/branches/new` — create the branch on the platform | `tasks:write` on the task and `git:write` |

**Webhook:** `POST /git/webhooks/{endpointKey}` is public; the provider authenticates the delivery (signature), and the key only says which identity's secret to verify against.

---

## 11. UI

Each Git screen explains itself when nothing is set up: an empty state says what to do and gives the button for it, and the text to type in a comment has a copy button.

* **Project → Settings → Git** (`git:read`; changes need `git:write`): **Connections** (the workspace's and the project's own, each with its kind, whether it serves *All projects* or *This project*, the bot's `@handle` and any expiry; **Add** opens a dialog that asks the kind where the provider offers several, who may use it, and the kind's own fields; **Remove** is offered for what the user may manage; with none, an empty state says what to do or whom to ask). `/git/setup/callback` completes a leg of a connection setup or an access grant from the raw query string, exactly once per URL, and then continues or returns to the sanitized same-origin return URL; the browser notes which flow started a round trip (workspace connection, project connection, access grant) so the callback calls the right route. **Repositories** (link by path or URL with an optional description of what the repository is for, editable in place on each row; a connection picker appears when several can serve the platform; unlink; install the App on the owner when needed; each repository shows the bot's `@handle` to mention, and a notice with a retry when a token connection's webhook could not be registered); **Automation**: the default responder agent (with a warning when none is set), the minimum repository permission to start an agent and a pause switch, held back until a repository is linked; and **Using it**, with the mention, `agent:<handle>` and label forms.
* **Agent → Git** (project agents): **Identity**, per provider, whether the agent uses the shared bot or its own (with its `@handle`), what an own identity gives, **Create identity** (with the administrator hint when the user cannot), **Remove**, and a collapsed **Install on another account or organization**; then **Git triggers** grouped as *Respond to requests* (mention, label, review request, assignment), *Follow up on its own pull requests* (feedback, CI failure) and *Act on its own* (auto review, with target-branch and author filters). Triggers no installed provider can deliver stay visible, switched off, and say so. A closing note states that the agent's Git access follows its project role.
* **Task detail:** **Branches** and **Pull requests** sections, shown once the project has a linked repository. Pull requests show state, repository, head → base, author, a link, and the CI state of open ones (passing, failing, running, with check counts and failing names in a tooltip). Branches show the repository and a copy-checkout-command button. Both can be linked by name or pasted link and unlinked; a new branch can be created on the platform when the provider supports it and the user has `git:write`, or created locally with a command. A CR or branch is also linked automatically when its branch or title names the task key.
* **Roles:** the project role editor lists `git:read`, `git:write`, `git:approve`, `git:merge` and `git.connections:manage`; the workspace role editor lists `git.identities:manage`.

---

## 12. Security model

### 12.1 Credentials

| Risk | Control |
|---|---|
| Provider secrets (private keys, tokens) | Encrypted at rest (`ENCRYPTION_KEY`); decrypted only inside the provider plugin; never returned by any API, tool or log. |
| Credential scope | Per call, per repository, `read` vs `write`; held in memory for at most 30 seconds. |
| Token in the sandbox | Never in the environment, files, remote URLs, arguments or tool output; output scrubbing stays as defence in depth. |
| Credential over a weaker channel | The broker answers only the command's own scheme, host and path. |
| Cross-repository access | Core checks the repository belongs to the calling project (another project's repository is "not found"); the platform independently enforces the identity's reach. |
| Compromised agent | Bounded to the permitted repositories and actions for the run's duration; every call is in `git_action_log`, every comment in the comment index. |

### 12.2 Who may trigger an agent

* The actor needs the project's minimum repository permission (default `write`), resolved live through the provider. Others are ignored silently; a project can lower the bar.
* Bots never trigger, except CI results on an agent's own CR.

### 12.3 What an agent may change

* `git_push` refuses the default branch and never force-pushes; changes land through a change request.
* `git_commit` refuses CI/CD configuration (`ci_config_globs`) unless the operator's environment allows it; an agent cannot unlock it.
* Approving and merging are separate permissions that no role template grants.
* A change request from a fork is flagged in the prompt: the agent cannot push to it and is told to review and comment only.

### 12.4 Prompt injection and untrusted content

Everything from the platform — issue and CR text, comments, diffs, files, logs, branch names, logins — is untrusted.

* In tool results and in the event prompt it appears inside `UNTRUSTED` fences labelled as data. The fence markers inside the text are neutralized in one linear pass, so a long run of brackets can neither close the fence nor make it slow.
* A repository's description is written by project members, not the platform, but is still free text that ends up in an agent's prompt: the event prompt carries it inside an `UNTRUSTED` fence like platform text, and `list_repositories` collapses it to one line. It is capped at 1000 characters by the service (`GIT_DESCRIPTION_TOO_LONG`).
* Short values embedded outside a fence (logins, branches, paths and check names) have control characters, newlines, backticks and angle brackets removed and are length-capped, so they cannot start a line of "instructions".
* Privileged operations are unlocked only by server-side policy, never by model judgment. Secrets are masked in CI logs and in provider error messages passed to callers. Reads are size-limited with explicit truncation markers.

### 12.5 Webhooks and abuse

Signature verification happens inside the provider's `VerifyAndParseWebhook`; core adds per-identity endpoint keys, a per-repository replay table keyed by event id, the inbox, the router's caps and guards (§6.3, §7.4) and an audit of what each routed event did (`ignored` with its reason, `routed`, `failed`).

---

## 13. Data model

**Core** (`services/api`, migration `000073`):

```sql
git_identities        (id, provider_id, host, provider_identity_id, kind, scope CHECK ('shared','project','agent'), project_id, handle,
                       display_name, provider_ref JSONB, expires_at, created_by, created_at, updated_at)
  -- project_id is set exactly for scope 'project'; any number of connections per (provider_id, host)
git_agent_identities  (agent_id, provider_id → identity_id, PRIMARY KEY (agent_id, provider_id))
git_repositories      (id, project_id, provider_id, host, external_id, full_path, default_branch, clone_url, description,
                       identity_id, provider_ref JSONB, webhook_ref JSONB, created_by, created_at, updated_at)
  -- unique (project_id, provider_id, host, lower(full_path))
git_webhook_endpoints (key PK, provider_id, identity_id, created_at)
git_setup_sessions    (state PK, provider_id, host, scope, agent_id, endpoint_key, return_url, created_by, expires_at, completed_at)
git_access_sessions   (state PK, provider_id, identity_id, project_id, owner, return_url, created_by, expires_at, completed_at)
git_event_inbox       (event_id, repository_id, endpoint_key, kind, action, payload JSONB, status, reason, received_at, processed_at,
                       PRIMARY KEY (event_id, repository_id))      -- status: received | ignored | routed | failed
git_change_requests   (id, repository_id, number, title, state, draft, head_ref, head_sha, base_ref, author, html_url, merged_at,
                       opened_by_agent_id, opened_conversation_id, created_at, updated_at,  UNIQUE (repository_id, number))
git_task_links        (task_id, change_request_id) ;  git_task_branches (task_id, repository_id, branch_name)
git_comment_index     (repository_id, comment_id, subject_kind, subject_number, thread_id, agent_id, conversation_id, created_at)
git_project_settings  (project_id PK, default_responder_agent_id, paused, min_actor_permission)
git_agent_triggers    (agent_id, kind, enabled, config JSONB,  PRIMARY KEY (agent_id, kind))
git_runs              (id, event_id, repository_id, agent_id, trigger, subject_kind, subject_number, head_sha, conversation_id,
                       status, status_ref, created_at, finished_at,  UNIQUE (event_id, repository_id, agent_id))
  -- unique (repository_id, agent_id, subject_kind, subject_number) while status is queued or running
git_action_log        (id, project_id, repository_id, conversation_id, actor_account_id, tool, subject, ok, detail, at)
```

A run's conversation is an automation-message conversation; its first message is the event prompt.

**Provider plugin** keeps only platform-private tables. The GitHub plugin has `github_apps` (App id, slug, encrypted private key, client secret and webhook secret), `github_app_installations`, `github_tokens` (login, encrypted token and webhook secret) and `github_manifest_sessions`.

---

## 14. GitHub provider

`paca-plugin-github` implements the SPI with GitHub Apps and personal access tokens.

| Area | Behaviour |
|---|---|
| Identity | **App:** a GitHub App created through the manifest flow, any number per workspace or project and optionally one per agent. **Token:** a personal access token and a webhook secret pasted into a form (a project's token from the previous release is migrated into a project connection, see the plugin README); the token is validated with `GET /user` and its expiry (when GitHub reports one) is kept. A token acts as its owner, so a dedicated machine user is recommended.  The App's key material is stored encrypted. A *public* App can be installed on other accounts; a private one only on the owner's account. |
| Access | Installed per repository owner when a repository is linked (and right after an agent's App is created). Installations are discovered lazily from GitHub; a revoked token is refreshed; an installation GitHub no longer has is forgotten and replaced on reinstall. A repository resolves only if a credential can actually be minted for it. |
| Credentials | **App:** installation tokens narrowed to one repository and to `contents: read` or `write`. They last an hour; a requested lifetime is reported as the expiry. |
| Webhooks | **Token:** a webhook registered on each linked repository (idempotent, signed with the user's secret), removed when the last link goes; check runs are replaced by commit statuses. **App:** one App-level webhook per identity. Comments, reviews, review comments, pull requests (with their page URL), issues, labels, check runs and pushes are normalized. A review request or assignment is reported only when it names the App's own bot (`<slug>[bot]`); requests and assignments for people or teams are dropped. Label events are reported for every label and core decides which name an agent. The App's own mentions (`@slug` or `@slug[bot]`) are decided in the provider. |
| Status | A check run named `Paca / <agent>`, created on the head commit and updated in place. |
| Reviews | Batch review (pending review submitted once, with inline comments). |
| Reviewer and assignee | The triggers fire when GitHub delivers a request or assignment naming the App's bot. GitHub's reviewer and assignee pickers list people and teams, so for an ordinary App a mention or the `paca` label is the dependable way to ask. |
| Triggers declared | `mention`, `label`, `review_request`, `assignment`, `ci_failure`, `own_cr_feedback`, `auto_review`. |
| CI config globs | `.github/workflows/**`, `.github/actions/**`. |

---

## 15. Limitations

* Static-credential platforms have a larger blast radius than ephemeral ones: a token cannot be narrowed per call. Server-side API calls, separate read and write tokens, and expiring tokens limit it.
* A workspace connection is only as narrow as its repository selection on the platform: any user who can link repositories (`git:write` on the project) can point agents at any repository a workspace connection can see. A project that must not reach those repositories needs its own connection and the workspace connection kept narrow; workspace connections are shared by every project and do not isolate them from each other.
* A token connection acts as a person and cannot be narrowed per call or per repository; prefer a machine user with access to just the repositories needed.
* The footer on posted comments is advisory (a person can edit it out); the comment index is the source of truth for thread ownership.
* Platforms that register webhooks per repository need a permission the identity may not have (the Maintainer role on GitLab); without it the webhook is not registered, events do not reach Paca, and the fix is a token with the right.
* Events are coalesced by the router's ten-second claim age and the one-run-per-subject rule rather than by a timer.
* GitHub's review-comment webhooks carry no thread id, so feedback routing uses CR ownership rather than thread ownership.
* Auto-review filters cover target branches and authors; there is no path filter.
* There is no `@bot stop` command and no cancelling of a queued run when a newer commit arrives.
* There is no mapping from a platform login to a Paca member.
* Task-detail pull request and branch links come from the task key in a branch or title, an agent opening a CR for the task, or by hand; there is no search across platforms.
