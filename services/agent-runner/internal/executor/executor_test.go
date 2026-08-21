package executor

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/acp"
	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/secret"
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

func TestAuthoritativePrivateTurnExcludesAgentCredentialsAndMCP(t *testing.T) {
	encryptor, err := secret.NewEncryptor(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	encrypt := func(value string) string {
		t.Helper()
		ciphertext, encryptErr := encryptor.Encrypt(value)
		if encryptErr != nil {
			t.Fatal(encryptErr)
		}
		return ciphertext
	}

	e := &Executor{
		encryptor: encryptor,
		opts: Options{
			PacaAPIKey:     "long-lived-paca-key",
			PacaAPIURL:     "http://api",
			PacaGatewayURL: "http://gateway",
		},
	}
	turnID := uuid.New()
	trigger := agent.Trigger{
		ConversationID: uuid.New(),
		AgentID:        uuid.New(),
		TurnID:         &turnID,
		RepoPluginIDs:  []string{"com.paca.github"},
	}
	cfg := agent.Config{
		ID:              uuid.New(),
		LLMProvider:     "openai",
		LLMModel:        "gpt-test",
		LLMAPIKeySecret: encrypt("provider-key"),
		EnvVars: []agent.EnvVar{
			{Key: "PACA_API_KEY", EncryptedValue: encrypt("agent-paca-key")},
			{Key: "USER_SECRET", EncryptedValue: encrypt("agent-secret")},
		},
		MCPServers: []agent.MCPServer{{
			ServerName: "user-mcp",
			Transport:  "stdio",
			Command:    "/usr/bin/user-mcp",
			Env:        map[string]string{"MCP_SECRET": "mcp-secret"},
			IsEnabled:  true,
		}},
	}

	env, servers, err := e.buildContainerEnvironment(trigger, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 0 {
		t.Fatalf("authoritative private turn received MCP servers: %+v", servers)
	}
	for _, forbidden := range []string{"PACA_API_KEY", "PACA_API_URL", "PACA_GATEWAY_URL", "PACA_REPO_PLUGIN_IDS", "USER_SECRET", "MCP_SECRET"} {
		if _, ok := env[forbidden]; ok {
			t.Errorf("authoritative private turn received forbidden environment variable %q", forbidden)
		}
	}
	if env["OPENAI_API_KEY"] != "provider-key" || env["GOOSE_PROVIDER"] != "openai" || env["GOOSE_MODEL"] != "gpt-test" {
		t.Fatalf("required provider environment missing or changed: %+v", env)
	}
}
