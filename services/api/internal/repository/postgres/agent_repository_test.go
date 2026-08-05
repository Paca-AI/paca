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

// TestActivityFeedItemFromRecord_MapsFields verifies the UNION row mapping
// preserves source type/id and title needed to render and link each row.
func TestActivityFeedItemFromRecord_MapsFields(t *testing.T) {
	id := uuid.New()
	sourceID := uuid.New()
	rec := agentActivityFeedRecord{
		ID:           id.String(),
		SourceType:   "task",
		SourceID:     sourceID.String(),
		SourceTitle:  "Fix the bug",
		ActivityType: "task.created",
		Content:      []byte(`{"foo":"bar"}`),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	item := activityFeedItemFromRecord(rec)

	assert.Equal(t, id, item.ID)
	assert.Equal(t, agentdom.ActivitySourceTask, item.SourceType)
	assert.Equal(t, sourceID, item.SourceID)
	assert.Equal(t, "Fix the bug", item.SourceTitle)
	assert.False(t, item.SourceDeleted)
	assert.Equal(t, "task.created", item.ActivityType)
	assert.JSONEq(t, `{"foo":"bar"}`, string(item.Content))
}

// TestActivityFeedItemFromRecord_EmptyContentDefaultsToEmptyObject mirrors
// ActivityFromEntity's task-activity behavior: a nil/empty content column
// (the JSONB default is '{}', but defensively handle a genuinely empty
// value too) surfaces as "{}" rather than an invalid empty json.RawMessage.
func TestActivityFeedItemFromRecord_EmptyContentDefaultsToEmptyObject(t *testing.T) {
	rec := agentActivityFeedRecord{
		ID:         uuid.New().String(),
		SourceType: "doc",
		SourceID:   uuid.New().String(),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	item := activityFeedItemFromRecord(rec)

	assert.Equal(t, agentdom.ActivitySourceDoc, item.SourceType)
	assert.JSONEq(t, `{}`, string(item.Content))
}

// TestActivityFeedItemFromRecord_PreservesSourceDeleted verifies that an
// activity whose task/doc has since been (soft-)deleted still maps through
// with SourceDeleted set — the feed keeps the activity (e.g. the delete
// action itself is history worth showing) but flags it so the UI can skip
// linking to a source that no longer resolves.
func TestActivityFeedItemFromRecord_PreservesSourceDeleted(t *testing.T) {
	rec := agentActivityFeedRecord{
		ID:            uuid.New().String(),
		SourceType:    "task",
		SourceID:      uuid.New().String(),
		SourceTitle:   "Fix the bug",
		SourceDeleted: true,
		ActivityType:  "task.deleted",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	item := activityFeedItemFromRecord(rec)

	assert.True(t, item.SourceDeleted)
	assert.Equal(t, "Fix the bug", item.SourceTitle)
}
