package executor

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/acp"
	"github.com/Paca-AI/agent-runner/internal/agent"
)

func TestBuildMCPServers_HTTPResearchIsOptIn(t *testing.T) {
	e := &Executor{opts: Options{PacaAPIKey: "key", PacaAPIURL: "http://api"}}
	trigger := agent.Trigger{ConversationID: uuid.New(), AgentID: uuid.New()}
	cfg := agent.Config{ID: uuid.New(), AgentType: agent.AgentTypeLLM}
	if servers := e.buildMCPServers(trigger, cfg); len(servers) != 1 || servers[0].Name != "paca" {
		t.Fatalf("unconfigured agent should have only Paca: %+v", servers)
	}

	cfg.MCPServers = []agent.MCPServer{
		{ServerName: "existing", Transport: "stdio", Command: "npx", Args: []string{"existing-mcp"}, Env: map[string]string{"KEEP": "value"}, IsEnabled: true},
		{ServerName: "parallel-search", Transport: "http", URL: "https://search.parallel.ai/mcp", IsEnabled: true},
		{ServerName: "disabled", Transport: "http", URL: "https://example.com/mcp", IsEnabled: false},
	}
	servers := e.buildMCPServers(trigger, cfg)
	if len(servers) != 3 || servers[0].Name != "existing" || servers[1].Name != "parallel-search" || servers[2].Name != "paca" {
		t.Fatalf("configured server order/filtering changed: %+v", servers)
	}
	if servers[0].Command != "npx" || servers[0].Args == nil || (*servers[0].Args)[0] != "existing-mcp" {
		t.Fatalf("existing stdio command changed: %+v", servers[0])
	}
	if value, ok := envValue(servers[0].Env, "KEEP"); !ok || value != "value" {
		t.Fatal("existing stdio environment changed")
	}
	// Assert the serialized caller arguments, not only the in-memory header.
	wire, err := json.Marshal(servers[1])
	if err != nil {
		t.Fatal(err)
	}
	var remote struct {
		Type    string           `json:"type"`
		URL     string           `json:"url"`
		Headers []acp.HTTPHeader `json:"headers"`
	}
	if err := json.Unmarshal(wire, &remote); err != nil {
		t.Fatal(err)
	}
	if remote.Type != "http" || remote.URL != "https://search.parallel.ai/mcp" || len(remote.Headers) != 1 || remote.Headers[0].Name != "User-Agent" || remote.Headers[0].Value != "Paca/agent-runner (+https://github.com/Paca-AI/paca)" {
		t.Fatalf("incorrect HTTP MCP caller configuration: %s", wire)
	}
	cfg.MCPServers[1].IsEnabled = false
	if servers := e.buildMCPServers(trigger, cfg); len(servers) != 2 || servers[1].Name != "paca" {
		t.Fatalf("disabled research server still loaded: %+v", servers)
	}
	cfg.AgentType = agent.AgentTypeProviderCLI
	if servers := e.buildMCPServers(trigger, cfg); servers != nil {
		t.Fatalf("provider CLI must retain its separate configuration path: %+v", servers)
	}
}

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
// unlike ProjectID/ActorUserID) — services/api's GetConversationForAgent
// relies on it being present to authorize a read_conversation call against
// the calling conversation's own project/actor/owning member; see that
// method's doc comment.
func TestBuildMCPServers_AlwaysSetsConversationID(t *testing.T) {
	e := &Executor{opts: Options{PacaAPIKey: "key", PacaAPIURL: "http://api", PacaGatewayURL: "http://gw"}}
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
