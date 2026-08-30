package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

// ErrNotLLMAgent is returned by FindByID for an agent that exists but isn't
// agent_type='llm' — this service only ever executes llm-type conversations
// (acp-type agents stay on apps/acp-bridge), so a caller that reaches this
// for an acp agent has a routing bug upstream, not a data problem to paper
// over here.
var ErrNotLLMAgent = errors.New("postgres: agent is not agent_type=llm")

type agentRecord struct {
	ID                uuid.UUID `db:"id"`
	Name              string    `db:"name"`
	Handle            string    `db:"handle"`
	AgentScope        string    `db:"agent_scope"`
	AgentType         string    `db:"agent_type"`
	LLMProvider       string    `db:"llm_provider"`
	LLMModel          string    `db:"llm_model"`
	LLMAPIKeySecret   string    `db:"llm_api_key_secret"`
	LLMBaseURL        string    `db:"llm_base_url"`
	SystemPrompt      string    `db:"system_prompt"`
	MaxIterations     int       `db:"max_iterations"`
	TimeoutMinutes    int       `db:"timeout_minutes"`
	GitCommitterName  string    `db:"git_committer_name"`
	GitCommitterEmail string    `db:"git_committer_email"`
	DockerEnabled     bool      `db:"docker_enabled"`
}

type mcpServerRecord struct {
	ServerName string          `db:"server_name"`
	Transport  string          `db:"transport"`
	Command    *string         `db:"command"`
	Args       json.RawMessage `db:"args"`
	URL        *string         `db:"url"`
	Env        json.RawMessage `db:"env"`
	IsEnabled  bool            `db:"is_enabled"`
}

type skillRecord struct {
	SkillName    string          `db:"skill_name"`
	SkillContent string          `db:"skill_content"`
	Triggers     json.RawMessage `db:"triggers"`
	IsEnabled    bool            `db:"is_enabled"`
}

type envVarRecord struct {
	Key            string `db:"key"`
	EncryptedValue string `db:"encrypted_value"`
}

// AgentRepository reads llm-type agent configuration. Read-only — agent
// CRUD stays owned by services/api (see docs/architecture/service-
// boundaries.md's Boundary Rule); this service only ever reads what it
// needs to run a conversation.
type AgentRepository struct {
	db *sqlx.DB
}

// NewAgentRepository builds an AgentRepository backed by db.
func NewAgentRepository(db *sqlx.DB) *AgentRepository {
	return &AgentRepository{db: db}
}

// FindByID loads one agent's full llm-type conversation config — the base
// row plus its MCP servers, skills, and environment variables. Returns
// ErrNotLLMAgent for a non-llm agent and sql.ErrNoRows (via the underlying
// Get) for a missing/deleted one.
func (r *AgentRepository) FindByID(ctx context.Context, id uuid.UUID) (*agent.Config, error) {
	var rec agentRecord
	err := r.db.GetContext(ctx, &rec, `
		SELECT id, name, handle, agent_scope, agent_type, llm_provider, llm_model,
		       llm_api_key_secret, llm_base_url, system_prompt,
		       max_iterations, timeout_minutes,
		       git_committer_name, git_committer_email, docker_enabled
		FROM agents
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	if err != nil {
		return nil, fmt.Errorf("postgres: find agent %s: %w", id, err)
	}
	if rec.AgentType != "llm" {
		return nil, fmt.Errorf("postgres: agent %s: %w", id, ErrNotLLMAgent)
	}

	mcpServers, err := r.listMCPServers(ctx, id)
	if err != nil {
		return nil, err
	}
	skills, err := r.listSkills(ctx, id)
	if err != nil {
		return nil, err
	}
	envVars, err := r.listEnvVars(ctx, id)
	if err != nil {
		return nil, err
	}

	return &agent.Config{
		ID:                rec.ID,
		Name:              rec.Name,
		Handle:            rec.Handle,
		AgentScope:        rec.AgentScope,
		LLMProvider:       rec.LLMProvider,
		LLMModel:          rec.LLMModel,
		LLMAPIKeySecret:   rec.LLMAPIKeySecret,
		LLMBaseURL:        rec.LLMBaseURL,
		SystemPrompt:      rec.SystemPrompt,
		MaxIterations:     rec.MaxIterations,
		TimeoutMinutes:    rec.TimeoutMinutes,
		GitCommitterName:  rec.GitCommitterName,
		GitCommitterEmail: rec.GitCommitterEmail,
		DockerEnabled:     rec.DockerEnabled,
		MCPServers:        mcpServers,
		Skills:            skills,
		EnvVars:           envVars,
	}, nil
}

func (r *AgentRepository) listMCPServers(ctx context.Context, agentID uuid.UUID) ([]agent.MCPServer, error) {
	var recs []mcpServerRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT server_name, transport, command, args, url, env, is_enabled
		FROM agent_mcp_servers WHERE agent_id = $1
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list mcp servers for agent %s: %w", agentID, err)
	}

	out := make([]agent.MCPServer, 0, len(recs))
	for _, rec := range recs {
		var args []string
		_ = json.Unmarshal(rec.Args, &args)
		var env map[string]string
		_ = json.Unmarshal(rec.Env, &env)
		out = append(out, agent.MCPServer{
			ServerName: rec.ServerName,
			Transport:  rec.Transport,
			Command:    derefOr(rec.Command, ""),
			Args:       args,
			URL:        derefOr(rec.URL, ""),
			Env:        env,
			IsEnabled:  rec.IsEnabled,
		})
	}
	return out, nil
}

func (r *AgentRepository) listSkills(ctx context.Context, agentID uuid.UUID) ([]agent.Skill, error) {
	var recs []skillRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT skill_name, skill_content, triggers, is_enabled
		FROM agent_skills WHERE agent_id = $1
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list skills for agent %s: %w", agentID, err)
	}

	out := make([]agent.Skill, 0, len(recs))
	for _, rec := range recs {
		var triggers []string
		_ = json.Unmarshal(rec.Triggers, &triggers)
		out = append(out, agent.Skill{
			SkillName:    rec.SkillName,
			SkillContent: rec.SkillContent,
			Triggers:     triggers,
			IsEnabled:    rec.IsEnabled,
		})
	}
	return out, nil
}

func (r *AgentRepository) listEnvVars(ctx context.Context, agentID uuid.UUID) ([]agent.EnvVar, error) {
	var recs []envVarRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT key, encrypted_value FROM agent_environment_variables WHERE agent_id = $1
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list env vars for agent %s: %w", agentID, err)
	}

	out := make([]agent.EnvVar, 0, len(recs))
	for _, rec := range recs {
		out = append(out, agent.EnvVar{Key: rec.Key, EncryptedValue: rec.EncryptedValue})
	}
	return out, nil
}

func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}

type acpAgentRecord struct {
	ID             uuid.UUID       `db:"id"`
	ProjectID      *uuid.UUID      `db:"project_id"`
	ACPProvider    *string         `db:"acp_provider"`
	ACPCommand     json.RawMessage `db:"acp_command"`
	TimeoutMinutes int             `db:"timeout_minutes"`
}

// FindByBridgeTokenHash resolves the acp-type agent whose current bridge
// token hashes to tokenHash — mirrors
// find_agent_by_bridge_token_hash, used to authenticate an incoming
// /agent-bridge/ws connection (see internal/acpbridge/server.go). Returns
// (nil, nil) — not an error — for a hash miss, a non-acp agent, or a
// soft-deleted one; sql.ErrNoRows is deliberately swallowed here rather
// than surfaced, since "no match" is an ordinary, expected outcome for an
// unauthenticated caller, not a data-layer failure.
func (r *AgentRepository) FindByBridgeTokenHash(ctx context.Context, tokenHash string) (*agent.ACPConfig, error) {
	var rec acpAgentRecord
	err := r.db.GetContext(ctx, &rec, `
		SELECT id, project_id, acp_provider, acp_command, timeout_minutes
		FROM agents
		WHERE acp_bridge_token_hash = $1 AND agent_type = 'acp' AND deleted_at IS NULL
	`, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: find agent by bridge token hash: %w", err)
	}
	return rec.toACPConfig()
}

// FindACPByID loads one acp-type agent's dispatch configuration by ID —
// used by acpdispatch to resolve the agent a trigger names, after the
// caller has already established (via handler.Handler's own agent_type
// lookup) that this agent is acp-type. Returns sql.ErrNoRows, via the
// underlying Get, for a missing/deleted agent or one that turns out not to
// be acp-type after all — that second case is a routing bug upstream, not
// something this method distinguishes from "not found".
func (r *AgentRepository) FindACPByID(ctx context.Context, id uuid.UUID) (*agent.ACPConfig, error) {
	var rec acpAgentRecord
	err := r.db.GetContext(ctx, &rec, `
		SELECT id, project_id, acp_provider, acp_command, timeout_minutes
		FROM agents
		WHERE id = $1 AND agent_type = 'acp' AND deleted_at IS NULL
	`, id)
	if err != nil {
		return nil, fmt.Errorf("postgres: find acp agent %s: %w", id, err)
	}
	return rec.toACPConfig()
}

func (rec acpAgentRecord) toACPConfig() (*agent.ACPConfig, error) {
	var acpCommand []string
	if len(rec.ACPCommand) > 0 {
		if err := json.Unmarshal(rec.ACPCommand, &acpCommand); err != nil {
			return nil, fmt.Errorf("postgres: decode acp_command for agent %s: %w", rec.ID, err)
		}
	}
	return &agent.ACPConfig{
		ID:             rec.ID,
		ProjectID:      rec.ProjectID,
		ACPProvider:    derefOr(rec.ACPProvider, ""),
		ACPCommand:     acpCommand,
		TimeoutMinutes: rec.TimeoutMinutes,
	}, nil
}
