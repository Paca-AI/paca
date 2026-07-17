// Package sprintdom defines the sprint aggregate and its domain contracts.
package sprintdom

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SprintStatus describes the lifecycle state of a Sprint.
type SprintStatus string

// SprintStatus constants for planned, active, and completed sprints.
const (
	SprintStatusPlanned   SprintStatus = "planned"
	SprintStatusActive    SprintStatus = "active"
	SprintStatusCompleted SprintStatus = "completed"
)

// ValidSprintStatuses is the set of allowed sprint status values.
var ValidSprintStatuses = map[SprintStatus]bool{
	SprintStatusPlanned:   true,
	SprintStatusActive:    true,
	SprintStatusCompleted: true,
}

// Sprint is a time-boxed iteration containing a set of tasks.
type Sprint struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
	StartDate *time.Time
	EndDate   *time.Time
	Goal      *string
	Status    SprintStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ViewContext identifies which interaction a project-level view belongs to.
// Sprint views always have SprintID set; for project-level views (SprintID nil)
// ViewContext distinguishes between product-backlog and timeline rows.
type ViewContext string

// ViewContext constants.
const (
	ViewContextSprint   ViewContext = "sprint"
	ViewContextBacklog  ViewContext = "backlog"
	ViewContextTimeline ViewContext = "timeline"
)

// ValidViewContexts is the allowed set of view_context values.
var ValidViewContexts = map[ViewContext]bool{
	ViewContextSprint:   true,
	ViewContextBacklog:  true,
	ViewContextTimeline: true,
}

// ViewType is the layout variant of a sprint view.
type ViewType string

// View type constants.
const (
	ViewTypeTable   ViewType = "table"
	ViewTypeBoard   ViewType = "board"
	ViewTypeRoadmap ViewType = "roadmap"
	ViewTypePlugin  ViewType = "plugin"
)

// ValidViewTypes is the set of allowed view_type values.
var ValidViewTypes = map[ViewType]bool{
	ViewTypeTable:   true,
	ViewTypeBoard:   true,
	ViewTypeRoadmap: true,
	ViewTypePlugin:  true,
}

// FilterEntry is a discriminated union stored inside a FilterConfig's Items
// map.  It represents either a plain boolean (include/exclude a specific item
// by ID) or a nested FilterConfig (for named virtual groups such as "normal"
// in task-type filters, which expands to all non-system types).
//
// JSON encoding: a boolean entry marshals as JSON true/false; a nested
// FilterConfig entry marshals as a JSON object.
type FilterEntry struct {
	flag   bool
	nested *FilterConfig
}

// FilterEntryInclude returns a FilterEntry that includes the item.
func FilterEntryInclude() FilterEntry { return FilterEntry{flag: true} }

// FilterEntryExclude returns a FilterEntry that excludes the item.
func FilterEntryExclude() FilterEntry { return FilterEntry{flag: false} }

// FilterEntryNested returns a FilterEntry wrapping a nested FilterConfig.
func FilterEntryNested(c FilterConfig) FilterEntry { return FilterEntry{nested: &c, flag: false} }

// IsNested reports whether this entry holds a nested FilterConfig.
func (e FilterEntry) IsNested() bool { return e.nested != nil }

// Flag returns the boolean flag value.  Only meaningful when IsNested is false.
func (e FilterEntry) Flag() bool { return e.flag }

// Config returns the nested FilterConfig pointer.  Only meaningful when IsNested is true.
func (e FilterEntry) Config() *FilterConfig { return e.nested }

// MarshalJSON encodes a boolean entry as JSON true/false and a nested entry as
// a JSON object.
func (e FilterEntry) MarshalJSON() ([]byte, error) {
	if e.nested != nil {
		return json.Marshal(e.nested)
	}
	return json.Marshal(e.flag)
}

// UnmarshalJSON decodes JSON true/false into a boolean entry and a JSON object
// into a nested FilterConfig entry.
func (e *FilterEntry) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		e.flag = b
		e.nested = nil
		return nil
	}
	var c FilterConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("FilterEntry: expected bool or object: %w", err)
	}
	e.nested = &c
	return nil
}

// FilterConfig is a recursive, dimension-agnostic filter selector stored
// inside a view's config.  It applies uniformly to task types, statuses,
// assignees, sprints, and any future filter dimension.
//
// Semantics:
//   - All=true  → start with every item included; Items entries act as
//     exclusion (false) or sub-group overrides.
//   - All=false → start with nothing included; Items entries act as inclusions.
//
// Item keys are either entity IDs (UUID strings) or named virtual groups.
// The "normal" key in a task-type filter expands to all non-system types on
// the client side, enabling dynamic inclusion without hard-coded ID snapshots.
type FilterConfig struct {
	All   bool                   `json:"all"`
	Items map[string]FilterEntry `json:"items,omitempty"`
}

// CustomFieldFilter is a per-custom-field filter selector stored inside a
// view's ViewFilters, keyed by the target custom field's FieldKey.  Which
// members are meaningful depends on the field's type (resolved against the
// project's CustomFieldDefinition at read time, not stored here):
//   - select / multi_select / boolean → Selector (matches ANY of the
//     resolved option values, same recursive semantics as FilterConfig)
//   - number → Min / Max (inclusive range; either may be nil)
//   - date → After / Before (inclusive range, "YYYY-MM-DD"; either may be nil)
//   - text / url → Contains (case-insensitive substring match)
type CustomFieldFilter struct {
	Selector *FilterConfig `json:"selector,omitempty"`
	Min      *float64      `json:"min,omitempty"`
	Max      *float64      `json:"max,omitempty"`
	After    *string       `json:"after,omitempty"`
	Before   *string       `json:"before,omitempty"`
	Contains *string       `json:"contains,omitempty"`
}

// ViewFilters holds the saved per-view filter configuration.
// Each dimension is an optional FilterConfig selector.  A nil dimension means
// no filter is applied for that dimension (i.e. include everything).
type ViewFilters struct {
	TaskTypes    *FilterConfig                 `json:"task_types,omitempty"`
	Statuses     *FilterConfig                 `json:"statuses,omitempty"`
	Assignees    *FilterConfig                 `json:"assignees,omitempty"`
	Sprints      *FilterConfig                 `json:"sprints,omitempty"`
	CustomFields map[string]*CustomFieldFilter `json:"custom_fields,omitempty"`
}

// ViewConfig holds the display settings for a sprint view.
// All fields are optional; when empty the client applies defaults.
type ViewConfig struct {
	Fields           []string     `json:"fields,omitempty"`
	ColumnBy         string       `json:"column_by,omitempty"`
	Swimlanes        string       `json:"swimlanes,omitempty"`
	SortBy           string       `json:"sort_by,omitempty"`
	FieldSum         string       `json:"field_sum,omitempty"`
	SliceBy          string       `json:"slice_by,omitempty"`
	Filters          *ViewFilters `json:"filters,omitempty"`
	CollapsedColumns []string     `json:"collapsed_columns,omitempty"`
	// PageSize is the saved number of tasks to fetch when paginating further
	// (e.g. "load more") within this view. Zero means unset; the client falls
	// back to its own default.
	PageSize int `json:"page_size,omitempty"`
	// InitialPageSize is the saved number of tasks to fetch on the first load
	// of this view, independent of PageSize. Zero means unset; the client
	// falls back to its own per-layout default.
	InitialPageSize int `json:"initial_page_size,omitempty"`
	// Plugin view fields (only set when view_type = "plugin")
	// PluginID stores the plugin manifest identifier (reverse-DNS), not the
	// plugin UUID used by plugin-extension-settings APIs.
	PluginID        string `json:"plugin_id,omitempty"`
	PluginComponent string `json:"plugin_component,omitempty"`
}

// SprintView is a named, persisted view configuration for a sprint,
// product-backlog, or timeline interaction.
// SprintID is nil for project-level views (backlog and timeline); ProjectID is always set.
// ViewContext distinguishes which interaction a project-level view belongs to.
type SprintView struct {
	ID          uuid.UUID
	SprintID    *uuid.UUID
	ProjectID   uuid.UUID
	Name        string
	ViewType    ViewType
	Config      ViewConfig
	Position    float64
	ViewContext ViewContext
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ViewTaskPosition records the manual ordering of a task within a specific
// view.  Only used when SprintView.Config.SortBy == "manual".
type ViewTaskPosition struct {
	ID       uuid.UUID
	ViewID   uuid.UUID
	TaskID   uuid.UUID
	Position float64
	GroupKey *string
}
