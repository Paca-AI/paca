package e2e_test

import (
	"net/http"
	"testing"
)

// TestGlobalAgentVisibility covers what a user without agents:read can learn
// about global agents (GHSA-98xv-pw56-x8xj): GET /agents lists them so they
// can be chatted with, but their configuration and roles stay behind
// agents:read.
func TestGlobalAgentVisibility(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedUser(t, env, "admin-visibility", "supersecret", "admin-visibility")
	assignPlatformRole(t, env, "admin-visibility", "SUPER_ADMIN")
	seedUser(t, env, "plain-visibility", "supersecret", "plain-visibility")
	admin := newLoggedInClient(t, env, "admin-visibility", "supersecret")
	plain := newLoggedInClient(t, env, "plain-visibility", "supersecret")

	status, created := doJSON(t, env, admin, http.MethodPost, "/api/v1/admin/agents", map[string]any{
		"name": "Release Bot", "handle": "release-bot", "description": "Release automation",
		"agent_type": "llm", "llm_provider": "openai", "llm_model": "gpt-4o",
		"llm_api_key": "sk-test", "llm_base_url": "https://llm.internal.example/v1",
		"system_prompt": "internal instructions", "git_committer_email": "bot@example.com",
	})
	if status != http.StatusCreated {
		t.Fatalf("create global agent: want 201, got %d (%s)", status, created.ErrorCode)
	}

	listed := func(t *testing.T, client *http.Client, path string) map[string]any {
		t.Helper()
		status, out := doJSON(t, env, client, http.MethodGet, path+"?search=release-bot", nil)
		if status != http.StatusOK {
			t.Fatalf("GET %s: want 200, got %d (%s)", path, status, out.ErrorCode)
		}
		items, _ := assertDataMap(t, out)["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("GET %s: want the one agent, got %v", path, items)
		}
		agent, _ := items[0].(map[string]any)
		return agent
	}

	t.Run("a_plain_user_sees_only_what_picking_an_agent_needs", func(t *testing.T) {
		agent := listed(t, plain, "/api/v1/agents")
		if agent["name"] != "Release Bot" || agent["handle"] != "release-bot" {
			t.Errorf("agent = %v", agent)
		}
		for _, field := range []string{
			"system_prompt", "llm_model", "llm_base_url", "acp_command",
			"git_committer_name", "git_committer_email", "roles", "created_by",
		} {
			if v, ok := agent[field]; ok {
				t.Errorf("%s is exposed: %v", field, v)
			}
		}
	})

	t.Run("a_plain_user_cannot_read_the_admin_listing", func(t *testing.T) {
		status, _ := doJSON(t, env, plain, http.MethodGet, "/api/v1/admin/agents", nil)
		if status != http.StatusForbidden {
			t.Fatalf("want 403, got %d", status)
		}
	})

	t.Run("the_admin_listing_still_returns_the_configuration", func(t *testing.T) {
		agent := listed(t, admin, "/api/v1/admin/agents")
		if agent["system_prompt"] != "internal instructions" || agent["llm_base_url"] != "https://llm.internal.example/v1" {
			t.Errorf("agent = %v", agent)
		}
	})
}
