package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// WorkspaceTrendRepository rebuilds the home page's workspace counters as
// daily series, so the stat cards can show how each one moved. Nothing here
// is a stored snapshot: every series is reconstructed from the rows' own
// timestamps (and, for open tasks, from the activity log), which means it
// works on existing data without a backfill.
//
// Each method returns one count per day end in [firstDayEnd, lastDayEnd],
// stepping one day at a time — the value of the counter at that instant.
type WorkspaceTrendRepository struct {
	db *sqlx.DB
}

// NewWorkspaceTrendRepository returns a WorkspaceTrendRepository backed by db.
func NewWorkspaceTrendRepository(db *sqlx.DB) *WorkspaceTrendRepository {
	return &WorkspaceTrendRepository{db: db}
}

// dayEndsSQL is the series of instants the counters are sampled at. ord is
// 1-based, matching the position in the returned slice plus one. The step is
// 24 hours rather than '1 day', which would follow DST in the session zone.
func dayEndsSQL(first, last string) string {
	return `SELECT ord, day_end
		FROM generate_series(` + first + `::timestamptz, ` + last + `::timestamptz, interval '24 hours')
			WITH ORDINALITY AS g(day_end, ord)`
}

func dayCount(firstDayEnd, lastDayEnd time.Time) int {
	if lastDayEnd.Before(firstDayEnd) {
		return 0
	}
	return int(lastDayEnd.Sub(firstDayEnd).Hours()/24) + 1
}

type trendRow struct {
	Ord   int   `db:"ord"`
	Count int64 `db:"count"`
}

// fillSeries spreads (ord, count) rows over a zeroed slice of n days, so a day
// the query returned no row for reads as zero.
func fillSeries(n int, rows []trendRow) []int64 {
	series := make([]int64, n)
	for _, row := range rows {
		if row.Ord >= 1 && row.Ord <= n {
			series[row.Ord-1] = row.Count
		}
	}
	return series
}

// ProjectCountTrend counts, at each day end, how many of projectIDs existed:
// created before it and not yet deleted. projectIDs is the caller's current
// set of accessible projects, so the last value equals the live project count.
func (r *WorkspaceTrendRepository) ProjectCountTrend(ctx context.Context, projectIDs []uuid.UUID, firstDayEnd, lastDayEnd time.Time) ([]int64, error) {
	n := dayCount(firstDayEnd, lastDayEnd)
	if n == 0 || len(projectIDs) == 0 {
		return make([]int64, n), nil
	}
	b := newQueryBuilder()
	first, last := b.addArg(firstDayEnd), b.addArg(lastDayEnd)
	b.addInClause("p.id", uuidSliceToStrSlice(projectIDs))

	query := `
		WITH days AS (` + dayEndsSQL(first, last) + `)
		SELECT d.ord, COUNT(p.id) AS count
		FROM days d
		JOIN projects p ON p.created_at < d.day_end
			AND (p.deleted_at IS NULL OR p.deleted_at >= d.day_end)
		WHERE ` + strings.Join(b.whereClauses, " AND ") + `
		GROUP BY d.ord`

	var rows []trendRow
	if err := r.db.SelectContext(ctx, &rows, query, b.args...); err != nil {
		return nil, fmt.Errorf("workspace trend: projects: %w", err)
	}
	return fillSeries(n, rows), nil
}

// UserCountTrend counts, at each day end, the users that existed then, with
// the same filter as UserRepository.CountUsers (live accounts, minus the
// internal agent bot), so the last value equals the team-member stat.
func (r *WorkspaceTrendRepository) UserCountTrend(ctx context.Context, firstDayEnd, lastDayEnd time.Time) ([]int64, error) {
	n := dayCount(firstDayEnd, lastDayEnd)
	if n == 0 {
		return []int64{}, nil
	}
	query := `
		WITH days AS (` + dayEndsSQL("$1", "$2") + `)
		SELECT d.ord, COUNT(a.id) AS count
		FROM days d
		JOIN accounts a ON a.created_at < d.day_end
			AND (a.deleted_at IS NULL OR a.deleted_at >= d.day_end)
		JOIN users ON users.id = a.id
		WHERE users.username != '_paca_agent_bot'
		GROUP BY d.ord`

	var rows []trendRow
	if err := r.db.SelectContext(ctx, &rows, query, firstDayEnd, lastDayEnd); err != nil {
		return nil, fmt.Errorf("workspace trend: users: %w", err)
	}
	return fillSeries(n, rows), nil
}

// OpenTaskTrend counts, at each day end, the tasks of projectIDs that were
// open then: created, not deleted, and in a status whose category is not
// "done". Like CountOpenTasksByProjects, the caller's per-project task scope
// attached to ctx applies, and a task with no status is not counted.
//
// Tasks don't store when they were finished, so each task's status over time
// is rebuilt from the "status" entries of its task.updated activities, which
// record the old and new status by name:
//
//   - before its first recorded change, a task was in that change's old status;
//   - between two changes, in the first one's new status;
//   - after its last change (or always, if it has none), in its current status.
//
// Using the current status for the last stretch makes the final value equal
// the live open-task count even when a change was never recorded. A name is
// matched to a status of the task's project case-insensitively; a name that
// no longer matches any status (renamed or deleted since) counts as open,
// since most statuses are. The result is an approximation for that reason,
// and because changes made outside the task update endpoint aren't logged.
func (r *WorkspaceTrendRepository) OpenTaskTrend(ctx context.Context, projectIDs []uuid.UUID, firstDayEnd, lastDayEnd time.Time) ([]int64, error) {
	n := dayCount(firstDayEnd, lastDayEnd)
	if n == 0 || len(projectIDs) == 0 {
		return make([]int64, n), nil
	}
	b := newQueryBuilder()
	first, last := b.addArg(firstDayEnd), b.addArg(lastDayEnd)
	b.addInClause("tasks.project_id", uuidSliceToStrSlice(projectIDs))

	clause, none, err := projectScopesSQL(ctx, "task", taskScopeColumns, "tasks.project_id", b)
	if err != nil {
		return nil, fmt.Errorf("workspace trend: open tasks: %w", err)
	}
	if none {
		return make([]int64, n), nil
	}
	if clause != "" {
		b.whereClauses = append(b.whereClauses, clause)
	}

	// A status name is open unless it matches a done status of the project.
	// NULL means "no status", which is never counted.
	nameIsOpen := func(name string) string {
		return `CASE WHEN ` + name + ` IS NULL THEN NULL ELSE NOT EXISTS (
			SELECT 1 FROM task_statuses ts
			WHERE ts.project_id = s.project_id AND ts.category = 'done'
				AND lower(ts.name) = lower(` + name + `)) END`
	}

	query := `
		WITH days AS (` + dayEndsSQL(first, last) + `),
		scoped AS (
			SELECT tasks.id, tasks.project_id, tasks.created_at, tasks.deleted_at, tasks.status_id
			FROM tasks
			WHERE tasks.created_at < ` + last + `
				AND (tasks.deleted_at IS NULL OR tasks.deleted_at >= ` + first + `)
				AND ` + strings.Join(b.whereClauses, " AND ") + `
		),
		changes AS (
			SELECT a.entity_id AS task_id, a.created_at AS at,
				ch->>'old' AS old_name, ch->>'new' AS new_name,
				ROW_NUMBER() OVER (PARTITION BY a.entity_id ORDER BY a.created_at, a.id) AS seq
			FROM activities a
			JOIN scoped ON scoped.id = a.entity_id
			CROSS JOIN LATERAL jsonb_array_elements(
				CASE WHEN jsonb_typeof(a.content->'changes') = 'array'
					THEN a.content->'changes' ELSE '[]'::jsonb END) ch
			WHERE a.entity_type = 'task' AND a.activity_type = 'task.updated'
				AND a.deleted_at IS NULL AND ch->>'field' = 'status'
		),
		segments AS (
			-- Before the first recorded change: the status it changed from.
			SELECT c.task_id, NULL::timestamptz AS seg_start, c.at AS seg_end,
				` + nameIsOpen("c.old_name") + ` AS is_open
			FROM changes c JOIN scoped s ON s.id = c.task_id
			WHERE c.seq = 1
			UNION ALL
			-- Between two recorded changes: the status the first changed to.
			SELECT c.task_id, c.at, nxt.at, ` + nameIsOpen("c.new_name") + `
			FROM changes c
			JOIN changes nxt ON nxt.task_id = c.task_id AND nxt.seq = c.seq + 1
			JOIN scoped s ON s.id = c.task_id
			UNION ALL
			-- After the last recorded change, or always without one: the
			-- current status.
			SELECT s.id, lc.at, NULL::timestamptz,
				CASE WHEN cur.id IS NULL THEN NULL ELSE cur.category != 'done' END
			FROM scoped s
			LEFT JOIN (SELECT task_id, MAX(at) AS at FROM changes GROUP BY task_id) lc
				ON lc.task_id = s.id
			LEFT JOIN task_statuses cur ON cur.id = s.status_id
		)
		SELECT d.ord, COUNT(DISTINCT s.id) AS count
		FROM days d
		JOIN scoped s ON s.created_at < d.day_end
			AND (s.deleted_at IS NULL OR s.deleted_at >= d.day_end)
		JOIN segments g ON g.task_id = s.id
			AND (g.seg_start IS NULL OR g.seg_start <= d.day_end)
			AND (g.seg_end IS NULL OR g.seg_end > d.day_end)
		WHERE g.is_open
		GROUP BY d.ord`

	var rows []trendRow
	if err := r.db.SelectContext(ctx, &rows, query, b.args...); err != nil {
		return nil, fmt.Errorf("workspace trend: open tasks: %w", err)
	}
	return fillSeries(n, rows), nil
}
