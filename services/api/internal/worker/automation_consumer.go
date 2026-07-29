package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/messaging"
	"github.com/Paca-AI/api/internal/platform/netguard"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	automationConsumerGroup = "api.automation_engine"
	automationReadBlock     = 5 * time.Second
	automationReadCount     = 50
)

// automationGraphReader is the minimal automationdom.Repository surface the
// engine needs to find matching triggers and walk a graph.
type automationGraphReader interface {
	ListEnabledTriggerNodesByType(ctx context.Context, projectID uuid.UUID, triggerType automationdom.TriggerType) ([]*automationdom.Node, error)
	ListPredecessorTriggersWatching(ctx context.Context, taskID uuid.UUID) ([]*automationdom.Node, error)
	FindAutomationByNodeID(ctx context.Context, nodeID uuid.UUID) (*automationdom.Automation, error)
	// FindNodeByID resolves a specific node — used by handleExternalTrigger,
	// which (unlike every stream-driven trigger type) already knows exactly
	// which node fired from the webhook handler's payload, with no
	// type/config matching needed.
	FindNodeByID(ctx context.Context, id uuid.UUID) (*automationdom.Node, error)
	LoadGraph(ctx context.Context, automationID uuid.UUID) (*automationdom.Graph, error)
	CreateRun(ctx context.Context, r *automationdom.Run) error
	UpdateRun(ctx context.Context, r *automationdom.Run) error
	CreateRunStep(ctx context.Context, s *automationdom.RunStep) error
	ListDueDateCandidates(ctx context.Context) ([]automationdom.DueDateCandidate, error)
	RecordDueDateFire(ctx context.Context, automationID, nodeID, taskID uuid.UUID) error
	ListCronCandidates(ctx context.Context) ([]automationdom.CronCandidate, error)
	RecordCronFire(ctx context.Context, automationID, nodeID uuid.UUID, firedAt time.Time) error
}

// automationTaskReader is the minimal task-domain surface the engine needs
// to read authoritative task/status state (the activity stream payload only
// carries resolved field *names*/diffs, not full task state), plus resolve
// a condition leaf's or action's TaskTarget relative to the walk's own
// bound task (parent, children, or linked tasks by relation type).
type automationTaskReader interface {
	FindTaskByID(ctx context.Context, id uuid.UUID) (*taskdom.Task, error)
	FindTaskStatusByID(ctx context.Context, id uuid.UUID) (*taskdom.TaskStatus, error)
	ListChildTasks(ctx context.Context, projectID, parentTaskID uuid.UUID) ([]*taskdom.Task, error)
	ListTaskLinks(ctx context.Context, taskID uuid.UUID) ([]*taskdom.TaskLink, error)
}

// automationTaskUpdater applies a mutation through the normal task service,
// so it gets the same validation and side effects as a human PATCH.
type automationTaskUpdater interface {
	UpdateTask(ctx context.Context, projectID, id uuid.UUID, in taskdom.UpdateTaskInput) (*taskdom.Task, error)
}

// automationActivityRecorder posts the automation.applied activity entry.
type automationActivityRecorder interface {
	RecordActivity(ctx context.Context, in taskdom.RecordActivityInput) error
}

// automationPluginRuntime is the minimal plugin-runtime surface the engine
// needs to dispatch plugin-contributed condition/action nodes. Both calls
// use the exact same WASM calling convention as HandleRequest — see
// platform/plugin.Runtime.EvaluateCondition/RunAction.
type automationPluginRuntime interface {
	ResolveAutomationCondition(nodeType string) (pluginName string, ok bool)
	ResolveAutomationAction(nodeType string) (pluginName string, ok bool)
	EvaluateCondition(ctx context.Context, pluginName string, payload []byte) ([]byte, error)
	RunAction(ctx context.Context, pluginName string, payload []byte) ([]byte, error)
}

// automationMemberReader resolves a project_members.id to its full record —
// used to check whether trigger_ai_agent's configured member is actually an
// agent, and to read its AgentID for automationAgentMessenger.
type automationMemberReader interface {
	FindMemberByID(ctx context.Context, memberID uuid.UUID) (*projectdom.ProjectMember, error)
}

// automationAgentMessenger fires a standalone message at an agent, no task
// involved — used by trigger_ai_agent when its trigger has no target task,
// so there's nothing to assign (see applyDirectAgentMessage).
type automationAgentMessenger interface {
	TriggerDirectMessage(ctx context.Context, projectID, agentID uuid.UUID, triggeredByMemberID *uuid.UUID, message string) (*agentdom.AgentConversation, error)
}

// AutomationConsumer reads task-activity events from StreamTaskActivities
// and evaluates the automation graph engine whenever a task event that
// could match a trigger occurs: created, status changed, assignee changed,
// priority changed, or a tag added. On a match it walks the matched
// automation's graph from the trigger node: Condition nodes branch on a
// single field comparison, Action nodes mutate the task (idempotently) and
// continue along their own edge(s). Every automation whose trigger matches
// an event runs independently.
type AutomationConsumer struct {
	client         *redis.Client
	repo           automationGraphReader
	taskRepo       automationTaskReader
	taskSvc        automationTaskUpdater
	activityRec    automationActivityRecorder
	pluginRuntime  automationPluginRuntime
	memberRepo     automationMemberReader
	agentMessenger automationAgentMessenger
	publisher      *messaging.Publisher
	httpClient     *http.Client
	log            *slog.Logger
	consumerName   string
	stopCh         chan struct{}
	doneCh         chan struct{}
}

// WithPluginRuntime attaches the plugin runtime so plugin-contributed
// condition/action nodes can be dispatched. Without it, a graph containing
// one fails that node's run step with "plugin runtime not configured"
// rather than panicking.
func (c *AutomationConsumer) WithPluginRuntime(rt automationPluginRuntime) *AutomationConsumer {
	c.pluginRuntime = rt
	return c
}

// WithHTTPClient overrides the client call_api uses to make outbound
// requests. Defaults to an SSRF-safe client (netguard.NewSafeHTTPClient) —
// override only for tests that need to reach a local httptest.Server
// (which the default client would otherwise reject as a private address).
func (c *AutomationConsumer) WithHTTPClient(client *http.Client) *AutomationConsumer {
	c.httpClient = client
	return c
}

// WithAgentMessaging attaches the dependencies trigger_ai_agent needs to
// fire a direct message when its trigger has no target task. Without it, a
// task-less trigger_ai_agent node fails its run step with "direct-message
// dispatch not configured" rather than panicking.
func (c *AutomationConsumer) WithAgentMessaging(memberRepo automationMemberReader, agentMessenger automationAgentMessenger) *AutomationConsumer {
	c.memberRepo = memberRepo
	c.agentMessenger = agentMessenger
	return c
}

// NewAutomationConsumer creates a consumer that is ready to be started.
func NewAutomationConsumer(
	client *redis.Client,
	repo automationGraphReader,
	taskRepo automationTaskReader,
	taskSvc automationTaskUpdater,
	activityRec automationActivityRecorder,
	publisher *messaging.Publisher,
	log *slog.Logger,
) *AutomationConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = uuid.New().String()
	}
	return &AutomationConsumer{
		client:       client,
		repo:         repo,
		taskRepo:     taskRepo,
		taskSvc:      taskSvc,
		activityRec:  activityRec,
		publisher:    publisher,
		httpClient:   netguard.NewSafeHTTPClient(30 * time.Second),
		log:          log,
		consumerName: fmt.Sprintf("%s.%s", automationConsumerGroup, hostname),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start creates the consumer group if needed and begins processing in a
// background goroutine. Call Stop to drain and exit cleanly.
func (c *AutomationConsumer) Start(ctx context.Context) {
	if err := c.ensureGroup(ctx); err != nil {
		c.log.Warn("automation consumer: could not create consumer group, will retry on first read", "err", err)
	}
	go c.run()
}

func (c *AutomationConsumer) ensureGroup(ctx context.Context) error {
	for _, stream := range []string{events.StreamTaskActivities, events.StreamAutomationExternalTriggers} {
		err := c.client.XGroupCreateMkStream(ctx, stream, automationConsumerGroup, "0").Err()
		if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
			return err
		}
	}
	return nil
}

// Stop signals the consumer to stop and waits for the goroutine to exit.
func (c *AutomationConsumer) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

func (c *AutomationConsumer) run() {
	defer close(c.doneCh)
	c.log.Info("automation consumer: started", "stream", events.StreamTaskActivities)

	c.processPending(context.Background())

	for {
		select {
		case <-c.stopCh:
			c.log.Info("automation consumer: stopping")
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), automationReadBlock+time.Second)
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    automationConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{events.StreamTaskActivities, events.StreamAutomationExternalTriggers, ">", ">"},
			Count:    automationReadCount,
			Block:    automationReadBlock,
		}).Result()
		cancel()

		if err != nil {
			if err == redis.Nil {
				continue
			}
			c.log.Error("automation consumer: xreadgroup error", "err", err)
			if strings.Contains(err.Error(), "NOGROUP") {
				if geErr := c.ensureGroup(context.Background()); geErr != nil {
					c.log.Warn("automation consumer: failed to recreate consumer group", "err", geErr)
				}
			}
			time.Sleep(2 * time.Second)
			continue
		}

		c.dispatchMessages(msgs)
	}
}

// dispatchMessages routes each message to the right handler based on which
// stream it came from — StreamAutomationExternalTriggers messages already
// know exactly which node/task to run (resolved by the webhook handler
// before publishing), everything else goes through the task-activity
// trigger-matching path.
func (c *AutomationConsumer) dispatchMessages(msgs []redis.XStream) {
	for _, stream := range msgs {
		for _, msg := range stream.Messages {
			if stream.Stream == events.StreamAutomationExternalTriggers {
				c.handleExternalTrigger(msg)
			} else {
				c.handle(msg)
			}
		}
	}
}

func (c *AutomationConsumer) processPending(ctx context.Context) {
	msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    automationConsumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{events.StreamTaskActivities, events.StreamAutomationExternalTriggers, "0", "0"},
		Count:    automationReadCount,
	}).Result()
	if err != nil && err != redis.Nil {
		c.log.Warn("automation consumer: could not read pending messages", "err", err)
		return
	}
	c.dispatchMessages(msgs)
}

// automationActivityStreamPayload mirrors the JSON shape produced by activityPayload()
// in activity_service.go.
type automationActivityStreamPayload struct {
	TaskID       string `json:"task_id"`
	ProjectID    string `json:"project_id"`
	ActivityType string `json:"activity_type"`
	Content      string `json:"content"`
}

type taskUpdatedContent struct {
	Changes []taskdom.FieldChange `json:"changes"`
}

func (c *AutomationConsumer) ack(ctx context.Context, stream, id string) {
	if err := c.client.XAck(ctx, stream, automationConsumerGroup, id).Err(); err != nil {
		c.log.Warn("automation consumer: xack failed", "stream", stream, "id", id, "err", err)
	}
}

func (c *AutomationConsumer) handle(msg redis.XMessage) {
	ctx := context.Background()

	raw, ok := msg.Values["payload"].(string)
	if !ok {
		c.ack(ctx, events.StreamTaskActivities, msg.ID)
		return
	}
	var p automationActivityStreamPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		c.log.Warn("automation consumer: failed to decode payload", "id", msg.ID, "err", err)
		c.ack(ctx, events.StreamTaskActivities, msg.ID)
		return
	}

	taskID, err := uuid.Parse(p.TaskID)
	if err != nil {
		c.ack(ctx, events.StreamTaskActivities, msg.ID)
		return
	}
	projectID, err := uuid.Parse(p.ProjectID)
	if err != nil {
		c.ack(ctx, events.StreamTaskActivities, msg.ID)
		return
	}

	var candidates []automationdom.TriggerType
	checkPredecessors := false

	switch p.ActivityType {
	case string(taskdom.ActivityTypeTaskCreated):
		candidates = append(candidates, automationdom.TriggerTaskCreated)
	case string(taskdom.ActivityTypeTaskUpdated):
		var content taskUpdatedContent
		if err := json.Unmarshal([]byte(p.Content), &content); err == nil {
			for _, change := range content.Changes {
				switch change.Field {
				case "status":
					candidates = append(candidates, automationdom.TriggerStatusChanged)
					checkPredecessors = true
				case "assignee":
					candidates = append(candidates, automationdom.TriggerAssigneeChanged)
				case "importance":
					candidates = append(candidates, automationdom.TriggerPriorityChanged)
				case "tags":
					candidates = append(candidates, automationdom.TriggerTagAdded)
				}
			}
		}
	}

	if len(candidates) == 0 && !checkPredecessors {
		c.ack(ctx, events.StreamTaskActivities, msg.ID)
		return
	}

	if err := c.processEvent(ctx, projectID, taskID, candidates, checkPredecessors); err != nil {
		c.log.Error("automation consumer: failed to process event", "id", msg.ID, "task_id", taskID, "err", err)
		// Do not ack — retried via processPending on next restart.
		return
	}
	c.ack(ctx, events.StreamTaskActivities, msg.ID)
}

// automationExternalTriggerPayload mirrors the payload
// automationsvc.Service.HandleWebhookTrigger appends to
// StreamAutomationExternalTriggers — unlike every other trigger source,
// this one already knows exactly which node and task to run (resolved by
// the webhook handler before publishing), so there's no type/config
// matching step here at all.
type automationExternalTriggerPayload struct {
	NodeID       string `json:"node_id"`
	AutomationID string `json:"automation_id"`
	ProjectID    string `json:"project_id"`
	TaskID       string `json:"task_id"`
}

func (c *AutomationConsumer) handleExternalTrigger(msg redis.XMessage) {
	ctx := context.Background()

	raw, ok := msg.Values["payload"].(string)
	if !ok {
		c.ack(ctx, events.StreamAutomationExternalTriggers, msg.ID)
		return
	}
	var p automationExternalTriggerPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		c.log.Warn("automation consumer: failed to decode external trigger payload", "id", msg.ID, "err", err)
		c.ack(ctx, events.StreamAutomationExternalTriggers, msg.ID)
		return
	}

	nodeID, err := uuid.Parse(p.NodeID)
	if err != nil {
		c.ack(ctx, events.StreamAutomationExternalTriggers, msg.ID)
		return
	}
	projectID, err := uuid.Parse(p.ProjectID)
	if err != nil {
		c.ack(ctx, events.StreamAutomationExternalTriggers, msg.ID)
		return
	}

	node, err := c.repo.FindNodeByID(ctx, nodeID)
	if err != nil {
		c.log.Error("automation consumer: external trigger find node", "id", msg.ID, "node_id", nodeID, "err", err)
		return // not acked — retried via processPending
	}
	// task_id is optional — absent means this node's TargetTaskID is unset,
	// so it may only reach call_api actions downstream
	// (validateTaskReachability enforces that at edge-creation time).
	var task *taskdom.Task
	if p.TaskID != "" {
		taskID, err := uuid.Parse(p.TaskID)
		if err != nil {
			c.ack(ctx, events.StreamAutomationExternalTriggers, msg.ID)
			return
		}
		task, err = c.taskRepo.FindTaskByID(ctx, taskID)
		if err != nil {
			c.log.Error("automation consumer: external trigger find task", "id", msg.ID, "task_id", taskID, "err", err)
			return
		}
	}
	if err := c.executeRun(ctx, projectID, node, task); err != nil {
		c.log.Error("automation consumer: external trigger run failed", "id", msg.ID, "node_id", nodeID, "err", err)
		return
	}
	c.ack(ctx, events.StreamAutomationExternalTriggers, msg.ID)
}

// processEvent fetches the authoritative task once, then matches it against
// every candidate trigger type plus (if checkPredecessors) any
// predecessor_done trigger watching it. Errors applying an individual
// automation are logged and skipped (best-effort) rather than aborting the
// whole batch. An error is returned only when the foundational task lookup
// fails, so the message is retried.
func (c *AutomationConsumer) processEvent(ctx context.Context, projectID, taskID uuid.UUID, candidates []automationdom.TriggerType, checkPredecessors bool) error {
	task, err := c.taskRepo.FindTaskByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}

	for _, t := range candidates {
		nodes, err := c.repo.ListEnabledTriggerNodesByType(ctx, projectID, t)
		if err != nil {
			c.log.Error("automation consumer: list trigger nodes", "type", t, "err", err)
			continue
		}
		for _, node := range nodes {
			if !c.triggerMatches(node, task) {
				continue
			}
			if err := c.executeRun(ctx, projectID, node, task); err != nil {
				c.log.Error("automation consumer: run failed", "node_id", node.ID, "err", err)
			}
		}
	}

	if checkPredecessors {
		nodes, err := c.repo.ListPredecessorTriggersWatching(ctx, taskID)
		if err != nil {
			c.log.Error("automation consumer: list predecessor triggers", "err", err)
			return nil
		}
		for _, node := range nodes {
			var cfg automationdom.TriggerConfig
			if err := json.Unmarshal(node.Config, &cfg); err != nil {
				continue
			}
			allDone, err := c.allWatchedTasksDone(ctx, cfg.WatchedTaskIDs)
			if err != nil {
				c.log.Error("automation consumer: check predecessors done", "node_id", node.ID, "err", err)
				continue
			}
			if !allDone {
				continue
			}
			// TargetTaskID is optional — nil means this node may only reach
			// call_api actions downstream (validateTaskReachability
			// enforces that at edge-creation/node-update/activate time).
			var targetTask *taskdom.Task
			if cfg.TargetTaskID != nil {
				targetTask, err = c.taskRepo.FindTaskByID(ctx, *cfg.TargetTaskID)
				if err != nil {
					c.log.Error("automation consumer: find predecessor_done target task", "node_id", node.ID, "err", err)
					continue
				}
			}
			if err := c.executeRun(ctx, projectID, node, targetTask); err != nil {
				c.log.Error("automation consumer: predecessor-done run failed", "node_id", node.ID, "err", err)
			}
		}
	}
	return nil
}

// triggerMatches checks a trigger node's config filter against the task's
// current authoritative state (not the raw activity payload — the payload
// only carries resolved names/diffs for some fields).
func (c *AutomationConsumer) triggerMatches(node *automationdom.Node, task *taskdom.Task) bool {
	var cfg automationdom.TriggerConfig
	if len(node.Config) > 0 {
		_ = json.Unmarshal(node.Config, &cfg)
	}
	switch automationdom.TriggerType(node.Type) {
	case automationdom.TriggerStatusChanged:
		if cfg.StatusID == nil {
			return true
		}
		return task.StatusID != nil && *task.StatusID == *cfg.StatusID
	case automationdom.TriggerTagAdded:
		if cfg.Tag == "" {
			return true
		}
		return slices.Contains(task.Tags, cfg.Tag)
	case automationdom.TriggerTaskCreated, automationdom.TriggerAssigneeChanged, automationdom.TriggerPriorityChanged:
		return true
	default:
		return false
	}
}

// allWatchedTasksDone re-derives, fresh, whether every watched task is
// currently in a status with category "done" — the exact stateless
// AND-join recheck the old workflow canvas used, not new merge-progress
// tracking.
func (c *AutomationConsumer) allWatchedTasksDone(ctx context.Context, watchedTaskIDs []uuid.UUID) (bool, error) {
	for _, id := range watchedTaskIDs {
		t, err := c.taskRepo.FindTaskByID(ctx, id)
		if err != nil {
			return false, fmt.Errorf("find watched task: %w", err)
		}
		if t.StatusID == nil {
			return false, nil
		}
		status, err := c.taskRepo.FindTaskStatusByID(ctx, *t.StatusID)
		if err != nil {
			return false, fmt.Errorf("find watched task status: %w", err)
		}
		if status.Category != taskdom.StatusCategoryDone {
			return false, nil
		}
	}
	return true, nil
}

// --- Graph walk --------------------------------------------------------------

// executeRun creates an automation_runs row and walks the graph from
// triggerNode, mutating task in place as actions apply. Not an error if the
// automation was archived/reverted-to-draft since the trigger was matched —
// that's just a run that does nothing.
func (c *AutomationConsumer) executeRun(ctx context.Context, projectID uuid.UUID, triggerNode *automationdom.Node, task *taskdom.Task) error {
	automation, err := c.repo.FindAutomationByNodeID(ctx, triggerNode.ID)
	if err != nil {
		return fmt.Errorf("find automation: %w", err)
	}
	if automation.Status != automationdom.StatusActive {
		return nil
	}

	graph, err := c.repo.LoadGraph(ctx, automation.ID)
	if err != nil {
		return fmt.Errorf("load graph: %w", err)
	}
	nodesByID := make(map[uuid.UUID]*automationdom.Node, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nodesByID[n.ID] = n
	}
	outgoing := make(map[uuid.UUID][]*automationdom.Edge, len(graph.Edges))
	for _, e := range graph.Edges {
		outgoing[e.SourceNodeID] = append(outgoing[e.SourceNodeID], e)
	}

	var taskID *uuid.UUID
	if task != nil {
		taskID = &task.ID
	}
	run := &automationdom.Run{
		ID:            uuid.New(),
		AutomationID:  automation.ID,
		TriggerNodeID: triggerNode.ID,
		TaskID:        taskID,
		Status:        automationdom.RunStatusRunning,
		StartedAt:     time.Now(),
	}
	if err := c.repo.CreateRun(ctx, run); err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	w := &walker{
		consumer:       c,
		projectID:      projectID,
		automationName: automation.Name,
		task:           task,
		nodesByID:      nodesByID,
		outgoing:       outgoing,
		runID:          run.ID,
		visited:        map[uuid.UUID]bool{},
	}
	for _, e := range outgoing[triggerNode.ID] {
		w.walk(ctx, e.TargetNodeID)
	}

	now := time.Now()
	run.FinishedAt = &now
	if w.failed {
		run.Status = automationdom.RunStatusFailed
	} else {
		run.Status = automationdom.RunStatusCompleted
	}
	if err := c.repo.UpdateRun(ctx, run); err != nil {
		c.log.Error("automation consumer: update run", "run_id", run.ID, "err", err)
	}
	return nil
}

// walker holds the mutable state of a single graph walk.
type walker struct {
	consumer       *AutomationConsumer
	projectID      uuid.UUID
	automationName string
	task           *taskdom.Task
	nodesByID      map[uuid.UUID]*automationdom.Node
	outgoing       map[uuid.UUID][]*automationdom.Edge
	runID          uuid.UUID
	visited        map[uuid.UUID]bool
	failed         bool
}

// walk visits nodeID and recurses into its outgoing edges. visited guards
// against a runaway loop defensively — cycles are already rejected at
// edge-creation time, but a walk must never hang if that invariant is ever
// violated by a data issue.
func (w *walker) walk(ctx context.Context, nodeID uuid.UUID) {
	if w.visited[nodeID] {
		return
	}
	w.visited[nodeID] = true

	node, ok := w.nodesByID[nodeID]
	if !ok {
		return
	}

	if w.task == nil && automationdom.NodeRequiresTask(node.Kind, node.Type) {
		// Defense in depth: validateTaskReachability (checked at
		// edge-creation/node-update/activate time) should make this
		// unreachable — a task-less trigger can only ever reach call_api
		// actions. Fail the step instead of a nil-pointer panic if that
		// invariant is ever violated.
		w.failed = true
		w.recordStep(ctx, node.ID, automationdom.RunStepFailed, nil, nil, "automation: this node requires a task, but the trigger has no target task configured")
		return
	}

	switch node.Kind {
	case automationdom.KindCondition:
		w.walkCondition(ctx, node)
	case automationdom.KindAction:
		w.walkAction(ctx, node)
	default:
		// A trigger node mid-walk shouldn't happen (edges into triggers are
		// rejected at creation time) — defensively skip.
	}
}

// pluginConditionTrueHandle / pluginConditionFalseHandle are the two
// branch handles a plugin-contributed condition node's edges use — a
// plugin condition is a simple boolean gate, not an N-branch switch, so it
// doesn't need the built-in condition node's Branches config shape.
const (
	pluginConditionTrueHandle = "true"
)

func (w *walker) walkCondition(ctx context.Context, node *automationdom.Node) {
	if node.Type != automationdom.ConditionNodeType {
		w.walkPluginCondition(ctx, node)
		return
	}

	var cfg automationdom.ConditionConfig
	_ = json.Unmarshal(node.Config, &cfg)

	matchedHandle := automationdom.ElseHandle
	for _, b := range cfg.Branches {
		matched, err := w.consumer.evaluateLeaf(ctx, w.projectID, w.task, b.Tree)
		if err != nil {
			w.failed = true
			w.recordStep(ctx, node.ID, automationdom.RunStepFailed, nil, nil, err.Error())
			return
		}
		if matched {
			matchedHandle = b.Handle
			break
		}
	}

	output, _ := json.Marshal(map[string]any{"matched_handle": matchedHandle})
	w.recordStep(ctx, node.ID, automationdom.RunStepCompleted, nil, output, "")

	for _, e := range w.outgoing[node.ID] {
		if e.SourceHandle != nil && *e.SourceHandle == matchedHandle {
			w.walk(ctx, e.TargetNodeID)
		}
	}
}

// evaluateLeaf evaluates leaf against baseTask (the walk's own bound task),
// retargeting first if leaf.Target names something other than "self" — see
// resolveTargetTasks and ConditionLeaf.EvaluateAgainstTasks.
func (c *AutomationConsumer) evaluateLeaf(ctx context.Context, projectID uuid.UUID, baseTask *taskdom.Task, leaf *automationdom.ConditionLeaf) (bool, error) {
	if leaf == nil {
		return true, nil
	}
	if leaf.Target == nil || leaf.Target.Kind == "" || leaf.Target.Kind == automationdom.TaskTargetSelf {
		return leaf.Evaluate(baseTask), nil
	}
	if baseTask == nil {
		// Defense in depth: a condition always requires a task
		// (NodeRequiresTask), so validateTaskReachability should make this
		// unreachable regardless of Target.
		return false, fmt.Errorf("condition: target %q requires a task to resolve relative to, but the walk has none", leaf.Target.Kind)
	}
	tasks, err := c.resolveTargetTasks(ctx, projectID, baseTask, leaf.Target)
	if err != nil {
		return false, err
	}
	return leaf.EvaluateAgainstTasks(tasks, leaf.MatchMode), nil
}

// resolveTargetTasks returns the task(s) target actually resolves to,
// relative to baseTask (the walk's own bound task): itself for nil/self,
// its parent, its children, tasks linked to it by relation type, or an
// explicit other task by ID. A relation that resolves to nothing (no
// parent, no children, no links of that type) returns an empty, non-error
// slice — callers treat that as "nothing to check/act on", not a failure.
func (c *AutomationConsumer) resolveTargetTasks(ctx context.Context, projectID uuid.UUID, baseTask *taskdom.Task, target *automationdom.TaskTarget) ([]*taskdom.Task, error) {
	if target == nil || target.Kind == "" || target.Kind == automationdom.TaskTargetSelf {
		return []*taskdom.Task{baseTask}, nil
	}
	switch target.Kind {
	case automationdom.TaskTargetOther:
		if target.OtherTaskID == nil {
			return nil, fmt.Errorf("target: other requires other_task_id")
		}
		t, err := c.taskRepo.FindTaskByID(ctx, *target.OtherTaskID)
		if err != nil {
			return nil, fmt.Errorf("target: find other task: %w", err)
		}
		return []*taskdom.Task{t}, nil
	case automationdom.TaskTargetParent:
		if baseTask.ParentTaskID == nil {
			return nil, nil
		}
		t, err := c.taskRepo.FindTaskByID(ctx, *baseTask.ParentTaskID)
		if err != nil {
			return nil, fmt.Errorf("target: find parent task: %w", err)
		}
		return []*taskdom.Task{t}, nil
	case automationdom.TaskTargetChildren:
		children, err := c.taskRepo.ListChildTasks(ctx, projectID, baseTask.ID)
		if err != nil {
			return nil, fmt.Errorf("target: list children: %w", err)
		}
		return children, nil
	case automationdom.TaskTargetBlocks, automationdom.TaskTargetIsBlockedBy, automationdom.TaskTargetRelatesTo, automationdom.TaskTargetDuplicates, automationdom.TaskTargetIsDuplicatedBy:
		links, err := c.taskRepo.ListTaskLinks(ctx, baseTask.ID)
		if err != nil {
			return nil, fmt.Errorf("target: list task links: %w", err)
		}
		var out []*taskdom.Task
		for _, link := range links {
			if link.DisplayLinkType != string(target.Kind) {
				continue
			}
			otherID := link.TargetTaskID
			if link.SourceTaskID != baseTask.ID {
				otherID = link.SourceTaskID
			}
			t, err := c.taskRepo.FindTaskByID(ctx, otherID)
			if err != nil {
				continue // best-effort: skip a since-deleted linked task
			}
			out = append(out, t)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("target: unknown kind %q", target.Kind)
	}
}

// walkPluginCondition dispatches a plugin-contributed condition node via
// the plugin runtime's EvaluateCondition bridge, then follows the "true"
// edge if matched or the "else" edge otherwise — the same edge-following
// logic as the built-in condition node, just with exactly two possible
// outcomes instead of N branches.
func (w *walker) walkPluginCondition(ctx context.Context, node *automationdom.Node) {
	input, _ := json.Marshal(pluginNodePayload(node.Config, w.task, ""))
	matched, err := w.consumer.evaluatePluginCondition(ctx, node, input)
	if err != nil {
		w.failed = true
		w.recordStep(ctx, node.ID, automationdom.RunStepFailed, input, nil, err.Error())
		return
	}
	matchedHandle := automationdom.ElseHandle
	if matched {
		matchedHandle = pluginConditionTrueHandle
	}
	output, _ := json.Marshal(map[string]any{"matched_handle": matchedHandle})
	w.recordStep(ctx, node.ID, automationdom.RunStepCompleted, input, output, "")

	for _, e := range w.outgoing[node.ID] {
		if e.SourceHandle != nil && *e.SourceHandle == matchedHandle {
			w.walk(ctx, e.TargetNodeID)
		}
	}
}

func (w *walker) walkAction(ctx context.Context, node *automationdom.Node) {
	inputData := map[string]any{}
	if w.task != nil {
		inputData["status_id"] = w.task.StatusID
		inputData["assignee_ids"] = w.task.AssigneeIDs
	}
	input, _ := json.Marshal(inputData)
	applied, detail, err := w.consumer.runAction(ctx, w.projectID, node, w.task, w.automationName, w.runID)
	if err != nil {
		w.failed = true
		w.recordStep(ctx, node.ID, automationdom.RunStepFailed, input, nil, err.Error())
		return
	}
	status := automationdom.RunStepCompleted
	if !applied {
		status = automationdom.RunStepSkipped
	}
	outputMap := map[string]any{"applied": applied}
	if len(detail) > 0 {
		var extra map[string]any
		if json.Unmarshal(detail, &extra) == nil {
			for k, v := range extra {
				outputMap[k] = v
			}
		}
	}
	output, _ := json.Marshal(outputMap)
	w.recordStep(ctx, node.ID, status, input, output, "")

	for _, e := range w.outgoing[node.ID] {
		w.walk(ctx, e.TargetNodeID)
	}
}

func (w *walker) recordStep(ctx context.Context, nodeID uuid.UUID, status automationdom.RunStepStatus, input, output json.RawMessage, errMsg string) {
	step := &automationdom.RunStep{
		ID:             uuid.New(),
		RunID:          w.runID,
		NodeID:         nodeID,
		Status:         status,
		InputSnapshot:  input,
		OutputSnapshot: output,
		Error:          errMsg,
		ExecutedAt:     time.Now(),
	}
	if err := w.consumer.repo.CreateRunStep(ctx, step); err != nil {
		w.consumer.log.Warn("automation consumer: record run step", "node_id", nodeID, "err", err)
	}
}

// --- Action handlers -----------------------------------------------------------

// runAction dispatches to the concrete handler for node's action type,
// mutating task in place on success so later nodes in the same walk see the
// up-to-date state. Every handler checks "already in target state" before
// mutating, which is what keeps a graph walk safe to retry after a crash.
// detail is extra output-snapshot data a handler wants recorded alongside
// "applied" (call_api's status_code/response_body, or a retargeted
// action's per-task results — see below); every other case leaves it nil,
// unchanged from before.
//
// cfg.Target (nil/self = every action's behavior before Target existed)
// retargets the action onto a task other than task: when it resolves to
// more than one task (children, or a link type with several matches), the
// action fans out — applied once per resolved task via applyActionForTask,
// aggregated into detail's "targets" array. Fanning out stops at the first
// error, same fail-fast behavior as a single-task action's error today.
func (c *AutomationConsumer) runAction(ctx context.Context, projectID uuid.UUID, node *automationdom.Node, task *taskdom.Task, automationName string, runID uuid.UUID) (applied bool, detail json.RawMessage, err error) {
	var cfg automationdom.ActionConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return false, nil, fmt.Errorf("decode action config: %w", err)
	}
	actionType := automationdom.ActionType(node.Type)

	// call_api doesn't touch a task at all — Target is rejected at
	// validation time for it, so there's nothing to retarget here either.
	if actionType == automationdom.ActionCallAPI {
		if cfg.Method == "" || cfg.URL == "" {
			// Defensive: a live automation's node can be edited (non-strict
			// validation) without re-running Activate's strict checks, so a
			// previously-valid call_api node could be edited down to an
			// empty method/url while the automation stays active.
			return false, nil, fmt.Errorf("call_api: missing method or url")
		}
		return c.applyCallAPI(ctx, cfg)
	}

	// trigger_ai_agent's task-less direct-message path doesn't involve any
	// task at all either — retargeting doesn't apply there.
	if actionType == automationdom.ActionTriggerAIAgent && task == nil {
		if cfg.MemberID == nil {
			return false, nil, fmt.Errorf("trigger_ai_agent: missing member_id")
		}
		applied, err := c.applyDirectAgentMessage(ctx, projectID, *cfg.MemberID, cfg.Message)
		return applied, nil, err
	}

	if cfg.Target == nil || cfg.Target.Kind == "" || cfg.Target.Kind == automationdom.TaskTargetSelf {
		// No explicit retarget — exactly today's behavior: one task, no
		// aggregated detail.
		applied, err := c.applyActionForTask(ctx, projectID, node, actionType, cfg, task, automationName, runID)
		return applied, nil, err
	}
	if task == nil {
		// Defense in depth: every non-call_api, non-task-less-trigger_ai_agent
		// action requires a task (NodeRequiresTask), so
		// validateTaskReachability should make this unreachable.
		return false, nil, fmt.Errorf("%s: target %q requires a task to resolve relative to, but the walk has none", actionType, cfg.Target.Kind)
	}

	targets, err := c.resolveTargetTasks(ctx, projectID, task, cfg.Target)
	if err != nil {
		return false, nil, err
	}
	if len(targets) == 0 {
		return false, nil, nil
	}
	type targetResult struct {
		TaskID  string `json:"task_id"`
		Applied bool   `json:"applied"`
	}
	results := make([]targetResult, 0, len(targets))
	anyApplied := false
	for _, t := range targets {
		a, applyErr := c.applyActionForTask(ctx, projectID, node, actionType, cfg, t, automationName, runID)
		if applyErr != nil {
			detailBytes, _ := json.Marshal(map[string]any{"targets": results})
			return anyApplied, detailBytes, fmt.Errorf("task %s: %w", t.ID, applyErr)
		}
		anyApplied = anyApplied || a
		results = append(results, targetResult{TaskID: t.ID.String(), Applied: a})
	}
	detailBytes, _ := json.Marshal(map[string]any{"targets": results})
	return anyApplied, detailBytes, nil
}

// applyActionForTask dispatches to the concrete handler for actionType
// against exactly one task — extracted from runAction so a retargeted
// action (cfg.Target set to something other than "self") can call this
// once per resolved task. call_api and trigger_ai_agent's task-less
// direct-message path are handled directly in runAction before this is
// ever reached, since neither operates on a task at all.
func (c *AutomationConsumer) applyActionForTask(ctx context.Context, projectID uuid.UUID, node *automationdom.Node, actionType automationdom.ActionType, cfg automationdom.ActionConfig, task *taskdom.Task, automationName string, runID uuid.UUID) (bool, error) {
	switch actionType {
	case automationdom.ActionAssign:
		if cfg.MemberID == nil {
			return false, fmt.Errorf("assign: missing member_id")
		}
		return c.applyAssign(ctx, projectID, task, *cfg.MemberID, automationName, "")
	case automationdom.ActionTriggerAIAgent:
		if cfg.MemberID == nil {
			return false, fmt.Errorf("trigger_ai_agent: missing member_id")
		}
		return c.applyAssign(ctx, projectID, task, *cfg.MemberID, automationName, cfg.Message)
	case automationdom.ActionSetStatus:
		if cfg.StatusID == nil {
			return false, fmt.Errorf("set_status: missing status_id")
		}
		return c.applySetStatus(ctx, projectID, task, *cfg.StatusID)
	case automationdom.ActionSetPriority:
		if cfg.Importance == nil {
			return false, fmt.Errorf("set_priority: missing importance")
		}
		return c.applySetPriority(ctx, projectID, task, *cfg.Importance)
	case automationdom.ActionAddTag:
		return c.applyAddTag(ctx, projectID, task, cfg.Tag)
	case automationdom.ActionSetCustomField:
		return c.applySetCustomField(ctx, projectID, task, cfg.FieldKey, cfg.Value)
	default:
		return c.runPluginAction(ctx, node, task, runID)
	}
}

// maxCallAPIResponseBody caps how much of an outbound call_api response body
// gets stored in the run step's output_snapshot — independent of (smaller
// than) the hard cap the http.Client's body read itself applies, so a
// malicious/misbehaving external API can't bloat run history.
const maxCallAPIResponseBody = 4096

// applyCallAPI makes the outbound HTTP request configured on a call_api
// action node. Unlike every other action, it has no task-state equivalent
// to check before running, so it always executes when the walk reaches it.
// A non-2xx response is treated as a Go error (walk-stopping, same as any
// other action's failure), not a "skipped" no-op.
func (c *AutomationConsumer) applyCallAPI(ctx context.Context, cfg automationdom.ActionConfig) (bool, json.RawMessage, error) {
	var bodyReader io.Reader
	if cfg.Body != "" {
		bodyReader = strings.NewReader(cfg.Body)
	}
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(cfg.Method), cfg.URL, bodyReader)
	if err != nil {
		return false, nil, fmt.Errorf("call_api: build request: %w", err)
	}
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, nil, fmt.Errorf("call_api: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxCallAPIResponseBody+1))
	respBody := string(raw)
	if len(raw) > maxCallAPIResponseBody {
		respBody = respBody[:maxCallAPIResponseBody] + "…(truncated)"
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, nil, fmt.Errorf("call_api: unexpected status %d: %s", resp.StatusCode, respBody)
	}

	detail, _ := json.Marshal(map[string]any{"status_code": resp.StatusCode, "response_body": respBody})
	return true, detail, nil
}

// runPluginAction dispatches a plugin-contributed action node via the
// plugin runtime's RunAction bridge. idempotencyKey is stable per
// (run, node) — not guaranteed unique across retries at the node-execution
// granularity the way built-in actions are (their idempotency comes from
// checking current task state, not a key), but it gives a plugin author
// something stable to dedupe against in their own schema-isolated tables if
// they choose to, since the platform can't enforce idempotency inside
// someone else's WASM code.
func (c *AutomationConsumer) runPluginAction(ctx context.Context, node *automationdom.Node, task *taskdom.Task, runID uuid.UUID) (bool, error) {
	if c.pluginRuntime == nil {
		return false, fmt.Errorf("unknown action type %q", node.Type)
	}
	pluginName, ok := c.pluginRuntime.ResolveAutomationAction(node.Type)
	if !ok {
		return false, fmt.Errorf("unknown action type %q", node.Type)
	}
	idempotencyKey := fmt.Sprintf("%s:%s", runID, node.ID)
	payload, _ := json.Marshal(pluginNodePayload(node.Config, task, idempotencyKey))
	resp, err := c.pluginRuntime.RunAction(ctx, pluginName, payload)
	if err != nil {
		return false, fmt.Errorf("plugin action %q: %w", node.Type, err)
	}
	var result struct {
		Applied bool   `json:"applied"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return false, fmt.Errorf("plugin action %q: decode response: %w", node.Type, err)
	}
	if result.Error != "" {
		return false, fmt.Errorf("plugin action %q: %s", node.Type, result.Error)
	}
	return result.Applied, nil
}

// evaluatePluginCondition dispatches a plugin-contributed condition node via
// the plugin runtime's EvaluateCondition bridge.
func (c *AutomationConsumer) evaluatePluginCondition(ctx context.Context, node *automationdom.Node, payload []byte) (bool, error) {
	if c.pluginRuntime == nil {
		return false, fmt.Errorf("unknown condition type %q", node.Type)
	}
	pluginName, ok := c.pluginRuntime.ResolveAutomationCondition(node.Type)
	if !ok {
		return false, fmt.Errorf("unknown condition type %q", node.Type)
	}
	resp, err := c.pluginRuntime.EvaluateCondition(ctx, pluginName, payload)
	if err != nil {
		return false, fmt.Errorf("plugin condition %q: %w", node.Type, err)
	}
	var result struct {
		Matched bool `json:"matched"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return false, fmt.Errorf("plugin condition %q: decode response: %w", node.Type, err)
	}
	return result.Matched, nil
}

// pluginNodePayload builds the JSON bundle handed to a plugin's
// EvaluateCondition/RunAction export: the node's own config plus enough task
// context for the plugin to decide, plus an idempotency key (RunAction
// only; empty for EvaluateCondition since evaluation has no side effects to
// dedupe).
func pluginNodePayload(config json.RawMessage, task *taskdom.Task, idempotencyKey string) map[string]any {
	payload := map[string]any{
		"config": json.RawMessage(config),
		"task": map[string]any{
			"id":            task.ID,
			"status_id":     task.StatusID,
			"assignee_ids":  task.AssigneeIDs,
			"importance":    task.Importance,
			"tags":          task.Tags,
			"custom_fields": task.CustomFields,
		},
	}
	if idempotencyKey != "" {
		payload["idempotency_key"] = idempotencyKey
	}
	return payload
}

// applyAssign reassigns task to memberID, recording an activity and
// publishing the shared task.assigned event so the existing
// notification/agent-trigger pipeline picks it up uniformly. agentMessage,
// when non-empty (trigger_ai_agent only), is threaded through as the real
// instruction the agent's first conversation turn should see.
func (c *AutomationConsumer) applyAssign(ctx context.Context, projectID uuid.UUID, task *taskdom.Task, memberID uuid.UUID, automationName, agentMessage string) (bool, error) {
	if len(task.AssigneeIDs) == 1 && task.AssigneeIDs[0] == memberID {
		return false, nil
	}
	oldAssignees := task.AssigneeIDs
	newIDs := []uuid.UUID{memberID}
	if _, err := c.taskSvc.UpdateTask(ctx, projectID, task.ID, taskdom.UpdateTaskInput{AssigneeIDs: &newIDs}); err != nil {
		return false, fmt.Errorf("update assignee: %w", err)
	}
	task.AssigneeIDs = newIDs

	c.recordAppliedActivity(ctx, projectID, task.ID, automationName, map[string]any{
		"old_assignees": oldAssignees,
		"new_assignee":  memberID,
	})

	extra := map[string]any{"automation_name": automationName}
	if agentMessage != "" {
		extra["agent_message"] = agentMessage
	}
	if !slices.Contains(oldAssignees, memberID) {
		_ = events.PublishAssignmentChanged(ctx, c.publisher, task.ID, projectID, memberID, nil, userdom.SystemActorUserID, extra)
	}
	return true, nil
}

// applyDirectAgentMessage fires message straight at the agent behind
// memberID, no task involved — used when trigger_ai_agent's walk has no
// task (a cron/api_trigger/predecessor_done trigger with no
// target_task_id), since there's nothing to assign. Unlike applyAssign,
// there's no "already in target state" check to make — every visit sends a
// fresh message, and it always reports "applied" on success.
func (c *AutomationConsumer) applyDirectAgentMessage(ctx context.Context, projectID uuid.UUID, memberID uuid.UUID, message string) (bool, error) {
	if c.memberRepo == nil || c.agentMessenger == nil {
		return false, fmt.Errorf("trigger_ai_agent: direct-message dispatch not configured")
	}
	if strings.TrimSpace(message) == "" {
		// Unlike a task assignment (which falls back to a default "get
		// started" prompt on the agent side), a direct message has no task
		// for the agent to load context from — an empty prompt would just
		// error out downstream, so require one here instead.
		return false, fmt.Errorf("trigger_ai_agent: a message is required when there's no target task to assign")
	}
	member, err := c.memberRepo.FindMemberByID(ctx, memberID)
	if err != nil {
		return false, fmt.Errorf("trigger_ai_agent: find member: %w", err)
	}
	if !member.IsAgent() || member.AgentID == nil {
		return false, fmt.Errorf("trigger_ai_agent: member %s is not an agent", memberID)
	}
	if _, err := c.agentMessenger.TriggerDirectMessage(ctx, projectID, *member.AgentID, nil, message); err != nil {
		return false, fmt.Errorf("trigger_ai_agent: trigger direct message: %w", err)
	}
	return true, nil
}

func (c *AutomationConsumer) applySetStatus(ctx context.Context, projectID uuid.UUID, task *taskdom.Task, statusID uuid.UUID) (bool, error) {
	if task.StatusID != nil && *task.StatusID == statusID {
		return false, nil
	}
	id := statusID
	ptr := &id
	if _, err := c.taskSvc.UpdateTask(ctx, projectID, task.ID, taskdom.UpdateTaskInput{StatusID: &ptr}); err != nil {
		return false, fmt.Errorf("update status: %w", err)
	}
	task.StatusID = &id
	return true, nil
}

func (c *AutomationConsumer) applySetPriority(ctx context.Context, projectID uuid.UUID, task *taskdom.Task, importance int) (bool, error) {
	if task.Importance == importance {
		return false, nil
	}
	if _, err := c.taskSvc.UpdateTask(ctx, projectID, task.ID, taskdom.UpdateTaskInput{Importance: &importance}); err != nil {
		return false, fmt.Errorf("update priority: %w", err)
	}
	task.Importance = importance
	return true, nil
}

func (c *AutomationConsumer) applyAddTag(ctx context.Context, projectID uuid.UUID, task *taskdom.Task, tag string) (bool, error) {
	if tag == "" || slices.Contains(task.Tags, tag) {
		return false, nil
	}
	newTags := append(append([]string{}, task.Tags...), tag)
	if _, err := c.taskSvc.UpdateTask(ctx, projectID, task.ID, taskdom.UpdateTaskInput{Tags: &newTags}); err != nil {
		return false, fmt.Errorf("add tag: %w", err)
	}
	task.Tags = newTags
	return true, nil
}

func (c *AutomationConsumer) applySetCustomField(ctx context.Context, projectID uuid.UUID, task *taskdom.Task, fieldKey string, value any) (bool, error) {
	if fieldKey == "" {
		return false, fmt.Errorf("set_custom_field: missing field_key")
	}
	if existing, ok := task.CustomFields[fieldKey]; ok && fmt.Sprintf("%v", existing) == fmt.Sprintf("%v", value) {
		return false, nil
	}
	newFields := make(map[string]any, len(task.CustomFields)+1)
	for k, v := range task.CustomFields {
		newFields[k] = v
	}
	newFields[fieldKey] = value
	if _, err := c.taskSvc.UpdateTask(ctx, projectID, task.ID, taskdom.UpdateTaskInput{CustomFields: &newFields}); err != nil {
		return false, fmt.Errorf("set custom field: %w", err)
	}
	task.CustomFields = newFields
	return true, nil
}

func (c *AutomationConsumer) recordAppliedActivity(ctx context.Context, projectID, taskID uuid.UUID, automationName string, extra map[string]any) {
	if c.activityRec == nil {
		return
	}
	fields := map[string]any{"automation_name": automationName}
	for k, v := range extra {
		fields[k] = v
	}
	content, _ := json.Marshal(fields)
	_ = c.activityRec.RecordActivity(ctx, taskdom.RecordActivityInput{
		TaskID:       taskID,
		ProjectID:    projectID,
		ActivityType: taskdom.ActivityTypeAutomationApplied,
		Content:      content,
	})
}
