package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
)

// -------------------------------------------------------------------------
// sqlx record types
// -------------------------------------------------------------------------

type agentRecord struct {
	ID                 string     `db:"id"`
	ProjectID          string     `db:"project_id"`
	Name               string     `db:"name"`
	Handle             string     `db:"handle"`
	AvatarURL          *string    `db:"avatar_url"`
	AgentType          string     `db:"agent_type"`
	LLMProvider        string     `db:"llm_provider"`
	LLMModel           string     `db:"llm_model"`
	LLMAPIKeySecret    string     `db:"llm_api_key_secret"`
	LLMBaseURL         string     `db:"llm_base_url"`
	ACPProvider        *string    `db:"acp_provider"`
	ACPCommand         []byte     `db:"acp_command"`
	ACPBridgeTokenHash *string    `db:"acp_bridge_token_hash"`
	SystemPrompt       string     `db:"system_prompt"`
	MaxIterations      int        `db:"max_iterations"`
	TimeoutMinutes     int        `db:"timeout_minutes"`
	GitCommitterName   string     `db:"git_committer_name"`
	GitCommitterEmail  string     `db:"git_committer_email"`
	CreatedBy          *string    `db:"created_by"`
	CreatedAt          time.Time  `db:"created_at"`
	UpdatedAt          time.Time  `db:"updated_at"`
	DeletedAt          *time.Time `db:"deleted_at"`
	MemberID           *string    `db:"member_id"` // populated when joining with project_members
}

type agentMCPServerRecord struct {
	ID         string    `db:"id"`
	AgentID    string    `db:"agent_id"`
	ServerName string    `db:"server_name"`
	Transport  string    `db:"transport"`
	Command    *string   `db:"command"`
	Args       []byte    `db:"args"`
	URL        *string   `db:"url"`
	Env        []byte    `db:"env"`
	IsEnabled  bool      `db:"is_enabled"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

type agentEnvVarRecord struct {
	ID             string    `db:"id"`
	AgentID        string    `db:"agent_id"`
	Key            string    `db:"key"`
	EncryptedValue string    `db:"encrypted_value"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

type agentSkillRecord struct {
	ID           string    `db:"id"`
	AgentID      string    `db:"agent_id"`
	SkillName    string    `db:"skill_name"`
	SkillSource  string    `db:"skill_source"`
	SkillContent string    `db:"skill_content"`
	SourceURL    *string   `db:"source_url"`
	Triggers     []byte    `db:"triggers"`
	IsEnabled    bool      `db:"is_enabled"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

type agentConversationRecord struct {
	ID                  string     `db:"id"`
	AgentID             string     `db:"agent_id"`
	ProjectID           string     `db:"project_id"`
	TriggerType         string     `db:"trigger_type"`
	TaskID              *string    `db:"task_id"`
	CommentID           *string    `db:"comment_id"`
	ChatSessionID       *string    `db:"chat_session_id"`
	TriggeredByMemberID *string    `db:"triggered_by_member_id"`
	Status              string     `db:"status"`
	ContainerID         *string    `db:"container_id"`
	HostPort            *int       `db:"host_port"`
	IterationCount      int64      `db:"iteration_count"`
	ErrorMessage        *string    `db:"error_message"`
	RepoPluginID        *string    `db:"repo_plugin_id"`
	RepoCloneURL        *string    `db:"repo_clone_url"`
	BranchName          *string    `db:"branch_name"`
	PRUrl               *string    `db:"pr_url"`
	PersistenceDir      *string    `db:"persistence_dir"`
	StartedAt           *time.Time `db:"started_at"`
	FinishedAt          *time.Time `db:"finished_at"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
}

type agentConversationEventRecord struct {
	ID             string    `db:"id"`
	ConversationID string    `db:"conversation_id"`
	EventIndex     int       `db:"event_index"`
	EventType      string    `db:"event_type"`
	EventSource    string    `db:"event_source"`
	Payload        []byte    `db:"payload"`
	CreatedAt      time.Time `db:"created_at"`
}

type agentChatSessionRecord struct {
	ID            string     `db:"id"`
	AgentID       string     `db:"agent_id"`
	ProjectID     string     `db:"project_id"`
	MemberID      string     `db:"member_id"`
	Title         *string    `db:"title"`
	LastMessageAt *time.Time `db:"last_message_at"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
}

// -------------------------------------------------------------------------
// Repository
// -------------------------------------------------------------------------

// AgentRepository is the sqlx implementation of agentdom.Repository.
type AgentRepository struct {
	db *sqlx.DB
}

// NewAgentRepository returns a new AgentRepository.
func NewAgentRepository(db *sqlx.DB) *AgentRepository {
	return &AgentRepository{db: db}
}

const agentSelectCols = `a.id, a.project_id, a.name, a.handle, a.avatar_url, a.agent_type, a.llm_provider, a.llm_model,
	a.llm_api_key_secret, a.llm_base_url, a.acp_provider, a.acp_command, a.acp_bridge_token_hash, a.system_prompt,
	a.max_iterations, a.timeout_minutes,
	a.git_committer_name, a.git_committer_email, a.created_by, a.created_at, a.updated_at, a.deleted_at,
	pm.id AS member_id`

// -------------------------------------------------------------------------
// Agents
// -------------------------------------------------------------------------

// ListAgents returns all agents belonging to the given project.
func (r *AgentRepository) ListAgents(ctx context.Context, projectID uuid.UUID) ([]*agentdom.Agent, error) {
	var rows []agentRecord
	err := r.db.SelectContext(ctx, &rows, `
		SELECT `+agentSelectCols+`
		FROM agents a
		LEFT JOIN project_members pm ON pm.agent_id = a.id AND pm.deleted_at IS NULL
		WHERE a.project_id = $1 AND a.deleted_at IS NULL`, projectID.String())
	if err != nil {
		return nil, err
	}

	result := make([]*agentdom.Agent, 0, len(rows))
	for _, row := range rows {
		a, err := agentFromReadRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, nil
}

// FindAgentByID returns a single agent with its MCP servers and skills.
func (r *AgentRepository) FindAgentByID(ctx context.Context, id uuid.UUID) (*agentdom.Agent, error) {
	var row agentRecord
	err := r.db.GetContext(ctx, &row, `
		SELECT `+agentSelectCols+`
		FROM agents a
		LEFT JOIN project_members pm ON pm.agent_id = a.id AND pm.deleted_at IS NULL
		WHERE a.id = $1 AND a.deleted_at IS NULL`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, agentdom.ErrAgentNotFound
	}
	if err != nil {
		return nil, err
	}
	agent, err := agentFromReadRow(row)
	if err != nil {
		return nil, err
	}
	// Load MCP servers and skills
	mcpServers, err := r.ListMCPServers(ctx, id)
	if err != nil {
		return nil, err
	}
	skills, err := r.ListSkills(ctx, id)
	if err != nil {
		return nil, err
	}
	envVars, err := r.ListEnvVars(ctx, id)
	if err != nil {
		return nil, err
	}
	agent.MCPServers = mcpServers
	agent.Skills = skills
	agent.EnvVars = envVars
	return agent, nil
}

// FindAgentByHandle returns an agent by its unique handle within a project.
func (r *AgentRepository) FindAgentByHandle(ctx context.Context, projectID uuid.UUID, handle string) (*agentdom.Agent, error) {
	var row agentRecord
	err := r.db.GetContext(ctx, &row, `
		SELECT `+agentSelectCols+`
		FROM agents a
		LEFT JOIN project_members pm ON pm.agent_id = a.id AND pm.deleted_at IS NULL
		WHERE a.project_id = $1 AND a.handle = $2 AND a.deleted_at IS NULL`,
		projectID.String(), handle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, agentdom.ErrAgentNotFound
	}
	if err != nil {
		return nil, err
	}
	return agentFromReadRow(row)
}

// CreateAgent inserts a new agent record.
func (r *AgentRepository) CreateAgent(ctx context.Context, a *agentdom.Agent) error {
	rec, err := agentToRecord(a)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO agents (id, project_id, name, handle, avatar_url, agent_type, llm_provider, llm_model,
		  llm_api_key_secret, llm_base_url, acp_provider, acp_command, system_prompt,
		  max_iterations, timeout_minutes,
		  git_committer_name, git_committer_email, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		rec.ID, rec.ProjectID, rec.Name, rec.Handle, rec.AvatarURL, rec.AgentType,
		rec.LLMProvider, rec.LLMModel, rec.LLMAPIKeySecret, rec.LLMBaseURL,
		rec.ACPProvider, rec.ACPCommand,
		rec.SystemPrompt,
		rec.MaxIterations, rec.TimeoutMinutes,
		rec.GitCommitterName, rec.GitCommitterEmail, rec.CreatedBy, rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

// UpdateAgent patches the mutable fields of an existing agent.
// When LLMAPIKeySecret is non-empty it is updated atomically with the rest of
// the fields inside a single transaction so no partial update is possible.
func (r *AgentRepository) UpdateAgent(ctx context.Context, a *agentdom.Agent) error {
	rec, err := agentToRecord(a)
	if err != nil {
		return err
	}
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE agents SET
			  name=$1, handle=$2, avatar_url=$3, llm_provider=$4, llm_model=$5, llm_base_url=$6,
			  acp_provider=$7, acp_command=$8,
			  system_prompt=$9,
			  max_iterations=$10, timeout_minutes=$11,
			  git_committer_name=$12, git_committer_email=$13, updated_at=$14
			WHERE id=$15`,
			a.Name, a.Handle, a.AvatarURL, a.LLMProvider, a.LLMModel, a.LLMBaseURL,
			rec.ACPProvider, rec.ACPCommand,
			a.SystemPrompt,
			a.MaxIterations, a.TimeoutMinutes,
			a.GitCommitterName, a.GitCommitterEmail, time.Now(), a.ID.String(),
		)
		if err != nil {
			return err
		}
		if a.LLMAPIKeySecret != "" {
			_, err = tx.ExecContext(ctx, `UPDATE agents SET llm_api_key_secret=$1 WHERE id=$2`, a.LLMAPIKeySecret, a.ID.String())
		}
		return err
	})
}

// SoftDeleteAgent sets deleted_at on the agent row.
func (r *AgentRepository) SoftDeleteAgent(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE agents SET deleted_at=$1 WHERE id=$2`, time.Now(), id.String())
	return err
}

// SetACPBridgeTokenHash stores the SHA-256 hash of a newly generated
// local-bridge auth token, replacing any previous one.
func (r *AgentRepository) SetACPBridgeTokenHash(ctx context.Context, agentID uuid.UUID, hash string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE agents SET acp_bridge_token_hash=$1, updated_at=$2 WHERE id=$3`,
		hash, time.Now(), agentID.String(),
	)
	return err
}

// SetAgentMemberID is a no-op; membership is derived from project_members JOIN.
func (r *AgentRepository) SetAgentMemberID(_ context.Context, _, _ uuid.UUID) error {
	// Member ID is derived from the project_members table by JOIN; no separate column needed.
	return nil
}

// CreateAgentWithMembership atomically inserts the agent and its project_members
// row within a single database transaction.
func (r *AgentRepository) CreateAgentWithMembership(ctx context.Context, a *agentdom.Agent, memberID, projectID, roleID uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		rec, err := agentToRecord(a)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO agents (id, project_id, name, handle, avatar_url, agent_type, llm_provider, llm_model,
			  llm_api_key_secret, llm_base_url, acp_provider, acp_command, system_prompt,
			  max_iterations, timeout_minutes,
			  git_committer_name, git_committer_email, created_by, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
			rec.ID, rec.ProjectID, rec.Name, rec.Handle, rec.AvatarURL, rec.AgentType,
			rec.LLMProvider, rec.LLMModel, rec.LLMAPIKeySecret, rec.LLMBaseURL,
			rec.ACPProvider, rec.ACPCommand,
			rec.SystemPrompt,
			rec.MaxIterations, rec.TimeoutMinutes,
			rec.GitCommitterName, rec.GitCommitterEmail, rec.CreatedBy, rec.CreatedAt, rec.UpdatedAt,
		)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO project_members (id, project_id, agent_id, project_role_id, member_type, user_id, created_at, deleted_at)
			VALUES ($1, $2, $3, $4, 'agent', NULL, NOW(), NULL)`,
			memberID.String(), projectID.String(), a.ID.String(), roleID.String(),
		)
		return err
	})
}

// SoftDeleteAgentWithMembership atomically soft-deletes both the agent and its
// project_members row within a single database transaction.
func (r *AgentRepository) SoftDeleteAgentWithMembership(ctx context.Context, projectID, agentID uuid.UUID) error {
	now := time.Now()
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE agents SET deleted_at=$1 WHERE id=$2`, now, agentID.String()); err != nil {
			return err
		}
		// Soft-delete the membership row; 0 rows affected is fine for orphaned agents.
		_, err := tx.ExecContext(ctx, `
			UPDATE project_members SET deleted_at=$1
			WHERE project_id=$2 AND agent_id=$3 AND member_type='agent'`, now, projectID.String(), agentID.String())
		return err
	})
}

// -------------------------------------------------------------------------
// MCP Servers
// -------------------------------------------------------------------------

const mcpServerCols = `id, agent_id, server_name, transport, command, args, url, env, is_enabled, created_at, updated_at`

// ListMCPServers returns all MCP server records for the given agent.
func (r *AgentRepository) ListMCPServers(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentMCPServer, error) {
	var recs []agentMCPServerRecord
	if err := r.db.SelectContext(ctx, &recs, `SELECT `+mcpServerCols+` FROM agent_mcp_servers WHERE agent_id = $1`, agentID.String()); err != nil {
		return nil, err
	}
	result := make([]*agentdom.AgentMCPServer, 0, len(recs))
	for _, rec := range recs {
		s, err := mcpServerFromRecord(rec)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

// FindMCPServerByID returns a single MCP server by its primary key.
func (r *AgentRepository) FindMCPServerByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentMCPServer, error) {
	var rec agentMCPServerRecord
	if err := r.db.GetContext(ctx, &rec, `SELECT `+mcpServerCols+` FROM agent_mcp_servers WHERE id = $1`, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, agentdom.ErrMCPServerNotFound
		}
		return nil, err
	}
	return mcpServerFromRecord(rec)
}

// CreateMCPServer inserts a new MCP server record.
func (r *AgentRepository) CreateMCPServer(ctx context.Context, s *agentdom.AgentMCPServer) error {
	rec, err := mcpServerToRecord(s)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO agent_mcp_servers (id, agent_id, server_name, transport, command, args, url, env, is_enabled, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		rec.ID, rec.AgentID, rec.ServerName, rec.Transport, rec.Command,
		rec.Args, rec.URL, rec.Env, rec.IsEnabled, rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

// UpdateMCPServer saves the full MCP server record.
func (r *AgentRepository) UpdateMCPServer(ctx context.Context, s *agentdom.AgentMCPServer) error {
	rec, err := mcpServerToRecord(s)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE agent_mcp_servers SET agent_id=$1, server_name=$2, transport=$3, command=$4,
		  args=$5, url=$6, env=$7, is_enabled=$8, updated_at=$9
		WHERE id=$10`,
		rec.AgentID, rec.ServerName, rec.Transport, rec.Command,
		rec.Args, rec.URL, rec.Env, rec.IsEnabled, rec.UpdatedAt, rec.ID,
	)
	return err
}

// DeleteMCPServer permanently removes an MCP server record.
func (r *AgentRepository) DeleteMCPServer(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM agent_mcp_servers WHERE id = $1`, id.String())
	return err
}

// -------------------------------------------------------------------------
// Skills
// -------------------------------------------------------------------------

const skillCols = `id, agent_id, skill_name, skill_source, skill_content, source_url, triggers, is_enabled, created_at, updated_at`

// ListSkills returns all skill records for the given agent.
func (r *AgentRepository) ListSkills(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentSkill, error) {
	var recs []agentSkillRecord
	if err := r.db.SelectContext(ctx, &recs, `SELECT `+skillCols+` FROM agent_skills WHERE agent_id = $1`, agentID.String()); err != nil {
		return nil, err
	}
	result := make([]*agentdom.AgentSkill, 0, len(recs))
	for _, rec := range recs {
		s, err := skillFromRecord(rec)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

// FindSkillByID returns a single skill by its primary key.
func (r *AgentRepository) FindSkillByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentSkill, error) {
	var rec agentSkillRecord
	if err := r.db.GetContext(ctx, &rec, `SELECT `+skillCols+` FROM agent_skills WHERE id = $1`, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, agentdom.ErrSkillNotFound
		}
		return nil, err
	}
	return skillFromRecord(rec)
}

// CreateSkill inserts a new skill record.
func (r *AgentRepository) CreateSkill(ctx context.Context, s *agentdom.AgentSkill) error {
	rec, err := skillToRecord(s)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO agent_skills (id, agent_id, skill_name, skill_source, skill_content, source_url, triggers, is_enabled, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		rec.ID, rec.AgentID, rec.SkillName, rec.SkillSource, rec.SkillContent,
		rec.SourceURL, rec.Triggers, rec.IsEnabled, rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

// UpdateSkill saves the full skill record.
func (r *AgentRepository) UpdateSkill(ctx context.Context, s *agentdom.AgentSkill) error {
	rec, err := skillToRecord(s)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE agent_skills SET agent_id=$1, skill_name=$2, skill_source=$3, skill_content=$4,
		  source_url=$5, triggers=$6, is_enabled=$7, updated_at=$8
		WHERE id=$9`,
		rec.AgentID, rec.SkillName, rec.SkillSource, rec.SkillContent,
		rec.SourceURL, rec.Triggers, rec.IsEnabled, rec.UpdatedAt, rec.ID,
	)
	return err
}

// DeleteSkill permanently removes a skill record.
func (r *AgentRepository) DeleteSkill(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM agent_skills WHERE id = $1`, id.String())
	return err
}

// -------------------------------------------------------------------------
// Environment Variables
// -------------------------------------------------------------------------

const envVarCols = `id, agent_id, key, encrypted_value, created_at, updated_at`

// ListEnvVars returns all environment variable records for the given agent.
func (r *AgentRepository) ListEnvVars(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentEnvironmentVariable, error) {
	var recs []agentEnvVarRecord
	if err := r.db.SelectContext(ctx, &recs, `SELECT `+envVarCols+` FROM agent_environment_variables WHERE agent_id = $1 ORDER BY key`, agentID.String()); err != nil {
		return nil, err
	}
	result := make([]*agentdom.AgentEnvironmentVariable, 0, len(recs))
	for _, rec := range recs {
		result = append(result, envVarFromRecord(rec))
	}
	return result, nil
}

// FindEnvVarByID returns a single environment variable by its primary key.
func (r *AgentRepository) FindEnvVarByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentEnvironmentVariable, error) {
	var rec agentEnvVarRecord
	if err := r.db.GetContext(ctx, &rec, `SELECT `+envVarCols+` FROM agent_environment_variables WHERE id = $1`, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, agentdom.ErrEnvVarNotFound
		}
		return nil, err
	}
	return envVarFromRecord(rec), nil
}

// FindEnvVarByKey returns a single environment variable by agent and key.
func (r *AgentRepository) FindEnvVarByKey(ctx context.Context, agentID uuid.UUID, key string) (*agentdom.AgentEnvironmentVariable, error) {
	var rec agentEnvVarRecord
	if err := r.db.GetContext(ctx, &rec, `SELECT `+envVarCols+` FROM agent_environment_variables WHERE agent_id = $1 AND key = $2`, agentID.String(), key); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, agentdom.ErrEnvVarNotFound
		}
		return nil, err
	}
	return envVarFromRecord(rec), nil
}

// CreateEnvVar inserts a new environment variable record.
func (r *AgentRepository) CreateEnvVar(ctx context.Context, v *agentdom.AgentEnvironmentVariable) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO agent_environment_variables (id, agent_id, key, encrypted_value, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		v.ID.String(), v.AgentID.String(), v.Key, v.EncryptedValue, v.CreatedAt, v.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return agentdom.ErrEnvVarKeyTaken
		}
		return err
	}
	return nil
}

// UpdateEnvVar saves the full environment variable record.
func (r *AgentRepository) UpdateEnvVar(ctx context.Context, v *agentdom.AgentEnvironmentVariable) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE agent_environment_variables SET encrypted_value=$1, updated_at=$2
		WHERE id=$3`,
		v.EncryptedValue, v.UpdatedAt, v.ID.String(),
	)
	return err
}

// DeleteEnvVar permanently removes an environment variable record.
func (r *AgentRepository) DeleteEnvVar(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM agent_environment_variables WHERE id = $1`, id.String())
	return err
}

// -------------------------------------------------------------------------
// Conversations
// -------------------------------------------------------------------------

// iteration_count is computed live from agent_conversation_events (one
// ActionEvent per agent step) rather than stored: see #314 and migration
// 000026_drop_conversation_iteration_count.sql for why.
const conversationCols = `id, agent_id, project_id, trigger_type, task_id, comment_id, chat_session_id,
	triggered_by_member_id, status, container_id, host_port,
	(SELECT COUNT(*) FROM agent_conversation_events e
	 WHERE e.conversation_id = agent_conversations.id AND e.event_type = 'ActionEvent') AS iteration_count,
	error_message,
	repo_plugin_id, repo_clone_url, branch_name, pr_url, persistence_dir,
	started_at, finished_at, created_at, updated_at`

// ListConversations returns a keyset-paginated page of conversations matching
// the filter, ordered newest-first. It fetches one row beyond limit to detect
// whether more pages remain, without a separate COUNT query.
func (r *AgentRepository) ListConversations(ctx context.Context, in agentdom.ListConversationsFilter, limit int) ([]*agentdom.AgentConversation, bool, error) {
	b := newQueryBuilder()

	if len(in.AgentIDs) > 0 {
		b.addInClause("agent_id", uuidSliceToStrSlice(in.AgentIDs))
	}
	if in.ProjectID != nil {
		p := b.placeholder()
		b.args = append(b.args, in.ProjectID.String())
		b.whereClauses = append(b.whereClauses, "project_id = "+p)
	}
	if in.TaskID != nil {
		p := b.placeholder()
		b.args = append(b.args, in.TaskID.String())
		b.whereClauses = append(b.whereClauses, "task_id = "+p)
	}
	if len(in.Statuses) > 0 {
		b.addInClause("status", in.Statuses)
	}
	if len(in.TriggerTypes) > 0 {
		b.addInClause("trigger_type", in.TriggerTypes)
	}
	if in.CreatedAfter != nil {
		p := b.placeholder()
		b.args = append(b.args, *in.CreatedAfter)
		b.whereClauses = append(b.whereClauses, "created_at >= "+p)
	}
	if in.CreatedBefore != nil {
		p := b.placeholder()
		b.args = append(b.args, *in.CreatedBefore)
		b.whereClauses = append(b.whereClauses, "created_at < "+p)
	}
	if in.Search != nil {
		if q := strings.TrimSpace(*in.Search); q != "" {
			p := b.placeholder()
			b.args = append(b.args, q)
			// Matches conversations with at least one event whose extracted
			// text (agent_conversation_event_search_text, migration 000028)
			// contains the search terms. plainto_tsquery (not to_tsquery) so
			// arbitrary user input never raises a tsquery syntax error.
			b.whereClauses = append(b.whereClauses,
				"EXISTS (SELECT 1 FROM agent_conversation_events e WHERE e.conversation_id = agent_conversations.id "+
					"AND to_tsvector('simple', agent_conversation_event_search_text(e.payload)) @@ plainto_tsquery('simple', "+p+"))")
		}
	}
	if in.CursorAfter != nil {
		cur, err := agentdom.DecodeConversationCursor(*in.CursorAfter)
		if err != nil {
			return nil, false, fmt.Errorf("%w: %s", agentdom.ErrConversationInvalidCursor, err)
		}
		p1 := b.placeholder()
		p2 := b.placeholder()
		b.args = append(b.args, cur.CreatedAt, cur.ID)
		b.whereClauses = append(b.whereClauses, fmt.Sprintf("(created_at, id) < (%s, %s)", p1, p2))
	}

	limitP := b.placeholder()
	b.args = append(b.args, limit+1)

	whereSQL := "1=1"
	if len(b.whereClauses) > 0 {
		whereSQL += " AND " + strings.Join(b.whereClauses, " AND ")
	}

	var recs []agentConversationRecord
	query := `SELECT ` + conversationCols + ` FROM agent_conversations WHERE ` + whereSQL +
		fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT %s`, limitP)
	if err := r.db.SelectContext(ctx, &recs, query, b.args...); err != nil {
		return nil, false, err
	}

	hasMore := len(recs) > limit
	if hasMore {
		recs = recs[:limit]
	}

	result := make([]*agentdom.AgentConversation, 0, len(recs))
	for _, rec := range recs {
		result = append(result, conversationFromRecord(rec))
	}
	return result, hasMore, nil
}

// FindConversationByID returns a single conversation by its primary key.
func (r *AgentRepository) FindConversationByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
	var rec agentConversationRecord
	if err := r.db.GetContext(ctx, &rec, `SELECT `+conversationCols+` FROM agent_conversations WHERE id = $1`, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, agentdom.ErrConversationNotFound
		}
		return nil, err
	}
	return conversationFromRecord(rec), nil
}

// FindLatestConversationByChatSession returns the most recently created
// conversation for a chat session, or (nil, nil) if none exists yet.
func (r *AgentRepository) FindLatestConversationByChatSession(ctx context.Context, chatSessionID uuid.UUID) (*agentdom.AgentConversation, error) {
	var rec agentConversationRecord
	err := r.db.GetContext(ctx, &rec,
		`SELECT `+conversationCols+` FROM agent_conversations
		 WHERE chat_session_id = $1 ORDER BY created_at DESC LIMIT 1`,
		chatSessionID.String(),
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return conversationFromRecord(rec), nil
}

// CreateConversation inserts a new conversation record.
func (r *AgentRepository) CreateConversation(ctx context.Context, c *agentdom.AgentConversation) error {
	rec := conversationToRecord(c)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, task_id, comment_id, chat_session_id,
		  triggered_by_member_id, status, container_id, host_port, error_message,
		  repo_plugin_id, repo_clone_url, branch_name, pr_url, persistence_dir,
		  started_at, finished_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		rec.ID, rec.AgentID, rec.ProjectID, rec.TriggerType, rec.TaskID, rec.CommentID, rec.ChatSessionID,
		rec.TriggeredByMemberID, rec.Status, rec.ContainerID, rec.HostPort, rec.ErrorMessage,
		rec.RepoPluginID, rec.RepoCloneURL, rec.BranchName, rec.PRUrl, rec.PersistenceDir,
		rec.StartedAt, rec.FinishedAt, rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

// UpdateConversationStatus sets the status field of a conversation.
func (r *AgentRepository) UpdateConversationStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE agent_conversations SET status=$1, updated_at=$2 WHERE id=$3`, status, time.Now(), id.String())
	return err
}

// ClaimConversationStatus atomically moves a conversation from fromStatus to
// toStatus. Only one caller racing on the same conversation observes true.
func (r *AgentRepository) ClaimConversationStatus(ctx context.Context, id uuid.UUID, fromStatus, toStatus string) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE agent_conversations SET status=$1, updated_at=$2 WHERE id=$3 AND status=$4`,
		toStatus, time.Now(), id.String(), fromStatus,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// UpdateConversation saves the full conversation record.
func (r *AgentRepository) UpdateConversation(ctx context.Context, c *agentdom.AgentConversation) error {
	rec := conversationToRecord(c)
	_, err := r.db.ExecContext(ctx, `
		UPDATE agent_conversations SET
		  agent_id=$1, project_id=$2, trigger_type=$3, task_id=$4, comment_id=$5, chat_session_id=$6,
		  triggered_by_member_id=$7, status=$8, container_id=$9, host_port=$10,
		  error_message=$11, repo_plugin_id=$12, repo_clone_url=$13, branch_name=$14, pr_url=$15,
		  persistence_dir=$16, started_at=$17, finished_at=$18, updated_at=$19
		WHERE id=$20`,
		rec.AgentID, rec.ProjectID, rec.TriggerType, rec.TaskID, rec.CommentID, rec.ChatSessionID,
		rec.TriggeredByMemberID, rec.Status, rec.ContainerID, rec.HostPort,
		rec.ErrorMessage, rec.RepoPluginID, rec.RepoCloneURL, rec.BranchName, rec.PRUrl,
		rec.PersistenceDir, rec.StartedAt, rec.FinishedAt, rec.UpdatedAt, rec.ID,
	)
	return err
}

// ListConversationEvents returns a paginated list of events for a conversation.
func (r *AgentRepository) ListConversationEvents(ctx context.Context, conversationID uuid.UUID, offset, limit int) ([]*agentdom.AgentConversationEvent, int64, error) {
	var total int64
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM agent_conversation_events WHERE conversation_id = $1`, conversationID.String()); err != nil {
		return nil, 0, err
	}

	var recs []agentConversationEventRecord
	if err := r.db.SelectContext(ctx, &recs, `
		SELECT id, conversation_id, event_index, event_type, event_source, payload, created_at
		FROM agent_conversation_events
		WHERE conversation_id = $1
		ORDER BY event_index ASC
		OFFSET $2 LIMIT $3`, conversationID.String(), offset, limit); err != nil {
		return nil, 0, err
	}

	result := make([]*agentdom.AgentConversationEvent, 0, len(recs))
	for _, rec := range recs {
		e, err := conversationEventFromRecord(rec)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, e)
	}
	return result, total, nil
}

// CreateConversationEvent inserts a new conversation event record.
func (r *AgentRepository) CreateConversationEvent(ctx context.Context, e *agentdom.AgentConversationEvent) error {
	rec, err := conversationEventToRecord(e)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO agent_conversation_events (id, conversation_id, event_index, event_type, event_source, payload, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		rec.ID, rec.ConversationID, rec.EventIndex, rec.EventType, rec.EventSource, rec.Payload, rec.CreatedAt,
	)
	return err
}

// -------------------------------------------------------------------------
// Chat Sessions
// -------------------------------------------------------------------------

const chatSessionCols = `id, agent_id, project_id, member_id, title, last_message_at, created_at, updated_at`

// ListChatSessions returns all chat sessions for the given agent and member.
func (r *AgentRepository) ListChatSessions(ctx context.Context, agentID, memberID uuid.UUID) ([]*agentdom.AgentChatSession, error) {
	var recs []agentChatSessionRecord
	if err := r.db.SelectContext(ctx, &recs, `
		SELECT `+chatSessionCols+` FROM agent_chat_sessions
		WHERE agent_id = $1 AND member_id = $2
		ORDER BY created_at DESC`, agentID.String(), memberID.String()); err != nil {
		return nil, err
	}
	result := make([]*agentdom.AgentChatSession, 0, len(recs))
	for _, rec := range recs {
		result = append(result, chatSessionFromRecord(rec))
	}
	return result, nil
}

// FindChatSessionByID returns a single chat session by its primary key.
func (r *AgentRepository) FindChatSessionByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentChatSession, error) {
	var rec agentChatSessionRecord
	if err := r.db.GetContext(ctx, &rec, `SELECT `+chatSessionCols+` FROM agent_chat_sessions WHERE id = $1`, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, agentdom.ErrChatSessionNotFound
		}
		return nil, err
	}
	return chatSessionFromRecord(rec), nil
}

// CreateChatSession inserts a new chat session record.
func (r *AgentRepository) CreateChatSession(ctx context.Context, s *agentdom.AgentChatSession) error {
	rec := chatSessionToRecord(s)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO agent_chat_sessions (id, agent_id, project_id, member_id, title, last_message_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		rec.ID, rec.AgentID, rec.ProjectID, rec.MemberID, rec.Title, rec.LastMessageAt, rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

// UpdateChatSession saves the full chat session record.
func (r *AgentRepository) UpdateChatSession(ctx context.Context, s *agentdom.AgentChatSession) error {
	rec := chatSessionToRecord(s)
	_, err := r.db.ExecContext(ctx, `
		UPDATE agent_chat_sessions SET agent_id=$1, project_id=$2, member_id=$3, title=$4, last_message_at=$5, updated_at=$6
		WHERE id=$7`,
		rec.AgentID, rec.ProjectID, rec.MemberID, rec.Title, rec.LastMessageAt, rec.UpdatedAt, rec.ID,
	)
	return err
}

// -------------------------------------------------------------------------
// Mapping helpers
// -------------------------------------------------------------------------

func agentFromReadRow(row agentRecord) (*agentdom.Agent, error) {
	a := &agentdom.Agent{
		ID:                mustParseUUID(row.ID),
		ProjectID:         mustParseUUID(row.ProjectID),
		Name:              row.Name,
		Handle:            row.Handle,
		AvatarURL:         row.AvatarURL,
		AgentType:         row.AgentType,
		LLMProvider:       row.LLMProvider,
		LLMModel:          row.LLMModel,
		LLMAPIKeySecret:   row.LLMAPIKeySecret,
		LLMBaseURL:        row.LLMBaseURL,
		ACPProvider:       row.ACPProvider,
		HasACPBridgeToken: row.ACPBridgeTokenHash != nil && *row.ACPBridgeTokenHash != "",
		SystemPrompt:      row.SystemPrompt,
		MaxIterations:     row.MaxIterations,
		TimeoutMinutes:    row.TimeoutMinutes,
		GitCommitterName:  row.GitCommitterName,
		GitCommitterEmail: row.GitCommitterEmail,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
		DeletedAt:         row.DeletedAt,
	}
	if row.ACPBridgeTokenHash != nil {
		a.ACPBridgeTokenHash = *row.ACPBridgeTokenHash
	}
	if len(row.ACPCommand) > 0 {
		var cmd []string
		if err := json.Unmarshal(row.ACPCommand, &cmd); err != nil {
			return nil, fmt.Errorf("unmarshal acp_command for agent %s: %w", row.ID, err)
		}
		a.ACPCommand = cmd
	}
	if row.CreatedBy != nil {
		id := mustParseUUID(*row.CreatedBy)
		a.CreatedBy = &id
	}
	if row.MemberID != nil {
		mid := mustParseUUID(*row.MemberID)
		a.MemberID = &mid
	}
	return a, nil
}

func agentToRecord(a *agentdom.Agent) (agentRecord, error) {
	agentType := a.AgentType
	if agentType == "" {
		agentType = agentdom.AgentTypeLLM
	}
	cmd := a.ACPCommand
	if cmd == nil {
		cmd = []string{}
	}
	cmdJSON, err := json.Marshal(cmd)
	if err != nil {
		return agentRecord{}, fmt.Errorf("marshal acp_command for agent %s: %w", a.ID, err)
	}
	rec := agentRecord{
		ID:                a.ID.String(),
		ProjectID:         a.ProjectID.String(),
		Name:              a.Name,
		Handle:            a.Handle,
		AvatarURL:         a.AvatarURL,
		AgentType:         agentType,
		LLMProvider:       a.LLMProvider,
		LLMModel:          a.LLMModel,
		LLMAPIKeySecret:   a.LLMAPIKeySecret,
		LLMBaseURL:        a.LLMBaseURL,
		ACPProvider:       a.ACPProvider,
		ACPCommand:        cmdJSON,
		SystemPrompt:      a.SystemPrompt,
		MaxIterations:     a.MaxIterations,
		TimeoutMinutes:    a.TimeoutMinutes,
		GitCommitterName:  a.GitCommitterName,
		GitCommitterEmail: a.GitCommitterEmail,
		CreatedAt:         a.CreatedAt,
		UpdatedAt:         a.UpdatedAt,
	}
	if a.CreatedBy != nil {
		s := a.CreatedBy.String()
		rec.CreatedBy = &s
	}
	return rec, nil
}

func mcpServerFromRecord(rec agentMCPServerRecord) (*agentdom.AgentMCPServer, error) {
	var args []string
	if err := json.Unmarshal(rec.Args, &args); err != nil {
		return nil, err
	}
	var env map[string]string
	if err := json.Unmarshal(rec.Env, &env); err != nil {
		return nil, err
	}
	return &agentdom.AgentMCPServer{
		ID:         mustParseUUID(rec.ID),
		AgentID:    mustParseUUID(rec.AgentID),
		ServerName: rec.ServerName,
		Transport:  rec.Transport,
		Command:    rec.Command,
		Args:       args,
		URL:        rec.URL,
		Env:        env,
		IsEnabled:  rec.IsEnabled,
		CreatedAt:  rec.CreatedAt,
		UpdatedAt:  rec.UpdatedAt,
	}, nil
}

func mcpServerToRecord(s *agentdom.AgentMCPServer) (agentMCPServerRecord, error) {
	args, err := json.Marshal(s.Args)
	if err != nil {
		return agentMCPServerRecord{}, err
	}
	env, err := json.Marshal(s.Env)
	if err != nil {
		return agentMCPServerRecord{}, err
	}
	return agentMCPServerRecord{
		ID:         s.ID.String(),
		AgentID:    s.AgentID.String(),
		ServerName: s.ServerName,
		Transport:  s.Transport,
		Command:    s.Command,
		Args:       args,
		URL:        s.URL,
		Env:        env,
		IsEnabled:  s.IsEnabled,
		CreatedAt:  s.CreatedAt,
		UpdatedAt:  s.UpdatedAt,
	}, nil
}

func skillFromRecord(rec agentSkillRecord) (*agentdom.AgentSkill, error) {
	var triggers []string
	if err := json.Unmarshal(rec.Triggers, &triggers); err != nil {
		return nil, err
	}
	return &agentdom.AgentSkill{
		ID:           mustParseUUID(rec.ID),
		AgentID:      mustParseUUID(rec.AgentID),
		SkillName:    rec.SkillName,
		SkillSource:  rec.SkillSource,
		SkillContent: rec.SkillContent,
		SourceURL:    rec.SourceURL,
		Triggers:     triggers,
		IsEnabled:    rec.IsEnabled,
		CreatedAt:    rec.CreatedAt,
		UpdatedAt:    rec.UpdatedAt,
	}, nil
}

func skillToRecord(s *agentdom.AgentSkill) (agentSkillRecord, error) {
	triggers, err := json.Marshal(s.Triggers)
	if err != nil {
		return agentSkillRecord{}, err
	}
	return agentSkillRecord{
		ID:           s.ID.String(),
		AgentID:      s.AgentID.String(),
		SkillName:    s.SkillName,
		SkillSource:  s.SkillSource,
		SkillContent: s.SkillContent,
		SourceURL:    s.SourceURL,
		Triggers:     triggers,
		IsEnabled:    s.IsEnabled,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
	}, nil
}

func envVarFromRecord(rec agentEnvVarRecord) *agentdom.AgentEnvironmentVariable {
	return &agentdom.AgentEnvironmentVariable{
		ID:             mustParseUUID(rec.ID),
		AgentID:        mustParseUUID(rec.AgentID),
		Key:            rec.Key,
		EncryptedValue: rec.EncryptedValue,
		CreatedAt:      rec.CreatedAt,
		UpdatedAt:      rec.UpdatedAt,
	}
}

func conversationFromRecord(rec agentConversationRecord) *agentdom.AgentConversation {
	c := &agentdom.AgentConversation{
		ID:             mustParseUUID(rec.ID),
		AgentID:        mustParseUUID(rec.AgentID),
		ProjectID:      mustParseUUID(rec.ProjectID),
		TriggerType:    rec.TriggerType,
		Status:         rec.Status,
		ContainerID:    rec.ContainerID,
		HostPort:       rec.HostPort,
		IterationCount: int(rec.IterationCount),
		ErrorMessage:   rec.ErrorMessage,
		RepoCloneURL:   rec.RepoCloneURL,
		BranchName:     rec.BranchName,
		PRUrl:          rec.PRUrl,
		PersistenceDir: rec.PersistenceDir,
		StartedAt:      rec.StartedAt,
		FinishedAt:     rec.FinishedAt,
		CreatedAt:      rec.CreatedAt,
		UpdatedAt:      rec.UpdatedAt,
	}
	if rec.TaskID != nil {
		id := mustParseUUID(*rec.TaskID)
		c.TaskID = &id
	}
	if rec.CommentID != nil {
		id := mustParseUUID(*rec.CommentID)
		c.CommentID = &id
	}
	if rec.ChatSessionID != nil {
		id := mustParseUUID(*rec.ChatSessionID)
		c.ChatSessionID = &id
	}
	if rec.TriggeredByMemberID != nil {
		id := mustParseUUID(*rec.TriggeredByMemberID)
		c.TriggeredByMemberID = &id
	}
	if rec.RepoPluginID != nil {
		id := mustParseUUID(*rec.RepoPluginID)
		c.RepoPluginID = &id
	}
	return c
}

func conversationToRecord(c *agentdom.AgentConversation) agentConversationRecord {
	rec := agentConversationRecord{
		ID:             c.ID.String(),
		AgentID:        c.AgentID.String(),
		ProjectID:      c.ProjectID.String(),
		TriggerType:    c.TriggerType,
		Status:         c.Status,
		ContainerID:    c.ContainerID,
		HostPort:       c.HostPort,
		ErrorMessage:   c.ErrorMessage,
		RepoCloneURL:   c.RepoCloneURL,
		BranchName:     c.BranchName,
		PRUrl:          c.PRUrl,
		PersistenceDir: c.PersistenceDir,
		StartedAt:      c.StartedAt,
		FinishedAt:     c.FinishedAt,
		CreatedAt:      c.CreatedAt,
		UpdatedAt:      c.UpdatedAt,
	}
	if c.TaskID != nil {
		s := c.TaskID.String()
		rec.TaskID = &s
	}
	if c.CommentID != nil {
		s := c.CommentID.String()
		rec.CommentID = &s
	}
	if c.ChatSessionID != nil {
		s := c.ChatSessionID.String()
		rec.ChatSessionID = &s
	}
	if c.TriggeredByMemberID != nil {
		s := c.TriggeredByMemberID.String()
		rec.TriggeredByMemberID = &s
	}
	if c.RepoPluginID != nil {
		s := c.RepoPluginID.String()
		rec.RepoPluginID = &s
	}
	return rec
}

func conversationEventFromRecord(rec agentConversationEventRecord) (*agentdom.AgentConversationEvent, error) {
	var payload map[string]any
	if err := json.Unmarshal(rec.Payload, &payload); err != nil {
		return nil, err
	}
	return &agentdom.AgentConversationEvent{
		ID:             mustParseUUID(rec.ID),
		ConversationID: mustParseUUID(rec.ConversationID),
		EventIndex:     rec.EventIndex,
		EventType:      rec.EventType,
		EventSource:    rec.EventSource,
		Payload:        payload,
		CreatedAt:      rec.CreatedAt,
	}, nil
}

func conversationEventToRecord(e *agentdom.AgentConversationEvent) (agentConversationEventRecord, error) {
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return agentConversationEventRecord{}, err
	}
	return agentConversationEventRecord{
		ID:             e.ID.String(),
		ConversationID: e.ConversationID.String(),
		EventIndex:     e.EventIndex,
		EventType:      e.EventType,
		EventSource:    e.EventSource,
		Payload:        payload,
		CreatedAt:      e.CreatedAt,
	}, nil
}

func chatSessionFromRecord(rec agentChatSessionRecord) *agentdom.AgentChatSession {
	return &agentdom.AgentChatSession{
		ID:            mustParseUUID(rec.ID),
		AgentID:       mustParseUUID(rec.AgentID),
		ProjectID:     mustParseUUID(rec.ProjectID),
		MemberID:      mustParseUUID(rec.MemberID),
		Title:         rec.Title,
		LastMessageAt: rec.LastMessageAt,
		CreatedAt:     rec.CreatedAt,
		UpdatedAt:     rec.UpdatedAt,
	}
}

func chatSessionToRecord(s *agentdom.AgentChatSession) agentChatSessionRecord {
	return agentChatSessionRecord{
		ID:            s.ID.String(),
		AgentID:       s.AgentID.String(),
		ProjectID:     s.ProjectID.String(),
		MemberID:      s.MemberID.String(),
		Title:         s.Title,
		LastMessageAt: s.LastMessageAt,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

// -------------------------------------------------------------------------
// Activity Feed
// -------------------------------------------------------------------------

type agentActivityFeedRecord struct {
	ID            string          `db:"id"`
	SourceType    string          `db:"source_type"`
	SourceID      string          `db:"source_id"`
	SourceTitle   string          `db:"source_title"`
	SourceDeleted bool            `db:"source_deleted"`
	ActivityType  string          `db:"activity_type"`
	Content       json.RawMessage `db:"content"`
	CreatedAt     time.Time       `db:"created_at"`
	UpdatedAt     time.Time       `db:"updated_at"`
}

// ListAgentActivities returns a keyset-paginated page of an agent's unified
// task+doc activity feed, ordered newest-first. task_activities and
// doc_activities are combined via UNION ALL into a common column shape
// (agentActivityFeedRecord) up front, filtered by actor_id — both branches
// reference the same $1 placeholder since it's one value used twice, not
// two separate args. The dynamic filters (source type, date range, search,
// cursor) are then applied to the combined result exactly like
// ListConversations applies its filters to agent_conversations, fetching
// one row beyond limit to detect whether more pages remain.
func (r *AgentRepository) ListAgentActivities(ctx context.Context, in agentdom.ListAgentActivitiesFilter, limit int) ([]*agentdom.ActivityFeedItem, bool, error) {
	b := newQueryBuilder()
	b.idx = 2 // $1 is reserved for the actor member id, shared by both UNION branches

	if len(in.SourceTypes) > 0 {
		types := make([]string, len(in.SourceTypes))
		for i, t := range in.SourceTypes {
			types[i] = string(t)
		}
		b.addInClause("source_type", types)
	}
	if in.CreatedAfter != nil {
		p := b.placeholder()
		b.args = append(b.args, *in.CreatedAfter)
		b.whereClauses = append(b.whereClauses, "created_at >= "+p)
	}
	if in.CreatedBefore != nil {
		p := b.placeholder()
		b.args = append(b.args, *in.CreatedBefore)
		b.whereClauses = append(b.whereClauses, "created_at < "+p)
	}
	if in.Search != nil {
		if q := strings.TrimSpace(*in.Search); q != "" {
			p := b.placeholder()
			b.args = append(b.args, "%"+q+"%")
			// Matches either the linked task/document title or the raw
			// activity content (field diffs, comment text, etc.) — a plain
			// case-insensitive substring match, not full-text search, since
			// activity content is small compared to conversation event
			// payloads (which do warrant the tsvector/GIN approach used by
			// listConversations' search).
			b.whereClauses = append(b.whereClauses,
				fmt.Sprintf("(source_title ILIKE %s OR content::text ILIKE %s)", p, p))
		}
	}
	if in.CursorAfter != nil {
		cur, err := agentdom.DecodeActivityFeedCursor(*in.CursorAfter)
		if err != nil {
			return nil, false, fmt.Errorf("%w: %s", agentdom.ErrActivityFeedInvalidCursor, err)
		}
		p1 := b.placeholder()
		p2 := b.placeholder()
		b.args = append(b.args, cur.CreatedAt, cur.ID)
		b.whereClauses = append(b.whereClauses, fmt.Sprintf("(created_at, id) < (%s, %s)", p1, p2))
	}

	limitP := b.placeholder()
	b.args = append(b.args, limit+1)

	whereSQL := "1=1"
	if len(b.whereClauses) > 0 {
		whereSQL += " AND " + strings.Join(b.whereClauses, " AND ")
	}

	query := `WITH agent_activities AS (
		SELECT ta.id, 'task' AS source_type, ta.task_id AS source_id, t.title AS source_title,
		       (t.deleted_at IS NOT NULL) AS source_deleted,
		       ta.activity_type, ta.content, ta.created_at, ta.updated_at
		FROM task_activities ta
		JOIN tasks t ON t.id = ta.task_id
		WHERE ta.actor_id = $1 AND ta.deleted_at IS NULL
		UNION ALL
		SELECT da.id, 'doc' AS source_type, da.document_id AS source_id, d.title AS source_title,
		       (d.deleted_at IS NOT NULL) AS source_deleted,
		       da.activity_type, da.content, da.created_at, da.updated_at
		FROM doc_activities da
		JOIN documents d ON d.id = da.document_id
		WHERE da.actor_id = $1 AND da.deleted_at IS NULL
	)
	SELECT id, source_type, source_id, source_title, source_deleted, activity_type, content, created_at, updated_at
	FROM agent_activities WHERE ` + whereSQL + fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT %s`, limitP)

	args := append([]interface{}{in.ActorMemberID.String()}, b.args...)

	var recs []agentActivityFeedRecord
	if err := r.db.SelectContext(ctx, &recs, query, args...); err != nil {
		return nil, false, err
	}

	hasMore := len(recs) > limit
	if hasMore {
		recs = recs[:limit]
	}

	result := make([]*agentdom.ActivityFeedItem, 0, len(recs))
	for _, rec := range recs {
		result = append(result, activityFeedItemFromRecord(rec))
	}
	return result, hasMore, nil
}

func activityFeedItemFromRecord(rec agentActivityFeedRecord) *agentdom.ActivityFeedItem {
	content := rec.Content
	if len(content) == 0 {
		content = json.RawMessage("{}")
	}
	return &agentdom.ActivityFeedItem{
		ID:            mustParseUUID(rec.ID),
		SourceType:    agentdom.ActivitySourceType(rec.SourceType),
		SourceID:      mustParseUUID(rec.SourceID),
		SourceTitle:   rec.SourceTitle,
		SourceDeleted: rec.SourceDeleted,
		ActivityType:  rec.ActivityType,
		Content:       content,
		CreatedAt:     rec.CreatedAt,
		UpdatedAt:     rec.UpdatedAt,
	}
}
