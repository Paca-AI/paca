package tasksvc

import (
	"context"
	"time"

	"github.com/google/uuid"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// ListTaskLinks returns all links for a task, computing display labels.
func (s *Service) ListTaskLinks(ctx context.Context, projectID, taskID uuid.UUID) ([]*taskdom.TaskLink, error) {
	task, err := s.repo.FindTaskByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.ProjectID != projectID {
		return nil, taskdom.ErrTaskNotFound
	}
	return s.repo.ListTaskLinks(ctx, taskID)
}

// CreateTaskLink validates and persists a new directed link.
func (s *Service) CreateTaskLink(ctx context.Context, in taskdom.CreateTaskLinkInput) (*taskdom.TaskLink, error) {
	if in.SourceTaskID == in.TargetTaskID {
		return nil, taskdom.ErrTaskLinkSelf
	}
	if !taskdom.ValidLinkTypes[in.LinkType] {
		return nil, taskdom.ErrTaskLinkTypeInvalid
	}

	source, err := s.repo.FindTaskByID(ctx, in.SourceTaskID)
	if err != nil {
		return nil, err
	}
	if source.ProjectID != in.ProjectID {
		return nil, taskdom.ErrTaskNotFound
	}

	target, err := s.repo.FindTaskByID(ctx, in.TargetTaskID)
	if err != nil {
		return nil, err
	}
	if target.ProjectID != in.ProjectID {
		return nil, taskdom.ErrTaskLinkCrossProject
	}

	link := &taskdom.TaskLink{
		ID:           uuid.New(),
		SourceTaskID: in.SourceTaskID,
		TargetTaskID: in.TargetTaskID,
		LinkType:     in.LinkType,
		CreatedBy:    in.CreatedBy,
		CreatedAt:    time.Now(),
	}
	created, err := s.repo.CreateTaskLinkIfNotExists(ctx, link)
	if err != nil {
		return nil, err
	}
	if !created {
		return nil, taskdom.ErrTaskLinkDuplicate
	}

	// Populate the linked task summary for the response.
	link.LinkedTask = &taskdom.LinkedTaskSummary{
		ID:         target.ID,
		TaskNumber: target.TaskNumber,
		Title:      target.Title,
		StatusID:   target.StatusID,
		TaskTypeID: target.TaskTypeID,
	}
	link.DisplayLinkType = string(in.LinkType)
	return link, nil
}

// DeleteTaskLink removes a link after verifying it belongs to a task in projectID.
func (s *Service) DeleteTaskLink(ctx context.Context, projectID, taskID, linkID uuid.UUID) error {
	link, err := s.repo.FindTaskLinkByID(ctx, linkID)
	if err != nil {
		return err
	}
	// Verify the link actually belongs to the given task and project.
	if link.SourceTaskID != taskID && link.TargetTaskID != taskID {
		return taskdom.ErrTaskLinkNotFound
	}
	// Ensure the task is in the requested project.
	task, err := s.repo.FindTaskByID(ctx, taskID)
	if err != nil {
		return err
	}
	if task.ProjectID != projectID {
		return taskdom.ErrTaskLinkNotFound
	}
	return s.repo.DeleteTaskLink(ctx, linkID)
}
