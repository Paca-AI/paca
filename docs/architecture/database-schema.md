# Database Schema

Interactive diagram: [https://dbdiagram.io/d/Paca-69c212ae78c6c4bc7a4fc190](https://dbdiagram.io/d/Paca-69c212ae78c6c4bc7a4fc190)

> **Note:** The DBML diagram above may lag behind the latest migrations. The authoritative source is `services/api/migrations/`. The schema below reflects the current migration state.

## Current Migration State

| File | Purpose |
|---|---|
| `000001_init.sql` | Full schema: `global_roles`, `users` (with `role_id` FK and `must_change_password`), projects, project roles/members, seed data |

## Schema (DBML)

```dbml
// --- USER & GLOBAL ROLE MANAGEMENT ---
Table users {
  id uuid [primary key]
  username varchar [unique, not null]
  password_hash varchar [not null]
  full_name varchar
  role_id uuid [ref: > global_roles.id, not null]
  must_change_password boolean [not null, default: false]
  created_at timestamp
  updated_at timestamp
  deleted_at timestamp [null]
}

Table global_roles {
  id uuid [primary key]
  name varchar [unique, not null]
  permissions jsonb [not null]
  created_at timestamp
  updated_at timestamp
}

// --- PROJECT & TEAM MANAGEMENT ---
Table projects {
  id uuid [primary key]
  name varchar
  description text
  settings jsonb
  created_by uuid
  created_at timestamp
}

Table project_roles {
  id uuid [primary key]
  project_id uuid
  role_name varchar
  permissions jsonb
}

Table project_members {
  id uuid [primary key]
  project_id uuid
  user_id uuid
  project_role_id uuid

  indexes {
    (project_id, user_id) [unique]
  }
}

// --- TASK CONFIGURATION ---
Table task_types {
  id uuid [primary key]
  project_id uuid
  name varchar
  icon varchar
  color varchar
  description text
}

Table task_statuses {
  id uuid [primary key]
  project_id uuid
  name varchar
  color varchar
  position integer
  category varchar // backlog, refinement, ready, todo, inprogress, done
}

// --- TASKS ---
Table tasks {
  id uuid [primary key]
  project_id uuid
  task_type_id uuid
  status_id uuid
  sprint_id uuid
  parent_task_id uuid [null]
  title varchar
  description text
  priority varchar
  assignee_id uuid
  reporter_id uuid
  custom_fields jsonb
  created_at timestamp
}

Table custom_field_definitions {
  id uuid [primary key]
  project_id uuid
  field_key varchar
  display_name varchar
  field_type varchar
  options jsonb [null]
  is_required boolean [default: false]
}

// --- SPRINTS & VIEWS ---
Table sprints {
  id uuid [primary key]
  project_id uuid
  name varchar
  start_date date
  end_date date
  goal text
  status varchar
}

Table sprint_views {
  id uuid [primary key]
  sprint_id uuid
  name varchar
  view_type varchar // kanban, list, gantt, burndown
  config jsonb
}

// --- FEATURES & UTILITIES ---
Table bdd_scenarios {
  id uuid [primary key]
  task_id uuid
  title varchar
  given text
  when text
  then text
  created_at timestamp
}

Table time_logs {
  id uuid [primary key]
  task_id uuid
  member_id uuid
  duration_minutes integer
  logged_date date
}

Table documents {
  id uuid [primary key]
  project_id uuid
  title varchar
  content text
  created_by uuid
}

Table dashboards {
  id uuid [primary key]
  project_id uuid
  name varchar
  layout jsonb
}

Table task_activities {
  id uuid [primary key]
  task_id uuid
  member_id uuid
  activity_type varchar
  content text
  created_at timestamp
}

// --- RELATIONSHIPS ---
Ref: projects.id < project_members.project_id
Ref: users.id < project_members.user_id
Ref: project_roles.id < project_members.project_role_id
Ref: projects.id < project_roles.project_id

Ref: projects.id < task_types.project_id
Ref: projects.id < task_statuses.project_id
Ref: task_types.id < tasks.task_type_id
Ref: task_statuses.id < tasks.status_id

Ref: projects.id < tasks.project_id
Ref: projects.id < sprints.project_id
Ref: sprints.id < tasks.sprint_id
Ref: tasks.id < tasks.parent_task_id
Ref: projects.id < custom_field_definitions.project_id
Ref: tasks.id < bdd_scenarios.task_id
Ref: tasks.id < time_logs.task_id
Ref: tasks.id < task_activities.task_id
Ref: projects.id < documents.project_id
Ref: projects.id < dashboards.project_id

Ref: users.id < projects.created_by
Ref: project_members.id < documents.created_by
Ref: project_members.id < time_logs.member_id
Ref: project_members.id < task_activities.member_id
Ref: project_members.id < tasks.assignee_id
Ref: project_members.id < tasks.reporter_id
Ref: sprints.id < sprint_views.sprint_id
```
