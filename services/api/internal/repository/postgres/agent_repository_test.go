package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// TestAgentFromReadRow_ValidACPCommand verifies the happy path still maps
// acp_command JSONB into a string slice.
func TestAgentFromReadRow_ValidACPCommand(t *testing.T) {
	row := agentRecord{
		ID:         uuid.New().String(),
		ProjectID:  uuid.New().String(),
		AgentType:  "acp",
		ACPCommand: []byte(`["my-server","--flag"]`),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	a, err := agentFromReadRow(row)

	assert.NoError(t, err)
	assert.Equal(t, []string{"my-server", "--flag"}, a.ACPCommand)
}

// TestAgentFromReadRow_MalformedACPCommand verifies that malformed
// acp_command JSONB surfaces as an error rather than being silently dropped
// (previously agentFromReadRow swallowed the json.Unmarshal error, returning
// an agent with an empty ACPCommand as if the column had simply been unset).
func TestAgentFromReadRow_MalformedACPCommand(t *testing.T) {
	row := agentRecord{
		ID:         uuid.New().String(),
		ProjectID:  uuid.New().String(),
		AgentType:  "acp",
		ACPCommand: []byte(`not-valid-json`),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	a, err := agentFromReadRow(row)

	assert.Error(t, err)
	assert.Nil(t, a)
}
