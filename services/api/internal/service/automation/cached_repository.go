package automationsvc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	"github.com/Paca-AI/api/internal/platform/cache"
)

// CachedRepository decorates an automationdom.Repository with a Redis-backed
// cache for LoadGraph — the hot read on every graph-walk step (see
// worker.AutomationConsumer) — invalidated whenever a node or edge is
// created, updated, or deleted through this same decorator. All other
// methods are delegated directly to the underlying repository.
//
// A single CachedRepository instance is shared between automationsvc.Service
// (HTTP writes) and worker.AutomationConsumer (automation reads) — see
// bootstrap wiring — so a node/edge edited via the API is visible to the
// very next automation event, not just once some TTL expires.
//
// Cache errors are non-fatal: on a read error the decorator falls through
// to the real repository; on a write/delete error it logs and continues so
// mutations always succeed even when the cache is temporarily unavailable.
type CachedRepository struct {
	repo automationdom.Repository
	st   *cache.Store
	ttl  time.Duration
	log  *slog.Logger
}

// NewCachedRepository wraps repo with a caching layer backed by st.
// ttl controls how long a cached graph lives; zero disables caching.
func NewCachedRepository(repo automationdom.Repository, st *cache.Store, ttl time.Duration, log *slog.Logger) *CachedRepository {
	return &CachedRepository{repo: repo, st: st, ttl: ttl, log: log}
}

func graphKey(automationID uuid.UUID) string {
	return fmt.Sprintf("automation:%s:graph", automationID)
}

// LoadGraph returns automationID's full node/edge set, reading from cache
// when available and populating it on a miss.
func (c *CachedRepository) LoadGraph(ctx context.Context, automationID uuid.UUID) (*automationdom.Graph, error) {
	if c.ttl == 0 {
		return c.repo.LoadGraph(ctx, automationID)
	}
	key := graphKey(automationID)
	var result automationdom.Graph
	if ok, err := c.st.Get(ctx, key, &result); ok {
		return &result, nil
	} else if err != nil {
		c.log.WarnContext(ctx, "cache: LoadGraph get", "err", err)
	}

	graph, err := c.repo.LoadGraph(ctx, automationID)
	if err != nil {
		return nil, err
	}
	if err := c.st.Set(ctx, key, graph, c.ttl); err != nil {
		c.log.WarnContext(ctx, "cache: LoadGraph set", "err", err)
	}
	return graph, nil
}

func (c *CachedRepository) invalidateGraph(ctx context.Context, automationID uuid.UUID) {
	if c.ttl == 0 {
		return
	}
	if err := c.st.Delete(ctx, graphKey(automationID)); err != nil {
		c.log.WarnContext(ctx, "cache: invalidate graph", "err", err)
	}
}

// --- Nodes (invalidate on write) ---------------------------------------------

// CreateNode creates n and invalidates its automation's cached graph.
func (c *CachedRepository) CreateNode(ctx context.Context, n *automationdom.Node) error {
	if err := c.repo.CreateNode(ctx, n); err != nil {
		return err
	}
	c.invalidateGraph(ctx, n.AutomationID)
	return nil
}

// UpdateNode updates n and invalidates its automation's cached graph.
func (c *CachedRepository) UpdateNode(ctx context.Context, n *automationdom.Node) error {
	if err := c.repo.UpdateNode(ctx, n); err != nil {
		return err
	}
	c.invalidateGraph(ctx, n.AutomationID)
	return nil
}

// DeleteNode looks up the node first (the Repository interface only takes an
// ID) so it knows which automation's graph cache to invalidate.
func (c *CachedRepository) DeleteNode(ctx context.Context, id uuid.UUID) error {
	n, err := c.repo.FindNodeByID(ctx, id)
	if err != nil {
		return err
	}
	if err := c.repo.DeleteNode(ctx, id); err != nil {
		return err
	}
	c.invalidateGraph(ctx, n.AutomationID)
	return nil
}

// FindNodeByID delegates to the underlying repository.
func (c *CachedRepository) FindNodeByID(ctx context.Context, id uuid.UUID) (*automationdom.Node, error) {
	return c.repo.FindNodeByID(ctx, id)
}

// ListNodesByAutomation delegates to the underlying repository.
func (c *CachedRepository) ListNodesByAutomation(ctx context.Context, automationID uuid.UUID) ([]*automationdom.Node, error) {
	return c.repo.ListNodesByAutomation(ctx, automationID)
}

// --- Edges (invalidate on write) ---------------------------------------------

// CreateEdge creates e and invalidates its automation's cached graph.
func (c *CachedRepository) CreateEdge(ctx context.Context, e *automationdom.Edge) error {
	if err := c.repo.CreateEdge(ctx, e); err != nil {
		return err
	}
	c.invalidateGraph(ctx, e.AutomationID)
	return nil
}

// DeleteEdge looks up the edge first (the Repository interface only takes an
// ID) so it knows which automation's graph cache to invalidate.
func (c *CachedRepository) DeleteEdge(ctx context.Context, id uuid.UUID) error {
	e, err := c.repo.FindEdgeByID(ctx, id)
	if err != nil {
		return err
	}
	if err := c.repo.DeleteEdge(ctx, id); err != nil {
		return err
	}
	c.invalidateGraph(ctx, e.AutomationID)
	return nil
}

// FindEdgeByID delegates to the underlying repository.
func (c *CachedRepository) FindEdgeByID(ctx context.Context, id uuid.UUID) (*automationdom.Edge, error) {
	return c.repo.FindEdgeByID(ctx, id)
}

// ListEdgesByAutomation delegates to the underlying repository.
func (c *CachedRepository) ListEdgesByAutomation(ctx context.Context, automationID uuid.UUID) ([]*automationdom.Edge, error) {
	return c.repo.ListEdgesByAutomation(ctx, automationID)
}

// --- Everything else (pass-through) ------------------------------------------

// CreateAutomation delegates to the underlying repository.
func (c *CachedRepository) CreateAutomation(ctx context.Context, a *automationdom.Automation) error {
	return c.repo.CreateAutomation(ctx, a)
}

// FindAutomationByID delegates to the underlying repository.
func (c *CachedRepository) FindAutomationByID(ctx context.Context, id uuid.UUID) (*automationdom.Automation, error) {
	return c.repo.FindAutomationByID(ctx, id)
}

// ListAutomations delegates to the underlying repository.
func (c *CachedRepository) ListAutomations(ctx context.Context, projectID uuid.UUID, status *automationdom.Status) ([]*automationdom.Automation, error) {
	return c.repo.ListAutomations(ctx, projectID, status)
}

// UpdateAutomation delegates to the underlying repository.
func (c *CachedRepository) UpdateAutomation(ctx context.Context, a *automationdom.Automation) error {
	return c.repo.UpdateAutomation(ctx, a)
}

// DeleteAutomation delegates to the underlying repository.
func (c *CachedRepository) DeleteAutomation(ctx context.Context, id uuid.UUID) error {
	return c.repo.DeleteAutomation(ctx, id)
}

// ListEnabledTriggerNodesByType delegates to the underlying repository.
func (c *CachedRepository) ListEnabledTriggerNodesByType(ctx context.Context, projectID uuid.UUID, triggerType automationdom.TriggerType) ([]*automationdom.Node, error) {
	return c.repo.ListEnabledTriggerNodesByType(ctx, projectID, triggerType)
}

// ListPredecessorTriggersWatching delegates to the underlying repository.
func (c *CachedRepository) ListPredecessorTriggersWatching(ctx context.Context, taskID uuid.UUID) ([]*automationdom.Node, error) {
	return c.repo.ListPredecessorTriggersWatching(ctx, taskID)
}

// ListOutgoingEdges delegates to the underlying repository.
func (c *CachedRepository) ListOutgoingEdges(ctx context.Context, sourceNodeID uuid.UUID) ([]*automationdom.Edge, error) {
	return c.repo.ListOutgoingEdges(ctx, sourceNodeID)
}

// FindAutomationByNodeID delegates to the underlying repository.
func (c *CachedRepository) FindAutomationByNodeID(ctx context.Context, nodeID uuid.UUID) (*automationdom.Automation, error) {
	return c.repo.FindAutomationByNodeID(ctx, nodeID)
}

// CreateRun delegates to the underlying repository.
func (c *CachedRepository) CreateRun(ctx context.Context, r *automationdom.Run) error {
	return c.repo.CreateRun(ctx, r)
}

// UpdateRun delegates to the underlying repository.
func (c *CachedRepository) UpdateRun(ctx context.Context, r *automationdom.Run) error {
	return c.repo.UpdateRun(ctx, r)
}

// ListRunsByAutomation delegates to the underlying repository.
func (c *CachedRepository) ListRunsByAutomation(ctx context.Context, automationID uuid.UUID, limit int) ([]*automationdom.Run, error) {
	return c.repo.ListRunsByAutomation(ctx, automationID, limit)
}

// CreateRunStep delegates to the underlying repository.
func (c *CachedRepository) CreateRunStep(ctx context.Context, s *automationdom.RunStep) error {
	return c.repo.CreateRunStep(ctx, s)
}

// ListRunStepsByRun delegates to the underlying repository.
func (c *CachedRepository) ListRunStepsByRun(ctx context.Context, runID uuid.UUID) ([]*automationdom.RunStep, error) {
	return c.repo.ListRunStepsByRun(ctx, runID)
}

// CreatePendingAgentWait delegates to the underlying repository.
func (c *CachedRepository) CreatePendingAgentWait(ctx context.Context, w *automationdom.PendingAgentWait) error {
	return c.repo.CreatePendingAgentWait(ctx, w)
}

// FindPendingAgentWait delegates to the underlying repository.
func (c *CachedRepository) FindPendingAgentWait(ctx context.Context, conversationID uuid.UUID) (*automationdom.PendingAgentWait, error) {
	return c.repo.FindPendingAgentWait(ctx, conversationID)
}

// DeletePendingAgentWait delegates to the underlying repository.
func (c *CachedRepository) DeletePendingAgentWait(ctx context.Context, id uuid.UUID) error {
	return c.repo.DeletePendingAgentWait(ctx, id)
}

// CountPendingAgentWaits delegates to the underlying repository.
func (c *CachedRepository) CountPendingAgentWaits(ctx context.Context, runID uuid.UUID) (int, error) {
	return c.repo.CountPendingAgentWaits(ctx, runID)
}

// CountPendingAgentWaitsForNode delegates to the underlying repository.
func (c *CachedRepository) CountPendingAgentWaitsForNode(ctx context.Context, runID, nodeID uuid.UUID) (int, error) {
	return c.repo.CountPendingAgentWaitsForNode(ctx, runID, nodeID)
}

// CreatePendingDelay delegates to the underlying repository.
func (c *CachedRepository) CreatePendingDelay(ctx context.Context, d *automationdom.PendingDelay) error {
	return c.repo.CreatePendingDelay(ctx, d)
}

// ListDueDelays delegates to the underlying repository.
func (c *CachedRepository) ListDueDelays(ctx context.Context) ([]*automationdom.PendingDelay, error) {
	return c.repo.ListDueDelays(ctx)
}

// DeletePendingDelay delegates to the underlying repository.
func (c *CachedRepository) DeletePendingDelay(ctx context.Context, id uuid.UUID) error {
	return c.repo.DeletePendingDelay(ctx, id)
}

// CountPendingDelays delegates to the underlying repository.
func (c *CachedRepository) CountPendingDelays(ctx context.Context, runID uuid.UUID) (int, error) {
	return c.repo.CountPendingDelays(ctx, runID)
}

// ListDueDateCandidates delegates to the underlying repository.
func (c *CachedRepository) ListDueDateCandidates(ctx context.Context) ([]automationdom.DueDateCandidate, error) {
	return c.repo.ListDueDateCandidates(ctx)
}

// HasDueDateFired delegates to the underlying repository.
func (c *CachedRepository) HasDueDateFired(ctx context.Context, nodeID, taskID uuid.UUID) (bool, error) {
	return c.repo.HasDueDateFired(ctx, nodeID, taskID)
}

// RecordDueDateFire delegates to the underlying repository.
func (c *CachedRepository) RecordDueDateFire(ctx context.Context, automationID, nodeID, taskID uuid.UUID) error {
	return c.repo.RecordDueDateFire(ctx, automationID, nodeID, taskID)
}

// StatusUsedByAutomation delegates to the underlying repository.
func (c *CachedRepository) StatusUsedByAutomation(ctx context.Context, statusID uuid.UUID) (bool, error) {
	return c.repo.StatusUsedByAutomation(ctx, statusID)
}

// ListCronCandidates delegates to the underlying repository.
func (c *CachedRepository) ListCronCandidates(ctx context.Context) ([]automationdom.CronCandidate, error) {
	return c.repo.ListCronCandidates(ctx)
}

// RecordCronFire delegates to the underlying repository.
func (c *CachedRepository) RecordCronFire(ctx context.Context, automationID, nodeID uuid.UUID, firedAt time.Time) error {
	return c.repo.RecordCronFire(ctx, automationID, nodeID, firedAt)
}

// CreateOrRotateWebhookToken delegates to the underlying repository.
func (c *CachedRepository) CreateOrRotateWebhookToken(ctx context.Context, tok *automationdom.WebhookToken, tokenHash string) (*automationdom.WebhookToken, error) {
	return c.repo.CreateOrRotateWebhookToken(ctx, tok, tokenHash)
}

// FindActiveWebhookTokenByNodeID delegates to the underlying repository.
func (c *CachedRepository) FindActiveWebhookTokenByNodeID(ctx context.Context, nodeID uuid.UUID) (*automationdom.WebhookToken, error) {
	return c.repo.FindActiveWebhookTokenByNodeID(ctx, nodeID)
}

// VerifyWebhookToken delegates to the underlying repository.
func (c *CachedRepository) VerifyWebhookToken(ctx context.Context, nodeID uuid.UUID, tokenHash string) (*automationdom.WebhookToken, error) {
	return c.repo.VerifyWebhookToken(ctx, nodeID, tokenHash)
}

// RecordWebhookTokenUsed delegates to the underlying repository.
func (c *CachedRepository) RecordWebhookTokenUsed(ctx context.Context, tokenID uuid.UUID, at time.Time) error {
	return c.repo.RecordWebhookTokenUsed(ctx, tokenID, at)
}
