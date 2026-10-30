package taskdom

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the combined persistence contract for the task aggregate.
type Repository interface {
	TaskTypeRepository
	TaskStatusRepository
	TaskRepository
	TaskLinkRepository
	CustomFieldDefinitionRepository
}

// TaskLinkRepository defines persistence operations for task links.
type TaskLinkRepository interface {
	// ListTaskLinks returns one page of the links where taskID is either source
	// or target, with LinkedTask populated, ordered by created_at then id
	// ascending. cursorAfter is the cursor from EncodeLinkCursor (nil for the
	// first page); hasMore reports whether more follow and total counts every
	// link of the task.
	ListTaskLinks(ctx context.Context, taskID uuid.UUID, cursorAfter *string, limit int) (links []*TaskLink, hasMore bool, total int, err error)
	// ListAllTaskLinks returns every link of the task. For internal jobs such
	// as automations; not exposed over the API.
	ListAllTaskLinks(ctx context.Context, taskID uuid.UUID) ([]*TaskLink, error)
	// FindTaskLinkByID returns a single link by primary key.
	FindTaskLinkByID(ctx context.Context, id uuid.UUID) (*TaskLink, error)
	// CreateTaskLinkIfNotExists persists l, unless an equivalent link already
	// exists (checked in both directions for relates_to), in which case it
	// returns created=false and no error. The existence check and insert run
	// in the same transaction with both task rows locked, so concurrent
	// attempts to link the same pair of tasks serialize instead of racing
	// past the duplicate check.
	CreateTaskLinkIfNotExists(ctx context.Context, l *TaskLink) (created bool, err error)
	// DeleteTaskLink removes the link identified by id.
	DeleteTaskLink(ctx context.Context, id uuid.UUID) error
}

// TaskTypeRepository defines persistence operations for task types.
type TaskTypeRepository interface {
	ListTaskTypes(ctx context.Context, projectID uuid.UUID) ([]*TaskType, error)
	FindTaskTypeByID(ctx context.Context, id uuid.UUID) (*TaskType, error)
	FindDefaultTaskType(ctx context.Context, projectID uuid.UUID) (*TaskType, error)
	CreateTaskType(ctx context.Context, t *TaskType) error
	UpdateTaskType(ctx context.Context, t *TaskType) error
	DeleteTaskType(ctx context.Context, id uuid.UUID) error
	// SetDefaultTaskType atomically clears is_default for every type in the
	// project and then marks the given type as the default.
	SetDefaultTaskType(ctx context.Context, projectID, typeID uuid.UUID) error
}

// TaskStatusRepository defines persistence operations for task statuses.
type TaskStatusRepository interface {
	ListTaskStatuses(ctx context.Context, projectID uuid.UUID) ([]*TaskStatus, error)
	FindTaskStatusByID(ctx context.Context, id uuid.UUID) (*TaskStatus, error)
	FindDefaultTaskStatus(ctx context.Context, projectID uuid.UUID) (*TaskStatus, error)
	CreateTaskStatus(ctx context.Context, s *TaskStatus) error
	UpdateTaskStatus(ctx context.Context, s *TaskStatus) error
	DeleteTaskStatus(ctx context.Context, id uuid.UUID) error
	// SetDefaultTaskStatus atomically clears is_default for every status in the
	// project and then marks the given status as the default.
	SetDefaultTaskStatus(ctx context.Context, projectID, statusID uuid.UUID) error
	// ReorderTaskStatuses atomically assigns position = index in statusIDs to
	// each status, verifying statusIDs is an exact permutation of the
	// project's existing statuses.
	ReorderTaskStatuses(ctx context.Context, projectID uuid.UUID, statusIDs []uuid.UUID) error
}

// TaskRepository defines persistence operations for tasks.
type TaskRepository interface {
	ListTasks(ctx context.Context, projectID uuid.UUID, filter TaskFilter, limit int, sort TaskSort) ([]*Task, bool, error)
	CountTasks(ctx context.Context, projectID uuid.UUID, filter TaskFilter) (int64, error)
	// SumTaskField sums a numeric field across all tasks matching the filter,
	// ignoring cursor-based pagination. fieldKey is "story_points" or a custom field key.
	SumTaskField(ctx context.Context, projectID uuid.UUID, filter TaskFilter, fieldKey string) (float64, error)
	FindTaskByID(ctx context.Context, id uuid.UUID) (*Task, error)
	FindTaskByNumber(ctx context.Context, projectID uuid.UUID, taskNumber int64) (*Task, error)
	CreateTask(ctx context.Context, t *Task) error
	UpdateTask(ctx context.Context, t *Task) error
	// UpdateTaskFields writes only the listed fields of t (plus updated_at),
	// leaving every other column as stored, so overlapping saves that touch
	// different fields can't overwrite each other with stale values.
	//
	// For TaskFieldCustomFields, customFieldsPatch is merged key by key into
	// the stored custom_fields in SQL rather than replacing it.
	UpdateTaskFields(ctx context.Context, t *Task, fields []TaskField, customFieldsPatch map[string]any) error
	// UpdateTaskAtomic loads id under a row-level lock (SELECT ... FOR
	// UPDATE) and, within that same transaction, calls decide with the
	// freshly-locked task. decide returns (nil, nil) to abort with no write
	// (used when the locked read shows the caller's intended update no
	// longer applies), a non-nil *Task to persist via the same write
	// UpdateTask itself performs, or a non-nil error to abort the
	// transaction entirely. Because the read and the write share one
	// transaction and the row stays locked for its whole duration, no other
	// UpdateTask/UpdateTaskAtomic call for the same id can land between
	// decide's read and its write — unlike a plain FindTaskByID followed
	// later by UpdateTask, which leaves exactly that gap open to whatever
	// the caller does in between (e.g. a slow external API call).
	UpdateTaskAtomic(ctx context.Context, id uuid.UUID, decide func(current *Task) (*Task, error)) (*Task, error)
	DeleteTask(ctx context.Context, id uuid.UUID) error
	// BulkMoveSprintTasks reassigns all non-done tasks in sourceSprintID to
	// targetSprintID. A nil targetSprintID moves tasks to the backlog (sprint_id = NULL).
	// Tasks whose status has category "done" are not moved.
	BulkMoveSprintTasks(ctx context.Context, projectID, sourceSprintID uuid.UUID, targetSprintID *uuid.UUID) error
	// ListAssignedTasks returns open (non-done) tasks assigned to any of
	// memberIDs, across whatever projects those members belong to — each
	// project_members.id already scopes a member to a single project, so no
	// separate project filter is needed. Results are ordered by importance
	// descending then created_at ascending, keyset-paginated via cursorAfter
	// (same cursor format as the "importance" ListTasks sort).
	// Used by the cross-project "assigned to me" home page widget.
	ListAssignedTasks(ctx context.Context, memberIDs []uuid.UUID, limit int, cursorAfter *string) ([]*Task, bool, error)
	// CountOpenTasksByProjects returns the number of non-done tasks across all
	// of projectIDs in a single query, for cross-project aggregates like the
	// home page's workspace stats widget — avoids an N-query per-project loop.
	CountOpenTasksByProjects(ctx context.Context, projectIDs []uuid.UUID) (int64, error)
	// ListDistinctTags returns every distinct tag value used by any
	// non-deleted task in the project, sorted alphabetically. Used to build
	// Jev's tag-autofill candidate set, which — unlike other autofill
	// fields — must only ever suggest tags the project has already used,
	// never invent new ones.
	ListDistinctTags(ctx context.Context, projectID uuid.UUID) ([]string, error)
	// ListGroupColumns returns the groups (columns) that exist for a group-by
	// dimension among the tasks filter matches: one per tag, per assignee, per
	// reporter, or per distinct value of a free-valued custom field (number,
	// text, date, …), ordered for display. It does not include the "no value"
	// group — that always exists and is selected with a GroupKey* sentinel.
	// It returns at most MaxGroupColumns+1 columns; the extra one means there
	// are more than MaxGroupColumns. Note a tag or value literally equal to a
	// GroupKey* sentinel can't be told apart from "no value" when selected.
	// by and fieldType are as in TaskGroup.
	ListGroupColumns(ctx context.Context, projectID uuid.UUID, filter TaskFilter, by, fieldType string) ([]GroupColumn, error)
}

// TaskSort carries resolved sort configuration for ListTasks.
// Kept separate from TaskFilter because sort order is a presentation concern,
// not a filter predicate.
//
// For built-in fields (importance, title, story_points, start_date, due_date,
// created) only By is set.  For custom field sorts, CFType and (for select)
// CFOpts are populated by the caller after looking up the field definition.
type TaskSort struct {
	By     string     // built-in field key, custom field key, or "" (default order)
	CFType string     // "number" | "date" | "select" — only for custom field sorts
	CFOpts []string   // ordered option values — only for "select" custom field sorts
	ViewID *uuid.UUID // when By == "view_position", JOIN view_task_positions for this view
}

// TaskFilter carries optional criteria for listing tasks.
type TaskFilter struct {
	SprintID     *uuid.UUID  // single-value compat; ignored when SprintIDs is non-empty
	SprintIDs    []uuid.UUID // multi-value; takes priority over SprintID and BacklogOnly
	StatusID     *uuid.UUID  // single-value compat; ignored when StatusIDs is non-empty
	StatusIDs    []uuid.UUID // multi-value; takes priority over StatusID
	AssigneeID   *uuid.UUID  // single-value compat; ignored when AssigneeIDs is non-empty
	AssigneeIDs  []uuid.UUID // multi-value; takes priority over AssigneeID
	AssigneeNull bool        // true → only tasks with no rows in task_assignees
	ParentTaskID *uuid.UUID  // non-nil → only subtasks of this parent
	TaskTypeIDs  []uuid.UUID // multi-value; when non-empty, only tasks of these types
	TaskTypeNull bool        // true → only tasks where task_type_id IS NULL
	// ExcludeSubtasks, when true, omits tasks whose parent is a live task in the
	// same sprint (or both in the backlog) that is not an Epic — they are listed
	// under that parent instead. Tasks that merely belong to an Epic, or whose
	// parent sits in another sprint, stay in the list. Ignored when
	// ParentTaskID is set.
	ExcludeSubtasks bool
	BacklogOnly     bool    // true → only tasks where sprint_id IS NULL
	CursorAfter     *string // opaque base64 cursor; when set, replaces offset-based paging
	// Search, when non-nil and non-blank, restricts results to tasks whose title
	// or "#<task_number>" id contains the text (case-insensitive).
	Search *string
	// SearchContent, when true and Search is set, also matches the task's
	// description text (inline BlockNote "text" nodes), not just title/number.
	SearchContent bool
	// CustomFieldFilters filters tasks by custom field values, keyed by field key.
	CustomFieldFilters map[string]CustomFieldFilterQuery
	// StartDateAfter/StartDateBefore filter tasks by start_date (inclusive,
	// "YYYY-MM-DD"). Either may be nil.
	StartDateAfter  *string
	StartDateBefore *string
	// DueDateAfter/DueDateBefore filter tasks by due_date (inclusive,
	// "YYYY-MM-DD"). Either may be nil.
	DueDateAfter  *string
	DueDateBefore *string
	// StoryPointsMin/StoryPointsMax filter tasks by story_points (inclusive).
	// Either may be nil. Tasks with no story_points never match when either is set.
	StoryPointsMin *int
	StoryPointsMax *int
	// ImportanceRanges filters tasks whose importance falls within ANY of these
	// inclusive ranges (OR'd together, so non-contiguous selections like
	// "Low or Critical" are representable). Empty means no filter.
	ImportanceRanges []IntRange
	// Tags filters tasks that have ANY of these tag values (exact match).
	Tags []string
	// Group, when set, narrows the listing to a single group (column) of a
	// "group by" dimension — see TaskGroup. It is ANDed with every other
	// predicate, so a view's filters and one of its columns compose.
	Group *TaskGroup
}

// Group dimensions a listing can be narrowed by (TaskGroup.By). Any other
// value is the key of a custom field.
const (
	GroupByStatus     = "status"
	GroupBySprint     = "sprint"
	GroupByAssignee   = "assignee"
	GroupByReporter   = "reporter"
	GroupByType       = "type"
	GroupByImportance = "importance"
	GroupByTags       = "tags"
)

// Sentinel group keys for "the tasks that have no value". Each dimension
// recognises only its own: GroupKeyBacklog for sprint, GroupKeyUnassigned for
// assignee and GroupKeyNone for the rest (importance has no sentinel — its
// "None" level is bucket "0"). A tag or custom field value that is literally
// one of these strings can't be told apart from the sentinel.
const (
	GroupKeyNone       = "__none"
	GroupKeyBacklog    = "__backlog"
	GroupKeyUnassigned = "__unassigned"
)

// IsGroupKeyNone reports whether key is the sentinel for "no value" in the
// group-by dimension by.
func IsGroupKeyNone(by, key string) bool {
	switch by {
	case GroupBySprint:
		return key == GroupKeyBacklog
	case GroupByAssignee:
		return key == GroupKeyUnassigned
	case GroupByImportance:
		return false
	}
	return key == GroupKeyNone
}

// TaskGroup identifies one group of a group-by dimension.
//   - status / sprint / assignee / reporter / type: Key is the entity id
//   - importance: Key is the bucket index "0".."4" (None, Low, Medium, High,
//     Critical — see ImportanceBucketRange)
//   - tags: Key is a tag value
//   - a custom field key: Key is the stored value (a select option, "true" /
//     "false", or the raw number / text / date)
//
// Key may instead be the dimension's GroupKey* sentinel (see IsGroupKeyNone) to
// select the tasks with no value for it.
type TaskGroup struct {
	By  string
	Key string
	// FieldType is the custom field's type when By names a custom field. It is
	// resolved server-side from CustomFieldDefinition, never taken from the
	// client, because it decides how the stored value is compared.
	FieldType string
}

// MaxGroupColumns is how many columns a group-by dimension lists. A repository's
// ListGroupColumns returns up to one more than this, so the transport layer can
// tell the list was cut short.
const MaxGroupColumns = 100

// GroupColumn is one group (column) of a group-by dimension, as listed for a
// view that has to show a column per value found in the data.
type GroupColumn struct {
	Key   string
	Label string
	// Count is the number of tasks in the group among those the listing's
	// filter matches.
	Count int64
}

// ImportanceBucketRange returns the inclusive importance range of a bucket
// index (0 None, 1 Low, 2 Medium, 3 High, 4 Critical). ok is false for an
// index that names no bucket. It mirrors the buckets the web app shows.
func ImportanceBucketRange(bucket int) (r IntRange, ok bool) {
	switch bucket {
	case 0:
		return IntRange{Min: 0, Max: 0}, true
	case 1:
		return IntRange{Min: 1, Max: 19}, true
	case 2:
		return IntRange{Min: 20, Max: 49}, true
	case 3:
		return IntRange{Min: 50, Max: 99}, true
	case 4:
		// Postgres' INTEGER max: the bound is bound straight into a SQL
		// integer comparison.
		return IntRange{Min: 100, Max: 2147483647}, true
	}
	return IntRange{}, false
}

// IntRange is an inclusive [Min, Max] integer range.
type IntRange struct {
	Min int
	Max int
}

// CustomFieldFilterQuery carries resolved filter criteria for one custom
// field, keyed by field key in TaskFilter.CustomFieldFilters. FieldType is
// resolved server-side from CustomFieldDefinition (never trusted from the
// client, since it determines how the value is cast in SQL) and determines
// which of the other members are meaningful:
//   - "select" / "boolean" → Values (exact-match, ANY-of)
//   - "multi_select" → Values (array-containment, ANY-of)
//   - "number" → Min / Max (inclusive range)
//   - "date" → After / Before (inclusive range, "YYYY-MM-DD")
//   - "text" / "url" / "long_text" → Contains (case-insensitive substring match)
type CustomFieldFilterQuery struct {
	FieldType string
	Values    []string
	Min       *float64
	Max       *float64
	After     *string
	Before    *string
	Contains  *string
}

// CustomFieldDefinitionRepository defines persistence operations for custom
// field definitions.
type CustomFieldDefinitionRepository interface {
	ListCustomFieldDefinitions(ctx context.Context, projectID uuid.UUID) ([]*CustomFieldDefinition, error)
	FindCustomFieldDefinitionByID(ctx context.Context, id uuid.UUID) (*CustomFieldDefinition, error)
	CreateCustomFieldDefinition(ctx context.Context, f *CustomFieldDefinition) error
	UpdateCustomFieldDefinition(ctx context.Context, f *CustomFieldDefinition) error
	DeleteCustomFieldDefinition(ctx context.Context, id uuid.UUID) error
}
