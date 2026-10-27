// Package bundledskills fetches Paca's bundled "agent"-flavor skill set
// (services/api's GET /api/v1/skills?target=agent) — the scaffolding that
// teaches every llm-type agent how to route a trigger, and for task work,
// how to discover and clone its project's linked repository via the Paca
// MCP server's list_repositories/clone_repository tools (see
// services/api/internal/platform/bundledskills's `paca` and `paca-do`
// skills). Every conversation gets this regardless of what's in
// agent_skills, mirroring services/ai-agent's builder.load_default_skills()
// from before the Python->Go migration — an agent's own agent_skills rows
// are opt-in customizations layered on top, never a replacement for it.
package bundledskills

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

type skillsEnvelope struct {
	Data struct {
		Skills []struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"skills"`
	} `json:"data"`
}

// cacheTTL bounds how long a fetched skill set is reused. The set is not
// static for the life of the process: it includes skills (and `paca` router
// rows) contributed by plugins, which can be installed, enabled or disabled at
// any time, and that must take effect on a following conversation. A short TTL
// keeps that promise without an API call per conversation.
const cacheTTL = 30 * time.Second

// Client fetches the agent-flavor skill set — Paca's bundled skills plus those
// of enabled plugins — and caches it for cacheTTL. A failed fetch is never
// cached as a result: it is retried after the TTL elapses, and if an earlier fetch
// succeeded the last good set is served meanwhile, so a transient failure
// (e.g. racing an API restart) neither leaves a conversation without this
// scaffolding nor pins an empty result.
type Client struct {
	baseURL    string
	httpClient *http.Client
	now        func() time.Time

	mu        sync.Mutex
	cache     []agent.Skill
	fetchedAt time.Time
	// group collapses concurrent refreshes into one request, so conversations
	// starting at TTL expiry neither pile up behind a lock nor each fetch.
	group singleflight.Group
}

// NewClient returns a Client that fetches from baseURL — services/api's
// PACA_API_URL (e.g. "http://api:8080", no trailing /api/v1).
func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, httpClient: &http.Client{Timeout: 10 * time.Second}, now: time.Now}
}

// Load returns the agent-flavor skill set, each marked IsEnabled so
// executor.buildInitialMessage includes it unconditionally. The returned slice
// is a copy; callers may modify it freely.
func (c *Client) Load(ctx context.Context) ([]agent.Skill, error) {
	if skills, ok := c.fresh(); ok {
		return skills, nil
	}
	// The fetch runs detached from any single caller's ctx so one cancelled
	// conversation cannot fail the refresh for the others; each caller still
	// stops waiting when its own ctx is done.
	ch := c.group.DoChan("skills", func() (any, error) {
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.httpClient.Timeout)
		defer cancel()
		return c.refresh(fetchCtx)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return cloneSkills(res.Val.([]agent.Skill)), nil
	}
}

func (c *Client) fresh() ([]agent.Skill, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache != nil && c.now().Sub(c.fetchedAt) < cacheTTL {
		return cloneSkills(c.cache), true
	}
	return nil, false
}

func (c *Client) refresh(ctx context.Context) ([]agent.Skill, error) {
	skills, err := c.fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		if c.cache != nil {
			// Stale beats nothing. Restart the TTL so a sustained outage costs
			// one attempt per cacheTTL, not one on every conversation.
			slog.Warn("bundledskills: refresh failed, serving stale skill set", "error", err)
			c.fetchedAt = c.now()
			return c.cache, nil
		}
		return nil, err
	}
	c.cache, c.fetchedAt = skills, c.now()
	return skills, nil
}

func cloneSkills(in []agent.Skill) []agent.Skill { return append([]agent.Skill(nil), in...) }

func (c *Client) fetch(ctx context.Context) ([]agent.Skill, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/skills?target=agent", nil)
	if err != nil {
		return nil, fmt.Errorf("bundledskills: build request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bundledskills: fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bundledskills: fetch: unexpected status %d", resp.StatusCode)
	}

	var env skillsEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("bundledskills: decode response: %w", err)
	}

	skills := make([]agent.Skill, 0, len(env.Data.Skills))
	for _, s := range env.Data.Skills {
		skills = append(skills, agent.Skill{
			SkillName:    s.Name,
			SkillContent: s.Content,
			IsEnabled:    true,
		})
	}
	return skills, nil
}
