// Package search holds the result types of the workspace-wide search
// (GET /search). Which rows a caller may see is decided by the handler, which
// passes the repository only the projects (and, for tasks, the per-project
// IAM scope) the caller may read.
package search

import "github.com/google/uuid"

// ProjectHit is a project whose name, description or task-key prefix matched.
type ProjectHit struct {
	ID             uuid.UUID `db:"id"`
	Name           string    `db:"name"`
	Description    string    `db:"description"`
	TaskIDPrefix   string    `db:"task_id_prefix"`
	AvatarThumbKey *string   `db:"avatar_thumb_key"`
}

// TaskHit is a task whose title or key matched.
type TaskHit struct {
	ID             uuid.UUID `db:"id"`
	ProjectID      uuid.UUID `db:"project_id"`
	ProjectName    string    `db:"project_name"`
	TaskIDPrefix   string    `db:"task_id_prefix"`
	TaskNumber     int       `db:"task_number"`
	Title          string    `db:"title"`
	StatusName     *string   `db:"status_name"`
	StatusCategory *string   `db:"status_category"`
	// The task's type (Epic, Story, Bug, ...) as the project defines it; nil
	// when the task has none.
	TypeName  *string `db:"type_name"`
	TypeIcon  *string `db:"type_icon"`
	TypeColor *string `db:"type_color"`
}

// PersonHit is a user whose display name or username matched.
type PersonHit struct {
	ID             uuid.UUID `db:"id"`
	Name           string    `db:"name"`
	Username       string    `db:"username"`
	AvatarThumbKey *string   `db:"avatar_thumb_key"`
	// ProjectID is one project the person is a member of that the caller may
	// list the members of (the first by name), so the result can link to its
	// team page. Nil when they share none, e.g. when a user admin (users:write)
	// finds someone outside their projects.
	ProjectID *uuid.UUID `db:"project_id"`
}
