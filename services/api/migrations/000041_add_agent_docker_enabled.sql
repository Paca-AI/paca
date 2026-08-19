-- 000041_add_agent_docker_enabled.sql
-- Adds docker_enabled: a per-agent opt-in for agent-runner's per-conversation
-- Docker-in-Docker sidecar (see services/agent-runner/internal/sandbox/dind.go).
-- That sidecar is a privileged container plus a private network started on
-- every cold-started conversation, whether or not the agent ever uses it —
-- real per-session latency and resource cost most agents never need. Default
-- FALSE: only agents that actually run Docker commands should opt in.
--
-- IF NOT EXISTS so this migration is safe to re-run.

BEGIN;

ALTER TABLE agents ADD COLUMN IF NOT EXISTS docker_enabled BOOLEAN NOT NULL DEFAULT FALSE;

COMMIT;
