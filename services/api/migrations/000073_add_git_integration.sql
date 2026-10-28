-- Platform-neutral Git integration (docs/ai-agent/git-agent-workflow.md).
--
-- Core owns repositories, identities, change requests, webhook intake and audit
-- for every Git platform; a provider plugin (GitHub, GitLab, …) only adapts one
-- platform to the Git Provider SPI. Provider-private material (private keys,
-- tokens, installation ids beyond the opaque provider_ref) lives in the
-- provider plugin's own schema, never here.

-- NOTE: connections (project scope, several per host) were folded into this
-- still-unreleased migration. A database that already applied the earlier
-- version of 000073 must be reset: the IF NOT EXISTS guards leave its old
-- unique index and CHECK constraints in place.

-- A connection to a platform host: the credential (a GitHub App, an access token)
-- Paca reaches it with. 'shared' ones serve every project, 'project' ones only
-- project_id, 'agent' ones one agent; any number per host. The secret behind it
-- stays with the provider plugin.
CREATE TABLE IF NOT EXISTS git_identities (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id  TEXT        NOT NULL,            -- plugin id, e.g. com.paca.github
    host         TEXT        NOT NULL,            -- e.g. github.com, gitlab.acme.com
    -- The provider's own id for this identity (IdentityRef.ID); with provider_ref
    -- it is everything the provider needs handed back on every call.
    provider_identity_id TEXT NOT NULL,
    kind         TEXT        NOT NULL CHECK (kind IN ('app', 'bot_user', 'token')),
    scope        TEXT        NOT NULL CHECK (scope IN ('shared', 'project', 'agent')),
    project_id   UUID        REFERENCES projects (id) ON DELETE CASCADE,
    handle       TEXT        NOT NULL DEFAULT '', -- what users @mention
    display_name TEXT        NOT NULL DEFAULT '',
    provider_ref JSONB       NOT NULL DEFAULT '{}'::jsonb,
    expires_at   TIMESTAMPTZ,                     -- credential expiry, when the platform has one
    created_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((scope = 'project') = (project_id IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_git_identities_project ON git_identities (project_id) WHERE project_id IS NOT NULL;

-- An agent's own identity, at most one per provider.
CREATE TABLE IF NOT EXISTS git_agent_identities (
    agent_id    UUID NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL,
    identity_id UUID NOT NULL REFERENCES git_identities (id) ON DELETE CASCADE,
    PRIMARY KEY (agent_id, provider_id)
);

-- A repository linked to a project. Events for it are routed to that project.
CREATE TABLE IF NOT EXISTS git_repositories (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    provider_id    TEXT        NOT NULL,
    host           TEXT        NOT NULL,
    external_id    TEXT        NOT NULL DEFAULT '',
    full_path      TEXT        NOT NULL,          -- owner/name or group/subgroup/project
    default_branch TEXT        NOT NULL DEFAULT 'main',
    clone_url      TEXT        NOT NULL DEFAULT '', -- https, never carries credentials
    identity_id    UUID        REFERENCES git_identities (id) ON DELETE SET NULL,
    provider_ref   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    webhook_ref    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_by     UUID,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- A repository path is case-insensitive on every platform, and the webhook
-- lookup compares lower(full_path): "Acme/Web" and "acme/web" must not be
-- linkable twice, or one delivery would fan out twice.
CREATE UNIQUE INDEX IF NOT EXISTS uq_git_repositories_project_path
    ON git_repositories (project_id, provider_id, host, lower(full_path));

CREATE INDEX IF NOT EXISTS idx_git_repositories_lookup
    ON git_repositories (provider_id, host, lower(full_path));

-- Where a platform delivers webhooks for one identity. The key is unguessable
-- and appears in the URL; the signature check is still the provider's job. An
-- endpoint is created when identity setup starts (the platform needs the URL
-- first) and bound to the identity when setup completes, so identity_id is
-- NULL for a pending one; webhook secrets live with the provider, not here.
CREATE TABLE IF NOT EXISTS git_webhook_endpoints (
    key         TEXT        PRIMARY KEY,
    provider_id TEXT        NOT NULL,
    identity_id UUID        REFERENCES git_identities (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Every normalized event a delivery produced, one row per linked repository it
-- fans out to. The primary key makes redelivery idempotent.
CREATE TABLE IF NOT EXISTS git_event_inbox (
    event_id      TEXT        NOT NULL,
    repository_id UUID        NOT NULL REFERENCES git_repositories (id) ON DELETE CASCADE,
    endpoint_key  TEXT        NOT NULL,
    kind          TEXT        NOT NULL,
    action        TEXT        NOT NULL DEFAULT '',
    payload       JSONB       NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'received'
                  CHECK (status IN ('received', 'ignored', 'routed', 'failed')),
    reason        TEXT        NOT NULL DEFAULT '',
    received_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at  TIMESTAMPTZ,
    PRIMARY KEY (event_id, repository_id)
);

CREATE INDEX IF NOT EXISTS idx_git_event_inbox_received ON git_event_inbox (received_at);
CREATE INDEX IF NOT EXISTS idx_git_event_inbox_pending ON git_event_inbox (status) WHERE status = 'received';

-- Cache of change requests (pull/merge requests) seen on linked repositories.
CREATE TABLE IF NOT EXISTS git_change_requests (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID        NOT NULL REFERENCES git_repositories (id) ON DELETE CASCADE,
    number        TEXT        NOT NULL,
    title         TEXT        NOT NULL DEFAULT '',
    state         TEXT        NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'closed', 'merged')),
    draft         BOOLEAN     NOT NULL DEFAULT FALSE,
    head_ref      TEXT        NOT NULL DEFAULT '',
    head_sha      TEXT        NOT NULL DEFAULT '',
    base_ref      TEXT        NOT NULL DEFAULT '',
    author        TEXT        NOT NULL DEFAULT '',
    html_url      TEXT        NOT NULL DEFAULT '',
    merged_at     TIMESTAMPTZ,
    -- The agent run that opened it through Paca owns its feedback and CI failures.
    opened_by_agent_id     UUID,
    opened_conversation_id UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_git_change_requests_repo_number UNIQUE (repository_id, number)
);

CREATE TABLE IF NOT EXISTS git_task_links (
    task_id           UUID        NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    change_request_id UUID        NOT NULL REFERENCES git_change_requests (id) ON DELETE CASCADE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (task_id, change_request_id)
);

CREATE INDEX IF NOT EXISTS idx_git_task_links_cr ON git_task_links (change_request_id);

CREATE TABLE IF NOT EXISTS git_task_branches (
    task_id       UUID        NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    repository_id UUID        NOT NULL REFERENCES git_repositories (id) ON DELETE CASCADE,
    branch_name   TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (task_id, repository_id, branch_name)
);

-- Audit of what agents did through the Git tools and credential service.
CREATE TABLE IF NOT EXISTS git_action_log (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID        REFERENCES projects (id) ON DELETE CASCADE,
    repository_id   UUID        REFERENCES git_repositories (id) ON DELETE SET NULL,
    conversation_id UUID,
    actor_account_id UUID,
    tool            TEXT        NOT NULL,
    subject         TEXT        NOT NULL DEFAULT '',
    ok              BOOLEAN     NOT NULL,
    detail          TEXT        NOT NULL DEFAULT '',
    at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_git_action_log_project_at ON git_action_log (project_id, at DESC);

-- ── Identity setup and access grants ─────────────────────────────────────────

-- One row per identity-setup attempt driven through a Git provider plugin
-- (create the bot identity on the platform, install it, ...). The provider
-- hands back an opaque `state`; this row ties it to the user who started the
-- flow, the scope it is for and the webhook endpoint it will own, so the
-- browser callback can be completed only by that user, once.
CREATE TABLE IF NOT EXISTS git_setup_sessions (
    state        TEXT        PRIMARY KEY,
    provider_id  TEXT        NOT NULL,
    host         TEXT        NOT NULL,
    scope        TEXT        NOT NULL CHECK (scope IN ('shared', 'project', 'agent')),
    agent_id     UUID        REFERENCES agents (id) ON DELETE CASCADE,
    project_id   UUID        REFERENCES projects (id) ON DELETE CASCADE,
    endpoint_key TEXT        NOT NULL REFERENCES git_webhook_endpoints (key) ON DELETE CASCADE,
    return_url   TEXT        NOT NULL DEFAULT '',
    created_by   UUID        NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((scope = 'agent') = (agent_id IS NOT NULL) AND (scope = 'project') = (project_id IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_git_setup_sessions_expires_at ON git_setup_sessions (expires_at);

-- One row per "grant this identity access to another account or organization"
-- attempt (for GitHub: installing the App on the owner of a repository a project
-- wants to link). Like git_setup_sessions it ties the provider's opaque state to
-- the user who started it, so the platform's redirect can be completed only by
-- that user, once, for the project it was started from.
CREATE TABLE IF NOT EXISTS git_access_sessions (
    state        TEXT        PRIMARY KEY,
    provider_id  TEXT        NOT NULL,
    identity_id  UUID        NOT NULL REFERENCES git_identities (id) ON DELETE CASCADE,
    project_id   UUID        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    owner        TEXT        NOT NULL DEFAULT '',
    return_url   TEXT        NOT NULL DEFAULT '',
    created_by   UUID        NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_git_access_sessions_expires_at ON git_access_sessions (expires_at);

-- ── Workflow: attribution, triggers and runs (docs/ai-agent/git-agent-workflow.md §7, §9) ──

-- Which comments Paca posted and for whom. The primary way to tell "feedback on
-- my thread" from "someone else's comment" on every platform, and the
-- loop-prevention record for bot-authored comments.
CREATE TABLE IF NOT EXISTS git_comment_index (
    repository_id   UUID        NOT NULL REFERENCES git_repositories (id) ON DELETE CASCADE,
    comment_id      TEXT        NOT NULL,
    subject_kind    TEXT        NOT NULL CHECK (subject_kind IN ('issue', 'change_request')),
    subject_number  TEXT        NOT NULL,
    thread_id       TEXT        NOT NULL DEFAULT '',
    agent_id        UUID,
    conversation_id UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (repository_id, comment_id)
);

CREATE INDEX IF NOT EXISTS idx_git_comment_index_subject
    ON git_comment_index (repository_id, subject_kind, subject_number);

-- Per-project Git workflow settings.
CREATE TABLE IF NOT EXISTS git_project_settings (
    project_id                 UUID        PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    -- Answers mentions of the shared bot that name no agent.
    default_responder_agent_id UUID        REFERENCES agents (id) ON DELETE SET NULL,
    paused                     BOOLEAN     NOT NULL DEFAULT FALSE,
    -- Lowest repository permission an actor needs to start an agent run.
    min_actor_permission       TEXT        NOT NULL DEFAULT 'write'
                               CHECK (min_actor_permission IN ('read', 'triage', 'write', 'maintain', 'admin')),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Which events start which agent. Absent row = the trigger's default.
CREATE TABLE IF NOT EXISTS git_agent_triggers (
    agent_id UUID    NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    kind     TEXT    NOT NULL CHECK (kind IN
             ('mention', 'own_cr_feedback', 'ci_failure', 'review_request', 'assignment', 'label', 'auto_review')),
    enabled  BOOLEAN NOT NULL DEFAULT TRUE,
    -- auto_review filters: {"branches": [...], "paths": [...], "authors": [...]}
    config   JSONB   NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (agent_id, kind)
);

-- One row per agent run an event started.
CREATE TABLE IF NOT EXISTS git_runs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id        TEXT        NOT NULL,
    repository_id   UUID        NOT NULL REFERENCES git_repositories (id) ON DELETE CASCADE,
    agent_id        UUID        NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    trigger         TEXT        NOT NULL,
    subject_kind    TEXT        NOT NULL DEFAULT '',
    subject_number  TEXT        NOT NULL DEFAULT '',
    head_sha        TEXT        NOT NULL DEFAULT '',
    conversation_id UUID,
    -- queued | running | succeeded | failed | cancelled
    status          TEXT        NOT NULL DEFAULT 'running',
    -- Provider reference of the run's status comment/check, to update in place.
    status_ref      TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at     TIMESTAMPTZ,
    CONSTRAINT uq_git_runs_event_agent UNIQUE (event_id, repository_id, agent_id)
);

-- One unfinished run per agent and subject. The router checks before it
-- creates, but two replicas can both pass the check; the database refuses the
-- second.
CREATE UNIQUE INDEX IF NOT EXISTS uq_git_runs_active_subject
    ON git_runs (repository_id, agent_id, subject_kind, subject_number)
    WHERE status IN ('queued', 'running') AND subject_number <> '';

CREATE INDEX IF NOT EXISTS idx_git_runs_active ON git_runs (status) WHERE status IN ('queued', 'running');
CREATE INDEX IF NOT EXISTS idx_git_runs_subject ON git_runs (repository_id, subject_kind, subject_number, agent_id);
