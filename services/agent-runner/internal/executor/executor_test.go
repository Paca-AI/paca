package executor

import (
	"context"
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

// TestBuildMCPServers_AlwaysSetsConversationID pins that PACA_CONVERSATION_ID
// is forwarded unconditionally (trigger.ConversationID is always set,
// unlike ProjectID/ActorAccountID).
func TestBuildMCPServers_AlwaysSetsConversationID(t *testing.T) {
	e := &Executor{opts: Options{AgentKeys: fakeAgentKeys{}, PacaAPIURL: "http://api", PacaGatewayURL: "http://gw"}}
	conversationID := uuid.New()
	trigger := agent.Trigger{ConversationID: conversationID, AgentID: uuid.New()}
	cfg := agent.Config{ID: uuid.New()}

	servers := e.buildMCPServers(trigger, cfg)
	paca := findPacaServer(t, servers)

	val, ok := envValue(paca.Env, "PACA_CONVERSATION_ID")
	if !ok {
		t.Fatalf("PACA_CONVERSATION_ID not set in paca MCP server env: %+v", paca.Env)
	}
	if val != conversationID.String() {
		t.Errorf("PACA_CONVERSATION_ID = %q, want %q", val, conversationID.String())
	}
}

func TestBuildMCPServers_SetsOwnerMemberIDWhenPresent(t *testing.T) {
	e := &Executor{opts: Options{AgentKeys: fakeAgentKeys{}, PacaAPIURL: "http://api", PacaGatewayURL: "http://gw"}}
	owner := uuid.New()
	cfg := agent.Config{ID: uuid.New()}

	with := findPacaServer(t, e.buildMCPServers(agent.Trigger{ConversationID: uuid.New(), AgentID: uuid.New(), ActorMemberID: &owner}, cfg))
	if val, ok := envValue(with.Env, "PACA_OWNER_MEMBER_ID"); !ok || val != owner.String() {
		t.Errorf("PACA_OWNER_MEMBER_ID = %q (set=%v), want %q", val, ok, owner.String())
	}

	without := findPacaServer(t, e.buildMCPServers(agent.Trigger{ConversationID: uuid.New(), AgentID: uuid.New()}, cfg))
	if _, ok := envValue(without.Env, "PACA_OWNER_MEMBER_ID"); ok {
		t.Error("PACA_OWNER_MEMBER_ID must be absent when the run has no actor member")
	}
}

func TestBuildMCPServers_SetsRepoPluginIDsWhenPresent(t *testing.T) {
	e := &Executor{opts: Options{AgentKeys: fakeAgentKeys{}, PacaAPIURL: "http://api", PacaGatewayURL: "http://gw"}}
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
	e := &Executor{opts: Options{AgentKeys: fakeAgentKeys{}, PacaAPIURL: "http://api", PacaGatewayURL: "http://gw"}}
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
// environment/sandbox somehow carries a non-nil trigger.ActorAccountID (its
// origin predates this fix and isn't itself reproduced here), and that
// value gets baked into the sandbox's env at creation and persists across
// every resume. Forwarding it to a project-scoped agent's MCP calls made
// every one of them fail auth with "unable to verify claimed agent
// identity" (401) — including read_conversation, list_tasks, anything —
// because services/api's verifyAgentIdentity rejects an actor claim
// outright for a non-global agent, in required (not optional) auth
// middleware that runs before any handler-specific logic. AgentScope is
// the guard: it must never be forwarded for a project-scoped agent
// regardless of what trigger.ActorAccountID holds.
func TestBuildMCPServers_NeverForwardsAnActorUser(t *testing.T) {
	e := &Executor{opts: Options{AgentKeys: fakeAgentKeys{}, PacaAPIURL: "http://api", PacaGatewayURL: "http://gw"}}
	actorAccountID := uuid.New()
	trigger := agent.Trigger{ConversationID: uuid.New(), AgentID: uuid.New(), ActorAccountID: &actorAccountID}
	for _, scope := range []string{"project", agent.AgentScopeGlobal} {
		cfg := agent.Config{ID: uuid.New(), AgentScope: scope}
		paca := findPacaServer(t, e.buildMCPServers(trigger, cfg))
		if val, ok := envValue(paca.Env, "PACA_ACTOR_USER_ID"); ok {
			t.Errorf("%s agent: an agent acts with its own permissions only, but PACA_ACTOR_USER_ID = %q", scope, val)
		}
	}
}

// The agent's MCP server authenticates with the key issued for that agent.
func TestBuildMCPServers_UsesTheAgentsOwnKey(t *testing.T) {
	e := &Executor{opts: Options{AgentKeys: fakeAgentKeys{}, PacaAPIURL: "http://api", PacaGatewayURL: "http://gw"}}
	cfg := agent.Config{ID: uuid.New(), AgentScope: "project"}
	paca := findPacaServer(t, e.buildMCPServers(agent.Trigger{ConversationID: uuid.New(), AgentID: cfg.ID}, cfg))
	if val, _ := envValue(paca.Env, "PACA_API_KEY"); val != "paca_issued-key" {
		t.Errorf("PACA_API_KEY = %q", val)
	}
}

// fakeAgentKeys is an AgentKeyIssuer returning a fixed key.
type fakeAgentKeys struct{}

func (fakeAgentKeys) APIKeyFor(context.Context, uuid.UUID) (string, error) {
	return "paca_issued-key", nil
}
