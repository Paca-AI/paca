package postgres

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// NewIAMAttributeLoaders returns the Postgres-backed loaders for every
// built-in resource kind that has typed attributes (task, view, doc, agent,
// environment, conversation). Register them on an iam.Authorizer.
func NewIAMAttributeLoaders(db *sqlx.DB) []iam.AttributeLoader {
	return []iam.AttributeLoader{
		&scalarLoader{db: db, kind: "task", table: "tasks", where: "deleted_at IS NULL", cols: map[string]string{
			"task.sprint_id": "sprint_id",
			"task.status_id": "status_id",
			"task.type_id":   "task_type_id",
		}, multi: map[string]string{
			// Assignees are project_members rows; expose the member's
			// principal id (user_id or agent_id) so the value is comparable
			// with principal.id.
			"task.assignee_id": `SELECT COALESCE(pm.user_id, pm.agent_id)::text
				FROM task_assignees ta JOIN project_members pm ON pm.id = ta.member_id
				WHERE ta.task_id = $1::uuid AND pm.deleted_at IS NULL
				  AND ($2::text = '' OR EXISTS (SELECT 1 FROM tasks t WHERE t.id = ta.task_id AND t.project_id::text = $2::text))`,
		}},
		// sprint_views has no deleted_at (views are hard-deleted). Backlog and
		// timeline views are project-level: sprint_id is NULL, so view.sprint_id
		// is absent for them.
		&scalarLoader{db: db, kind: "view", table: "sprint_views", cols: map[string]string{
			"view.sprint_id": "sprint_id",
		}},
		&docLoader{scalarLoader{db: db, kind: "doc", table: "documents", where: "deleted_at IS NULL", cols: map[string]string{
			"doc.folder_id": "folder_id",
		}, multi: map[string]string{
			// The document's folder plus every ancestor folder. UNION (no
			// depth column) deduplicates rows, so a corrupt parent cycle
			// terminates instead of recursing forever.
			"doc.ancestor_folder_ids": `WITH RECURSIVE anc(id, parent_id) AS (
					SELECT f.id, f.parent_id FROM doc_folders f
					WHERE f.id = (SELECT d.folder_id FROM documents d WHERE d.id = $1::uuid AND d.deleted_at IS NULL)
					  AND ($2::text = '' OR f.project_id::text = $2::text)
				  UNION
					SELECT f.id, f.parent_id FROM doc_folders f JOIN anc a ON f.id = a.parent_id
					WHERE ($2::text = '' OR f.project_id::text = $2::text)
				) SELECT id::text FROM anc`,
		}}},
		&scalarLoader{db: db, kind: "agent", table: "agents", where: "deleted_at IS NULL",
			// Project agents carry project_id; global agents (project_id NULL)
			// reach a project through an active membership.
			projectWhere: `(project_id = $2::uuid OR EXISTS (SELECT 1 FROM project_members pm
				WHERE pm.agent_id = agents.id AND pm.project_id = $2::uuid AND pm.deleted_at IS NULL))`,
			cols: map[string]string{
				"agent.environment_id": "default_environment_id",
			}},
		// environments has no "type" column; the runtime flavour is
		// `backend` ('docker' | 'kubernetes'), which environment.type maps to.
		&scalarLoader{db: db, kind: "environment", table: "environments", where: "deleted_at IS NULL", cols: map[string]string{
			"environment.type": "backend",
		}},
		&scalarLoader{db: db, kind: "conversation", table: "agent_conversations", cols: map[string]string{
			"conversation.environment_id": "environment_id",
		}},
	}
}

// scalarLoader loads single-column attributes from one row (cols: key ->
// column) and set-valued attributes from dedicated queries taking the
// resource id as $1 (multi: key -> SQL returning one text column). Only the
// requested keys are queried; NULLs, missing rows and malformed ids yield
// absent attributes. All SQL text is static (never built from input).
type scalarLoader struct {
	db    *sqlx.DB
	kind  string
	table string
	where string
	// projectWhere is the project-membership predicate ($2 = project id,
	// applied only when the resource string names a project). Default:
	// project_id = $2::uuid.
	projectWhere string
	cols         map[string]string
	multi        map[string]string
}

func (l *scalarLoader) Kind() string { return l.kind }

// Load implements iam.AttributeLoader. The row must exist and (when projectID
// is non-empty) belong to that project, else iam.ErrResourceNotInProject.
func (l *scalarLoader) Load(ctx context.Context, resourceID, projectID string, keys []string) (map[string][]string, error) {
	out := map[string][]string{}
	if _, err := uuid.Parse(resourceID); err != nil {
		return nil, iam.ErrResourceNotInProject // cannot exist
	}
	if projectID != "" {
		if _, err := uuid.Parse(projectID); err != nil {
			return nil, iam.ErrResourceNotInProject
		}
	}

	var colKeys []string
	for _, k := range keys {
		if _, ok := l.cols[k]; ok {
			colKeys = append(colKeys, k)
		}
	}
	sort.Strings(colKeys)
	sel := "1::text"
	if len(colKeys) > 0 {
		sel = ""
		for i, k := range colKeys {
			if i > 0 {
				sel += ", "
			}
			sel += l.cols[k] + "::text"
		}
	}
	q := fmt.Sprintf("SELECT %s FROM %s WHERE id = $1::uuid", sel, l.table)
	args := []any{resourceID}
	if l.where != "" {
		q += " AND " + l.where
	}
	if projectID != "" {
		pw := l.projectWhere
		if pw == "" {
			pw = "project_id = $2::uuid"
		}
		q += " AND " + pw
		args = append(args, projectID)
	}
	vals := make([]*string, len(colKeys)+1)
	dest := make([]any, 0, len(vals))
	if len(colKeys) == 0 {
		dest = append(dest, &vals[0])
	} else {
		for i := range colKeys {
			dest = append(dest, &vals[i])
		}
	}
	rows, err := l.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("iam loader %s: %w", l.kind, err)
	}
	found := rows.Next()
	if found {
		if err := rows.Scan(dest...); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("iam loader %s: %w", l.kind, err)
		}
		for i, k := range colKeys {
			if vals[i] != nil {
				out[k] = []string{*vals[i]}
			}
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iam loader %s: %w", l.kind, err)
	}
	_ = rows.Close()
	if !found {
		return nil, iam.ErrResourceNotInProject
	}

	for _, k := range keys {
		q, ok := l.multi[k]
		if !ok {
			continue
		}
		var vs []string
		if err := l.db.SelectContext(ctx, &vs, q, resourceID, projectID); err != nil {
			return nil, fmt.Errorf("iam loader %s: %w", l.kind, err)
		}
		if len(vs) > 0 {
			out[k] = dedupStrings(vs)
		}
	}
	return out, nil
}

func dedupStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// docLoader is the scalarLoader for documents plus iam.AttributeExpander:
// given a new doc.folder_id it derives doc.ancestor_folder_ids.
type docLoader struct{ scalarLoader }

// folderAncestorsSQL walks the folder and its parents, staying inside the
// project ($2; ” = no project check). UNION deduplicates, so cycles end.
const folderAncestorsSQL = `WITH RECURSIVE anc(id, parent_id) AS (
		SELECT f.id, f.parent_id FROM doc_folders f
		WHERE f.id = $1::uuid AND ($2::text = '' OR f.project_id::text = $2::text)
	  UNION
		SELECT f.id, f.parent_id FROM doc_folders f JOIN anc a ON f.id = a.parent_id
		WHERE ($2::text = '' OR f.project_id::text = $2::text)
	) SELECT id::text FROM anc`

// Expand implements iam.AttributeExpander. An empty folder id (document at the
// root) yields empty folder and ancestor attributes.
func (l *docLoader) Expand(ctx context.Context, _, projectID string, newAttrs map[string][]string) (map[string][]string, error) {
	out := make(map[string][]string, len(newAttrs)+1)
	for k, v := range newAttrs {
		out[k] = v
	}
	folders, ok := newAttrs["doc.folder_id"]
	if !ok {
		return out, nil
	}
	if len(folders) == 0 || folders[0] == "" {
		out["doc.folder_id"], out["doc.ancestor_folder_ids"] = nil, nil
		return out, nil
	}
	if _, err := uuid.Parse(folders[0]); err != nil {
		return nil, iam.ErrResourceNotInProject
	}
	if projectID != "" {
		if _, err := uuid.Parse(projectID); err != nil {
			return nil, iam.ErrResourceNotInProject
		}
	}
	var anc []string
	if err := l.db.SelectContext(ctx, &anc, folderAncestorsSQL, folders[0], projectID); err != nil {
		return nil, fmt.Errorf("iam loader doc: expand: %w", err)
	}
	if len(anc) == 0 {
		return nil, iam.ErrResourceNotInProject // folder missing or in another project
	}
	out["doc.ancestor_folder_ids"] = dedupStrings(anc)
	return out, nil
}
