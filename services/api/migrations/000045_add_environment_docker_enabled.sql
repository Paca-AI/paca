-- 000045_add_environment_docker_enabled.sql
-- Adds docker_enabled: a per-environment opt-in for agent-runner's
-- long-lived Docker-in-Docker sidecar (see
-- services/agent-runner/internal/sandbox/docker/environment_dind.go and
-- services/agent-runner/internal/sandbox/k8s/dind.go), the static-environment
-- counterpart to agents.docker_enabled (migration 000041). Decided once at
-- CreateEnvironment time — unlike agents.docker_enabled, this column is
-- never patched after creation (see UpdateEnvironmentRequest, which has no
-- docker_enabled field): the sidecar's own network/container pairing is
-- baked in when the environment's container is first created, so changing
-- it later would require the same "delete and recreate" step CPULimit/
-- MemoryLimit already require. Default FALSE: only environments that
-- actually need to run Docker commands should opt in.
--
-- IF NOT EXISTS so this migration is safe to re-run.

BEGIN;

ALTER TABLE environments ADD COLUMN IF NOT EXISTS docker_enabled BOOLEAN NOT NULL DEFAULT FALSE;

COMMIT;
