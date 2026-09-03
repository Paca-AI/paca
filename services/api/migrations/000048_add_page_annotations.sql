-- 000048_add_page_annotations.sql
-- Adds page annotations: comments a user pins to a specific element on a
-- page running inside an environment's forwarded port, created via the
-- Paca browser extension (apps/extension) the same way Vercel's preview
-- toolbar lets you comment directly on a deployed preview. See
-- docs/ai-agent/environment-management.md's "Port Forwarding" section for
-- why there is no Paca proxy in the forwarded-port request path — the
-- extension talks to this API directly from a content script running on
-- the forwarded page itself.
--
-- A comment belongs to the specific port forward it was made through, not
-- the environment as a whole (an environment can have several port
-- forwards, each its own running dev-server app) — environment_id is kept
-- only as a server-derived, denormalized display field, always copied from
-- the owning port forward at creation and never independently settable.
--
-- page_annotations.page_path deliberately excludes host/port: identity is
-- (port_forward_id, page_path), not (host, port, page_path), since the same
-- port forward's dev server can move between host_port values across a
-- Docker "restart to apply port changes" cycle (see
-- environments.ports_pending_restart) without the comment being about a
-- different page.
--
-- Every statement below converges to the same final schema regardless of
-- starting state (see database.RunMigrationsFS's own doc comment — there
-- is no migration-tracking table, every file here re-runs every startup).

BEGIN;

CREATE TABLE IF NOT EXISTS page_annotations (
    id                         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id                 UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- environment_id is derived, denormalized display context — always the
    -- owning port_forward_id's own environment, copied in at creation and
    -- never independently settable. port_forward_id is the actual owner: a
    -- comment belongs to one specific port forward's running app, not the
    -- environment as a whole, since an environment can have several.
    environment_id             UUID        NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    port_forward_id            UUID        NOT NULL REFERENCES environment_port_forwards(id) ON DELETE CASCADE,
    -- page_path is pathname+query only — no host, no port (see header
    -- comment above), no hash (hash is client-side routing state, not a
    -- stable identity for "which page is this").
    page_path                  TEXT        NOT NULL,
    element_selector           TEXT        NOT NULL,
    element_selector_fallbacks JSONB       NOT NULL DEFAULT '[]',
    -- bounding_box: {x_pct, y_pct, width_pct, height_pct, viewport_width,
    -- viewport_height} — the last-resort re-anchoring signal when every
    -- selector fails to resolve on a later visit (see
    -- annotationdom.BoundingBox).
    bounding_box                JSONB      NOT NULL,
    -- element_snapshot: {tag_name, text_excerpt, outer_html_excerpt,
    -- accessible_name, role} — captured once at comment time so a human or
    -- agent can tell what was being pointed at without re-opening the page
    -- (see annotationdom.ElementSnapshot).
    element_snapshot            JSONB      NOT NULL,
    -- console_errors / failed_requests: captured by the extension at
    -- comment time (see annotationdom.ConsoleEntry / FailedRequest) —
    -- empty arrays, never null, when nothing was captured.
    console_errors              JSONB      NOT NULL DEFAULT '[]',
    failed_requests              JSONB      NOT NULL DEFAULT '[]',
    screenshot_file_id           UUID       REFERENCES files(id) ON DELETE SET NULL,
    body                         TEXT       NOT NULL,
    status                       TEXT       NOT NULL DEFAULT 'open',
    task_id                      UUID       REFERENCES tasks(id) ON DELETE SET NULL,
    created_by                   UUID       NOT NULL REFERENCES users(id),
    resolved_by                  UUID       REFERENCES users(id),
    resolved_at                  TIMESTAMPTZ,
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at                   TIMESTAMPTZ
);

ALTER TABLE page_annotations DROP CONSTRAINT IF EXISTS page_annotations_status_check;
ALTER TABLE page_annotations ADD CONSTRAINT page_annotations_status_check
    CHECK (status IN ('open', 'resolved'));

-- The extension's own hot path: "give me every annotation for this port
-- forward+page" on every preview page load.
CREATE INDEX IF NOT EXISTS idx_page_annotations_lookup
    ON page_annotations (port_forward_id, page_path) WHERE deleted_at IS NULL;

-- The web app's Comments view: "every annotation for this port forward."
CREATE INDEX IF NOT EXISTS idx_page_annotations_port_forward
    ON page_annotations (port_forward_id) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_page_annotations_project
    ON page_annotations (project_id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS page_annotation_comments (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    annotation_id UUID        NOT NULL REFERENCES page_annotations(id) ON DELETE CASCADE,
    body          TEXT        NOT NULL,
    created_by    UUID        NOT NULL REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_page_annotation_comments_annotation
    ON page_annotation_comments (annotation_id) WHERE deleted_at IS NULL;

-- Grant the new annotations.read/write/resolve permissions to existing
-- project_roles rows for the built-in role names — mirrors
-- 000044_add_environment_permissions.sql's own backfill exactly (see that
-- file's header comment for why both the global role *templates*, kept in
-- sync defensively even though bootstrap re-syncs them on every startup,
-- and the actual per-project Admin/Editor/Viewer rows, never re-synced
-- after project creation, both need this backfill).
UPDATE project_roles
SET permissions = permissions || '{"annotations.*": true}'::jsonb,
    updated_at = NOW()
WHERE role_name IN ('PROJECT_OWNER', 'PROJECT_MANAGER', 'Admin');

UPDATE project_roles
SET permissions = permissions || '{"annotations.read": true, "annotations.write": true, "annotations.resolve": true}'::jsonb,
    updated_at = NOW()
WHERE role_name IN ('PROJECT_MEMBER', 'Editor');

UPDATE project_roles
SET permissions = permissions || '{"annotations.read": true}'::jsonb,
    updated_at = NOW()
WHERE role_name IN ('PROJECT_VIEWER', 'Viewer');

COMMIT;
