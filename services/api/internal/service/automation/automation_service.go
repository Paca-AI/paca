// Package automationsvc implements automationdom.Service.
package automationsvc

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"context"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/messaging"
)

// taskLookup is the minimal task-domain surface the automation service needs.
type taskLookup interface {
	FindTaskByID(ctx context.Context, id uuid.UUID) (*taskdom.Task, error)
	FindTaskStatusByID(ctx context.Context, id uuid.UUID) (*taskdom.TaskStatus, error)
}

// memberLookup is the minimal project-domain surface the automation service needs.
type memberLookup interface {
	FindMemberByID(ctx context.Context, memberID uuid.UUID) (*projectdom.ProjectMember, error)
	// FindMemberByActor resolves an authenticated actor (user, or agent when
	// agentID is non-nil) to their project_members.id.
	FindMemberByActor(ctx context.Context, projectID, actorID uuid.UUID, agentID *uuid.UUID) (*projectdom.ProjectMember, error)
}

// Service implements automationdom.Service.
type Service struct {
	repo           automationdom.Repository
	taskRepo       taskLookup
	memberRepo     memberLookup
	publisher      *messaging.Publisher
	pluginResolver automationdom.PluginNodeResolver
}

// New returns a Service backed by repo, taskRepo, and memberRepo.
// publisher may be nil; real-time events are then skipped silently.
func New(repo automationdom.Repository, taskRepo taskLookup, memberRepo memberLookup, publisher *messaging.Publisher) *Service {
	return &Service{repo: repo, taskRepo: taskRepo, memberRepo: memberRepo, publisher: publisher}
}

// WithPluginNodeResolver configures a fallback checked whenever a node's
// type isn't one of the built-ins, so plugin-contributed trigger/condition/
// action types validate successfully. Without it, only built-in types
// validate (e.g. in tests).
func (s *Service) WithPluginNodeResolver(resolver automationdom.PluginNodeResolver) *Service {
	s.pluginResolver = resolver
	return s
}

// publish sends a real-time pub/sub notification for an automation-graph
// change. Errors are silently swallowed so a messaging failure never blocks
// the primary HTTP response.
func (s *Service) publish(ctx context.Context, topic string, payload map[string]any) {
	if s.publisher == nil {
		return
	}
	_ = s.publisher.Publish(ctx, events.ChannelRealtime, map[string]any{
		"type":    topic,
		"payload": payload,
	})
}

// --- Automation lifecycle ------------------------------------------------------

// ListAutomations returns a project's automations, optionally filtered by status.
func (s *Service) ListAutomations(ctx context.Context, projectID uuid.UUID, status *automationdom.Status) ([]*automationdom.Automation, error) {
	return s.repo.ListAutomations(ctx, projectID, status)
}

// GetAutomation returns one automation together with its full node/edge graph.
func (s *Service) GetAutomation(ctx context.Context, projectID, automationID uuid.UUID) (*automationdom.Graph, error) {
	a, err := s.findOwnedAutomation(ctx, projectID, automationID)
	if err != nil {
		return nil, err
	}
	graph, err := s.repo.LoadGraph(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	graph.Automation = a
	return graph, nil
}

// CreateAutomation creates a new draft automation.
func (s *Service) CreateAutomation(ctx context.Context, in automationdom.CreateAutomationInput) (*automationdom.Automation, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, automationdom.ErrNameInvalid
	}
	now := time.Now()
	a := &automationdom.Automation{
		ID:          uuid.New(),
		ProjectID:   in.ProjectID,
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		Status:      automationdom.StatusInactive,
		CreatedBy:   s.resolveMember(ctx, in.CreatedBy, in.AgentID, in.ProjectID),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.CreateAutomation(ctx, a); err != nil {
		return nil, err
	}
	s.publish(ctx, events.TopicAutomationCreated, map[string]any{
		"project_id":    a.ProjectID.String(),
		"automation_id": a.ID.String(),
	})
	return a, nil
}

// UpdateAutomation updates an automation's name/description.
func (s *Service) UpdateAutomation(ctx context.Context, projectID, automationID uuid.UUID, in automationdom.UpdateAutomationInput) (*automationdom.Automation, error) {
	a, err := s.findOwnedAutomation(ctx, projectID, automationID)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, automationdom.ErrNameInvalid
		}
		a.Name = name
	}
	if in.Description != nil {
		a.Description = strings.TrimSpace(*in.Description)
	}
	a.UpdatedAt = time.Now()
	if err := s.repo.UpdateAutomation(ctx, a); err != nil {
		return nil, err
	}
	s.publish(ctx, events.TopicAutomationUpdated, map[string]any{
		"project_id":    projectID.String(),
		"automation_id": a.ID.String(),
	})
	return a, nil
}

// DeleteAutomation soft-deletes an automation and its graph.
func (s *Service) DeleteAutomation(ctx context.Context, projectID, automationID uuid.UUID) error {
	a, err := s.findOwnedAutomation(ctx, projectID, automationID)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteAutomation(ctx, a.ID); err != nil {
		return err
	}
	s.publish(ctx, events.TopicAutomationDeleted, map[string]any{
		"project_id":    projectID.String(),
		"automation_id": a.ID.String(),
	})
	return nil
}

// Activate transitions an automation to active, after validating the graph
// is safe to run: at least one trigger node, at least one action node, and
// the graph remains a DAG. Always re-validates and re-confirms active even
// if already active, so re-invoking it is a harmless idempotent no-op.
func (s *Service) Activate(ctx context.Context, projectID, automationID uuid.UUID) (*automationdom.Automation, error) {
	a, err := s.findOwnedAutomation(ctx, projectID, automationID)
	if err != nil {
		return nil, err
	}

	nodes, err := s.repo.ListNodesByAutomation(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	hasTrigger, hasAction := false, false
	for _, n := range nodes {
		switch n.Kind {
		case automationdom.KindTrigger:
			hasTrigger = true
		case automationdom.KindAction:
			hasAction = true
		case automationdom.KindCondition:
			// Neither required nor sufficient on its own; nothing to record.
		}
	}
	if !hasTrigger {
		return nil, automationdom.ErrActivateNoTrigger
	}
	if !hasAction {
		return nil, automationdom.ErrActivateNoAction
	}
	for _, n := range nodes {
		if err := s.validateNodeTypeAndConfig(ctx, projectID, n.Kind, n.Type, n.Config, true); err != nil {
			return nil, fmt.Errorf("node %s: %w", n.ID, err)
		}
	}

	edges, err := s.repo.ListEdgesByAutomation(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if hasCycle(nodes, edges) {
		return nil, automationdom.ErrEdgeCycle
	}
	if err := validateTaskReachability(nodes, edges); err != nil {
		return nil, err
	}

	a.Status = automationdom.StatusActive
	a.UpdatedAt = time.Now()
	if err := s.repo.UpdateAutomation(ctx, a); err != nil {
		return nil, err
	}
	s.publish(ctx, events.TopicAutomationActivated, map[string]any{
		"project_id":    projectID.String(),
		"automation_id": a.ID.String(),
	})
	return a, nil
}

// Deactivate transitions an automation to inactive, stopping it from firing
// without deleting its graph — the graph stays fully editable either way.
// Always sets inactive unconditionally, so re-invoking it (or calling it on
// an already-inactive automation) is a harmless idempotent no-op.
func (s *Service) Deactivate(ctx context.Context, projectID, automationID uuid.UUID) (*automationdom.Automation, error) {
	a, err := s.findOwnedAutomation(ctx, projectID, automationID)
	if err != nil {
		return nil, err
	}
	a.Status = automationdom.StatusInactive
	a.UpdatedAt = time.Now()
	if err := s.repo.UpdateAutomation(ctx, a); err != nil {
		return nil, err
	}
	s.publish(ctx, events.TopicAutomationDeactivated, map[string]any{
		"project_id":    projectID.String(),
		"automation_id": a.ID.String(),
	})
	return a, nil
}

// --- Nodes --------------------------------------------------------------------

// AddNode adds a new Trigger/Condition/Action node to an editable automation.
func (s *Service) AddNode(ctx context.Context, projectID, automationID uuid.UUID, in automationdom.AddNodeInput) (*automationdom.Node, error) {
	a, err := s.findOwnedAutomation(ctx, projectID, automationID)
	if err != nil {
		return nil, err
	}
	if err := s.validateNodeTypeAndConfig(ctx, projectID, in.Kind, in.Type, in.Config, false); err != nil {
		return nil, err
	}

	now := time.Now()
	n := &automationdom.Node{
		ID:           uuid.New(),
		AutomationID: a.ID,
		Kind:         in.Kind,
		Type:         in.Type,
		Config:       normalizeConfig(in.Config),
		PosX:         in.PosX,
		PosY:         in.PosY,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.CreateNode(ctx, n); err != nil {
		return nil, err
	}
	s.publish(ctx, events.TopicAutomationNodeAdded, map[string]any{
		"project_id":    projectID.String(),
		"automation_id": a.ID.String(),
		"node_id":       n.ID.String(),
	})
	return n, nil
}

// UpdateNode updates a node's config and/or canvas position.
func (s *Service) UpdateNode(ctx context.Context, projectID, automationID, nodeID uuid.UUID, in automationdom.UpdateNodeInput) (*automationdom.Node, error) {
	a, err := s.findOwnedAutomation(ctx, projectID, automationID)
	if err != nil {
		return nil, err
	}
	n, err := s.findOwnedNode(ctx, a.ID, nodeID)
	if err != nil {
		return nil, err
	}

	if in.Config != nil {
		if err := s.validateNodeTypeAndConfig(ctx, projectID, n.Kind, n.Type, *in.Config, true); err != nil {
			return nil, err
		}
		newConfig := normalizeConfig(*in.Config)
		if n.Kind == automationdom.KindTrigger {
			// A trigger's TargetTaskID can change here (e.g. cleared to
			// nil) — re-check the whole graph, not just this node's own
			// config, since that can retroactively make an already-valid
			// downstream edge invalid (see validateTaskReachability).
			nodes, err := s.repo.ListNodesByAutomation(ctx, a.ID)
			if err != nil {
				return nil, err
			}
			edges, err := s.repo.ListEdgesByAutomation(ctx, a.ID)
			if err != nil {
				return nil, err
			}
			tentativeNodes := make([]*automationdom.Node, len(nodes))
			for i, existing := range nodes {
				if existing.ID == n.ID {
					clone := *existing
					clone.Config = newConfig
					tentativeNodes[i] = &clone
				} else {
					tentativeNodes[i] = existing
				}
			}
			if err := validateTaskReachability(tentativeNodes, edges); err != nil {
				return nil, err
			}
		}
		n.Config = newConfig
	}
	if in.PosX != nil {
		n.PosX = *in.PosX
	}
	if in.PosY != nil {
		n.PosY = *in.PosY
	}
	n.UpdatedAt = time.Now()
	if err := s.repo.UpdateNode(ctx, n); err != nil {
		return nil, err
	}
	s.publish(ctx, events.TopicAutomationNodeUpdated, map[string]any{
		"project_id":    projectID.String(),
		"automation_id": a.ID.String(),
		"node_id":       n.ID.String(),
	})
	return n, nil
}

// RemoveNode deletes a node and its connected edges from an editable automation.
func (s *Service) RemoveNode(ctx context.Context, projectID, automationID, nodeID uuid.UUID) error {
	a, err := s.findOwnedAutomation(ctx, projectID, automationID)
	if err != nil {
		return err
	}
	n, err := s.findOwnedNode(ctx, a.ID, nodeID)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteNode(ctx, n.ID); err != nil {
		return err
	}
	s.publish(ctx, events.TopicAutomationNodeRemoved, map[string]any{
		"project_id":    projectID.String(),
		"automation_id": a.ID.String(),
		"node_id":       n.ID.String(),
	})
	return nil
}

// --- Edges ----------------------------------------------------------------

// AddEdge connects two nodes in an editable automation, rejecting self-loops,
// cycles, duplicates, edges into a Trigger, and edges that would make a
// downstream node unreachable from a task.
func (s *Service) AddEdge(ctx context.Context, projectID, automationID uuid.UUID, in automationdom.AddEdgeInput) (*automationdom.Edge, error) {
	a, err := s.findOwnedAutomation(ctx, projectID, automationID)
	if err != nil {
		return nil, err
	}
	if in.SourceNodeID == in.TargetNodeID {
		return nil, automationdom.ErrEdgeSelfLoop
	}
	source, err := s.findOwnedNode(ctx, a.ID, in.SourceNodeID)
	if err != nil {
		return nil, err
	}
	target, err := s.findOwnedNode(ctx, a.ID, in.TargetNodeID)
	if err != nil {
		return nil, err
	}
	if target.Kind == automationdom.KindTrigger {
		return nil, automationdom.ErrEdgeIntoTrigger
	}
	if err := validateEdgeHandle(source, in.SourceHandle); err != nil {
		return nil, err
	}

	edges, err := s.repo.ListEdgesByAutomation(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if wouldCreateCycle(edges, source.ID, target.ID) {
		return nil, automationdom.ErrEdgeCycle
	}
	for _, e := range edges {
		if e.SourceNodeID == source.ID && e.TargetNodeID == target.ID && handlesEqual(e.SourceHandle, in.SourceHandle) {
			return nil, automationdom.ErrEdgeDuplicate
		}
	}

	nodes, err := s.repo.ListNodesByAutomation(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	tentativeEdges := make([]*automationdom.Edge, len(edges), len(edges)+1)
	copy(tentativeEdges, edges)
	tentativeEdges = append(tentativeEdges, &automationdom.Edge{SourceNodeID: source.ID, TargetNodeID: target.ID})
	if err := validateTaskReachability(nodes, tentativeEdges); err != nil {
		return nil, err
	}

	e := &automationdom.Edge{
		ID:           uuid.New(),
		AutomationID: a.ID,
		SourceNodeID: source.ID,
		SourceHandle: in.SourceHandle,
		TargetNodeID: target.ID,
		CreatedAt:    time.Now(),
	}
	if err := s.repo.CreateEdge(ctx, e); err != nil {
		return nil, err
	}
	s.publish(ctx, events.TopicAutomationEdgeAdded, map[string]any{
		"project_id":     projectID.String(),
		"automation_id":  a.ID.String(),
		"edge_id":        e.ID.String(),
		"source_node_id": e.SourceNodeID.String(),
		"target_node_id": e.TargetNodeID.String(),
	})
	return e, nil
}

// RemoveEdge deletes one edge from an editable automation.
func (s *Service) RemoveEdge(ctx context.Context, projectID, automationID, edgeID uuid.UUID) error {
	a, err := s.findOwnedAutomation(ctx, projectID, automationID)
	if err != nil {
		return err
	}
	e, err := s.repo.FindEdgeByID(ctx, edgeID)
	if err != nil {
		return err
	}
	if e.AutomationID != a.ID {
		return automationdom.ErrEdgeNotFound
	}
	if err := s.repo.DeleteEdge(ctx, e.ID); err != nil {
		return err
	}
	s.publish(ctx, events.TopicAutomationEdgeRemoved, map[string]any{
		"project_id":    projectID.String(),
		"automation_id": a.ID.String(),
		"edge_id":       e.ID.String(),
	})
	return nil
}

// --- Runs -------------------------------------------------------------------

// ListRuns returns an automation's most recent executions, newest first.
func (s *Service) ListRuns(ctx context.Context, projectID, automationID uuid.UUID, limit int) ([]*automationdom.Run, error) {
	if _, err := s.findOwnedAutomation(ctx, projectID, automationID); err != nil {
		return nil, err
	}
	return s.repo.ListRunsByAutomation(ctx, automationID, limit)
}

// ListRunSteps returns every node visited during one run, in execution order.
func (s *Service) ListRunSteps(ctx context.Context, projectID, automationID, runID uuid.UUID) ([]*automationdom.RunStep, error) {
	if _, err := s.findOwnedAutomation(ctx, projectID, automationID); err != nil {
		return nil, err
	}
	if _, err := s.findOwnedRun(ctx, automationID, runID); err != nil {
		return nil, err
	}
	return s.repo.ListRunStepsByRun(ctx, runID)
}

// --- Dependency map ---------------------------------------------------------

// DependencyMap surfaces every active automation's predecessor_done trigger
// configs in projectID, for the read-only, auto-generated dependency view.
func (s *Service) DependencyMap(ctx context.Context, projectID uuid.UUID) ([]automationdom.DependencyMapEntry, error) {
	active := automationdom.StatusActive
	automations, err := s.repo.ListAutomations(ctx, projectID, &active)
	if err != nil {
		return nil, err
	}
	var entries []automationdom.DependencyMapEntry
	for _, a := range automations {
		nodes, err := s.repo.ListNodesByAutomation(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			if n.Kind != automationdom.KindTrigger || automationdom.TriggerType(n.Type) != automationdom.TriggerPredecessorDone {
				continue
			}
			var cfg automationdom.TriggerConfig
			if err := json.Unmarshal(n.Config, &cfg); err != nil || len(cfg.WatchedTaskIDs) == 0 || cfg.TargetTaskID == nil {
				continue
			}
			entries = append(entries, automationdom.DependencyMapEntry{
				AutomationID:   a.ID,
				AutomationName: a.Name,
				NodeID:         n.ID,
				TargetTaskID:   *cfg.TargetTaskID,
				WatchedTaskIDs: cfg.WatchedTaskIDs,
			})
		}
	}
	return entries, nil
}

const (
	webhookTokenPrefix     = "pacahk_" // distinct from apikeysvc's "paca_" so the two can't be confused
	webhookTokenDisplayLen = 8         // first N hex chars stored for display
)

// GenerateWebhookToken (re)generates the secret token for an api_trigger
// node. The raw token is returned only here — it's hashed before storage
// and never persisted or retrievable again afterward, same pattern as
// apikeysvc.Create.
func (s *Service) GenerateWebhookToken(ctx context.Context, projectID, automationID, nodeID uuid.UUID, actor automationdom.WebhookTokenActor) (*automationdom.WebhookToken, string, error) {
	if _, err := s.findOwnedAutomation(ctx, projectID, automationID); err != nil {
		return nil, "", err
	}
	node, err := s.findOwnedNode(ctx, automationID, nodeID)
	if err != nil {
		return nil, "", err
	}
	if node.Kind != automationdom.KindTrigger || automationdom.TriggerType(node.Type) != automationdom.TriggerAPITrigger {
		return nil, "", automationdom.ErrNodeInvalidType
	}

	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return nil, "", fmt.Errorf("automation: generate webhook token: %w", err)
	}
	rawHex := hex.EncodeToString(rawBytes)
	rawToken := webhookTokenPrefix + rawHex

	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	tok := &automationdom.WebhookToken{
		ID:           uuid.New(),
		NodeID:       nodeID,
		AutomationID: automationID,
		TokenPrefix:  rawHex[:webhookTokenDisplayLen],
		CreatedBy:    s.resolveMember(ctx, actor.UserID, actor.AgentID, projectID),
	}
	created, err := s.repo.CreateOrRotateWebhookToken(ctx, tok, tokenHash)
	if err != nil {
		return nil, "", err
	}
	return created, rawToken, nil
}

// HandleWebhookTrigger verifies rawToken against nodeID's active webhook
// token and, if valid, dispatches an async graph-walk run against the
// node's configured target task. Every failure path deliberately returns
// the same class of error (ErrNotFound or ErrWebhookTokenInvalid)
// regardless of which specific thing was wrong — an unauthenticated caller
// can't use this to probe whether a webhook exists, or is just misconfigured.
func (s *Service) HandleWebhookTrigger(ctx context.Context, nodeID uuid.UUID, rawToken string) error {
	node, err := s.repo.FindNodeByID(ctx, nodeID)
	if err != nil {
		return err
	}
	if node.Kind != automationdom.KindTrigger || automationdom.TriggerType(node.Type) != automationdom.TriggerAPITrigger {
		return automationdom.ErrNotFound
	}
	automation, err := s.repo.FindAutomationByNodeID(ctx, nodeID)
	if err != nil {
		return err
	}
	if automation.Status != automationdom.StatusActive {
		return automationdom.ErrNotFound
	}

	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])
	tok, err := s.repo.VerifyWebhookToken(ctx, nodeID, tokenHash)
	if err != nil {
		return err
	}

	var cfg automationdom.TriggerConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		// Not the caller's fault, so don't leak it as a 4xx.
		return fmt.Errorf("automation: api_trigger node %s has invalid config: %w", nodeID, err)
	}

	// Best-effort — a failure to record usage shouldn't block the trigger.
	_ = s.repo.RecordWebhookTokenUsed(ctx, tok.ID, time.Now())

	// task_id is optional — TargetTaskID may be unset (validateTaskReachability
	// guarantees that's only possible when this node has no downstream nodes
	// other than call_api actions), in which case the consumer runs the walk
	// with no task at all.
	payload := map[string]any{
		"node_id":       nodeID.String(),
		"automation_id": automation.ID.String(),
		"project_id":    automation.ProjectID.String(),
	}
	if cfg.TargetTaskID != nil {
		payload["task_id"] = cfg.TargetTaskID.String()
	}
	return s.publisher.Append(ctx, events.StreamAutomationExternalTriggers, events.TopicAutomationAPITriggerFired, payload)
}

// --- helpers ----------------------------------------------------------------

func (s *Service) findOwnedAutomation(ctx context.Context, projectID, automationID uuid.UUID) (*automationdom.Automation, error) {
	a, err := s.repo.FindAutomationByID(ctx, automationID)
	if err != nil {
		return nil, err
	}
	if a.ProjectID != projectID {
		return nil, automationdom.ErrNotFound
	}
	return a, nil
}

func (s *Service) findOwnedNode(ctx context.Context, automationID, nodeID uuid.UUID) (*automationdom.Node, error) {
	n, err := s.repo.FindNodeByID(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if n.AutomationID != automationID {
		return nil, automationdom.ErrNodeNotFound
	}
	return n, nil
}

func (s *Service) findOwnedRun(ctx context.Context, automationID, runID uuid.UUID) (*automationdom.Run, error) {
	run, err := s.repo.FindRunByID(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.AutomationID != automationID {
		return nil, automationdom.ErrRunNotFound
	}
	return run, nil
}

// resolveMember resolves an authenticated actor to their project_members.id
// for storage in Automation.CreatedBy. Returns nil (no error) when userID is
// nil or the actor can't be resolved as a member of this project —
// CreatedBy is purely informational.
func (s *Service) resolveMember(ctx context.Context, userID, agentID *uuid.UUID, projectID uuid.UUID) *uuid.UUID {
	if userID == nil {
		return nil
	}
	member, err := s.memberRepo.FindMemberByActor(ctx, projectID, *userID, agentID)
	if err != nil {
		return nil
	}
	return &member.ID
}

func normalizeConfig(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

// validateNodeTypeAndConfig checks that kind/type is a recognized
// combination and that config unmarshals into the right shape with
// cross-project references (status IDs, member IDs, watched task IDs) all
// belonging to projectID. Plugin-contributed node types are validated
// elsewhere (see the plugin automation bridge) — this only recognizes
// built-ins.
//
// strict controls whether a type's required fields (e.g. add_tag's tag,
// assign's member_id) must already be present. Nodes are always created with
// an empty config and configured afterward via UpdateNode, so AddNode calls
// this with strict=false — only structural checks (JSON shape, cross-project
// references for whatever fields are present) apply. UpdateNode and Activate
// call it with strict=true, since those are the points where a config is
// expected to be complete.
func (s *Service) validateNodeTypeAndConfig(ctx context.Context, projectID uuid.UUID, kind automationdom.Kind, nodeType string, config json.RawMessage, strict bool) error {
	if !automationdom.ValidKinds[kind] {
		return automationdom.ErrNodeInvalidKind
	}
	switch kind {
	case automationdom.KindTrigger:
		return s.validateTriggerConfig(ctx, projectID, automationdom.TriggerType(nodeType), config, strict)
	case automationdom.KindCondition:
		if nodeType != automationdom.ConditionNodeType {
			// Not the built-in N-branch switch — this must be a
			// plugin-contributed condition node instead (its own node type,
			// evaluated via the plugin runtime's EvaluateCondition bridge
			// rather than the built-in leaf-tree DSL). Config is opaque to
			// this service; the plugin owns its own validation.
			if s.pluginResolver != nil && s.pluginResolver.IsPluginCondition(nodeType) {
				return nil
			}
			return automationdom.ErrNodeInvalidType
		}
		return s.validateConditionConfig(ctx, projectID, config, strict)
	case automationdom.KindAction:
		return s.validateActionConfig(ctx, projectID, automationdom.ActionType(nodeType), config, strict)
	default:
		return automationdom.ErrNodeInvalidKind
	}
}

func (s *Service) validateTriggerConfig(ctx context.Context, projectID uuid.UUID, t automationdom.TriggerType, raw json.RawMessage, strict bool) error {
	if !automationdom.ValidBuiltinTriggerTypes[t] {
		// A plugin-contributed trigger type: config is opaque here — the
		// plugin's declared EventTopic is what the engine actually matches
		// on, not anything in this config blob.
		if s.pluginResolver != nil && s.pluginResolver.IsPluginTrigger(string(t)) {
			return nil
		}
		return automationdom.ErrNodeInvalidType
	}
	var cfg automationdom.TriggerConfig
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("%w: %v", automationdom.ErrNodeConfigInvalid, err)
		}
	}
	if cfg.StatusID != nil {
		if err := s.assertStatusInProject(ctx, projectID, *cfg.StatusID); err != nil {
			return err
		}
	}
	if t == automationdom.TriggerPredecessorDone {
		if strict && len(cfg.WatchedTaskIDs) == 0 {
			return fmt.Errorf("%w: predecessor_done requires at least one watched task", automationdom.ErrNodeConfigInvalid)
		}
		// TargetTaskID is optional — left nil, this trigger may only
		// connect to call_api actions downstream (validateTaskReachability
		// enforces that at edge-creation/node-update/activate time).
		for _, taskID := range cfg.WatchedTaskIDs {
			task, err := s.taskRepo.FindTaskByID(ctx, taskID)
			if err != nil {
				return err
			}
			if task.ProjectID != projectID {
				return automationdom.ErrNodeCrossProject
			}
		}
	}
	if t == automationdom.TriggerCron {
		if cfg.CronExpression != "" {
			if _, err := cron.ParseStandard(cfg.CronExpression); err != nil {
				return fmt.Errorf("%w: invalid cron_expression: %v", automationdom.ErrNodeConfigInvalid, err)
			}
		} else if strict {
			return fmt.Errorf("%w: cron requires a cron_expression", automationdom.ErrNodeConfigInvalid)
		}
		// TargetTaskID is optional — see the predecessor_done branch above.
	}
	// api_trigger has no fields of its own beyond the shared TargetTaskID
	// (optional, see above).
	// Shared across every trigger type that carries a TargetTaskID
	// (predecessor_done, cron, api_trigger): whatever task is set must
	// belong to the same project.
	if cfg.TargetTaskID != nil {
		targetTask, err := s.taskRepo.FindTaskByID(ctx, *cfg.TargetTaskID)
		if err != nil {
			return err
		}
		if targetTask.ProjectID != projectID {
			return automationdom.ErrNodeCrossProject
		}
	}
	return nil
}

func (s *Service) validateConditionConfig(ctx context.Context, projectID uuid.UUID, raw json.RawMessage, strict bool) error {
	var cfg automationdom.ConditionConfig
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("%w: %v", automationdom.ErrNodeConfigInvalid, err)
		}
	}
	seen := map[string]bool{}
	for _, b := range cfg.Branches {
		if b.Handle == "" || b.Handle == automationdom.ElseHandle {
			return fmt.Errorf("%w: branch handle must be non-empty and not the reserved %q value", automationdom.ErrNodeConfigInvalid, automationdom.ElseHandle)
		}
		if seen[b.Handle] {
			return fmt.Errorf("%w: duplicate branch handle %q", automationdom.ErrNodeConfigInvalid, b.Handle)
		}
		seen[b.Handle] = true
		if err := b.Tree.Validate(); err != nil {
			return fmt.Errorf("%w: %v", automationdom.ErrNodeConfigInvalid, err)
		}
		if b.Tree != nil {
			if b.Tree.MatchMode != "" && b.Tree.MatchMode != "any" && b.Tree.MatchMode != "all" {
				return fmt.Errorf("%w: match_mode must be \"any\" or \"all\"", automationdom.ErrNodeConfigInvalid)
			}
			if err := s.validateTaskTarget(ctx, projectID, b.Tree.Target, strict); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateTaskTarget validates a condition leaf's or action's TaskTarget:
// the kind is recognized, and — only for TaskTargetOther, the one kind that
// carries its own task reference — that task exists and belongs to
// projectID. Every other kind (parent/children/each link type) is resolved
// relative to whatever task the walk is bound to at runtime, so there's
// nothing further to check here.
func (s *Service) validateTaskTarget(ctx context.Context, projectID uuid.UUID, target *automationdom.TaskTarget, strict bool) error {
	if target == nil || target.Kind == "" {
		return nil
	}
	if !automationdom.ValidTaskTargetKinds[target.Kind] {
		return fmt.Errorf("%w: unknown target kind %q", automationdom.ErrNodeConfigInvalid, target.Kind)
	}
	if target.Kind != automationdom.TaskTargetOther {
		return nil
	}
	if target.OtherTaskID == nil {
		if strict {
			return fmt.Errorf("%w: target kind \"other\" requires other_task_id", automationdom.ErrNodeConfigInvalid)
		}
		return nil
	}
	otherTask, err := s.taskRepo.FindTaskByID(ctx, *target.OtherTaskID)
	if err != nil {
		return err
	}
	if otherTask.ProjectID != projectID {
		return automationdom.ErrNodeCrossProject
	}
	return nil
}

func (s *Service) validateActionConfig(ctx context.Context, projectID uuid.UUID, t automationdom.ActionType, raw json.RawMessage, strict bool) error {
	if !automationdom.ValidBuiltinActionTypes[t] {
		// A plugin-contributed action type: config is opaque to this
		// service — the plugin validates its own config when RunAction
		// executes.
		if s.pluginResolver != nil && s.pluginResolver.IsPluginAction(string(t)) {
			return nil
		}
		return automationdom.ErrNodeInvalidType
	}
	var cfg automationdom.ActionConfig
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("%w: %v", automationdom.ErrNodeConfigInvalid, err)
		}
	}
	if t == automationdom.ActionCallAPI || t == automationdom.ActionWait || t == automationdom.ActionUpdateSprint || t == automationdom.ActionCompleteSprint {
		if cfg.Target != nil {
			return fmt.Errorf("%w: %s does not support a target — it resolves its own sprint/nothing from context instead", automationdom.ErrNodeConfigInvalid, t)
		}
	} else if err := s.validateTaskTarget(ctx, projectID, cfg.Target, strict); err != nil {
		return err
	}
	switch t {
	case automationdom.ActionUpdateTask:
		return s.validateTaskFieldUpdate(ctx, projectID, cfg.Update, strict)
	case automationdom.ActionTriggerAIAgent:
		if cfg.MemberID == nil {
			if strict {
				return fmt.Errorf("%w: trigger_ai_agent requires member_id", automationdom.ErrNodeConfigInvalid)
			}
			return nil
		}
		return s.assertMemberInProject(ctx, projectID, *cfg.MemberID)
	case automationdom.ActionCallAPI:
		if cfg.Method != "" && !validCallAPIMethods[strings.ToUpper(cfg.Method)] {
			return fmt.Errorf("%w: call_api method must be one of GET, POST, PUT, PATCH, DELETE", automationdom.ErrNodeConfigInvalid)
		}
		if cfg.URL != "" {
			u, err := url.ParseRequestURI(cfg.URL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				return fmt.Errorf("%w: call_api url must be a valid http(s) URL", automationdom.ErrNodeConfigInvalid)
			}
		}
		if strict {
			if cfg.Method == "" {
				return fmt.Errorf("%w: call_api requires method", automationdom.ErrNodeConfigInvalid)
			}
			if cfg.URL == "" {
				return fmt.Errorf("%w: call_api requires url", automationdom.ErrNodeConfigInvalid)
			}
		}
	case automationdom.ActionWait:
		if cfg.WaitMinutes != nil && *cfg.WaitMinutes <= 0 {
			return fmt.Errorf("%w: wait_minutes must be greater than 0", automationdom.ErrNodeConfigInvalid)
		}
		if strict && cfg.WaitMinutes == nil {
			return fmt.Errorf("%w: wait requires wait_minutes", automationdom.ErrNodeConfigInvalid)
		}
	case automationdom.ActionUpdateSprint:
		return s.validateSprintFieldUpdate(cfg.SprintUpdate, strict)
	case automationdom.ActionCompleteSprint:
		// MoveToSprintID is optional (nil = move to backlog, mirroring
		// sprintdom.CompleteSprintInput) — nothing to require.
	}
	return nil
}

// validateSprintFieldUpdate validates ActionUpdateSprint's config: in strict
// mode at least one field must actually be set (same "config-less node is
// almost certainly a mistake" reasoning as validateTaskFieldUpdate), and a
// set Status must be a recognized sprintdom.SprintStatus value. No FK/
// cross-project check for anything here — same precedent
// validateTaskFieldUpdate already established for sprint_id: an invalid
// reference just fails later at the sprint service's own lookup when the
// action actually runs.
func (s *Service) validateSprintFieldUpdate(upd *automationdom.SprintFieldUpdate, strict bool) error {
	if upd == nil {
		if strict {
			return fmt.Errorf("%w: update_sprint requires at least one field to update", automationdom.ErrNodeConfigInvalid)
		}
		return nil
	}
	if strict && !sprintFieldUpdateHasAnyField(upd) {
		return fmt.Errorf("%w: update_sprint requires at least one field to update", automationdom.ErrNodeConfigInvalid)
	}
	if upd.Status != nil && !sprintdom.ValidSprintStatuses[*upd.Status] {
		return fmt.Errorf("%w: invalid sprint status %q", automationdom.ErrNodeConfigInvalid, *upd.Status)
	}
	return nil
}

// sprintFieldUpdateHasAnyField reports whether upd sets at least one field —
// used only to reject a config-less update_sprint node in strict mode.
func sprintFieldUpdateHasAnyField(upd *automationdom.SprintFieldUpdate) bool {
	return upd.Name != "" ||
		upd.StartDate != nil ||
		upd.EndDate != nil ||
		upd.Goal != nil ||
		upd.Status != nil
}

// validateTaskFieldUpdate validates ActionUpdateTask's config: a
// referenced status/reporter/assignee/parent-task must exist and belong to
// projectID, and in strict mode at least one field must actually be set —
// an update_task node with nothing to update is almost certainly an
// author mistake (no built-in action before this one could be created
// config-less and silently do nothing). task_type_id and sprint_id aren't
// FK-checked here, matching this package's existing precedent (e.g.
// validateTriggerConfig never checks a trigger's task_type_id either) —
// an invalid ID just fails later at the task service's own FK layer when
// the action actually runs.
func (s *Service) validateTaskFieldUpdate(ctx context.Context, projectID uuid.UUID, upd *automationdom.TaskFieldUpdate, strict bool) error {
	if upd == nil {
		if strict {
			return fmt.Errorf("%w: update_task requires at least one field to update", automationdom.ErrNodeConfigInvalid)
		}
		return nil
	}
	if strict && !taskFieldUpdateHasAnyField(upd) {
		return fmt.Errorf("%w: update_task requires at least one field to update", automationdom.ErrNodeConfigInvalid)
	}
	if upd.StatusID != nil {
		if err := s.assertStatusInProject(ctx, projectID, *upd.StatusID); err != nil {
			return err
		}
	}
	for _, memberID := range upd.AssigneeIDs {
		if err := s.assertMemberInProject(ctx, projectID, memberID); err != nil {
			return err
		}
	}
	if upd.ReporterID != nil {
		if err := s.assertMemberInProject(ctx, projectID, *upd.ReporterID); err != nil {
			return err
		}
	}
	if upd.ParentTaskID != nil {
		parent, err := s.taskRepo.FindTaskByID(ctx, *upd.ParentTaskID)
		if err != nil {
			return err
		}
		if parent.ProjectID != projectID {
			return automationdom.ErrNodeCrossProject
		}
	}
	return nil
}

// taskFieldUpdateHasAnyField reports whether upd sets at least one field —
// used only to reject a config-less update_task node in strict mode.
func taskFieldUpdateHasAnyField(upd *automationdom.TaskFieldUpdate) bool {
	return upd.TaskTypeID != nil ||
		upd.StatusID != nil ||
		upd.SprintID != nil ||
		upd.ParentTaskID != nil ||
		upd.Title != "" ||
		len(upd.Description) > 0 ||
		upd.Importance != nil ||
		upd.StoryPoints != nil ||
		len(upd.AssigneeIDs) > 0 ||
		upd.ReporterID != nil ||
		len(upd.CustomFields) > 0 ||
		upd.StartDate != nil ||
		upd.DueDate != nil ||
		len(upd.Tags) > 0
}

var validCallAPIMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
}

func (s *Service) assertStatusInProject(ctx context.Context, projectID, statusID uuid.UUID) error {
	status, err := s.taskRepo.FindTaskStatusByID(ctx, statusID)
	if err != nil {
		return err
	}
	if status.ProjectID != projectID {
		return automationdom.ErrNodeCrossProject
	}
	return nil
}

func (s *Service) assertMemberInProject(ctx context.Context, projectID, memberID uuid.UUID) error {
	member, err := s.memberRepo.FindMemberByID(ctx, memberID)
	if err != nil {
		return err
	}
	if member.ProjectID != projectID {
		return automationdom.ErrNodeCrossProject
	}
	return nil
}

func handlesEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// validateEdgeHandle enforces: a condition-sourced edge must specify a
// handle that is valid for that condition node, or the reserved "else"
// fallback; a trigger/action-sourced edge must not specify a handle at all
// (it has exactly one outgoing path). A condition node's valid non-else
// handles depend on its Type: the built-in N-branch switch (Type ==
// ConditionNodeType) accepts any handle declared in its own
// ConditionConfig.Branches, while a plugin-contributed condition — a
// boolean gate, not a switch — accepts exactly one, PluginConditionTrueHandle
// (mirrors walker.walkPluginCondition in automation_consumer.go, which is
// the only handle it ever follows on a match).
func validateEdgeHandle(source *automationdom.Node, handle *string) error {
	if source.Kind != automationdom.KindCondition {
		if handle != nil {
			return automationdom.ErrEdgeHandleNotAllowed
		}
		return nil
	}
	if handle == nil || *handle == "" {
		return automationdom.ErrEdgeHandleRequired
	}
	if *handle == automationdom.ElseHandle {
		return nil
	}
	if source.Type != automationdom.ConditionNodeType {
		if *handle == automationdom.PluginConditionTrueHandle {
			return nil
		}
		return fmt.Errorf("%w: %q is not a valid handle for a plugin condition node", automationdom.ErrNodeConfigInvalid, *handle)
	}
	var cfg automationdom.ConditionConfig
	if len(source.Config) > 0 {
		_ = json.Unmarshal(source.Config, &cfg)
	}
	for _, b := range cfg.Branches {
		if b.Handle == *handle {
			return nil
		}
	}
	return fmt.Errorf("%w: %q is not a declared branch on this condition node", automationdom.ErrNodeConfigInvalid, *handle)
}

// wouldCreateCycle reports whether adding an edge sourceID -> targetID would
// create a cycle, i.e. whether targetID can already reach sourceID via
// existing edges. Ported from the workflow feature's edge-add-time check.
func wouldCreateCycle(edges []*automationdom.Edge, sourceID, targetID uuid.UUID) bool {
	adjacency := make(map[uuid.UUID][]uuid.UUID, len(edges))
	for _, e := range edges {
		adjacency[e.SourceNodeID] = append(adjacency[e.SourceNodeID], e.TargetNodeID)
	}
	visited := make(map[uuid.UUID]bool)
	var dfs func(uuid.UUID) bool
	dfs = func(node uuid.UUID) bool {
		if node == sourceID {
			return true
		}
		if visited[node] {
			return false
		}
		visited[node] = true
		for _, next := range adjacency[node] {
			if dfs(next) {
				return true
			}
		}
		return false
	}
	return dfs(targetID)
}

// hasCycle reports whether the given node/edge set contains any cycle at
// all, used as a defensive re-check at activation time. Ported from the
// workflow feature.
func hasCycle(nodes []*automationdom.Node, edges []*automationdom.Edge) bool {
	adjacency := make(map[uuid.UUID][]uuid.UUID, len(edges))
	for _, e := range edges {
		adjacency[e.SourceNodeID] = append(adjacency[e.SourceNodeID], e.TargetNodeID)
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	state := make(map[uuid.UUID]int, len(nodes))
	var dfs func(uuid.UUID) bool
	dfs = func(node uuid.UUID) bool {
		state[node] = gray
		for _, next := range adjacency[node] {
			switch state[next] {
			case gray:
				return true
			case white:
				if dfs(next) {
					return true
				}
			}
		}
		state[node] = black
		return false
	}
	for _, n := range nodes {
		if state[n.ID] == white {
			if dfs(n.ID) {
				return true
			}
		}
	}
	return false
}

// validateTaskReachability enforces that a trigger with no target task
// (predecessor_done, cron, or api_trigger whose TriggerConfig.TargetTaskID
// is nil — the only triggers not derived from a task-activity event, so the
// only ones that can lack a task at all) never reaches a node that needs
// one downstream. automationdom.NodeRequiresTask is the single source of
// truth for which nodes those are; the worker's graph walk enforces the
// same rule defensively at runtime in case this invariant is ever violated.
//
// Checked as a whole-graph pass (not just "does this one new edge cause a
// problem") so it catches the transitive case too: a task-less trigger
// reaching a task-needing node through several hops, or through a node that
// only became reachable because a different, unrelated edge was added.
func validateTaskReachability(nodes []*automationdom.Node, edges []*automationdom.Edge) error {
	nodesByID := make(map[uuid.UUID]*automationdom.Node, len(nodes))
	for _, n := range nodes {
		nodesByID[n.ID] = n
	}
	outgoing := make(map[uuid.UUID][]uuid.UUID, len(edges))
	for _, e := range edges {
		outgoing[e.SourceNodeID] = append(outgoing[e.SourceNodeID], e.TargetNodeID)
	}

	for _, n := range nodes {
		if n.Kind != automationdom.KindTrigger {
			continue
		}
		switch automationdom.TriggerType(n.Type) {
		case automationdom.TriggerPredecessorDone, automationdom.TriggerCron, automationdom.TriggerAPITrigger:
		default:
			continue
		}
		var cfg automationdom.TriggerConfig
		if err := json.Unmarshal(n.Config, &cfg); err != nil || cfg.TargetTaskID != nil {
			continue
		}

		visited := map[uuid.UUID]bool{n.ID: true}
		queue := append([]uuid.UUID{}, outgoing[n.ID]...)
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if visited[id] {
				continue
			}
			visited[id] = true
			target, ok := nodesByID[id]
			if !ok {
				continue
			}
			if automationdom.NodeRequiresTask(target.Kind, target.Type) {
				return automationdom.ErrEdgeRequiresTargetTask
			}
			queue = append(queue, outgoing[id]...)
		}
	}
	return nil
}
