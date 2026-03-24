# Database Schema

Interactive diagram: [https://dbdiagram.io/d/Paca-69c212ae78c6c4bc7a4fc190](https://dbdiagram.io/d/Paca-69c212ae78c6c4bc7a4fc190)

## Schema (DBML)

```dbml
// --- USER & GLOBAL ROLE MANAGEMENT ---
Table users {
  id integer [primary key]
  username varchar [unique]
  password_hash varchar
  full_name varchar
  created_at timestamp
}

Table global_roles {
  id integer [primary key]
  name varchar
  permissions jsonb
}

Table user_global_roles {
  user_id integer
  role_id integer
  indexes {
    (user_id, role_id) [unique]
  }
}

// --- PROJECT & TEAM MANAGEMENT ---
Table projects {
  id integer [primary key]
  name varchar
  description text
  settings jsonb
  created_by integer
  created_at timestamp
}

Table project_roles {
  id integer [primary key]
  project_id integer
  role_name varchar
  permissions jsonb
}

Table project_members {
  id integer [primary key]
  project_id integer
  user_id integer
  project_role_id integer

  indexes {
    (project_id, user_id) [unique]
  }
}

// --- TASK CONFIGURATION ---
Table task_types {
  id integer [primary key]
  project_id integer
  name varchar
  icon varchar
  color varchar
  description text
}

Table task_statuses {
  id integer [primary key]
  project_id integer
  name varchar
  color varchar
  position integer
  category varchar // backlog, refinement, ready, todo, inprogress, done
}

// --- TASKS ---
Table tasks {
  id integer [primary key]
  project_id integer
  task_type_id integer
  status_id integer
  sprint_id integer
  parent_task_id integer [null]
  title varchar
  description text
  priority varchar
  assignee_id integer
  reporter_id integer
  custom_fields jsonb
  created_at timestamp
}

Table custom_field_definitions {
  id integer [primary key]
  project_id integer
  field_key varchar
  display_name varchar
  field_type varchar
  options jsonb [null]
  is_required boolean [default: false]
}

// --- SPRINTS & VIEWS ---
Table sprints {
  id integer [primary key]
  project_id integer
  name varchar
  start_date date
  end_date date
  goal text
  status varchar
}

Table sprint_views {
  id integer [primary key]
  sprint_id integer
  name varchar
  view_type varchar // kanban, list, gantt, burndown
  config jsonb
}

// --- FEATURES & UTILITIES ---
Table bdd_scenarios {
  id integer [primary key]
  task_id integer
  title varchar
  given text
  when text
  then text
  created_at timestamp
}

Table time_logs {
  id integer [primary key]
  task_id integer
  user_id integer
  duration_minutes integer
  logged_date date
}

Table documents {
  id integer [primary key]
  project_id integer
  title varchar
  content text
  created_by integer
}

Table dashboards {
  id integer [primary key]
  project_id integer
  name varchar
  layout jsonb
}

Table task_activities {
  id integer [primary key]
  task_id integer
  user_id integer
  activity_type varchar
  content text
  created_at timestamp
}

// --- RELATIONSHIPS ---
Ref: users.id < user_global_roles.user_id
Ref: global_roles.id < user_global_roles.role_id
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
Ref: users.id < documents.created_by
Ref: users.id < time_logs.user_id
Ref: users.id < task_activities.user_id
Ref: users.id < tasks.assignee_id
Ref: users.id < tasks.reporter_id
Ref: sprints.id < sprint_views.sprint_id
```
