package handler

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// A new sprint's board starts with the columns that hold work not ready for
// the sprint (Backlog, and Refinement drafts) collapsed.
func TestDefaultSprintBoardCollapsesBacklogAndRefinement(t *testing.T) {
	ref, backlog, todo := uuid.New(), uuid.New(), uuid.New()
	statuses := []*taskdom.TaskStatus{
		{ID: ref, Category: taskdom.StatusCategoryRefinement},
		{ID: backlog, Category: taskdom.StatusCategoryBacklog},
		{ID: todo, Category: taskdom.StatusCategoryTodo},
	}
	views := defaultSprintViewInputs(uuid.New(), uuid.New(), nil, statuses)
	got := views[0].Config.CollapsedColumns
	for _, id := range []uuid.UUID{ref, backlog} {
		if !slices.Contains(got, id.String()) {
			t.Errorf("collapsed columns %v missing %s", got, id)
		}
	}
	if slices.Contains(got, todo.String()) {
		t.Errorf("collapsed columns %v include the Todo column", got)
	}
}
