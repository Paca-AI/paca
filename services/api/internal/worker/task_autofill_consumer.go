package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
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
	taskAutofillConsumerGroup = "api.task_autofill"
	taskAutofillReadBlock     = 5 * time.Second
	taskAutofillReadCount     = 50

	// defaultConfidenceThreshold gates choice/score answers (task type,
	// importance) — below this, the field is left blank rather than risk a
	// wrong guess. Matches the docs' "medium confidence, proceed cautiously"
	// starting point (see https://docs.typesafe.ai/confidence).
	defaultConfidenceThreshold = 0.6
	// defaultNoulThreshold gates boolean/multi-select-option answers,
	// slightly above the "uncertain" midpoint (0.5) so an uncertain noul
	// leaves the field/option unset rather than including it.
	defaultNoulThreshold = 0.65
	// maxMultiSelectOptions caps how many parallel noul questions a single
	// multi_select custom field can generate — a field with more options
	// than this is skipped entirely rather than issuing an unbounded batch.
	// Also caps how many of a project's existing tags are offered as
	// candidates for tag auto-fill, for the same reason.
	maxMultiSelectOptions = 20
	// maxEpicCandidates caps how many of a project's Epic-type tasks are
	// offered as parent/epic candidates — Jev's choice criteria supports up
	// to 255 options, but a project with more open epics than this almost
	// certainly wants a human picking the parent, not Jev guessing among a
	// huge list.
	maxEpicCandidates = 50
	// maxStateDescriptionChars caps the plain text extracted from a task's
	// description before it's sent to Jev as state — well under Jev's 32k
	// token state limit, generous for classification purposes.
	maxStateDescriptionChars = 8000
)

// priorityBucketLabels/priorityBucketValues mirror
// apps/web/src/components/projects/interactions/priority.ts's
// PRIORITY_LABELS/IMPORTANCE_BUCKET_VALUES — the same fixed 5-level
// importance scale used everywhere else in the app (including the existing
// built-in automation condition node's field picker), reused here as the
// Jev score criteria so a "score" answer maps onto a scale users already
// see elsewhere, rather than inventing a second one.
var priorityBucketLabels = []string{
	"None — no urgency implied",
	"Low priority",
	"Medium priority",
	"High priority",
	"Critical — needs immediate attention",
}

var priorityBucketValues = []int{0, 10, 35, 75, 150}

// storyPointBucketLabels/storyPointBucketValues mirror the Fibonacci-ish
// scale from apps/web/.../property-field/story-points-editor.tsx
// (FIBONACCI_SP), trimmed to 8 levels — Jev's score criteria supports at
// most 10 levels, and the frontend's full scale (up to 144) has more
// granularity at the high end than a single classification question can
// meaningfully resolve, so 21+ is collapsed into one "very large" bucket.
var storyPointBucketLabels = []string{
	"0 — trivial, e.g. a one-line copy or config change",
	"1 — very small, well-understood work",
	"2 — small, a few hours of routine work",
	"3 — moderate, roughly half a day to a day",
	"5 — medium, a couple of days",
	"8 — large, most of a week",
	"13 — very large, more than a week",
	"21 — extremely large, consider splitting into smaller tasks",
}

var storyPointBucketValues = []int{0, 1, 2, 3, 5, 8, 13, 21}

// projectSettingsReader is the minimal project-service surface used to read
// a project's Jev settings (autofill enabled/excluded fields). Shared with
// worker.TaskAutoAssignConsumer, which reads the same settings for its own
// (separate) auto-assign concern.
type projectSettingsReader interface {
	GetByID(ctx context.Context, id uuid.UUID) (*projectdom.Project, error)
}

// taskActivityRecorder is the minimal activity-recording surface both Jev
// consumers (this one and TaskAutoAssignConsumer) use to record their own
// writes as task.updated activity — same shape as automationActivityRecorder
// (AutomationConsumer's own copy), duplicated here to keep each consumer's
// dependency surface independently narrow, per this file's existing pattern
// (see taskAutofillTaskService / taskAutoAssignTaskService).
type taskActivityRecorder interface {
	RecordActivity(ctx context.Context, in taskdom.RecordActivityInput) error
}

// taskAutofillTaskService is the minimal task-service surface
// TaskAutofillConsumer needs: reading a task and its project's task
// types/custom field definitions, and writing back resolved fields.
type taskAutofillTaskService interface {
	GetTask(ctx context.Context, projectID, id uuid.UUID) (*taskdom.Task, error)
	UpdateTask(ctx context.Context, projectID, id uuid.UUID, in taskdom.UpdateTaskInput) (*taskdom.Task, error)
	ListTaskTypes(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskType, error)
	ListCustomFieldDefinitions(ctx context.Context, projectID uuid.UUID) ([]*taskdom.CustomFieldDefinition, error)
	// ListTasks is used to find the project's existing Epic-type tasks for
	// the parent/epic auto-fill question — see buildQuestions.
	ListTasks(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, limit int, sort taskdom.TaskSort) ([]*taskdom.Task, bool, error)
	// ListDistinctTags is used to build the tag auto-fill candidate set —
	// tags are only ever suggested from this list, never invented.
	ListDistinctTags(ctx context.Context, projectID uuid.UUID) ([]string, error)
}

// TaskAutofillConsumer reads task.created events off StreamTaskActivities
// (the same stream AutomationConsumer reads for its task_created trigger —
// a second independent consumer group on the same stream, which Valkey
// Streams supports natively) and, if Jev is configured and the project has
// auto-fill enabled, asks Jev to fill in blank task fields (importance,
// task type, and typed custom fields) that the creating user left unset.
//
// Deliberately separate from TaskAutoAssignConsumer (auto-assign) even
// though both are Jev-driven and read the same stream — field auto-fill is
// one-shot at creation, auto-assign is re-triggered by later updates too
// (see that consumer's own doc comment), and keeping them as independent
// consumers/Jev calls keeps each one's eligibility logic simple rather than
// entangling two different lifecycles in one.
//
// Idempotent via taskdom.AutofillRepository's jev_autofilled_at bookkeeping,
// so unlike NotificationConsumer this consumer is safe to replay from the
// beginning of the stream after a NOGROUP recovery — see run()'s comment.
type TaskAutofillConsumer struct {
	client       *redis.Client
	taskService  taskAutofillTaskService
	autofillRepo taskdom.AutofillRepository
	projectSvc   projectSettingsReader
	activityRec  taskActivityRecorder
	encryptor    *secret.Encryptor
	log          *slog.Logger
	consumerName string
	stopCh       chan struct{}
	doneCh       chan struct{}
}

// NewTaskAutofillConsumer creates a consumer ready to be started. encryptor
// decrypts each project's stored Jev API key (see projectdom.Project.
// JevAPIKeySecret) at the point it's used — may be nil, matching every
// other at-rest secret in this codebase when ENCRYPTION_KEY is unset (the
// stored value is then treated as plaintext). A project that hasn't
// configured its own Jev key has every event acked as a no-op after being
// marked processed, rather than left to redeliver forever. activityRec may
// be nil (activity recording is then skipped, e.g. in older call sites/
// tests) but should normally be set — this consumer calls taskService.
// UpdateTask directly rather than going through TaskHandler, which is the
// only place activity recording otherwise happens (see applyAnswers).
func NewTaskAutofillConsumer(client *redis.Client, taskService taskAutofillTaskService, autofillRepo taskdom.AutofillRepository, projectSvc projectSettingsReader, activityRec taskActivityRecorder, encryptor *secret.Encryptor, log *slog.Logger) *TaskAutofillConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = uuid.New().String()
	}
	return &TaskAutofillConsumer{
		client:       client,
		taskService:  taskService,
		autofillRepo: autofillRepo,
		projectSvc:   projectSvc,
		activityRec:  activityRec,
		encryptor:    encryptor,
		log:          log,
		consumerName: fmt.Sprintf("%s.%s", taskAutofillConsumerGroup, hostname),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start creates the consumer group if needed and begins processing in a
// background goroutine. Call Stop to drain and exit cleanly.
func (c *TaskAutofillConsumer) Start(ctx context.Context) {
	if err := c.ensureGroup(ctx, "0"); err != nil {
		c.log.Warn("task autofill consumer: could not create consumer group, will retry on first read", "err", err)
	}
	go c.run()
}

func (c *TaskAutofillConsumer) ensureGroup(ctx context.Context, startID string) error {
	err := c.client.XGroupCreateMkStream(ctx, events.StreamTaskActivities, taskAutofillConsumerGroup, startID).Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// Stop signals the consumer to stop and waits for the goroutine to exit.
func (c *TaskAutofillConsumer) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

func (c *TaskAutofillConsumer) run() {
	defer close(c.doneCh)
	c.log.Info("task autofill consumer: started", "stream", events.StreamTaskActivities)

	c.processPending(context.Background())

	for {
		select {
		case <-c.stopCh:
			c.log.Info("task autofill consumer: stopping")
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), taskAutofillReadBlock+time.Second)
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    taskAutofillConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{events.StreamTaskActivities, ">"},
			Count:    taskAutofillReadCount,
			Block:    taskAutofillReadBlock,
		}).Result()
		cancel()

		if err != nil {
			if err == redis.Nil {
				continue
			}
			c.log.Error("task autofill consumer: xreadgroup error", "err", err)
			if strings.Contains(err.Error(), "NOGROUP") {
				// "0", not "$": unlike NotificationConsumer, this consumer's
				// processing is idempotent (guarded by
				// taskdom.AutofillRepository's jev_autofilled_at), so
				// replaying the stream's retained history after losing the
				// consumer group is safe — and preferable, since it means a
				// task-creation event this consumer hadn't gotten to yet
				// doesn't just silently go unprocessed.
				recoverCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				geErr := c.ensureGroup(recoverCtx, "0")
				cancel()
				if geErr != nil {
					c.log.Warn("task autofill consumer: failed to recreate consumer group", "err", geErr)
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

func (c *TaskAutofillConsumer) processPending(ctx context.Context) {
	msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    taskAutofillConsumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{events.StreamTaskActivities, "0"},
		Count:    taskAutofillReadCount,
	}).Result()
	if err != nil && err != redis.Nil {
		c.log.Warn("task autofill consumer: could not read pending messages", "err", err)
		return
	}
	for _, stream := range msgs {
		for _, msg := range stream.Messages {
			c.handle(msg)
		}
	}
}

func (c *TaskAutofillConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, events.StreamTaskActivities, taskAutofillConsumerGroup, id).Err(); err != nil {
		c.log.Warn("task autofill consumer: xack failed", "id", id, "err", err)
	}
}

// taskActivityStreamPayload mirrors the JSON shape produced by
// activityPayload() in activity_service.go — same payload
// worker.AutomationConsumer's automationActivityStreamPayload decodes, and
// worker.TaskAutoAssignConsumer's own copy decodes too.
type taskActivityStreamPayload struct {
	TaskID       string `json:"task_id"`
	ProjectID    string `json:"project_id"`
	ActivityType string `json:"activity_type"`
}

func (c *TaskAutofillConsumer) handle(msg redis.XMessage) {
	ctx := context.Background()

	raw, ok := msg.Values["payload"].(string)
	if !ok {
		c.ack(ctx, msg.ID)
		return
	}
	var p taskActivityStreamPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		c.log.Warn("task autofill consumer: failed to decode payload", "id", msg.ID, "err", err)
		c.ack(ctx, msg.ID)
		return
	}
	if p.ActivityType != string(taskdom.ActivityTypeTaskCreated) {
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
		c.log.Error("task autofill consumer: failed to process task", "id", msg.ID, "task_id", taskID, "err", err)
		// Do not ack — retried via processPending on next restart.
		return
	}
	c.ack(ctx, msg.ID)
}

// processTask decides whether taskID is eligible for auto-fill, asks Jev
// for the fields it can, and applies confident answers — always finishing
// by marking the task processed (success, nothing-to-do, or a Jev failure
// all count as "processed": a persistent Jev outage should not leave this
// consumer retrying the same task forever). Only genuine infrastructure
// errors (DB reads/writes failing) are returned for stream-level retry.
func (c *TaskAutofillConsumer) processTask(ctx context.Context, projectID, taskID uuid.UUID) error {
	alreadyDone, err := c.autofillRepo.IsTaskAutofilled(ctx, taskID)
	if err != nil {
		return fmt.Errorf("check autofill status: %w", err)
	}
	if alreadyDone {
		return nil
	}

	project, err := c.projectSvc.GetByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	jevClient := jev.ClientForProject(project.JevAPIKeySecret, project.JevBaseURL, project.JevModel, c.encryptor)
	if !jevClient.Enabled() {
		return c.autofillRepo.MarkTaskAutofilled(ctx, taskID)
	}
	settings := projectdom.ParseJevSettings(project.Settings)
	if !settings.AutofillEnabled {
		return c.autofillRepo.MarkTaskAutofilled(ctx, taskID)
	}

	task, err := c.taskService.GetTask(ctx, projectID, taskID)
	if err != nil {
		return fmt.Errorf("load task: %w", err)
	}

	userSet, err := c.autofillRepo.ListUserSetFields(ctx, taskID)
	if err != nil {
		return fmt.Errorf("list user-set fields: %w", err)
	}

	questions := c.buildQuestions(ctx, projectID, task, userSet, settings)
	if len(questions) == 0 {
		return c.autofillRepo.MarkTaskAutofilled(ctx, taskID)
	}

	state := map[string]any{
		"title":       task.Title,
		"description": truncate(extractBlockNoteText(task.Description), maxStateDescriptionChars),
		"tags":        task.Tags,
	}

	resp, err := jevClient.SystemOne(ctx, state, questions)
	if err != nil {
		c.log.Warn("task autofill consumer: jev call failed, leaving fields blank", "task_id", taskID, "err", err)
		return c.autofillRepo.MarkTaskAutofilled(ctx, taskID)
	}

	if err := c.applyAnswers(ctx, projectID, taskID, task, resp); err != nil {
		c.log.Warn("task autofill consumer: failed to apply answers", "task_id", taskID, "err", err)
	}

	return c.autofillRepo.MarkTaskAutofilled(ctx, taskID)
}

// buildQuestions returns one Jev question per eligible, not-yet-set,
// not-excluded field — importance (score), task type (choice), story points
// (score), parent epic (choice over the project's Epic-typed tasks), tags
// (N parallel nouls over the project's already-used tag values), and typed
// custom fields (select→choice, boolean→noul, multi_select→N parallel
// nouls, one per option). Fields whose Jev primitive can't express a
// meaningful question (free text, numbers, dates — Jev only returns
// constrained answers, never generated text or arbitrary numbers) are never
// included; see the plan's "Explicit exclusions" for the full reasoning.
func (c *TaskAutofillConsumer) buildQuestions(ctx context.Context, projectID uuid.UUID, task *taskdom.Task, userSet map[string]bool, settings projectdom.JevSettings) map[string]jev.Question {
	questions := map[string]jev.Question{}

	if !userSet["importance"] && !settings.Excludes("importance") {
		questions["importance"] = jev.Question{
			Type:         jev.TypeScore,
			Instructions: "How urgent or important does this task appear, based on its title and description?",
			Criteria:     priorityBucketLabels,
		}
	}

	// Fetched once and reused both for the task-type choice question below
	// and to locate the project's Epic task type for the parent/epic
	// question further down.
	taskTypes, err := c.taskService.ListTaskTypes(ctx, projectID)
	if err != nil {
		taskTypes = nil
	}

	if !userSet["task_type_id"] && !settings.Excludes("task_type_id") && len(taskTypes) > 1 {
		criteria := make(map[string]any, len(taskTypes))
		for _, tt := range taskTypes {
			desc := tt.Name
			if tt.Description != nil && *tt.Description != "" {
				desc = tt.Name + ": " + *tt.Description
			}
			criteria[tt.ID.String()] = desc
		}
		questions["task_type_id"] = jev.Question{
			Type:         jev.TypeChoice,
			Instructions: "Which task type best fits this task's title and description?",
			Criteria:     criteria,
		}
	}

	if !userSet["story_points"] && !settings.Excludes("story_points") {
		questions["story_points"] = jev.Question{
			Type:         jev.TypeScore,
			Instructions: "How much effort or complexity does this task appear to require, based on its title and description?",
			Criteria:     storyPointBucketLabels,
		}
	}

	// Parent/epic: only offered when the task's own type isn't Epic itself
	// (an Epic can't have a parent — see task_service.go's
	// ErrEpicCannotHaveParent — so asking here would guarantee the single
	// combined UpdateTask call in applyAnswers fails, dropping every other
	// confidently-answered field along with it).
	if epicType := findEpicTaskType(taskTypes); epicType != nil &&
		(task.TaskTypeID == nil || *task.TaskTypeID != epicType.ID) &&
		!userSet["parent_task_id"] && !settings.Excludes("parent_task_id") {
		epics, _, err := c.taskService.ListTasks(ctx, projectID, taskdom.TaskFilter{TaskTypeIDs: []uuid.UUID{epicType.ID}}, maxEpicCandidates, taskdom.TaskSort{})
		if err == nil && len(epics) > 0 {
			criteria := make(map[string]any, len(epics))
			for _, epic := range epics {
				criteria[epic.ID.String()] = epic.Title
			}
			questions["parent_task_id"] = jev.Question{
				Type:         jev.TypeChoice,
				Instructions: "Which existing epic, if any, is this task most likely part of?",
				Criteria:     criteria,
			}
		}
	}

	// Tags: only ever suggest values the project has already used elsewhere
	// — never invent new tags. No-op on a project with zero existing tags.
	if !userSet["tags"] && !settings.Excludes("tags") {
		existing := make(map[string]bool, len(task.Tags))
		for _, tg := range task.Tags {
			existing[tg] = true
		}
		if allTags, err := c.taskService.ListDistinctTags(ctx, projectID); err == nil {
			added := 0
			for _, tg := range allTags {
				if existing[tg] || added >= maxMultiSelectOptions {
					continue
				}
				questions["tags:"+tg] = jev.Question{
					Type:         jev.TypeNoul,
					Instructions: fmt.Sprintf("Should the tag %q be added to this task, based on its title and description?", tg),
				}
				added++
			}
		}
	}

	fieldDefs, err := c.taskService.ListCustomFieldDefinitions(ctx, projectID)
	if err != nil {
		return questions
	}
	for _, fd := range fieldDefs {
		key := "custom:" + fd.FieldKey
		if userSet[key] || settings.Excludes(key) {
			continue
		}
		switch fd.FieldType {
		case taskdom.FieldTypeSelect:
			if len(fd.Options) == 0 {
				continue
			}
			criteria := make(map[string]any, len(fd.Options))
			for _, opt := range fd.Options {
				criteria[opt.Value] = opt.Value
			}
			questions[key] = jev.Question{
				Type:         jev.TypeChoice,
				Instructions: fmt.Sprintf("Which %q value best fits this task?", fd.DisplayName),
				Criteria:     criteria,
			}
		case taskdom.FieldTypeBoolean:
			questions[key] = jev.Question{
				Type:         jev.TypeNoul,
				Instructions: fmt.Sprintf("Does %q apply to this task?", fd.DisplayName),
			}
		case taskdom.FieldTypeMultiSelect:
			if len(fd.Options) == 0 || len(fd.Options) > maxMultiSelectOptions {
				continue
			}
			for _, opt := range fd.Options {
				questions[key+":"+opt.Value] = jev.Question{
					Type:         jev.TypeNoul,
					Instructions: fmt.Sprintf("Should %q be included in this task's %q?", opt.Value, fd.DisplayName),
				}
			}
		default:
			// text/number/date/url: no Jev question type maps cleanly onto
			// free-form input, so these are left for a human to fill in.
		}
	}
	return questions
}

// applyAnswers maps confident Jev answers onto an UpdateTaskInput and, if
// anything actually changed, calls UpdateTask. Unlike a human's PATCH, this
// goes straight through the service layer rather than TaskHandler, so it
// doesn't get that handler's activity recording for free — this method
// builds the same []taskdom.FieldChange shape TaskHandler.taskChangedFields
// would and records it itself (best-effort; a recording failure never fails
// the autofill) so autofilled changes still show up in the task's activity
// feed.
func (c *TaskAutofillConsumer) applyAnswers(ctx context.Context, projectID, taskID uuid.UUID, task *taskdom.Task, resp *jev.Response) error {
	in := taskdom.UpdateTaskInput{}
	var changes []taskdom.FieldChange
	dirty := false

	if ans, ok := resp.Answers["importance"]; ok && meetsConfidence(ans, defaultConfidenceThreshold) {
		v := priorityBucketValues[nearestBucket(ans.Score)]
		in.Importance = &v
		changes = append(changes, taskdom.FieldChange{Field: "importance", Old: task.Importance, New: v})
		dirty = true
	}

	if ans, ok := resp.Answers["task_type_id"]; ok && meetsConfidence(ans, defaultConfidenceThreshold) {
		if id, err := uuid.Parse(ans.Choice); err == nil {
			idCopy := id
			ptr := &idCopy
			in.TaskTypeID = &ptr
			oldName, newName := c.resolveTaskTypeNames(ctx, projectID, task.TaskTypeID, &idCopy)
			changes = append(changes, taskdom.FieldChange{Field: "task_type", Old: oldName, New: newName})
			dirty = true
		}
	}

	if ans, ok := resp.Answers["story_points"]; ok && meetsConfidence(ans, defaultConfidenceThreshold) {
		v := storyPointBucketValues[nearestStoryPointBucket(ans.Score)]
		ptr := &v
		in.StoryPoints = &ptr
		changes = append(changes, taskdom.FieldChange{Field: "story_points", Old: task.StoryPoints, New: v})
		dirty = true
	}

	if ans, ok := resp.Answers["parent_task_id"]; ok && meetsConfidence(ans, defaultConfidenceThreshold) {
		if id, err := uuid.Parse(ans.Choice); err == nil {
			idCopy := id
			ptr := &idCopy
			in.ParentTaskID = &ptr
			changes = append(changes, taskdom.FieldChange{Field: "parent_task", Old: uuidPtrToStr(task.ParentTaskID), New: id.String()})
			dirty = true
		}
	}

	var tagsAccum []string
	for qKey, ans := range resp.Answers {
		tag, ok := strings.CutPrefix(qKey, "tags:")
		if !ok {
			continue
		}
		if ans.Noul >= defaultNoulThreshold {
			tagsAccum = append(tagsAccum, tag)
		}
	}
	if len(tagsAccum) > 0 {
		merged := append(append([]string{}, task.Tags...), tagsAccum...)
		in.Tags = &merged
		changes = append(changes, taskdom.FieldChange{Field: "tags", Old: task.Tags, New: merged})
		dirty = true
	}

	customFields := make(map[string]any, len(task.CustomFields))
	for k, v := range task.CustomFields {
		customFields[k] = v
	}
	customDirty := false
	multiSelectAccum := map[string][]string{}
	for qKey, ans := range resp.Answers {
		fieldKey, ok := strings.CutPrefix(qKey, "custom:")
		if !ok {
			continue
		}
		if base, option, isMulti := strings.Cut(fieldKey, ":"); isMulti {
			if ans.Noul >= defaultNoulThreshold {
				multiSelectAccum[base] = append(multiSelectAccum[base], option)
			}
			continue
		}
		switch ans.Type {
		case jev.TypeChoice:
			if meetsConfidence(ans, defaultConfidenceThreshold) {
				customFields[fieldKey] = ans.Choice
				customDirty = true
			}
		case jev.TypeNoul:
			customFields[fieldKey] = ans.Noul >= defaultNoulThreshold
			customDirty = true
		}
	}
	for fieldKey, values := range multiSelectAccum {
		customFields[fieldKey] = values
		customDirty = true
	}
	if customDirty {
		in.CustomFields = &customFields
		changes = append(changes, taskdom.FieldChange{Field: "custom_fields"})
		dirty = true
	}

	if !dirty {
		return nil
	}
	if _, err := c.taskService.UpdateTask(ctx, projectID, taskID, in); err != nil {
		return err
	}
	c.recordActivity(ctx, projectID, taskID, changes)
	return nil
}

// resolveTaskTypeNames looks up display names for an old/new task type ID
// pair (either may be nil), falling back to the raw UUID string on lookup
// failure — mirrors TaskHandler.resolveTaskTypeName's own fallback so
// autofill-attributed activity entries render the same way human-edited
// ones do.
func (c *TaskAutofillConsumer) resolveTaskTypeNames(ctx context.Context, projectID uuid.UUID, oldID, newID *uuid.UUID) (old, new any) {
	types, err := c.taskService.ListTaskTypes(ctx, projectID)
	if err != nil {
		return uuidPtrToStrOrNil(oldID), uuidPtrToStrOrNil(newID)
	}
	byID := make(map[uuid.UUID]string, len(types))
	for _, t := range types {
		byID[t.ID] = t.Name
	}
	resolve := func(id *uuid.UUID) any {
		if id == nil {
			return nil
		}
		if name, ok := byID[*id]; ok {
			return name
		}
		return id.String()
	}
	return resolve(oldID), resolve(newID)
}

func uuidPtrToStrOrNil(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

// uuidPtrToStr converts a *uuid.UUID to a string (empty string for nil) —
// used when building activity FieldChange values, mirroring the equivalent
// unexported helper in task_handler.go.
func uuidPtrToStr(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// recordActivity persists changes as a task.updated activity attributed to
// no human/agent actor (ActorID/ActorAgentID both nil — the activity feed
// renders this as "System", the same fallback used for automation-applied
// entries), skipping cleanly if there's nothing to record or no recorder
// configured. Best-effort: a failure here never fails the caller's autofill.
func (c *TaskAutofillConsumer) recordActivity(ctx context.Context, projectID, taskID uuid.UUID, changes []taskdom.FieldChange) {
	if c.activityRec == nil || len(changes) == 0 {
		return
	}
	content, err := json.Marshal(map[string]any{"changes": changes})
	if err != nil {
		return
	}
	if err := c.activityRec.RecordActivity(ctx, taskdom.RecordActivityInput{
		TaskID:       taskID,
		ProjectID:    projectID,
		ActivityType: taskdom.ActivityTypeTaskUpdated,
		Content:      content,
	}); err != nil {
		c.log.Warn("task autofill consumer: failed to record activity", "task_id", taskID, "err", err)
	}
}

func meetsConfidence(ans jev.Answer, threshold float64) bool {
	return ans.Confidence != nil && *ans.Confidence >= threshold
}

// nearestBucket rounds a fractional Jev score (0..len(priorityBucketLabels)-1)
// to the nearest importance bucket index, clamped to a valid range.
func nearestBucket(score float64) int {
	return nearestBucketIndex(score, len(priorityBucketValues))
}

// nearestStoryPointBucket rounds a fractional Jev score to the nearest
// story-point bucket index — same rounding/clamping as nearestBucket,
// parameterized over storyPointBucketValues' length instead.
func nearestStoryPointBucket(score float64) int {
	return nearestBucketIndex(score, len(storyPointBucketValues))
}

// nearestBucketIndex rounds a fractional score to the nearest integer bucket
// index, clamped to [0, count-1].
func nearestBucketIndex(score float64, count int) int {
	b := int(math.Round(score))
	if b < 0 {
		return 0
	}
	if b > count-1 {
		return count - 1
	}
	return b
}

// findEpicTaskType returns the project's system "Epic" task type from types,
// or nil if the project has none — mirrors service/task's private
// isEpicTaskType (Service.isEpicTaskType), which isn't exported.
func findEpicTaskType(types []*taskdom.TaskType) *taskdom.TaskType {
	for _, tt := range types {
		if tt.IsSystem && tt.Name == "Epic" {
			return tt
		}
	}
	return nil
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// extractBlockNoteText best-effort extracts plain text from a BlockNote
// document (a JSON array of blocks — see dto.ValidateBlockNoteContent) for
// use as Jev state. Walks the decoded JSON generically for "text" string
// values anywhere in the structure rather than hardcoding BlockNote's
// block/inline-content schema, so it degrades gracefully instead of
// breaking if that schema changes.
func extractBlockNoteText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	var b strings.Builder
	collectBlockNoteText(doc, &b)
	return strings.TrimSpace(b.String())
}

func collectBlockNoteText(v any, b *strings.Builder) {
	switch t := v.(type) {
	case map[string]any:
		if s, ok := t["text"].(string); ok {
			b.WriteString(s)
			b.WriteString(" ")
		}
		for _, val := range t {
			collectBlockNoteText(val, b)
		}
	case []any:
		for _, item := range t {
			collectBlockNoteText(item, b)
		}
	}
}
