package handler

import (
	"context"

	"github.com/google/uuid"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// taskTypeLister is the minimal task-service surface needed to seed default
// view filters from the project's built-in task types.
type taskTypeLister interface {
	ListTaskTypes(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskType, error)
}

func loadTaskTypes(ctx context.Context, taskSvc taskTypeLister, projectID uuid.UUID) ([]*taskdom.TaskType, error) {
	if taskSvc == nil {
		return nil, nil
	}
	return taskSvc.ListTaskTypes(ctx, projectID)
}

// taskStatusLister is the minimal task-service surface needed to seed default
// collapsed columns from the project's backlog statuses.
type taskStatusLister interface {
	ListTaskStatuses(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskStatus, error)
}

func loadTaskStatuses(ctx context.Context, taskSvc taskStatusLister, projectID uuid.UUID) ([]*taskdom.TaskStatus, error) {
	if taskSvc == nil {
		return nil, nil
	}
	return taskSvc.ListTaskStatuses(ctx, projectID)
}

func backlogStatusIDs(statuses []*taskdom.TaskStatus) []string {
	ids := make([]string, 0, len(statuses))
	for _, s := range statuses {
		if s != nil && s.Category == taskdom.StatusCategoryBacklog {
			ids = append(ids, s.ID.String())
		}
	}
	return ids
}

func filterTaskTypeIDs(taskTypes []*taskdom.TaskType, include func(*taskdom.TaskType) bool) []string {
	ids := make([]string, 0, len(taskTypes))
	for _, taskType := range taskTypes {
		if taskType != nil && include(taskType) {
			ids = append(ids, taskType.ID.String())
		}
	}
	return ids
}

func defaultEpicTaskTypeIDs(taskTypes []*taskdom.TaskType) []string {
	return filterTaskTypeIDs(taskTypes, func(taskType *taskdom.TaskType) bool {
		return taskType.IsSystem && taskType.Name == "Epic"
	})
}

// newNormalTypeFilter returns a FilterConfig that selects all non-system task
// types via the "normal" virtual group key.  Clients expand this group to the
// current set of non-system types at query time, so newly added types are
// automatically included without requiring stored-view updates.
func newNormalTypeFilter() *sprintdom.FilterConfig {
	return &sprintdom.FilterConfig{
		All: false,
		Items: map[string]sprintdom.FilterEntry{
			"normal": sprintdom.FilterEntryNested(sprintdom.FilterConfig{All: true}),
		},
	}
}

// newExplicitIDFilter returns a FilterConfig that selects exactly the given
// IDs.  Returns nil when ids is empty.
func newExplicitIDFilter(ids []string) *sprintdom.FilterConfig {
	if len(ids) == 0 {
		return nil
	}
	items := make(map[string]sprintdom.FilterEntry, len(ids))
	for _, id := range ids {
		items[id] = sprintdom.FilterEntryInclude()
	}
	return &sprintdom.FilterConfig{All: false, Items: items}
}

// newDefaultFilters builds a ViewFilters from the given parameters.
// includeAllNormal selects all non-system types via the "normal" group.
// taskTypeIDs adds explicit type IDs (used for the timeline Epic-only default).
// sprintIDs restricts the filter to specific sprints.
func newDefaultFilters(includeAllNormal bool, taskTypeIDs, sprintIDs []string) *sprintdom.ViewFilters {
	var taskTypes *sprintdom.FilterConfig
	if includeAllNormal {
		taskTypes = newNormalTypeFilter()
	} else {
		taskTypes = newExplicitIDFilter(taskTypeIDs)
	}

	sprints := newExplicitIDFilter(sprintIDs)

	if taskTypes == nil && sprints == nil {
		return nil
	}
	return &sprintdom.ViewFilters{
		TaskTypes: taskTypes,
		Sprints:   sprints,
	}
}

func defaultProjectViewInputs(projectID uuid.UUID, taskTypes []*taskdom.TaskType) []sprintdom.CreateViewInput {
	epicTaskTypeIDs := defaultEpicTaskTypeIDs(taskTypes)
	return []sprintdom.CreateViewInput{
		{
			ProjectID:   projectID,
			Name:        "Table",
			ViewType:    sprintdom.ViewTypeTable,
			Position:    0,
			ViewContext: sprintdom.ViewContextBacklog,
			Config: sprintdom.ViewConfig{
				ColumnBy: "sprint",
				// Use the "normal" virtual group so newly added task types are
				// automatically included without requiring a view update.
				Filters: newDefaultFilters(true, nil, nil),
			},
		},
		{
			ProjectID:   projectID,
			Name:        "Roadmap",
			ViewType:    sprintdom.ViewTypeRoadmap,
			Position:    0,
			ViewContext: sprintdom.ViewContextTimeline,
			Config: sprintdom.ViewConfig{
				// Timeline shows only Epics; Epic is a fixed system type so
				// explicit IDs are stable over the project lifetime.
				Filters: newDefaultFilters(false, epicTaskTypeIDs, nil),
			},
		},
	}
}

func defaultSprintViewInputs(projectID, sprintID uuid.UUID, _ []*taskdom.TaskType, statuses []*taskdom.TaskStatus) []sprintdom.CreateViewInput {
	sprintIDs := []string{sprintID.String()}
	boardConfig := sprintdom.ViewConfig{
		ColumnBy: "status",
		Filters:  newDefaultFilters(true, nil, sprintIDs),
	}
	if ids := backlogStatusIDs(statuses); len(ids) > 0 {
		boardConfig.CollapsedColumns = ids
	}
	return []sprintdom.CreateViewInput{
		{
			SprintID:    &sprintID,
			ProjectID:   projectID,
			Name:        "Board",
			ViewType:    sprintdom.ViewTypeBoard,
			Position:    0,
			ViewContext: sprintdom.ViewContextSprint,
			Config:      boardConfig,
		},
		{
			SprintID:    &sprintID,
			ProjectID:   projectID,
			Name:        "Table",
			ViewType:    sprintdom.ViewTypeTable,
			Position:    1,
			ViewContext: sprintdom.ViewContextSprint,
			Config: sprintdom.ViewConfig{
				ColumnBy: "status",
				Filters:  newDefaultFilters(true, nil, sprintIDs),
			},
		},
	}
}
