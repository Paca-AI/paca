package automationsvc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	"github.com/Paca-AI/api/internal/platform/cache"
	"github.com/google/uuid"
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

func (c *CachedRepository) CreateNode(ctx context.Context, n *automationdom.Node) error {
	if err := c.repo.CreateNode(ctx, n); err != nil {
		return err
	}
	c.invalidateGraph(ctx, n.AutomationID)
	return nil
}

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

func (c *CachedRepository) FindNodeByID(ctx context.Context, id uuid.UUID) (*automationdom.Node, error) {
	return c.repo.FindNodeByID(ctx, id)
}

func (c *CachedRepository) ListNodesByAutomation(ctx context.Context, automationID uuid.UUID) ([]*automationdom.Node, error) {
	return c.repo.ListNodesByAutomation(ctx, automationID)
}

// --- Edges (invalidate on write) ---------------------------------------------

func (c *CachedRepository) CreateEdge(ctx context.Context, e *automationdom.Edge) error {
	if err := c.repo.CreateEdge(ctx, e); err != nil {
		return err
	}
	c.invalidateGraph(ctx, e.AutomationID)
	return nil
}

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

func (c *CachedRepository) FindEdgeByID(ctx context.Context, id uuid.UUID) (*automationdom.Edge, error) {
	return c.repo.FindEdgeByID(ctx, id)
}

func (c *CachedRepository) ListEdgesByAutomation(ctx context.Context, automationID uuid.UUID) ([]*automationdom.Edge, error) {
	return c.repo.ListEdgesByAutomation(ctx, automationID)
}

// --- Everything else (pass-through) ------------------------------------------

func (c *CachedRepository) CreateAutomation(ctx context.Context, a *automationdom.Automation) error {
	return c.repo.CreateAutomation(ctx, a)
}

func (c *CachedRepository) FindAutomationByID(ctx context.Context, id uuid.UUID) (*automationdom.Automation, error) {
	return c.repo.FindAutomationByID(ctx, id)
}

func (c *CachedRepository) ListAutomations(ctx context.Context, projectID uuid.UUID, status *automationdom.Status) ([]*automationdom.Automation, error) {
	return c.repo.ListAutomations(ctx, projectID, status)
}

func (c *CachedRepository) UpdateAutomation(ctx context.Context, a *automationdom.Automation) error {
	return c.repo.UpdateAutomation(ctx, a)
}

func (c *CachedRepository) DeleteAutomation(ctx context.Context, id uuid.UUID) error {
	return c.repo.DeleteAutomation(ctx, id)
}

func (c *CachedRepository) ListEnabledTriggerNodesByType(ctx context.Context, projectID uuid.UUID, triggerType automationdom.TriggerType) ([]*automationdom.Node, error) {
	return c.repo.ListEnabledTriggerNodesByType(ctx, projectID, triggerType)
}

func (c *CachedRepository) ListPredecessorTriggersWatching(ctx context.Context, taskID uuid.UUID) ([]*automationdom.Node, error) {
	return c.repo.ListPredecessorTriggersWatching(ctx, taskID)
}

func (c *CachedRepository) ListOutgoingEdges(ctx context.Context, sourceNodeID uuid.UUID) ([]*automationdom.Edge, error) {
	return c.repo.ListOutgoingEdges(ctx, sourceNodeID)
}

func (c *CachedRepository) FindAutomationByNodeID(ctx context.Context, nodeID uuid.UUID) (*automationdom.Automation, error) {
	return c.repo.FindAutomationByNodeID(ctx, nodeID)
}

func (c *CachedRepository) CreateRun(ctx context.Context, r *automationdom.Run) error {
	return c.repo.CreateRun(ctx, r)
}

func (c *CachedRepository) UpdateRun(ctx context.Context, r *automationdom.Run) error {
	return c.repo.UpdateRun(ctx, r)
}

func (c *CachedRepository) ListRunsByAutomation(ctx context.Context, automationID uuid.UUID, limit int) ([]*automationdom.Run, error) {
	return c.repo.ListRunsByAutomation(ctx, automationID, limit)
}

func (c *CachedRepository) CreateRunStep(ctx context.Context, s *automationdom.RunStep) error {
	return c.repo.CreateRunStep(ctx, s)
}

func (c *CachedRepository) ListRunStepsByRun(ctx context.Context, runID uuid.UUID) ([]*automationdom.RunStep, error) {
	return c.repo.ListRunStepsByRun(ctx, runID)
}

func (c *CachedRepository) ListDueDateCandidates(ctx context.Context) ([]automationdom.DueDateCandidate, error) {
	return c.repo.ListDueDateCandidates(ctx)
}

func (c *CachedRepository) HasDueDateFired(ctx context.Context, nodeID, taskID uuid.UUID) (bool, error) {
	return c.repo.HasDueDateFired(ctx, nodeID, taskID)
}

func (c *CachedRepository) RecordDueDateFire(ctx context.Context, automationID, nodeID, taskID uuid.UUID) error {
	return c.repo.RecordDueDateFire(ctx, automationID, nodeID, taskID)
}

func (c *CachedRepository) StatusUsedByAutomation(ctx context.Context, statusID uuid.UUID) (bool, error) {
	return c.repo.StatusUsedByAutomation(ctx, statusID)
}

func (c *CachedRepository) ListCronCandidates(ctx context.Context) ([]automationdom.CronCandidate, error) {
	return c.repo.ListCronCandidates(ctx)
}

func (c *CachedRepository) RecordCronFire(ctx context.Context, automationID, nodeID uuid.UUID, firedAt time.Time) error {
	return c.repo.RecordCronFire(ctx, automationID, nodeID, firedAt)
}

func (c *CachedRepository) CreateOrRotateWebhookToken(ctx context.Context, tok *automationdom.WebhookToken, tokenHash string) (*automationdom.WebhookToken, error) {
	return c.repo.CreateOrRotateWebhookToken(ctx, tok, tokenHash)
}

func (c *CachedRepository) FindActiveWebhookTokenByNodeID(ctx context.Context, nodeID uuid.UUID) (*automationdom.WebhookToken, error) {
	return c.repo.FindActiveWebhookTokenByNodeID(ctx, nodeID)
}

func (c *CachedRepository) VerifyWebhookToken(ctx context.Context, nodeID uuid.UUID, tokenHash string) (*automationdom.WebhookToken, error) {
	return c.repo.VerifyWebhookToken(ctx, nodeID, tokenHash)
}

func (c *CachedRepository) RecordWebhookTokenUsed(ctx context.Context, tokenID uuid.UUID, at time.Time) error {
	return c.repo.RecordWebhookTokenUsed(ctx, tokenID, at)
}
