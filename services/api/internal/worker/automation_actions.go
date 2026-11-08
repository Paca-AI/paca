package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/pkg/vartemplate"
)

// automationLinkCreator creates task links on link_task's behalf, through the
// normal task service. Discovered by type assertion on the task service in
// NewAutomationConsumer, like automationTaskCreator.
type automationLinkCreator interface {
	CreateTaskLink(ctx context.Context, in taskdom.CreateTaskLinkInput) (*taskdom.TaskLink, error)
}

// automationNotifier delivers notify's in-app notifications.
type automationNotifier interface {
	NotifyAutomation(ctx context.Context, in notificationdom.NotifyAutomationInput) error
}

// WithNotifier enables the notify action.
func (c *AutomationConsumer) WithNotifier(n automationNotifier) *AutomationConsumer {
	c.notifier = n
	return c
}

// applyAddComment posts an automation-authored comment (no human author) on
// task. The text is a template rendered from the walk's vars, stored as one
// paragraph per line. Origin=automation keeps this consumer from re-firing a
// comment_added automation on its own comment.
func (c *AutomationConsumer) applyAddComment(ctx context.Context, projectID uuid.UUID, task *taskdom.Task, cfg *automationdom.CommentAction, automationName string, vars map[string]string) (bool, error) {
	if cfg == nil {
		return false, fmt.Errorf("add_comment: missing comment config")
	}
	if c.activityRec == nil {
		return false, fmt.Errorf("add_comment: comments are not configured")
	}
	text := strings.TrimSpace(vartemplate.Render(cfg.Text, vars))
	if text == "" {
		return false, fmt.Errorf("add_comment: comment is empty after rendering")
	}
	if err := c.activityRec.RecordActivity(ctx, taskdom.RecordActivityInput{
		TaskID:       task.ID,
		ProjectID:    projectID,
		Origin:       taskdom.OriginAutomation,
		ActivityType: taskdom.ActivityTypeComment,
		Content:      plainTextBlocks(text),
	}); err != nil {
		return false, fmt.Errorf("add_comment: %w", err)
	}
	c.recordAppliedActivity(ctx, projectID, task.ID, automationName, map[string]any{"comment": text})
	return true, nil
}

// applyNotify sends cfg.Message to every resolved recipient of task: the
// task's assignees / reporter / parent's assignees per cfg.Recipients, plus
// cfg.MemberIDs. Recipients are de-duplicated; agents and service accounts
// have no inbox and are skipped by the notifier. It reports applied when at
// least one recipient was notified.
func (c *AutomationConsumer) applyNotify(ctx context.Context, projectID uuid.UUID, task *taskdom.Task, cfg *automationdom.NotifyAction, vars map[string]string) (bool, error) {
	if cfg == nil {
		return false, fmt.Errorf("notify: missing notify config")
	}
	if c.notifier == nil {
		return false, fmt.Errorf("notify: notifications are not configured")
	}
	message := strings.TrimSpace(vartemplate.Render(cfg.Message, vars))
	if message == "" {
		return false, fmt.Errorf("notify: message is empty after rendering")
	}
	var recipients []uuid.UUID
	add := func(ids ...uuid.UUID) {
		for _, id := range ids {
			if !slices.Contains(recipients, id) {
				recipients = append(recipients, id)
			}
		}
	}
	for _, role := range cfg.Recipients {
		switch role {
		case automationdom.RecipientAssignees:
			add(task.AssigneeIDs...)
		case automationdom.RecipientReporter:
			if task.ReporterID != nil {
				add(*task.ReporterID)
			}
		case automationdom.RecipientParentAssignees:
			if task.ParentTaskID != nil {
				parent, err := c.taskRepo.FindTaskByID(ctx, *task.ParentTaskID)
				if err != nil {
					return false, fmt.Errorf("notify: find parent: %w", err)
				}
				add(parent.AssigneeIDs...)
			}
		default:
			return false, fmt.Errorf("notify: unknown recipient %q", role)
		}
	}
	add(cfg.MemberIDs...)
	if len(recipients) == 0 {
		return false, nil
	}
	for _, id := range recipients {
		if err := c.notifier.NotifyAutomation(ctx, notificationdom.NotifyAutomationInput{
			RecipientMemberID: id,
			ProjectID:         projectID,
			TaskID:            task.ID,
			Message:           message,
		}); err != nil {
			return false, fmt.Errorf("notify: %w", err)
		}
	}
	return true, nil
}

// applyLinkTask links task (the action's resolved target) to the task cfg.To
// resolves to, relative to the walk's own task.
func (c *AutomationConsumer) applyLinkTask(ctx context.Context, projectID uuid.UUID, walkTask, task *taskdom.Task, cfg *automationdom.LinkAction, automationName string) (bool, error) {
	if cfg == nil {
		return false, fmt.Errorf("link_task: missing link config")
	}
	to := cfg.To
	if to.Kind.MultiValued() {
		return false, fmt.Errorf("link_task: the linked task must be a single task, not %q", to.Kind)
	}
	others, err := c.resolveTargetTasks(ctx, projectID, walkTask, &to)
	if err != nil {
		return false, err
	}
	if len(others) == 0 {
		return false, nil
	}
	applied := false
	for _, other := range others {
		if other.ID == task.ID {
			continue
		}
		ok, err := c.linkTasks(ctx, projectID, task.ID, other.ID, cfg.Type)
		if err != nil {
			return applied, fmt.Errorf("link_task: %w", err)
		}
		applied = applied || ok
	}
	if applied {
		c.recordAppliedActivity(ctx, projectID, task.ID, automationName, map[string]any{"link_type": cfg.Type, "linked_task_id": others[0].ID})
	}
	return applied, nil
}

// linkTasks links from → to with typ, read from from's side: blocks,
// is_blocked_by, relates_to, duplicates, is_duplicated_by. The inverse kinds
// are stored as the forward type with source and target swapped. An existing
// identical link is a no-op (false, nil).
func (c *AutomationConsumer) linkTasks(ctx context.Context, projectID, from, to uuid.UUID, typ string) (bool, error) {
	if c.linkCreator == nil {
		return false, fmt.Errorf("task links are not configured")
	}
	source, target := from, to
	var lt taskdom.LinkType
	switch typ {
	case "blocks":
		lt = taskdom.LinkTypeBlocks
	case "is_blocked_by":
		lt, source, target = taskdom.LinkTypeBlocks, to, from
	case "relates_to":
		lt = taskdom.LinkTypeRelatesTo
	case "duplicates":
		lt = taskdom.LinkTypeDuplicates
	case "is_duplicated_by":
		lt, source, target = taskdom.LinkTypeDuplicates, to, from
	default:
		return false, fmt.Errorf("unknown link type %q", typ)
	}
	_, err := c.linkCreator.CreateTaskLink(ctx, taskdom.CreateTaskLinkInput{
		ProjectID: projectID, SourceTaskID: source, TargetTaskID: target, LinkType: lt,
	})
	if errors.Is(err, taskdom.ErrTaskLinkDuplicate) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if c.activityRec != nil {
		content, _ := json.Marshal(map[string]any{"target_task_id": target, "link_type": lt})
		_ = c.activityRec.RecordActivity(ctx, taskdom.RecordActivityInput{
			TaskID: source, ProjectID: projectID, Origin: taskdom.OriginAutomation,
			ActivityType: taskdom.ActivityTypeTaskLinkAdded, Content: content,
		})
	}
	return true, nil
}
