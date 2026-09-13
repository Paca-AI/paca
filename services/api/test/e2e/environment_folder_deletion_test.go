package e2e_test

import (
	"testing"

	"github.com/google/uuid"

	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
)

// TestDeleteFolder_SucceedsWithPendingTriggerStillReferencingIt is the
// regression guard for a schema bug found in code review on PR #466:
// migration 000053 added agent_pending_triggers.environment_id/
// environment_folder_id referencing environments/environment_folders with
// no ON DELETE action (the default, NO ACTION), unlike the sibling columns
// on agent_conversations (migration 000042), which use ON DELETE SET NULL
// specifically so environment/folder deletion is never blocked by history.
//
// Before the fix, EnvironmentRepository.DeleteFolder's plain `DELETE FROM
// environment_folders` failed with an unhandled foreign-key-violation
// whenever the folder still had a row in agent_pending_triggers pointing at
// it — precisely the steady state for any environment-backed agent sitting
// at its parallelism_limit (or blocked by another conversation occupying
// the same shared folder), which is the primary scenario this whole
// feature exists for. presenter.Error has no mapping for a raw driver
// error, so this surfaced to the client as a bare 500 "internal server
// error" instead of the folder simply being deleted.
//
// This seeds that exact state directly against the real, migrated schema
// (not a synthetic one) and calls the same repository method the HTTP
// handler uses, so a future re-introduction of the missing ON DELETE
// action is caught here rather than by a confused user report.
func TestDeleteFolder_SucceedsWithPendingTriggerStillReferencingIt(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	projectID := uuid.New().String()
	environmentID := uuid.New().String()
	folderID := uuid.New().String()
	agentID := uuid.New().String()
	conversationID := uuid.New().String()
	pendingID := uuid.New().String()

	if _, err := env.db.ExecContext(env.ctx,
		`INSERT INTO projects (id, name) VALUES ($1, 'fk-test-project')`,
		projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, `
		INSERT INTO environments (id, project_id, name, slug, backend, secret_key_encrypted)
		VALUES ($1, $2, 'fk-test-env', 'fk-test-env', 'docker', 'unused')`,
		environmentID, projectID); err != nil {
		t.Fatalf("insert environment: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, `
		INSERT INTO environment_folders (id, environment_id, path) VALUES ($1, $2, '/repo')`,
		folderID, environmentID); err != nil {
		t.Fatalf("insert environment folder: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, `
		INSERT INTO agents (id, project_id, name, handle, llm_provider, llm_model, llm_api_key_secret, default_environment_id)
		VALUES ($1, $2, 'FK Test Agent', 'fk-test-agent', 'anthropic', 'claude', 'unused', $3)`,
		agentID, projectID, environmentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	// status='queued': this conversation is waiting in the backlog for the
	// folder below to free up — the exact condition that produced the stuck
	// agent_pending_triggers row this test is about.
	if _, err := env.db.ExecContext(env.ctx, `
		INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, status, environment_id, environment_folder_id)
		VALUES ($1, $2, $3, 'chat_message', 'queued', $4, $5)`,
		conversationID, agentID, projectID, environmentID, folderID); err != nil {
		t.Fatalf("insert agent conversation: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, `
		INSERT INTO agent_pending_triggers (id, agent_id, conversation_id, topic, payload, environment_id, environment_folder_id)
		VALUES ($1, $2, $3, 'agent.chat_message', '{}'::jsonb, $4, $5)`,
		pendingID, agentID, conversationID, environmentID, folderID); err != nil {
		t.Fatalf("insert pending trigger: %v", err)
	}

	envRepo := pgRepo.NewEnvironmentRepository(env.db)
	if err := envRepo.DeleteFolder(env.ctx, uuid.MustParse(folderID)); err != nil {
		t.Fatalf("DeleteFolder must not fail just because a queued trigger still references this folder: %v", err)
	}

	var gotFolderID *string
	if err := env.db.GetContext(env.ctx, &gotFolderID,
		`SELECT environment_folder_id FROM agent_pending_triggers WHERE id = $1`, pendingID); err != nil {
		t.Fatalf("the pending trigger row itself must survive the folder's deletion: %v", err)
	}
	if gotFolderID != nil {
		t.Fatalf("environment_folder_id must be nulled out by ON DELETE SET NULL, got %q", *gotFolderID)
	}
}
