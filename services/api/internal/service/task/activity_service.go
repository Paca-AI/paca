package tasksvc

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	activitydom "github.com/Paca-AI/api/internal/domain/activity"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/events"
	mentionpkg "github.com/Paca-AI/api/internal/pkg/mention"
	activitysvc "github.com/Paca-AI/api/internal/service/activity"
)

// memberLookup resolves an actor to a project member: FindMemberByActor for
// comment authors (via activitysvc), FindMemberByAgent for @-mentioned agents.
type memberLookup interface {
	activitysvc.MemberLookup
	FindMemberByAgent(ctx context.Context, projectID, agentID uuid.UUID) (*projectdom.ProjectMember, error)
}

// taskLookup is the minimal interface ActivitySvc needs to verify that a
// task belongs to the project the caller was authorized against, before
// returning or adding to its timeline.
type taskLookup interface {
	FindTaskByID(ctx context.Context, id uuid.UUID) (*taskdom.Task, error)
}

// agentCommentTrigger creates an agent conversation when an agent is @-mentioned
// in a task comment.
type agentCommentTrigger interface {
	TriggerCommentMention(ctx context.Context, projectID, agentID, taskID, commentID, triggeredByMemberID uuid.UUID, message string) (*agentdom.AgentConversation, error)
}

// taskComments configures activitysvc's comment handling for tasks.
var taskComments = activitysvc.CommentKind{
	EntityType:           events.EntityTask,
	IDKey:                "task_id",
	TopicAdded:           events.TopicTaskCommentAdded,
	TopicUpdated:         events.TopicTaskCommentUpdated,
	TopicDeleted:         events.TopicTaskCommentDeleted,
	ErrNotFound:          taskdom.ErrActivityNotFound,
	ErrForbidden:         taskdom.ErrActivityForbidden,
	ErrNotAComment:       taskdom.ErrActivityNotAComment,
	ErrContentInvalid:    taskdom.ErrCommentContentInvalid,
	ErrActorUnidentified: taskdom.ErrCommentActorUnidentified,
}

// ActivitySvc implements taskdom.ActivityService on top of activitysvc: it
// adds the task-specific parts — scoping to the task's project, and @mention
// notifications and agent triggers on new comments.
type ActivitySvc struct {
	act             *activitysvc.Service
	taskRepo        taskLookup
	memberRepo      memberLookup
	notificationSvc notificationdom.Service
	agentTrigger    agentCommentTrigger
}

// NewActivityService returns an ActivitySvc over act. taskRepo verifies a
// task belongs to the caller's authorized project; memberRepo resolves
// @-mentioned agents.
func NewActivityService(act *activitysvc.Service, taskRepo taskLookup, memberRepo memberLookup) *ActivitySvc {
	return &ActivitySvc{act: act, taskRepo: taskRepo, memberRepo: memberRepo}
}

// taskInProject returns nil when taskID resolves to a task in projectID, and
// notFoundErr otherwise.
func (s *ActivitySvc) taskInProject(ctx context.Context, projectID, taskID uuid.UUID, notFoundErr error) error {
	t, err := s.taskRepo.FindTaskByID(ctx, taskID)
	if err != nil {
		return err
	}
	if t.ProjectID != projectID {
		return notFoundErr
	}
	return nil
}

// WithNotificationService attaches a notification service used to dispatch
// @mention notifications when comments are created.
func (s *ActivitySvc) WithNotificationService(svc notificationdom.Service) *ActivitySvc {
	s.notificationSvc = svc
	return s
}

// WithAgentTrigger attaches an agent trigger used to start a conversation when
// an agent is @-mentioned in a task comment.
func (s *ActivitySvc) WithAgentTrigger(trigger agentCommentTrigger) *ActivitySvc {
	s.agentTrigger = trigger
	return s
}

// RecordActivity records a system-generated task activity. The
// ActivityConsumer worker persists it from the activity stream, so this
// never touches the database itself.
func (s *ActivitySvc) RecordActivity(ctx context.Context, in taskdom.RecordActivityInput) error {
	now := time.Now()
	taskID := in.TaskID
	a := &activitydom.Activity{
		ID:           uuid.New(),
		ProjectID:    in.ProjectID,
		EntityType:   string(events.EntityTask),
		EntityID:     &taskID,
		ActorID:      in.ActorID,
		ActivityType: string(in.ActivityType),
		Content:      in.Content,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if len(a.Content) == 0 {
		a.Content = json.RawMessage("{}")
	}
	payload := activitysvc.EntityPayload(a, taskComments.IDKey)
	if in.ActorAgentID != nil {
		payload["actor_agent_id"] = in.ActorAgentID.String()
	}
	s.act.Record(ctx, activitysvc.Entry{
		ProjectID:    in.ProjectID,
		EntityType:   events.EntityTask,
		EntityID:     in.TaskID,
		Topic:        a.ActivityType,
		Payload:      payload,
		ActorID:      in.ActorID,
		ActorAgentID: in.ActorAgentID,
		Origin:       activitysvc.OriginFor(events.Origin(in.Origin), in.ActorID, in.ActorAgentID),
		Plugins:      true,
	})
	return nil
}

// ListActivities returns all non-deleted activities for a task, oldest first.
func (s *ActivitySvc) ListActivities(ctx context.Context, projectID, taskID uuid.UUID) ([]*taskdom.Activity, error) {
	if err := s.taskInProject(ctx, projectID, taskID, taskdom.ErrTaskNotFound); err != nil {
		return nil, err
	}
	items, err := s.act.ListForEntity(ctx, events.EntityTask, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]*taskdom.Activity, 0, len(items))
	for _, a := range items {
		out = append(out, toTaskActivity(a))
	}
	return out, nil
}

// AddComment creates a comment on the task, then notifies @-mentioned users
// and triggers @-mentioned agents.
func (s *ActivitySvc) AddComment(ctx context.Context, in taskdom.AddCommentInput) (*taskdom.Activity, error) {
	if err := s.taskInProject(ctx, in.ProjectID, in.TaskID, taskdom.ErrTaskNotFound); err != nil {
		return nil, err
	}
	a, member, err := s.act.AddComment(ctx, taskComments, activitysvc.CommentInput{
		ProjectID: in.ProjectID,
		EntityID:  in.TaskID,
		ActorID:   in.ActorID,
		AgentID:   in.AgentID,
		Content:   in.Content,
	})
	if err != nil {
		return nil, err
	}
	s.handleMentions(ctx, in, a.ID, member)
	return toTaskActivity(a), nil
}

// handleMentions notifies @-mentioned humans and starts a conversation for
// each @-mentioned agent. Best-effort: the comment is already posted.
func (s *ActivitySvc) handleMentions(ctx context.Context, in taskdom.AddCommentInput, commentID uuid.UUID, member *projectdom.ProjectMember) {
	if s.notificationSvc == nil && s.agentTrigger == nil {
		return
	}
	commentText := extractTextFromBlocks(in.Content)
	notify := func(mentionedUserID *uuid.UUID) {
		if s.notificationSvc == nil {
			return
		}
		_ = s.notificationSvc.NotifyMentioned(ctx, notificationdom.NotifyMentionedInput{
			TaskID:          in.TaskID,
			ProjectID:       in.ProjectID,
			CommentText:     commentText,
			ActorMemberID:   member.ID,
			ActorUserID:     in.ActorID,
			MentionedUserID: mentionedUserID,
		})
	}

	// Fall back to plain-text parsing when no structured mentions exist, to
	// keep manually typed @mentions and legacy clients working.
	teamMentions := mentionpkg.ExtractTeamMentionsFromBlocks(in.Content)
	if len(teamMentions) == 0 {
		notify(nil)
		return
	}
	for _, m := range teamMentions {
		mentionedID, err := uuid.Parse(m.ID)
		if err != nil {
			continue
		}
		// The web UI embeds agent_id (not user_id) as an agent mention's id,
		// so try it as an agent member first.
		if s.agentTrigger != nil && s.triggerMentionedAgent(ctx, in, commentID, member, mentionedID, commentText) {
			continue
		}
		notify(&mentionedID)
	}
}

// triggerMentionedAgent starts a conversation when mentionedID is an agent
// member, reporting whether it was one.
func (s *ActivitySvc) triggerMentionedAgent(ctx context.Context, in taskdom.AddCommentInput, commentID uuid.UUID, member *projectdom.ProjectMember, mentionedID uuid.UUID, commentText string) bool {
	agentMember, err := s.memberRepo.FindMemberByAgent(ctx, in.ProjectID, mentionedID)
	if err != nil || !agentMember.IsAgent() || agentMember.AgentID == nil {
		return false
	}
	// An agent mentioning itself (e.g. quoting its own comment) must not
	// retrigger its own conversation, or it can loop indefinitely.
	if agentMember.ID == member.ID {
		return true
	}
	agentID := *agentMember.AgentID
	conv, err := s.agentTrigger.TriggerCommentMention(ctx, in.ProjectID, agentID, in.TaskID, commentID, member.ID, commentText)
	if err != nil {
		// The comment still succeeds — this is visibility only. Without it a
		// restricted-agent denial is indistinguishable from the mention
		// doing nothing.
		slog.WarnContext(ctx, "comment mention trigger failed", "error", err, "project_id", in.ProjectID, "agent_id", agentID, "task_id", in.TaskID)
	}
	if conv != nil {
		content, _ := json.Marshal(map[string]any{
			"conversation_id": conv.ID.String(),
			"agent_id":        agentID.String(),
		})
		_ = s.RecordActivity(ctx, taskdom.RecordActivityInput{
			TaskID:       in.TaskID,
			ProjectID:    in.ProjectID,
			ActorAgentID: &agentID,
			ActivityType: taskdom.ActivityTypeAgentSessionStarted,
			Content:      content,
		})
	}
	return true
}

// UpdateComment edits the content of an existing comment.
func (s *ActivitySvc) UpdateComment(ctx context.Context, id uuid.UUID, projectID uuid.UUID, actorID uuid.UUID, agentID *uuid.UUID, content json.RawMessage) (*taskdom.Activity, error) {
	a, err := s.act.UpdateComment(ctx, taskComments, id, projectID, actorID, agentID, content)
	if err != nil {
		return nil, err
	}
	return toTaskActivity(a), nil
}

// DeleteComment soft-deletes a comment.
func (s *ActivitySvc) DeleteComment(ctx context.Context, id uuid.UUID, projectID uuid.UUID, actorID uuid.UUID, agentID *uuid.UUID) error {
	return s.act.DeleteComment(ctx, taskComments, id, projectID, actorID, agentID)
}

// toTaskActivity maps a log entry to the task timeline's shape.
func toTaskActivity(a *activitydom.Activity) *taskdom.Activity {
	return &taskdom.Activity{
		ID:                  a.ID,
		TaskID:              a.EntityIDOrNil(),
		ActorID:             a.ActorID,
		ActorName:           a.ActorName,
		ActorUsername:       a.ActorUsername,
		ActorAvatarKey:      a.ActorAvatarKey,
		ActorAvatarThumbKey: a.ActorAvatarThumbKey,
		ActivityType:        taskdom.ActivityType(a.ActivityType),
		Content:             a.Content,
		CreatedAt:           a.CreatedAt,
		UpdatedAt:           a.UpdatedAt,
		DeletedAt:           a.DeletedAt,
	}
}

// extractTextFromBlocks walks a BlockNote JSON blocks array and concatenates
// all "text" values found in inline content.  Falls back to the legacy
// {"text":"..."} object format for backward compatibility.
func extractTextFromBlocks(raw json.RawMessage) string {
	var blocks []struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &blocks) == nil && len(blocks) > 0 {
		var parts []string
		for _, b := range blocks {
			for _, c := range b.Content {
				if c.Text != "" {
					parts = append(parts, c.Text)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	var legacy struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &legacy) == nil {
		return legacy.Text
	}
	return ""
}
