package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/jev"
	"github.com/Paca-AI/api/internal/platform/secret"
)

const (
	taskAutoAssignConsumerGroup = "api.task_auto_assign"
	taskAutoAssignReadBlock     = 5 * time.Second
	taskAutoAssignReadCount     = 50

	// assigneeConfidenceThreshold gates the Jev choice answer — below this,
	// the task is left unassigned rather than risk assigning the wrong
	// person, which is more disruptive to undo than a task that's simply
	// still unassigned (see docs.typesafe.ai/patterns/confidence-routing's
	// own "gate stricter for higher-stakes actions" principle).
	assigneeConfidenceThreshold = 0.6
)

// taskAutoAssignTaskService is the minimal task-service surface
// TaskAutoAssignConsumer needs: reading a task and writing back a resolved
// assignee. The write goes through UpdateTaskAtomic, not UpdateTask — see
// processTask's own comment on why the final decision has to be made under
// the row lock UpdateTaskAtomic holds, not against the task read at the top
// of processTask.
type taskAutoAssignTaskService interface {
	GetTask(ctx context.Context, projectID, id uuid.UUID) (*taskdom.Task, error)
	UpdateTaskAtomic(ctx context.Context, projectID, id uuid.UUID, decide func(current *taskdom.Task) (taskdom.UpdateTaskInput, bool)) (*taskdom.Task, error)
}

// projectMemberLister is the minimal project-service surface used to list
// candidate members for auto-assign.
type projectMemberLister interface {
	ListMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error)
}

// TaskAutoAssignConsumer reads task.created and task.updated events off
// StreamTaskActivities (a third independent consumer group on the same
// stream TaskAutofillConsumer and AutomationConsumer already read — Valkey
// Streams supports this natively) and, if Jev is configured and the
// project has auto-assign enabled, asks Jev to pick an assignee for any
// task whose AssignmentMode is "auto" and AssigneeIDs is still empty.
//
// Deliberately its own consumer, separate from TaskAutofillConsumer, even
// though both are Jev-driven and read the same stream: field auto-fill is
// one-shot at task creation (gated by jev_autofilled_at), while auto-assign
// re-checks on every later task.updated event too, for as long as the task
// stays in AssignmentMode "auto" (switching a task to assignment_mode=auto
// happens via an ordinary PATCH). Entangling that different lifecycle into
// the field-autofill consumer's one-shot logic would make both harder to
// reason about — so each stays its own consumer with its own Jev call.
//
// Idempotent with no separate tracking column needed, because every path
// that reaches Jev re-checks the same guard first (AssignmentMode "auto" AND
// AssigneeIDs empty — see processTask). A confidently-resolved task leaves
// AssigneeIDs non-empty, and a low-confidence answer reverts the task to
// AssignmentMode "manual"; either way a later event finds the guard already
// failed and stops there, so a task is never reconsidered unless a human
// explicitly re-enables auto-assign.
//
// The one case that stays live is a task Jev couldn't be asked about at all
// — the call errored, the response carried no "assignee" answer key, or the
// chosen key wasn't one of the candidates. processTask leaves those sitting
// in "auto" with no assignee, so the next task.updated does retry them. That
// is the intent (a transient Jev outage self-heals), but it means the stream
// is not strictly one-shot per task, and it's why replay-from-"0" after a
// NOGROUP recovery is still safe: those retries can only ever resolve a task
// that is unassigned and explicitly in auto mode.
type TaskAutoAssignConsumer struct {
	client        *redis.Client
	taskService   taskAutoAssignTaskService
	memberLister  projectMemberLister
	projectSvc    projectSettingsReader
	activityRec   taskActivityRecorder
	encryptor     *secret.Encryptor
	jevHTTPClient *http.Client
	log           *slog.Logger
	consumerName  string
	stopCh        chan struct{}
	doneCh        chan struct{}
}

// WithHTTPClient overrides the transport the Jev client uses. Nil (the
// default) means jev.New's own SSRF-safe client — override only for tests
// that need to reach a local httptest.Server, which the default would
// otherwise reject as a private address. Mirrors
// AutomationConsumer.WithHTTPClient.
func (c *TaskAutoAssignConsumer) WithHTTPClient(client *http.Client) *TaskAutoAssignConsumer {
	c.jevHTTPClient = client
	return c
}

// NewTaskAutoAssignConsumer creates a consumer ready to be started.
// encryptor decrypts each project's stored Jev API key at the point it's
// used (see jev.ClientForProject) — may be nil, matching every other
// at-rest secret in this codebase when ENCRYPTION_KEY is unset. A project
// that hasn't configured its own Jev key has every event acked as a no-op
// immediately. activityRec may be nil (activity recording is then skipped)
// but should normally be set — this consumer calls taskService.UpdateTask
// directly rather than going through TaskHandler, which is the only place
// activity recording otherwise happens (see processTask).
func NewTaskAutoAssignConsumer(client *redis.Client, taskService taskAutoAssignTaskService, memberLister projectMemberLister, projectSvc projectSettingsReader, activityRec taskActivityRecorder, encryptor *secret.Encryptor, log *slog.Logger) *TaskAutoAssignConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = uuid.New().String()
	}
	return &TaskAutoAssignConsumer{
		client:       client,
		taskService:  taskService,
		memberLister: memberLister,
		projectSvc:   projectSvc,
		activityRec:  activityRec,
		encryptor:    encryptor,
		log:          log,
		consumerName: fmt.Sprintf("%s.%s", taskAutoAssignConsumerGroup, hostname),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start creates the consumer group if needed and begins processing in a
// background goroutine. Call Stop to drain and exit cleanly.
func (c *TaskAutoAssignConsumer) Start(ctx context.Context) {
	if err := c.ensureGroup(ctx, "0"); err != nil {
		c.log.Warn("task auto-assign consumer: could not create consumer group, will retry on first read", "err", err)
	}
	go c.run()
}

func (c *TaskAutoAssignConsumer) ensureGroup(ctx context.Context, startID string) error {
	err := c.client.XGroupCreateMkStream(ctx, events.StreamTaskActivities, taskAutoAssignConsumerGroup, startID).Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// Stop signals the consumer to stop and waits for the goroutine to exit.
func (c *TaskAutoAssignConsumer) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

func (c *TaskAutoAssignConsumer) run() {
	defer close(c.doneCh)
	c.log.Info("task auto-assign consumer: started", "stream", events.StreamTaskActivities)

	c.processPending(context.Background())

	for {
		select {
		case <-c.stopCh:
			c.log.Info("task auto-assign consumer: stopping")
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), taskAutoAssignReadBlock+time.Second)
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    taskAutoAssignConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{events.StreamTaskActivities, ">"},
			Count:    taskAutoAssignReadCount,
			Block:    taskAutoAssignReadBlock,
		}).Result()
		cancel()

		if err != nil {
			if err == redis.Nil {
				continue
			}
			c.log.Error("task auto-assign consumer: xreadgroup error", "err", err)
			if strings.Contains(err.Error(), "NOGROUP") {
				// "0": safe and preferable to replay — see this type's own
				// doc comment on why processing is idempotent.
				recoverCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				geErr := c.ensureGroup(recoverCtx, "0")
				cancel()
				if geErr != nil {
					c.log.Warn("task auto-assign consumer: failed to recreate consumer group", "err", geErr)
				}
			}
			time.Sleep(2 * time.Second)
			continue
		}

		for _, stream := range msgs {
			for _, msg := range stream.Messages {
				c.handle(msg)
			}
		}
	}
}

func (c *TaskAutoAssignConsumer) processPending(ctx context.Context) {
	msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    taskAutoAssignConsumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{events.StreamTaskActivities, "0"},
		Count:    taskAutoAssignReadCount,
	}).Result()
	if err != nil && err != redis.Nil {
		c.log.Warn("task auto-assign consumer: could not read pending messages", "err", err)
		return
	}
	for _, stream := range msgs {
		for _, msg := range stream.Messages {
			c.handle(msg)
		}
	}
}

func (c *TaskAutoAssignConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, events.StreamTaskActivities, taskAutoAssignConsumerGroup, id).Err(); err != nil {
		c.log.Warn("task auto-assign consumer: xack failed", "id", id, "err", err)
	}
}

func (c *TaskAutoAssignConsumer) handle(msg redis.XMessage) {
	ctx := context.Background()

	raw, ok := msg.Values["payload"].(string)
	if !ok {
		c.ack(ctx, msg.ID)
		return
	}
	// taskActivityStreamPayload is defined in task_autofill_consumer.go —
	// same package, same payload shape (activityPayload() in
	// activity_service.go).
	var p taskActivityStreamPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		c.log.Warn("task auto-assign consumer: failed to decode payload", "id", msg.ID, "err", err)
		c.ack(ctx, msg.ID)
		return
	}
	if p.ActivityType != string(taskdom.ActivityTypeTaskCreated) && p.ActivityType != string(taskdom.ActivityTypeTaskUpdated) {
		c.ack(ctx, msg.ID)
		return
	}

	taskID, err := uuid.Parse(p.TaskID)
	if err != nil {
		c.ack(ctx, msg.ID)
		return
	}
	projectID, err := uuid.Parse(p.ProjectID)
	if err != nil {
		c.ack(ctx, msg.ID)
		return
	}

	if err := c.processTask(ctx, projectID, taskID); err != nil {
		c.log.Error("task auto-assign consumer: failed to process task", "id", msg.ID, "task_id", taskID, "err", err)
		// Do not ack — retried via processPending on next restart.
		return
	}
	c.ack(ctx, msg.ID)
}

// processTask resolves an assignee for taskID via Jev when eligible. Not
// eligible — Jev unconfigured, AssignmentMode isn't "auto", AssigneeIDs
// already non-empty, or no candidates to choose from — is always a clean
// no-op, never an error. Low confidence reverts AssignmentMode to "manual"
// (see the type's own doc comment); a Jev call failure leaves the task as
// "auto" so it's retried on the next event instead.
func (c *TaskAutoAssignConsumer) processTask(ctx context.Context, projectID, taskID uuid.UUID) error {
	project, err := c.projectSvc.GetByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	jevClient := jev.ClientForProject(project.JevAPIKeySecret, project.JevBaseURL, project.JevModel, c.encryptor)
	if c.jevHTTPClient != nil && jevClient.Enabled() {
		jevClient = jevClient.WithHTTPClient(c.jevHTTPClient)
	}
	if !jevClient.Enabled() {
		return nil
	}
	settings := projectdom.ParseJevSettings(project.Settings)

	task, err := c.taskService.GetTask(ctx, projectID, taskID)
	if err != nil {
		return fmt.Errorf("load task: %w", err)
	}
	if task.AssignmentMode != taskdom.AssignmentModeAuto || len(task.AssigneeIDs) > 0 {
		return nil
	}

	candidates, err := c.assigneeCandidates(ctx, projectID, settings.AutoAssignScope)
	if err != nil {
		return fmt.Errorf("list assignee candidates: %w", err)
	}
	if len(candidates) == 0 {
		return nil
	}

	criteria := make(map[string]any, len(candidates))
	byID := make(map[string]*projectdom.ProjectMember, len(candidates))
	for _, m := range candidates {
		criteria[m.ID.String()] = m.ComposeJevDescription()
		byID[m.ID.String()] = m
	}

	state := map[string]any{
		"title":       task.Title,
		"description": truncate(extractBlockNoteText(task.Description), maxStateDescriptionChars),
		"tags":        task.Tags,
	}
	resp, err := jevClient.SystemOne(ctx, state, map[string]jev.Question{
		"assignee": {
			Type:         jev.TypeChoice,
			Instructions: "Which project member is best suited to be assigned this task, based on its title and description?",
			Criteria:     criteria,
		},
	})
	if err != nil {
		c.log.Warn("task auto-assign consumer: jev call failed, leaving task unassigned", "task_id", taskID, "err", err)
		return nil
	}

	ans, ok := resp.Answers["assignee"]
	if !ok {
		return nil
	}

	// From here on, the decision must be made and applied atomically under
	// UpdateTaskAtomic's row lock, not against the `task` read at the top of
	// this function. The Jev round trip above can take several seconds
	// (including retries) — long enough for a human to have manually
	// assigned the task, or otherwise changed its AssignmentMode, while it
	// was in flight. A plain GetTask-then-UpdateTask pair would leave that
	// exact window open for a stale decision to silently clobber a
	// concurrent, higher-priority human action, undoing the "once touched by
	// a human, stop auto-managing it" guarantee this type's doc comment
	// describes; deciding inside UpdateTaskAtomic's callback closes it,
	// since decide only runs once the row is already locked.
	var (
		lowConfidence bool
		confidence    any
		assigned      *projectdom.ProjectMember
	)
	if _, err := c.taskService.UpdateTaskAtomic(ctx, projectID, taskID, func(current *taskdom.Task) (taskdom.UpdateTaskInput, bool) {
		if current.AssignmentMode != taskdom.AssignmentModeAuto || len(current.AssigneeIDs) > 0 {
			return taskdom.UpdateTaskInput{}, false
		}
		if !meetsConfidence(ans, assigneeConfidenceThreshold) {
			lowConfidence = true
			if ans.Confidence != nil {
				confidence = *ans.Confidence
			}
			// Revert to "manual" rather than leaving the task sitting in
			// "auto" with no assignee — the latter renders as a
			// perpetually-pending "Auto" state in the UI, indistinguishable
			// from a task Jev simply hasn't gotten to yet. Reverting makes
			// the outcome legible: the task is unassigned, full stop. A
			// human who wants another attempt re-enables auto-assign, the
			// same PATCH that got it here.
			manualMode := taskdom.AssignmentModeManual
			return taskdom.UpdateTaskInput{AssignmentMode: &manualMode}, true
		}
		picked, ok := byID[ans.Choice]
		if !ok {
			return taskdom.UpdateTaskInput{}, false
		}
		assigned = picked
		// AssignmentMode is re-sent as "auto" alongside AssigneeIDs
		// specifically so service/task's UpdateTask doesn't treat this
		// system-driven write as a human manually picking an assignee
		// (which would otherwise flip the task back to "manual" — see
		// UpdateTask's own doc comment).
		autoMode := taskdom.AssignmentModeAuto
		assigneeIDs := []uuid.UUID{picked.ID}
		return taskdom.UpdateTaskInput{AssigneeIDs: &assigneeIDs, AssignmentMode: &autoMode}, true
	}); err != nil {
		return fmt.Errorf("apply assignee decision: %w", err)
	}

	switch {
	case lowConfidence:
		c.recordSkippedActivity(ctx, projectID, taskID, "low_confidence", map[string]any{
			"confidence": confidence,
			"threshold":  assigneeConfidenceThreshold,
		})
	case assigned != nil:
		c.recordActivity(ctx, projectID, taskID, assigned.ID)
	}
	return nil
}

// recordActivity persists the resolved assignment as a task.updated
// activity attributed to no human/agent actor (ActorID/ActorAgentID both
// nil — the activity feed renders this as "System", the same fallback used
// for automation-applied entries), so it renders through the exact same
// describeTaskChange("assignee", ...) path a human's assignment does. This
// call bypasses TaskHandler (the only place activity recording otherwise
// happens) since it writes via taskService directly, so it has to record
// this itself. Best-effort: a failure here never undoes the assignment.
func (c *TaskAutoAssignConsumer) recordActivity(ctx context.Context, projectID, taskID, assigneeID uuid.UUID) {
	if c.activityRec == nil {
		return
	}
	content, err := json.Marshal(map[string]any{
		"changes": []taskdom.FieldChange{
			{Field: "assignee", Old: []string{}, New: []string{assigneeID.String()}},
		},
	})
	if err != nil {
		return
	}
	if err := c.activityRec.RecordActivity(ctx, taskdom.RecordActivityInput{
		TaskID:       taskID,
		ProjectID:    projectID,
		ActivityType: taskdom.ActivityTypeTaskUpdated,
		Content:      content,
	}); err != nil {
		c.log.Warn("task auto-assign consumer: failed to record activity", "task_id", taskID, "err", err)
	}
}

// recordSkippedActivity records that auto-assign ran but deliberately left
// the task unassigned, so the activity feed carries a visible trace instead
// of total silence — see ActivityTypeAutoAssignSkipped's own doc comment.
// Attributed to no actor, same "System" convention as recordActivity.
// Best-effort: a failure here is not surfaced as a processing error, since
// the task itself is already correctly (un)assigned regardless.
func (c *TaskAutoAssignConsumer) recordSkippedActivity(ctx context.Context, projectID, taskID uuid.UUID, reason string, fields map[string]any) {
	if c.activityRec == nil {
		return
	}
	payload := map[string]any{"reason": reason}
	maps.Copy(payload, fields)
	content, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if err := c.activityRec.RecordActivity(ctx, taskdom.RecordActivityInput{
		TaskID:       taskID,
		ProjectID:    projectID,
		ActivityType: taskdom.ActivityTypeAutoAssignSkipped,
		Content:      content,
	}); err != nil {
		c.log.Warn("task auto-assign consumer: failed to record skipped activity", "task_id", taskID, "err", err)
	}
}

// assigneeCandidates lists the project members auto-assign may choose from,
// filtered to human-only when the project's JevSettings.AutoAssignScope is
// JevAutoAssignScopeHuman ("all", the default, includes agent members too —
// they can already be assigned a task manually, see
// notification_consumer.go's IsAgent() branch, which this reuses for free
// once a resolved agent lands in AssigneeIDs through the normal update
// path).
func (c *TaskAutoAssignConsumer) assigneeCandidates(ctx context.Context, projectID uuid.UUID, scope string) ([]*projectdom.ProjectMember, error) {
	members, err := c.memberLister.ListMembers(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if scope != projectdom.JevAutoAssignScopeHuman {
		return members, nil
	}
	humans := make([]*projectdom.ProjectMember, 0, len(members))
	for _, m := range members {
		if !m.IsAgent() {
			humans = append(humans, m)
		}
	}
	return humans, nil
}
