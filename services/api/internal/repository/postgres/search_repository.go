package postgres

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	searchdom "github.com/Paca-AI/api/internal/domain/search"
)

// SearchRepository backs the workspace-wide search (GET /search). It never
// decides on its own what a caller may see: every method is handed the
// projects to look in, and SearchTasks also applies the per-project task
// scope the handler attaches to ctx (iam.WithProjectScopes), so a role limited
// to some tasks of a project only finds those.
type SearchRepository struct {
	db *sqlx.DB
}

// NewSearchRepository returns a SearchRepository backed by db.
func NewSearchRepository(db *sqlx.DB) *SearchRepository {
	return &SearchRepository{db: db}
}

// taskRefRE matches a query that names a task by number: "PRJ-12", "#12" or
// "12". The prefix group is empty for the last two.
var taskRefRE = regexp.MustCompile(`^(?:([A-Za-z0-9]{1,10})-|#)?([1-9][0-9]{0,8})$`)

// parseTaskRef splits a task reference query into its key prefix (upper-case,
// "" when none was given) and number.
func parseTaskRef(q string) (prefix string, number int, ok bool) {
	m := taskRefRE.FindStringSubmatch(strings.TrimSpace(q))
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return strings.ToUpper(m[1]), n, true
}

func containsPattern(q string) string { return "%" + escapeLikePattern(q) + "%" }
func prefixPattern(q string) string   { return escapeLikePattern(q) + "%" }

// SearchProjects returns up to limit of projectIDs whose name or description
// contains q, or whose task-key prefix is q. Exact and prefix name matches
// come first.
func (r *SearchRepository) SearchProjects(ctx context.Context, projectIDs []uuid.UUID, q string, limit int) ([]searchdom.ProjectHit, error) {
	q = strings.TrimSpace(q)
	if q == "" || len(projectIDs) == 0 || limit <= 0 {
		return []searchdom.ProjectHit{}, nil
	}
	b := newQueryBuilder()
	b.addInClause("projects.id", uuidSliceToStrSlice(projectIDs))
	exact, contains, prefix := b.addArg(q), b.addArg(containsPattern(q)), b.addArg(prefixPattern(q))
	lim := b.addArg(limit)

	query := `
		SELECT id, name, description, task_id_prefix, avatar_thumb_key
		FROM projects
		WHERE projects.deleted_at IS NULL AND ` + strings.Join(b.whereClauses, " AND ") + `
			AND (name ILIKE ` + contains + ` OR description ILIKE ` + contains + `
				OR (task_id_prefix != '' AND lower(task_id_prefix) = lower(` + exact + `)))
		ORDER BY lower(name) = lower(` + exact + `) DESC, name ILIKE ` + prefix + ` DESC, lower(name), id
		LIMIT ` + lim

	hits := []searchdom.ProjectHit{}
	if err := r.db.SelectContext(ctx, &hits, query, b.args...); err != nil {
		return nil, fmt.Errorf("search: projects: %w", err)
	}
	return hits, nil
}

// SearchTasks returns up to limit live tasks of projectIDs that the caller's
// task scope allows and whose title contains q. A query shaped like a task
// reference ("PRJ-12", "#12", "12") also matches that task by number, and
// such matches come first; the rest are most recently updated first.
func (r *SearchRepository) SearchTasks(ctx context.Context, projectIDs []uuid.UUID, q string, limit int) ([]searchdom.TaskHit, error) {
	q = strings.TrimSpace(q)
	if q == "" || len(projectIDs) == 0 || limit <= 0 {
		return []searchdom.TaskHit{}, nil
	}
	b := newQueryBuilder()
	b.addInClause("tasks.project_id", uuidSliceToStrSlice(projectIDs))
	clause, none, err := projectScopesSQL(ctx, "task", taskScopeColumns, "tasks.project_id", b)
	if err != nil {
		return nil, fmt.Errorf("search: tasks: %w", err)
	}
	if none {
		return []searchdom.TaskHit{}, nil
	}
	if clause != "" {
		b.whereClauses = append(b.whereClauses, clause)
	}

	// A task named by its reference is matched by number, and sorted first.
	byRef, refFirst := "FALSE", ""
	if prefix, number, ok := parseTaskRef(q); ok {
		byRef = "tasks.task_number = " + b.addArg(number)
		if prefix != "" {
			byRef = "(" + byRef + " AND upper(p.task_id_prefix) = " + b.addArg(prefix) + ")"
		}
		refFirst = byRef + " DESC, "
	}
	contains := b.addArg(containsPattern(q))
	lim := b.addArg(limit)

	query := `
		SELECT tasks.id, tasks.project_id, p.name AS project_name, p.task_id_prefix,
			tasks.task_number, tasks.title, ts.name AS status_name, ts.category AS status_category,
			tt.name AS type_name, tt.icon AS type_icon, tt.color AS type_color
		FROM tasks
		JOIN projects p ON p.id = tasks.project_id AND p.deleted_at IS NULL
		LEFT JOIN task_statuses ts ON ts.id = tasks.status_id
		LEFT JOIN task_types tt ON tt.id = tasks.task_type_id
		WHERE tasks.deleted_at IS NULL AND ` + strings.Join(b.whereClauses, " AND ") + `
			AND (` + byRef + ` OR tasks.title ILIKE ` + contains + `)
		ORDER BY ` + refFirst + `tasks.updated_at DESC, tasks.id
		LIMIT ` + lim

	hits := []searchdom.TaskHit{}
	if err := r.db.SelectContext(ctx, &hits, query, b.args...); err != nil {
		return nil, fmt.Errorf("search: tasks: %w", err)
	}
	return hits, nil
}

// SearchPeople returns up to limit live users (not agents or service
// accounts, and never the internal agent bot) whose display name or username
// contains q. With allUsers false, only members of memberProjectIDs are
// returned: the projects whose member list the caller may read, which keeps a
// regular user's search to their own teams. With allUsers true (a user admin),
// any user matches. Either way each hit's ProjectID is a project of
// memberProjectIDs they belong to, for linking to its team page.
func (r *SearchRepository) SearchPeople(ctx context.Context, memberProjectIDs []uuid.UUID, allUsers bool, q string, limit int) ([]searchdom.PersonHit, error) {
	q = strings.TrimSpace(q)
	if q == "" || limit <= 0 || (!allUsers && len(memberProjectIDs) == 0) {
		return []searchdom.PersonHit{}, nil
	}
	b := newQueryBuilder()
	exact, contains, prefix := b.addArg(q), b.addArg(containsPattern(q)), b.addArg(prefixPattern(q))

	// The member projects as a SQL list; '{}' (no project) when empty, so the
	// shared-project lookup simply finds nothing.
	ids := b.addArg(uuidSliceToStrSlice(memberProjectIDs))
	sharedProject := `(SELECT pm.project_id FROM project_members pm
			JOIN projects p ON p.id = pm.project_id AND p.deleted_at IS NULL
			WHERE pm.account_id = a.id AND pm.deleted_at IS NULL
				AND pm.project_id = ANY(` + ids + `::uuid[])
			ORDER BY lower(p.name), p.id LIMIT 1)`

	where := ""
	if !allUsers {
		where = " AND " + sharedProject + " IS NOT NULL"
	}
	lim := b.addArg(limit)

	query := `
		SELECT a.id, a.name, users.username, a.avatar_thumb_key, ` + sharedProject + ` AS project_id
		FROM users JOIN accounts a ON a.id = users.id
		WHERE a.deleted_at IS NULL AND users.username != '_paca_agent_bot'
			AND (a.name ILIKE ` + contains + ` OR users.username ILIKE ` + contains + `)` + where + `
		ORDER BY (lower(a.name) = lower(` + exact + `) OR lower(users.username) = lower(` + exact + `)) DESC,
			(a.name ILIKE ` + prefix + ` OR users.username ILIKE ` + prefix + `) DESC, lower(a.name), a.id
		LIMIT ` + lim

	hits := []searchdom.PersonHit{}
	if err := r.db.SelectContext(ctx, &hits, query, b.args...); err != nil {
		return nil, fmt.Errorf("search: people: %w", err)
	}
	return hits, nil
}
