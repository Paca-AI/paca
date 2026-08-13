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
	"net/http"
	"sync"
	"time"

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

// Client fetches and caches the bundled agent-flavor skill set for the
// lifetime of the process — the content is static per deployed API
// version, so there's no reason to re-fetch on every conversation. A
// failed fetch is not cached, so a transient failure (e.g. racing an API
// restart) is retried on the next conversation instead of permanently
// leaving every subsequent one without this scaffolding.
type Client struct {
	baseURL    string
	httpClient *http.Client

	mu    sync.Mutex
	cache []agent.Skill
}

// NewClient returns a Client that fetches from baseURL — services/api's
// PACA_API_URL (e.g. "http://api:8080", no trailing /api/v1).
func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

// Load returns the bundled agent-flavor skill set, each marked IsEnabled so
// executor.buildInitialMessage includes it unconditionally.
func (c *Client) Load(ctx context.Context) ([]agent.Skill, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache != nil {
		return c.cache, nil
	}

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
	c.cache = skills
	return skills, nil
}
