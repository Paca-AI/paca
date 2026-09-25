package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
)

// TestAgentFromReadRow_ValidACPCommand verifies the happy path still maps
// acp_command JSONB into a string slice.
func TestAgentFromReadRow_ValidACPCommand(t *testing.T) {
	row := agentRecord{
		ID:         uuid.New().String(),
		ProjectID:  strPtr(uuid.New().String()),
		AgentType:  "acp",
		ACPCommand: []byte(`["my-server","--flag"]`),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	a, err := agentFromReadRow(row)

	assert.NoError(t, err)
	assert.Equal(t, []string{"my-server", "--flag"}, a.ACPCommand)
	assert.Equal(t, agentdom.AgentScopeProject, a.AgentScope)
}

// TestAgentFromReadRow_MalformedACPCommand verifies that malformed
// acp_command JSONB surfaces as an error rather than being silently dropped
// (previously agentFromReadRow swallowed the json.Unmarshal error, returning
// an agent with an empty ACPCommand as if the column had simply been unset).
func TestAgentFromReadRow_MalformedACPCommand(t *testing.T) {
	row := agentRecord{
		ID:         uuid.New().String(),
		ProjectID:  strPtr(uuid.New().String()),
		AgentType:  "acp",
		ACPCommand: []byte(`not-valid-json`),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	a, err := agentFromReadRow(row)

	assert.Error(t, err)
	assert.Nil(t, a)
}

// TestAgentFromReadRow_GlobalAgent verifies a global-scope agent row (NULL
// project_id, agent_scope='global', global_role_id set) maps to the
// zero-value ProjectID sentinel (uuid.Nil) rather than erroring — the
// mustParseUUID(nil-safe) conversion path exercised by uuidFromNullable.
func TestAgentFromReadRow_GlobalAgent(t *testing.T) {
	roleID := uuid.New()
	row := agentRecord{
		ID:           uuid.New().String(),
		ProjectID:    nil,
		AgentScope:   "global",
		GlobalRoleID: strPtr(roleID.String()),
		AgentType:    "llm",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	a, err := agentFromReadRow(row)

	assert.NoError(t, err)
	assert.Equal(t, uuid.Nil, a.ProjectID)
	assert.Equal(t, agentdom.AgentScopeGlobal, a.AgentScope)
	if assert.NotNil(t, a.GlobalRoleID) {
		assert.Equal(t, roleID, *a.GlobalRoleID)
	}
}

// TestAgentToRecord_GlobalAgent verifies the inverse mapping: a domain Agent
// with ProjectID == uuid.Nil (global scope) serializes to a nil project_id
// record field rather than the all-zeros UUID string.
func TestAgentToRecord_GlobalAgent(t *testing.T) {
	roleID := uuid.New()
	a := &agentdom.Agent{
		ID:           uuid.New(),
		AgentScope:   agentdom.AgentScopeGlobal,
		GlobalRoleID: &roleID,
		Handle:       "global-bot",
		Name:         "Global Bot",
		AgentType:    "llm",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	rec, err := agentToRecord(a)

	assert.NoError(t, err)
	assert.Nil(t, rec.ProjectID)
	assert.Equal(t, "global", rec.AgentScope)
	if assert.NotNil(t, rec.GlobalRoleID) {
		assert.Equal(t, roleID.String(), *rec.GlobalRoleID)
	}
}
