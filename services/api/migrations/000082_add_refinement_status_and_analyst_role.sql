-- 000082_add_refinement_status_and_analyst_role.sql
-- Draft tasks (see docs/ai-agent/pm-agent.md) live in a refinement-category
-- status, and the Analyst project role may only write tasks while they are in
-- one. New projects get both at creation; this gives them to existing ones.
--
-- Refinement: for every project with no refinement-category status yet, shift
-- its statuses down one position and insert "Refinement" at the old first
-- position. A project that already has one (any name) is left alone, and no
-- task changes status. A project that already uses the name "Refinement" for
-- a status of another category gets "Drafts" instead, so names stay distinct.
--
-- Analyst: inserted for every project that does not have a role of that name.
-- The policy must match internal/bootstrap/defaultroles/policies/
-- project_analyst.json with PROJECT_ID replaced (a migration test checks this).
-- Both steps are idempotent. Like every migration here this is one-way.

BEGIN;

WITH targets AS (
    SELECT p.id, COALESCE(MIN(ts.position), 1) AS first_position
      FROM projects p
      LEFT JOIN task_statuses ts ON ts.project_id = p.id
     WHERE NOT EXISTS (
            SELECT 1 FROM task_statuses r
             WHERE r.project_id = p.id AND r.category = 'refinement')
     GROUP BY p.id
), shifted AS (
    UPDATE task_statuses ts
       SET position = ts.position + 1, updated_at = NOW()
      FROM targets t
     WHERE ts.project_id = t.id
)
INSERT INTO task_statuses (project_id, name, color, position, category, is_default)
SELECT t.id,
       CASE WHEN EXISTS (SELECT 1 FROM task_statuses n
                          WHERE n.project_id = t.id AND lower(n.name) = 'refinement')
            THEN 'Drafts' ELSE 'Refinement' END,
       '#a855f7', t.first_position, 'refinement', false
  FROM targets t;

INSERT INTO roles (name, description, policy, project_id, is_system)
SELECT 'Analyst',
       'Create and refine draft tasks in Refinement; read code.',
       replace($policy${
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "ReadProject",
      "effect": "Allow",
      "actions": ["projects:read"],
      "resources": ["project/PROJECT_ID"]
    },
    {
      "sid": "Read",
      "effect": "Allow",
      "actions": [
        "agents:read",
        "annotations:read",
        "conversations:read",
        "docs:read",
        "environments:read",
        "git:read",
        "project.members:read",
        "sprints:read",
        "tasks:read",
        "views:read",
        "workflows:read"
      ],
      "resources": ["project/PROJECT_ID/*"]
    },
    {
      "sid": "Write",
      "effect": "Allow",
      "actions": ["conversations:write", "docs:write"],
      "resources": ["project/PROJECT_ID/*"]
    },
    {
      "sid": "CreateTasks",
      "effect": "Allow",
      "actions": ["tasks:write"],
      "resources": ["project/PROJECT_ID"]
    },
    {
      "sid": "RefineDrafts",
      "effect": "Allow",
      "actions": ["tasks:write"],
      "resources": ["project/PROJECT_ID/task/*"],
      "conditions": { "In": { "task.status_category": ["refinement"] } }
    },
    {
      "sid": "LeaveDraftsUnassigned",
      "effect": "Deny",
      "actions": ["tasks:write"],
      "resources": ["project/PROJECT_ID/task/*"],
      "conditions": { "StringLike": { "task.assignee_id": ["*"] } }
    },
    {
      "sid": "LeaveDraftsUnplanned",
      "effect": "Deny",
      "actions": ["tasks:write"],
      "resources": ["project/PROJECT_ID/task/*"],
      "conditions": { "StringLike": { "task.sprint_id": ["*"] } }
    },
    {
      "sid": "ReadRoles",
      "effect": "Allow",
      "actions": ["roles:read"],
      "resources": ["project/PROJECT_ID/role/*"]
    }
  ]
}$policy$, 'PROJECT_ID', p.id::text)::jsonb,
       p.id,
       false
  FROM projects p
 WHERE NOT EXISTS (
        SELECT 1 FROM roles r WHERE r.project_id = p.id AND r.name = 'Analyst');

COMMIT;
