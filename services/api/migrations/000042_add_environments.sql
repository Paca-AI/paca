-- 000042_add_environments.sql
-- Adds static environments: named, long-lived sandbox containers a user
-- explicitly creates and manages, which agent conversations can attach to
-- instead of getting a fresh disposable sandbox every time (see
-- docs/ai-agent/environment-management.md). Also adds environment_folders
-- (the multiple repos/working directories one environment can hold),
-- environment_ssh_keys (user-registered public keys for real SSH access),
-- and environment_port_forwards (user-managed port forwarding) — plus the
-- agents.default_environment_id and agent_conversations.environment_id/
-- environment_folder_id columns that reference environments, and the drop
-- of agent_conversations' legacy per-conversation-container columns those
-- replace.
--
-- Consolidated from what was originally five separate migrations
-- (000042-000046) written during this feature's own development — never
-- released, so folded into one clean migration rather than preserved as
-- historical add-then-drop churn. In particular: environments.ssh_port and
-- environment_port_forwards.host_port are each published directly on the
-- environment's own backing container/Pod (a native Docker -p binding, or
-- a Kubernetes NodePort Service entry — see docs/ai-agent/
-- environment-management.md's "Terminal / SSH Access" and "Port
-- Forwarding" sections) rather than routed by a shared-host subdomain or
-- relayed through agent-runner's own process, both approaches this
-- feature briefly took and discarded before ever shipping.
--
-- Every statement below is written to converge to the same final schema
-- regardless of starting state — a genuinely fresh database, or one that
-- already ran an earlier (pre-consolidation) version of these five
-- migrations and so already has some of these columns/tables in an
-- intermediate shape. This file has no migration-tracking table to consult
-- (see database.RunMigrationsFS's own doc comment: every .sql file here
-- re-runs on every service startup), so idempotency can't rely on "this
-- already ran once" — only on IF [NOT] EXISTS guards actually converging.

BEGIN;

-- -------------------------------------------------------------------------
-- ENVIRONMENTS
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS environments (
    id                    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id            UUID        NOT NULL,
    name                  TEXT        NOT NULL,
    slug                  TEXT        NOT NULL,
    status                TEXT        NOT NULL DEFAULT 'creating'
        CHECK (status IN ('creating', 'running', 'stopping', 'stopped', 'suspended', 'error', 'deleting')),
    backend               TEXT        NOT NULL CHECK (backend IN ('docker', 'kubernetes')),
    backend_ref           TEXT,
    -- image is nullable: NULL means "use this platform's pinned
    -- AGENT_SERVER_IMAGE", the same image ephemeral conversation sandboxes
    -- already run — see agent-runner's sandbox.EnvironmentConfig.Image.
    image                 TEXT,
    cpu_limit             TEXT        NOT NULL DEFAULT '2',
    memory_limit          TEXT        NOT NULL DEFAULT '4Gi',
    disk_limit_gb         INTEGER     NOT NULL DEFAULT 20,
    volume_ref            TEXT,
    -- secret_key_encrypted is generated once at creation and reused across
    -- every stop/start cycle (unlike an ephemeral conversation's sandbox,
    -- which regenerates a fresh secret key on every Start since that
    -- container never survives a restart) — encrypted with the same
    -- internal/secret.Encryptor already used for agents.llm_api_key_secret.
    secret_key_encrypted  TEXT        NOT NULL,
    idle_timeout_minutes  INTEGER     NOT NULL DEFAULT 60,
    last_active_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    error_message         TEXT,
    -- ssh_port is the dedicated external port a real `ssh` client connects
    -- to reach this environment's own sshd — published directly on the
    -- backing container/Pod (a Docker -p binding, or a Kubernetes NodePort
    -- Service entry), assigned once by agent-runner from its configured
    -- port range and nil until the first successful CreateEnvironment call
    -- returns, never reassigned across a stop/start cycle. Written
    -- directly by agent-runner's own Postgres connection, never by
    -- services/api itself — services/api only reads it back for the API
    -- response.
    ssh_port              INTEGER,
    -- ports_pending_restart is services/api's own bookkeeping (not
    -- agent-runner's): true whenever a port forward has been added/removed
    -- since the environment's backing container/Pod last had its full
    -- port-mapping set applied — see environment_port_forwards below.
    ports_pending_restart BOOLEAN     NOT NULL DEFAULT false,
    created_by            UUID,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at            TIMESTAMPTZ,
    CONSTRAINT fk_environments_project
        FOREIGN KEY (project_id)
        REFERENCES projects(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_environments_created_by
        FOREIGN KEY (created_by)
        REFERENCES users(id)
        ON DELETE SET NULL
);

-- The block below only does anything on a database that already had an
-- `environments` table from before this file was consolidated (the
-- CREATE TABLE above is a no-op there) — it brings that table to the same
-- final shape the CREATE TABLE already describes for a fresh install.
ALTER TABLE environments
    ADD COLUMN IF NOT EXISTS ssh_port INTEGER,
    ADD COLUMN IF NOT EXISTS ports_pending_restart BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE environments
    DROP COLUMN IF EXISTS subdomain_slug,
    DROP COLUMN IF EXISTS ssh_target_addr,
    DROP COLUMN IF EXISTS network_addr,
    DROP COLUMN IF EXISTS http_port,
    DROP COLUMN IF EXISTS http_target_addr;

CREATE UNIQUE INDEX IF NOT EXISTS uq_environments_project_slug
    ON environments (project_id, slug)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_environments_project_id ON environments (project_id);
-- Read by agent-runner's DB-backed idle reaper
-- (EnvironmentRepository.ClaimEnvironmentStatus).
CREATE INDEX IF NOT EXISTS idx_environments_idle_reap
    ON environments (status, last_active_at)
    WHERE deleted_at IS NULL;
-- Dropped and recreated rather than CREATE ... IF NOT EXISTS alone: an
-- earlier (pre-consolidation) version of this index may already exist
-- under this same name with a different definition, which IF NOT EXISTS
-- would silently leave in place. A deleted environment's port is never
-- actively reclaimed (ssh_port is left on the row as a harmless
-- historical value), but the partial index lets AssignSSHPort's candidate
-- query treat it as free for reuse by a brand new environment once
-- assigned.
DROP INDEX IF EXISTS uq_environments_ssh_port;
CREATE UNIQUE INDEX uq_environments_ssh_port
    ON environments (ssh_port)
    WHERE ssh_port IS NOT NULL AND deleted_at IS NULL;
-- Auto-dropped alongside subdomain_slug itself on any database that ever
-- had it, but harmless to also state explicitly here.
DROP INDEX IF EXISTS uq_environments_subdomain_slug;

-- -------------------------------------------------------------------------
-- ENVIRONMENT FOLDERS
-- One environment can hold multiple repos/working directories; this is
-- what the folder-picker at chat-start reads.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS environment_folders (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    environment_id  UUID        NOT NULL,
    path            TEXT        NOT NULL,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_environment_folders_environment
        FOREIGN KEY (environment_id)
        REFERENCES environments(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_environment_folders_created_by
        FOREIGN KEY (created_by)
        REFERENCES users(id)
        ON DELETE SET NULL
);

-- name/repo_plugin_id/repo_clone_url/default_branch were dropped before
-- ever shipping — folders are identified purely by path now (see
-- docs/ai-agent/environment-management.md's Folders section). Explicit
-- drops, not just absent from the CREATE TABLE above, so a database that
-- already ran an earlier version of this same file converges too.
ALTER TABLE environment_folders
    DROP COLUMN IF EXISTS name,
    DROP COLUMN IF EXISTS repo_plugin_id,
    DROP COLUMN IF EXISTS repo_clone_url,
    DROP COLUMN IF EXISTS default_branch;

CREATE UNIQUE INDEX IF NOT EXISTS uq_environment_folders_path
    ON environment_folders (environment_id, path);

-- -------------------------------------------------------------------------
-- ENVIRONMENT SSH KEYS
-- User-registered public keys, authenticated against by fingerprint by the
-- real sshd running inside each environment's own container/Pod — see
-- docs/ai-agent/environment-management.md's Terminal / SSH Access section.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS environment_ssh_keys (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    environment_id  UUID        NOT NULL,
    label           TEXT        NOT NULL,
    public_key      TEXT        NOT NULL,
    fingerprint     TEXT        NOT NULL,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_environment_ssh_keys_environment
        FOREIGN KEY (environment_id)
        REFERENCES environments(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_environment_ssh_keys_created_by
        FOREIGN KEY (created_by)
        REFERENCES users(id)
        ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_environment_ssh_keys_fingerprint
    ON environment_ssh_keys (environment_id, fingerprint);

-- -------------------------------------------------------------------------
-- ENVIRONMENT PORT FORWARDS
-- User-managed, one row per container port a user wants reachable from
-- outside — mirrors environment_folders/environment_ssh_keys rather than
-- being another auto-assigned column on environments itself. The one
-- thing still auto-created without user action is environments.ssh_port
-- above; every port forward is opt-in CRUD, added explicitly by the user
-- from the environment's Connect page. host_port is assigned once
-- agent-runner picks one from its own configured range, published the
-- same native way ssh_port is. Hard-deleted, not soft-deleted like
-- environments itself — a removed row's host_port is immediately free for
-- reuse, so no partial-on-deleted_at index is needed the way
-- uq_environments_ssh_port has one.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS environment_port_forwards (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    environment_id  UUID        NOT NULL,
    label           TEXT        NOT NULL,
    container_port  INTEGER     NOT NULL,
    host_port       INTEGER,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_environment_port_forwards_environment
        FOREIGN KEY (environment_id)
        REFERENCES environments(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_environment_port_forwards_created_by
        FOREIGN KEY (created_by)
        REFERENCES users(id)
        ON DELETE SET NULL
);

-- Brings a database that already had this table (from a pre-consolidation
-- run) to the same final shape a fresh CREATE TABLE above already has.
ALTER TABLE environment_port_forwards
    DROP COLUMN IF EXISTS target_addr;

CREATE UNIQUE INDEX IF NOT EXISTS uq_environment_port_forwards_container_port
    ON environment_port_forwards (environment_id, container_port);
CREATE UNIQUE INDEX IF NOT EXISTS uq_environment_port_forwards_host_port
    ON environment_port_forwards (host_port)
    WHERE host_port IS NOT NULL;

-- -------------------------------------------------------------------------
-- AGENTS: default environment
-- Lets a project-scoped agent name a static environment it should default
-- to instead of getting a fresh ephemeral sandbox on every conversation.
-- Left unenforced for global-scope agents (project_id IS NULL) at the DB
-- layer — services/api's CreateAgent/UpdateAgent validates the referenced
-- environment belongs to the agent's own project, and the frontend hides
-- the field entirely for global agents.
-- -------------------------------------------------------------------------

ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS default_environment_id UUID
    REFERENCES environments(id)
    ON DELETE SET NULL;

-- -------------------------------------------------------------------------
-- AGENT_CONVERSATIONS: attach to an environment + folder
-- Replaces 5 legacy per-conversation-container columns (container_id,
-- host_port, repo_clone_url, branch_name, persistence_dir — inherited from
-- the old Python services/ai-agent and never assigned by any current Go
-- call site) with a reference to a static environment plus the folder
-- within it a conversation is attached to. Dropped rather than
-- repurposed: the old columns encode one-conversation-owns-one-container;
-- a static environment is many-conversations-to-one-container, a
-- different cardinality that deserves its own column name. pr_url is
-- untouched — still a live, populated column. Both new columns are
-- nullable: most conversations still get an ephemeral per-conversation
-- sandbox and never reference an environment at all.
-- -------------------------------------------------------------------------

ALTER TABLE agent_conversations
    DROP COLUMN IF EXISTS container_id,
    DROP COLUMN IF EXISTS host_port,
    DROP COLUMN IF EXISTS repo_clone_url,
    DROP COLUMN IF EXISTS branch_name,
    DROP COLUMN IF EXISTS persistence_dir,
    ADD COLUMN IF NOT EXISTS environment_id UUID
        REFERENCES environments(id)
        ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS environment_folder_id UUID
        REFERENCES environment_folders(id)
        ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_agent_conversations_environment_id
    ON agent_conversations (environment_id)
    WHERE environment_id IS NOT NULL;

COMMIT;
