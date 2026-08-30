package executor

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/acp"
	"github.com/Paca-AI/agent-runner/internal/agent"
)

func findPacaServer(t *testing.T, servers []acp.MCPServerConfig) acp.MCPServerConfig {
	t.Helper()
	for _, s := range servers {
		if s.Name == "paca" {
			return s
		}
	}
	t.Fatalf("no MCP server named %q in %+v", "paca", servers)
	return acp.MCPServerConfig{}
}

func envValue(env *[]acp.EnvVariable, name string) (string, bool) {
	if env == nil {
		return "", false
	}
	for _, e := range *env {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

func TestBuildMCPServers_SetsRepoPluginIDsWhenPresent(t *testing.T) {
	e := &Executor{opts: Options{PacaAPIKey: "key", PacaAPIURL: "http://api", PacaGatewayURL: "http://gw"}}
	trigger := agent.Trigger{
		ConversationID: uuid.New(),
		AgentID:        uuid.New(),
		RepoPluginIDs:  []string{"com.paca.github", "com.paca.gitlab"},
	}
	cfg := agent.Config{ID: uuid.New()}

	servers := e.buildMCPServers(trigger, cfg)
	paca := findPacaServer(t, servers)

	val, ok := envValue(paca.Env, "PACA_REPO_PLUGIN_IDS")
	if !ok {
		t.Fatalf("PACA_REPO_PLUGIN_IDS not set in paca MCP server env: %+v", paca.Env)
	}
	if val != "com.paca.github,com.paca.gitlab" {
		t.Errorf("PACA_REPO_PLUGIN_IDS = %q, want %q", val, "com.paca.github,com.paca.gitlab")
	}
}

func TestBuildMCPServers_OmitsRepoPluginIDsWhenAbsent(t *testing.T) {
	e := &Executor{opts: Options{PacaAPIKey: "key", PacaAPIURL: "http://api", PacaGatewayURL: "http://gw"}}
	trigger := agent.Trigger{ConversationID: uuid.New(), AgentID: uuid.New()}
	cfg := agent.Config{ID: uuid.New()}

	servers := e.buildMCPServers(trigger, cfg)
	paca := findPacaServer(t, servers)

	if _, ok := envValue(paca.Env, "PACA_REPO_PLUGIN_IDS"); ok {
		t.Errorf("PACA_REPO_PLUGIN_IDS should be omitted when trigger.RepoPluginIDs is empty, env: %+v", paca.Env)
	}
}

// TestBuildMCPServers_OmitsActorUserIDForProjectScopedAgent reproduces the
// real failure: a project-scoped conversation resumed on a persistent
// environment/sandbox somehow carries a non-nil trigger.ActorUserID (its
// origin predates this fix and isn't itself reproduced here), and that
// value gets baked into the sandbox's env at creation and persists across
// every resume. Forwarding it to a project-scoped agent's MCP calls made
// every one of them fail auth with "unable to verify claimed agent
// identity" (401) — including read_conversation, list_tasks, anything —
// because services/api's verifyAgentIdentity rejects an actor claim
// outright for a non-global agent, in required (not optional) auth
// middleware that runs before any handler-specific logic. AgentScope is
// the guard: it must never be forwarded for a project-scoped agent
// regardless of what trigger.ActorUserID holds.
func TestBuildMCPServers_OmitsActorUserIDForProjectScopedAgent(t *testing.T) {
	e := &Executor{opts: Options{PacaAPIKey: "key", PacaAPIURL: "http://api", PacaGatewayURL: "http://gw"}}
	actorUserID := uuid.New()
	trigger := agent.Trigger{
		ConversationID: uuid.New(),
		AgentID:        uuid.New(),
		ProjectID:      uuid.New(),
		ActorUserID:    &actorUserID,
	}
	cfg := agent.Config{ID: uuid.New(), AgentScope: "project"}

	servers := e.buildMCPServers(trigger, cfg)
	paca := findPacaServer(t, servers)

	if val, ok := envValue(paca.Env, "PACA_ACTOR_USER_ID"); ok {
		t.Errorf("PACA_ACTOR_USER_ID must never be set for a project-scoped agent, got %q", val)
	}
}

// TestBuildMCPServers_SetsActorUserIDForGlobalAgent confirms the fix isn't
// overly broad — a genuinely global agent acting on behalf of a human (e.g.
// "create a project for me" from the home-page chat) must still get
// PACA_ACTOR_USER_ID, since that combination is exactly what
// verifyAgentIdentity allows server-side.
func TestBuildMCPServers_SetsActorUserIDForGlobalAgent(t *testing.T) {
	e := &Executor{opts: Options{PacaAPIKey: "key", PacaAPIURL: "http://api", PacaGatewayURL: "http://gw"}}
	actorUserID := uuid.New()
	trigger := agent.Trigger{
		ConversationID: uuid.New(),
		AgentID:        uuid.New(),
		ActorUserID:    &actorUserID,
	}
	cfg := agent.Config{ID: uuid.New(), AgentScope: agent.AgentScopeGlobal}

	servers := e.buildMCPServers(trigger, cfg)
	paca := findPacaServer(t, servers)

	val, ok := envValue(paca.Env, "PACA_ACTOR_USER_ID")
	if !ok {
		t.Fatalf("PACA_ACTOR_USER_ID not set in paca MCP server env for a global agent: %+v", paca.Env)
	}
	if val != actorUserID.String() {
		t.Errorf("PACA_ACTOR_USER_ID = %q, want %q", val, actorUserID.String())
	}
}
