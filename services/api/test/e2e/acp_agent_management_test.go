package e2e_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// seedACPUser creates a user, a project (the creator becomes the project's
// "Admin" role — full permissions, including agents.*, so no extra project
// role wiring is needed to exercise the agent endpoints below), and returns
// everything a test needs to drive the HTTP API plus the "Editor" role id
// (which has agents.read/write) used as new agents' own project_role_id.
func seedACPUser(t *testing.T, env *e2eEnv) (client *http.Client, token, projectID, editorRoleID string) {
	t.Helper()
	username := "acp-user-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "acp-test-pass-1")
	client, token = taskMemberLogin(t, env, username, "acp-test-pass-1")
	projectID = createProjectForTasksViaAPI(t, env, client, token)
	editorRoleID = projectRoleIDByName(t, env, client, token, projectID, "Editor")
	return client, token, projectID, editorRoleID
}

// projectRoleIDByName looks up a project role's id by its role_name (the
// three built-in roles — Admin/Editor/Viewer — are created alongside every
// project, see project_service.go's CreateProject).
func projectRoleIDByName(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, roleName string) string {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/roles", env.base, projectID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var e envelope
	decodeJSON(t, resp, &e)
	roles, ok := e.Data.([]any)
	if !ok {
		t.Fatalf("expected roles array, got %T", e.Data)
	}
	for _, r := range roles {
		role, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if role["role_name"] == roleName {
			id, _ := role["id"].(string)
			if id != "" {
				return id
			}
		}
	}
	t.Fatalf("project role %q not found", roleName)
	return ""
}

// acpAgentBody returns a minimal valid create-agent body for an ACP agent
// with the given handle, plus any overrides.
func acpAgentBody(roleID, handle string, overrides map[string]any) map[string]any {
	base := map[string]any{
		"name":            "ACP Agent " + handle,
		"handle":          handle,
		"agent_type":      "acp",
		"acp_provider":    "claude-code",
		"project_role_id": roleID,
	}
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

// llmAgentBody returns a minimal valid create-agent body for an LLM agent
// (agent_type defaults to "llm" when omitted) with the given handle.
func llmAgentBody(roleID, handle string, overrides map[string]any) map[string]any {
	base := map[string]any{
		"name":            "LLM Agent " + handle,
		"handle":          handle,
		"llm_provider":    "openai",
		"llm_model":       "gpt-4",
		"llm_api_key":     "sk-test",
		"project_role_id": roleID,
	}
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

// createAgentRequest POSTs a create-agent request and returns the response
// status and decoded envelope, without asserting on either — callers check
// both success and validation-failure paths.
func createAgentRequest(t *testing.T, env *e2eEnv, client *http.Client, token, projectID string, body map[string]any) (int, envelope) {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/agents", env.base, projectID), jsonBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	var e envelope
	decodeJSON(t, resp, &e)
	return resp.StatusCode, e
}

// getAgentRequest GETs a single agent and returns the response status and
// decoded envelope.
func getAgentRequest(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, agentID string) (int, envelope) {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/agents/%s", env.base, projectID, agentID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	var e envelope
	decodeJSON(t, resp, &e)
	return resp.StatusCode, e
}

// patchAgentRequest PATCHes an agent and returns the response status and
// decoded envelope.
func patchAgentRequest(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, agentID string, body map[string]any) (int, envelope) {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodPatch,
		fmt.Sprintf("%s/api/v1/projects/%s/agents/%s", env.base, projectID, agentID), jsonBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	var e envelope
	decodeJSON(t, resp, &e)
	return resp.StatusCode, e
}

// generateBridgeTokenRequest POSTs to the acp-bridge-token endpoint and
// returns the response status, the plaintext token, and the run_command.
func generateBridgeTokenRequest(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, agentID string) (int, string, string) {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/agents/%s/acp-bridge-token", env.base, projectID, agentID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	var e envelope
	decodeJSON(t, resp, &e)
	data, _ := e.Data.(map[string]any)
	tok, _ := data["token"].(string)
	runCommand, _ := data["run_command"].(string)
	return resp.StatusCode, tok, runCommand
}

// bridgeTokenHash queries the raw acp_bridge_token_hash column for agentID.
func bridgeTokenHash(t *testing.T, env *e2eEnv, agentID string) string {
	t.Helper()
	var hash *string
	if err := env.db.GetContext(env.ctx, &hash,
		"SELECT acp_bridge_token_hash FROM agents WHERE id = $1", agentID); err != nil {
		t.Fatalf("query acp_bridge_token_hash: %v", err)
	}
	if hash == nil {
		return ""
	}
	return *hash
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// newAgentAPIServer stands up a fresh httptest.Server sharing env's
// Postgres-backed agent service but wired with a specific aiAgentURL /
// internal key — used to test the GetACPBridgeStatus proxy against a fake
// ai-agent server, which the shared env's Agent handler (aiAgentURL == "")
// deliberately can't reach. Auth tokens minted against the shared env's
// router work here too: both routers share the same JWT secret and the same
// Postgres-backed Authorizer/permission state.
func newAgentAPIServer(t *testing.T, env *e2eEnv, aiAgentURL, aiAgentInternalKey string) string {
	t.Helper()
	agentHandler := handler.NewAgentHandler(env.agentSvc, aiAgentURL, aiAgentInternalKey, "").
		WithMemberRepo(env.projectRepo)
	engine := router.New(router.Deps{
		TokenManager: jwttoken.New(e2eJWTSecret, e2eAccessTTL, e2eRefreshTTL),
		Authorizer:   authz.NewAuthorizer(pgRepo.NewAuthzPermissionStore(env.db)),
		Health:       handler.NewHealthHandler(),
		Agent:        agentHandler,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return srv.URL
}

// newFakeAIAgentBridgeStatusServer fakes just enough of services/ai-agent's
// GET /agent-bridge/status/:agentId (see routes/bridge.py) to test the Go
// API's proxy: it checks the internal-auth header and returns a
// per-agent-id connected bool from statusByAgentID.
func newFakeAIAgentBridgeStatusServer(t *testing.T, internalKey string, statusByAgentID map[string]bool) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/agent-bridge/status/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != internalKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		agentID := strings.TrimPrefix(r.URL.Path, "/agent-bridge/status/")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"connected": statusByAgentID[agentID]})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// ---------------------------------------------------------------------------
// CreateAgent validation
// ---------------------------------------------------------------------------

func TestE2EACPAgent_CreateValidation(t *testing.T) {
	env := newE2EEnv(t)
	client, token, projID, roleID := seedACPUser(t, env)

	t.Run("missing_acp_provider_returns_400", func(t *testing.T) {
		status, e := createAgentRequest(t, env, client, token, projID, map[string]any{
			"name":            "Bad Agent",
			"handle":          "bad-agent-" + uuid.NewString(),
			"agent_type":      "acp",
			"project_role_id": roleID,
		})
		if status != http.StatusBadRequest {
			t.Fatalf("expected 400 for missing acp_provider, got %d: %+v", status, e)
		}
	})

	t.Run("custom_provider_without_command_returns_400", func(t *testing.T) {
		status, e := createAgentRequest(t, env, client, token, projID,
			acpAgentBody(roleID, "bad-custom-"+uuid.NewString(), map[string]any{"acp_provider": "custom"}))
		if status != http.StatusBadRequest {
			t.Fatalf("expected 400 for custom provider without acp_command, got %d: %+v", status, e)
		}
	})

	t.Run("invalid_acp_provider_returns_400", func(t *testing.T) {
		status, e := createAgentRequest(t, env, client, token, projID,
			acpAgentBody(roleID, "bad-provider-"+uuid.NewString(), map[string]any{"acp_provider": "not-a-real-provider"}))
		if status != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid acp_provider, got %d: %+v", status, e)
		}
	})

	t.Run("valid_claude_code_agent_created", func(t *testing.T) {
		handle := "claude-agent-" + uuid.NewString()
		status, e := createAgentRequest(t, env, client, token, projID, acpAgentBody(roleID, handle, nil))
		if status != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %+v", status, e)
		}
		data := assertDataMap(t, e)
		if data["agent_type"] != "acp" {
			t.Errorf("expected agent_type acp, got %v", data["agent_type"])
		}
		if data["acp_provider"] != "claude-code" {
			t.Errorf("expected acp_provider claude-code, got %v", data["acp_provider"])
		}
		if lp, _ := data["llm_provider"].(string); lp != "" {
			t.Errorf("expected empty llm_provider on an ACP agent, got %q", lp)
		}
		if data["has_acp_bridge_token"] != false {
			t.Errorf("expected has_acp_bridge_token false before token generation, got %v", data["has_acp_bridge_token"])
		}
		if _, exists := data["acp_bridge_token_hash"]; exists {
			t.Error("acp_bridge_token_hash must never be serialized in API responses")
		}
	})

	t.Run("valid_custom_agent_roundtrips_acp_command", func(t *testing.T) {
		handle := "custom-agent-" + uuid.NewString()
		status, e := createAgentRequest(t, env, client, token, projID, acpAgentBody(roleID, handle, map[string]any{
			"acp_provider": "custom",
			"acp_command":  []string{"my-server", "--flag"},
		}))
		if status != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %+v", status, e)
		}
		agentID, _ := assertDataMap(t, e)["id"].(string)

		getStatus, getEnv := getAgentRequest(t, env, client, token, projID, agentID)
		if getStatus != http.StatusOK {
			t.Fatalf("expected 200 on GET, got %d", getStatus)
		}
		cmd, ok := assertDataMap(t, getEnv)["acp_command"].([]any)
		if !ok || len(cmd) != 2 || cmd[0] != "my-server" || cmd[1] != "--flag" {
			t.Fatalf("expected acp_command [my-server --flag], got %v", assertDataMap(t, getEnv)["acp_command"])
		}
	})
}

// TestE2EACPAgent_LLMBaseURLNotRequired guards against a regression of the
// fix that stopped rejecting llm_base_url == "" — the agents.llm_base_url
// column defaults to ” and several LLM providers resolve their own default,
// so requiring it here would reject otherwise-valid requests.
func TestE2EACPAgent_LLMBaseURLNotRequired(t *testing.T) {
	env := newE2EEnv(t)
	client, token, projID, roleID := seedACPUser(t, env)

	status, e := createAgentRequest(t, env, client, token, projID,
		llmAgentBody(roleID, "llm-agent-"+uuid.NewString(), map[string]any{"llm_base_url": ""}))
	if status != http.StatusCreated {
		t.Fatalf("expected 201 for empty llm_base_url, got %d: %+v", status, e)
	}
}

// ---------------------------------------------------------------------------
// UpdateAgent: LLM/ACP field isolation by agent_type
// ---------------------------------------------------------------------------

func TestE2EACPAgent_UpdateDoesNotCrossContaminateFields(t *testing.T) {
	env := newE2EEnv(t)
	client, token, projID, roleID := seedACPUser(t, env)

	t.Run("acp_agent_ignores_llm_fields", func(t *testing.T) {
		handle := "acp-update-" + uuid.NewString()
		_, createEnv := createAgentRequest(t, env, client, token, projID, acpAgentBody(roleID, handle, nil))
		agentID, _ := assertDataMap(t, createEnv)["id"].(string)

		patchStatus, _ := patchAgentRequest(t, env, client, token, projID, agentID, map[string]any{
			"llm_provider": "openai",
			"llm_model":    "gpt-4",
			"llm_api_key":  "sk-should-not-be-stored",
			"llm_base_url": "https://api.openai.com/v1",
		})
		if patchStatus != http.StatusOK {
			t.Fatalf("expected 200 patching an ACP agent with LLM fields, got %d", patchStatus)
		}

		getStatus, getEnv := getAgentRequest(t, env, client, token, projID, agentID)
		if getStatus != http.StatusOK {
			t.Fatalf("expected 200 on GET, got %d", getStatus)
		}
		data := assertDataMap(t, getEnv)
		if data["agent_type"] != "acp" {
			t.Errorf("expected agent_type to remain acp, got %v", data["agent_type"])
		}
		if lm, _ := data["llm_model"].(string); lm != "" {
			t.Errorf("expected llm_model to stay empty on an ACP agent, got %q", lm)
		}
		if lb, _ := data["llm_base_url"].(string); lb != "" {
			t.Errorf("expected llm_base_url to stay empty on an ACP agent, got %q", lb)
		}
	})

	t.Run("llm_agent_ignores_acp_fields", func(t *testing.T) {
		handle := "llm-update-" + uuid.NewString()
		_, createEnv := createAgentRequest(t, env, client, token, projID, llmAgentBody(roleID, handle, nil))
		agentID, _ := assertDataMap(t, createEnv)["id"].(string)

		patchStatus, _ := patchAgentRequest(t, env, client, token, projID, agentID, map[string]any{
			"acp_provider": "claude-code",
			"acp_command":  []string{"should-not-be-stored"},
		})
		if patchStatus != http.StatusOK {
			t.Fatalf("expected 200 patching an LLM agent with ACP fields, got %d", patchStatus)
		}

		getStatus, getEnv := getAgentRequest(t, env, client, token, projID, agentID)
		if getStatus != http.StatusOK {
			t.Fatalf("expected 200 on GET, got %d", getStatus)
		}
		data := assertDataMap(t, getEnv)
		if data["agent_type"] != "llm" {
			t.Errorf("expected agent_type to remain llm, got %v", data["agent_type"])
		}
		if data["acp_provider"] != nil {
			t.Errorf("expected acp_provider to stay unset on an LLM agent, got %v", data["acp_provider"])
		}
		if cmd, ok := data["acp_command"].([]any); ok && len(cmd) != 0 {
			t.Errorf("expected acp_command to stay empty on an LLM agent, got %v", cmd)
		}
	})
}

// ---------------------------------------------------------------------------
// Bridge token generation / regeneration
// ---------------------------------------------------------------------------

func TestE2EACPAgent_BridgeTokenLifecycle(t *testing.T) {
	env := newE2EEnv(t)
	client, token, projID, roleID := seedACPUser(t, env)

	handle := "bridge-agent-" + uuid.NewString()
	_, createEnv := createAgentRequest(t, env, client, token, projID, acpAgentBody(roleID, handle, nil))
	agentID, _ := assertDataMap(t, createEnv)["id"].(string)

	var firstToken string

	t.Run("generate_token_stores_hash_only", func(t *testing.T) {
		status, tok, runCommand := generateBridgeTokenRequest(t, env, client, token, projID, agentID)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d", status)
		}
		if tok == "" {
			t.Fatal("expected a non-empty plaintext token")
		}
		if !strings.Contains(runCommand, agentID) || !strings.Contains(runCommand, tok) {
			t.Errorf("expected run_command to reference the agent id and token, got %q", runCommand)
		}
		firstToken = tok

		gotHash := bridgeTokenHash(t, env, agentID)
		if gotHash != sha256Hex(tok) {
			t.Errorf("expected stored hash to be sha256(token), got %q", gotHash)
		}
		if gotHash == tok {
			t.Error("token must never be stored in plaintext")
		}

		getStatus, getEnv := getAgentRequest(t, env, client, token, projID, agentID)
		if getStatus != http.StatusOK {
			t.Fatalf("expected 200 on GET, got %d", getStatus)
		}
		data := assertDataMap(t, getEnv)
		if data["has_acp_bridge_token"] != true {
			t.Errorf("expected has_acp_bridge_token true after generation, got %v", data["has_acp_bridge_token"])
		}
		if _, exists := data["acp_bridge_token_hash"]; exists {
			t.Error("acp_bridge_token_hash must never be serialized in API responses")
		}
	})

	t.Run("regenerate_token_replaces_hash", func(t *testing.T) {
		status, secondToken, _ := generateBridgeTokenRequest(t, env, client, token, projID, agentID)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d", status)
		}
		if secondToken == "" || secondToken == firstToken {
			t.Fatalf("expected a fresh token on regeneration, got %q (first was %q)", secondToken, firstToken)
		}

		gotHash := bridgeTokenHash(t, env, agentID)
		if gotHash != sha256Hex(secondToken) {
			t.Errorf("expected stored hash to match the regenerated token, got %q", gotHash)
		}
		if gotHash == sha256Hex(firstToken) {
			t.Error("expected the old token's hash to no longer be valid after regeneration")
		}
	})

	t.Run("generate_token_on_llm_agent_fails", func(t *testing.T) {
		llmHandle := "llm-for-bridge-" + uuid.NewString()
		_, llmCreateEnv := createAgentRequest(t, env, client, token, projID, llmAgentBody(roleID, llmHandle, nil))
		llmAgentID, _ := assertDataMap(t, llmCreateEnv)["id"].(string)

		status, _, _ := generateBridgeTokenRequest(t, env, client, token, projID, llmAgentID)
		if status == http.StatusOK {
			t.Fatal("expected generating a bridge token for an LLM agent to fail")
		}
	})
}

// ---------------------------------------------------------------------------
// Bridge status proxy
// ---------------------------------------------------------------------------

func TestE2EACPAgent_BridgeStatusProxy(t *testing.T) {
	env := newE2EEnv(t)
	client, token, projID, roleID := seedACPUser(t, env)

	connectedHandle := "status-connected-" + uuid.NewString()
	_, connectedEnv := createAgentRequest(t, env, client, token, projID, acpAgentBody(roleID, connectedHandle, nil))
	connectedAgentID, _ := assertDataMap(t, connectedEnv)["id"].(string)

	disconnectedHandle := "status-disconnected-" + uuid.NewString()
	_, disconnectedEnvResp := createAgentRequest(t, env, client, token, projID, acpAgentBody(roleID, disconnectedHandle, nil))
	disconnectedAgentID, _ := assertDataMap(t, disconnectedEnvResp)["id"].(string)

	const internalKey = "e2e-internal-key"
	aiAgentURL := newFakeAIAgentBridgeStatusServer(t, internalKey, map[string]bool{
		connectedAgentID: true,
	})
	base := newAgentAPIServer(t, env, aiAgentURL, internalKey)

	// GetACPBridgeStatus proxies ai-agent's raw JSON body straight through
	// (like GetLLMModels) rather than wrapping it in the usual envelope —
	// decode it as a plain object, not via the `envelope` helper type.
	fetchStatus := func(agentID string) (int, any) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/agents/%s/acp-bridge-status", base, projID, agentID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		var data map[string]any
		decodeJSON(t, resp, &data)
		return resp.StatusCode, data["connected"]
	}

	t.Run("connected_agent", func(t *testing.T) {
		status, connected := fetchStatus(connectedAgentID)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d", status)
		}
		if connected != true {
			t.Errorf("expected connected true, got %v", connected)
		}
	})

	t.Run("disconnected_agent", func(t *testing.T) {
		status, connected := fetchStatus(disconnectedAgentID)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d", status)
		}
		if connected != false {
			t.Errorf("expected connected false, got %v", connected)
		}
	})
}

// ---------------------------------------------------------------------------
// Malformed acp_command JSONB
// ---------------------------------------------------------------------------

// TestE2EACPAgent_MalformedACPCommandSurfacesAsError guards against silently
// dropping an acp_command column value of the wrong shape (previously
// agentFromReadRow swallowed the json.Unmarshal error and returned the agent
// with acp_command as if the column had simply never been set). The column
// is JSONB, so Postgres itself already rejects syntactically invalid JSON on
// write — the case that actually reaches Go's json.Unmarshal is
// syntactically valid JSON of the wrong shape (e.g. an object, not a string
// array).
//
// Uses the default "claude-code" provider rather than "custom": migration
// 000024 added a CHECK constraint requiring a non-empty acp_command array
// specifically for acp_provider = 'custom', so corrupting a custom-provider
// agent's acp_command to a non-array shape would now be rejected by Postgres
// at the UPDATE itself rather than reaching this test's actual target, the
// read-path json.Unmarshal handling in agentFromReadRow. A non-custom
// provider has no such constraint on acp_command, so it still reaches
// Postgres as a bare column write.
func TestE2EACPAgent_MalformedACPCommandSurfacesAsError(t *testing.T) {
	env := newE2EEnv(t)
	client, token, projID, roleID := seedACPUser(t, env)

	handle := "malformed-command-" + uuid.NewString()
	_, createEnv := createAgentRequest(t, env, client, token, projID, acpAgentBody(roleID, handle, nil))
	agentID, _ := assertDataMap(t, createEnv)["id"].(string)

	if _, err := env.db.ExecContext(env.ctx,
		`UPDATE agents SET acp_command = '{"unexpected":"shape"}'::jsonb WHERE id = $1`, agentID); err != nil {
		t.Fatalf("corrupt acp_command column: %v", err)
	}

	status, e := getAgentRequest(t, env, client, token, projID, agentID)
	if status == http.StatusOK {
		t.Fatalf("expected an error status for a wrong-shape acp_command column, got 200: %+v", e)
	}
}

// ---------------------------------------------------------------------------
// acp_bridge_token_hash index
// ---------------------------------------------------------------------------

// TestE2EACPAgent_BridgeTokenHashIndexExists confirms migration
// 000023_add_acp_bridge_token_hash_index.sql actually applied — the hash is
// looked up on every ACP bridge WebSocket handshake and needs an index to
// avoid a full table scan per connection attempt.
func TestE2EACPAgent_BridgeTokenHashIndexExists(t *testing.T) {
	env := newE2EEnv(t)

	var count int
	err := env.db.GetContext(env.ctx, &count,
		"SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'agents' AND indexname = 'uq_agents_acp_bridge_token_hash'")
	if err != nil {
		t.Fatalf("query pg_indexes: %v", err)
	}
	if count != 1 {
		t.Errorf("expected uq_agents_acp_bridge_token_hash index to exist, found %d", count)
	}
}
