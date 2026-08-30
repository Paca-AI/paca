package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// Environment is agent-runner's own minimal read/status-update view of one
// row in services/api's environments table (migration
// 000042_add_environments.sql, see docs/ai-agent/environment-management.md) —
// not the full environmentdom.Environment services/api's own domain package
// owns (a different Go module, unreachable from here — see db.go's package
// doc comment on the same module-boundary convention
// AgentRepository/ConversationRepository already follow). Only the fields
// agent-runner itself needs to cold-start a conversation against a static
// environment and run the idle reaper are included here.
type Environment struct {
	ID     uuid.UUID `db:"id"`
	Status string    `db:"status"`
	// SSHPort is the dedicated external port published straight to this
	// environment's own sshd — a native Docker -p binding or a Kubernetes
	// NodePort Service entry (see sandbox.EnvironmentConfig.PortMappings),
	// never relayed through agent-runner's own process. Assigned once by
	// AssignSSHPort and nil until then, or permanently nil on a deployment
	// that never configured the SSH port range at all. Unlike every other
	// field here, this one is written by agent-runner itself (AssignSSHPort
	// below), not services/api — there is no HTTP round-trip for
	// services/api to report it back through, and services/api's own copy
	// of ssh_port exists purely to display it in the API response (see
	// environmentdom.Environment.SSHPort's doc comment on that module's
	// side).
	SSHPort *int `db:"ssh_port"`
	// BackendRef is nil until CreateEnvironment has actually run once (see
	// executor.Executor.coldStartEnvironment's "not ready" check) — a
	// Docker container ID or a Kubernetes Deployment name, opaque to this
	// package, same as sandbox.Handle.ContainerID.
	BackendRef *string `db:"backend_ref"`
	// Image is nil when the row hasn't opted into a custom image — see
	// sandbox.EnvironmentConfig.Image's doc comment on how empty resolves
	// to the platform default.
	Image       *string `db:"image"`
	CPULimit    string  `db:"cpu_limit"`
	MemoryLimit string  `db:"memory_limit"`
	DiskLimitGB int     `db:"disk_limit_gb"`
	// DockerEnabled — see sandbox.EnvironmentConfig.DockerEnabled's doc
	// comment. Read-only from agent-runner's side, same as every other
	// field here except status/backend_ref/ssh_port.
	DockerEnabled bool    `db:"docker_enabled"`
	VolumeRef     *string `db:"volume_ref"`
	// PortsPendingRestart mirrors services/api's own ports_pending_restart
	// bookkeeping (set by its AddPortForward/DeletePortForward, cleared by
	// RestartEnvironmentPorts) — true when a port forward was added or
	// removed on a running Docker environment since its last (re)start,
	// so the container's actual published ports may not match
	// environment_port_forwards' current rows yet: Docker can't add a -p
	// binding to an already-running container, only an explicit
	// "Restart" click on the Connect page applies the change (see
	// docs/ai-agent/environment-management.md's "Port Forwarding"
	// section); coldStartEnvironment's own attach path never applies it
	// either. Read-only from agent-runner's side, same as DockerEnabled —
	// used by executor.buildEnvironmentContext to caveat the agent-facing
	// port list rather than assert every listed address is live right
	// now.
	PortsPendingRestart bool `db:"ports_pending_restart"`
	// SecretKeyEncrypted must be decrypted with the same secret.Encryptor
	// this process already uses for agents.llm_api_key_secret — see
	// executor.Executor.coldStartEnvironment. Never logged or returned
	// decrypted from this package.
	SecretKeyEncrypted string `db:"secret_key_encrypted"`
	IdleTimeoutMinutes int    `db:"idle_timeout_minutes"`
}

// environmentColumns is shared by every SELECT in this file so
// FindEnvironmentByID and ListIdleRunningEnvironments can't drift out of
// sync with each other's column list.
const environmentColumns = `
	id, status, ssh_port, backend_ref, image, cpu_limit, memory_limit,
	disk_limit_gb, docker_enabled, volume_ref, secret_key_encrypted, idle_timeout_minutes,
	ports_pending_restart
`

// EnvironmentRepository reads and writes agent-runner's own minimal slice
// of the environments table services/api owns the canonical row for — the
// full row lifecycle, including INSERT, stays there (see
// internal/acpbridge/environment_handlers.go's package doc comment);
// agent-runner only ever reads a row and updates its own
// status/backend_ref/last_active_at bookkeeping.
//
// A concrete sqlx-backed struct, not a Go interface — mirrors
// AgentRepository/ConversationRepository's own convention. This codebase
// has no precedent for repositories-as-interfaces, and every existing
// caller (Handler, Executor) already holds a concrete *postgres.X type
// directly.
type EnvironmentRepository struct {
	db *sqlx.DB
}

// NewEnvironmentRepository builds an EnvironmentRepository backed by db.
func NewEnvironmentRepository(db *sqlx.DB) *EnvironmentRepository {
	return &EnvironmentRepository{db: db}
}

// FindEnvironmentByID loads one environment's agent-runner-relevant
// fields. Returns sql.ErrNoRows (via the underlying Get) for a missing or
// soft-deleted environment.
func (r *EnvironmentRepository) FindEnvironmentByID(ctx context.Context, id uuid.UUID) (*Environment, error) {
	var env Environment
	err := r.db.GetContext(ctx, &env, `
		SELECT `+environmentColumns+`
		FROM environments
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	if err != nil {
		return nil, fmt.Errorf("postgres: find environment %s: %w", id, err)
	}
	return &env, nil
}

// UpdateEnvironmentStatus sets status and, unconditionally, error_message
// (mirroring ConversationRepository.UpdateStatus's own "always overwrite,
// including to NULL" convention — see that method's doc comment for why:
// a status transitioning away from an error must actually clear the old
// message, not just leave it stale). backendRef is handled differently —
// COALESCE'd rather than overwritten — since most callers (e.g.
// executor.Executor.coldStartEnvironment re-confirming "running" on every
// turn) pass nil to mean "leave backend_ref exactly as it is";
// services/api's own INSERT is what sets backend_ref the first time (see
// internal/acpbridge/environment_handlers.go), agent-runner never creates
// the row.
func (r *EnvironmentRepository) UpdateEnvironmentStatus(ctx context.Context, id uuid.UUID, status string, backendRef *string, errMsg *string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE environments
		SET status = $1,
		    backend_ref = COALESCE($2, backend_ref),
		    error_message = $3,
		    updated_at = now()
		WHERE id = $4
	`, status, backendRef, errMsg, id)
	if err != nil {
		return fmt.Errorf("postgres: update environment %s status: %w", id, err)
	}
	return nil
}

// ClaimEnvironmentRunning sets id's status to "running" (COALESCE-ing in
// backendRef exactly like UpdateEnvironmentStatus does) UNLESS the row is
// currently "stopping" or "deleting" — the idle reaper's own claim to
// "stopping" (see ClaimEnvironmentStatus below), or an explicit delete in
// flight, must win over a concurrent attach reporting itself started, so a
// turn that raced a stop can't silently revert the DB back to "running"
// out from under it (the actual container may already be stopped by the
// time this runs). Used by executor.Executor.coldStartEnvironment in place
// of a plain UpdateEnvironmentStatus(..., "running", ...) for exactly this
// reason. Reports whether the write actually applied.
func (r *EnvironmentRepository) ClaimEnvironmentRunning(ctx context.Context, id uuid.UUID, backendRef *string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE environments
		SET status = 'running',
		    backend_ref = COALESCE($2, backend_ref),
		    error_message = NULL,
		    updated_at = now()
		WHERE id = $1 AND status NOT IN ('stopping', 'deleting')
	`, id, backendRef)
	if err != nil {
		return false, fmt.Errorf("postgres: claim environment %s running: %w", id, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("postgres: claim environment %s running: rows affected: %w", id, err)
	}
	return n == 1, nil
}

// TouchEnvironment bumps last_active_at to now — called on every
// conversation turn that attaches to an environment, and periodically
// (every 30s) while a browser terminal session is open, so the idle
// reaper's clock reflects real activity rather than just when the
// environment was last started.
func (r *EnvironmentRepository) TouchEnvironment(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE environments SET last_active_at = now(), updated_at = now() WHERE id = $1`,
		id)
	if err != nil {
		return fmt.Errorf("postgres: touch environment %s: %w", id, err)
	}
	return nil
}

// ClaimEnvironmentStatus atomically transitions id from fromStatus to
// toStatus and reports whether this call won the race — mirrors
// ConversationRepository.FailIfNotTerminal's atomic
// UPDATE-with-a-status-guard-then-check-RowsAffected pattern, this
// codebase's established way of doing a compare-and-swap against a status
// column (there is no separate ClaimConversationStatus method on
// ConversationRepository to mirror despite that name appearing in this
// feature's design doc — FailIfNotTerminal is the actual precedent it was
// describing). Used by the idle reaper (cmd/agent-runner/main.go) so two
// agent-runner replicas racing to stop the same idle environment can't
// both "win" and double-stop it.
func (r *EnvironmentRepository) ClaimEnvironmentStatus(ctx context.Context, id uuid.UUID, fromStatus, toStatus string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE environments SET status = $1, updated_at = now()
		WHERE id = $2 AND status = $3
	`, toStatus, id, fromStatus)
	if err != nil {
		return false, fmt.Errorf("postgres: claim environment %s status %s->%s: %w", id, fromStatus, toStatus, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("postgres: claim environment %s status %s->%s: rows affected: %w", id, fromStatus, toStatus, err)
	}
	return n == 1, nil
}

// ListIdleRunningEnvironments returns every running environment whose
// last_active_at is older than its own idle_timeout_minutes — that column
// is per-row, not a single global config, so this compares against
// NOW() - (idle_timeout_minutes || ' minutes')::interval per row rather
// than a fixed Go-side duration. Read by the idle reaper goroutine in
// cmd/agent-runner/main.go, sibling to reapIdleChatSandboxes.
func (r *EnvironmentRepository) ListIdleRunningEnvironments(ctx context.Context) ([]*Environment, error) {
	var envs []*Environment
	err := r.db.SelectContext(ctx, &envs, `
		SELECT `+environmentColumns+`
		FROM environments
		WHERE status = 'running'
		  AND deleted_at IS NULL
		  AND last_active_at < NOW() - (idle_timeout_minutes || ' minutes')::interval
	`)
	if err != nil {
		return nil, fmt.Errorf("postgres: list idle running environments: %w", err)
	}
	return envs, nil
}

// ListRunningEnvironments returns every non-deleted environment whose
// status is "running" — every one, regardless of last_active_at, unlike
// ListIdleRunningEnvironments above (which deliberately excludes recently-
// active rows for the idle reaper's own purpose). Read once at startup by
// cmd/agent-runner/main.go's reconcileEnvironmentsOnStartup to verify each
// one's backing container/Pod still actually exists — status="running" can
// only go stale relative to reality while agent-runner itself isn't
// running to keep it in sync (a manual `docker rm`/`kubectl delete`, a host
// reboot, `docker system prune`), so this has nothing to do with how
// recently the environment was used.
func (r *EnvironmentRepository) ListRunningEnvironments(ctx context.Context) ([]*Environment, error) {
	var envs []*Environment
	err := r.db.SelectContext(ctx, &envs, `
		SELECT `+environmentColumns+`
		FROM environments
		WHERE status = 'running'
		  AND deleted_at IS NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("postgres: list running environments: %w", err)
	}
	return envs, nil
}

// TryLockEnvironmentForReconcile attempts to acquire a non-blocking,
// connection-scoped Postgres advisory lock scoped to id — cmd/agent-runner/
// main.go's startup reconciliation calls this before ever touching the
// backend, so two agent-runner replicas that both restart around the same
// time (a rolling deploy, or any deployment running agentRunner.replicaCount
// > 1) don't independently call SandboxMgr.StartEnvironment against the
// same container/Pod concurrently — the same "multiple agent-runner
// replicas can't double-act on the same environment" guarantee
// ClaimEnvironmentStatus already gives the idle reaper (see
// docs/ai-agent/environment-management.md's Idle-suspend section), applied
// here before the side-effecting call instead of after it.
//
// Deliberately session-scoped (pg_try_advisory_lock on a connection
// reserved via Connx) rather than this package's other advisory-lock idiom
// — a transaction-scoped pg_advisory_xact_lock via WithTx, see
// services/api's AutomationRepository.DeletePendingAgentWaitAndCountRemaining
// — because the critical section this guards spans a real Docker/
// Kubernetes API call that can legitimately run for sandbox's own
// readyTimeout (120s), far longer than this codebase's other advisory-lock
// use ever expects to hold a single pooled connection inside an open
// transaction for.
//
// Never blocks: pg_try_advisory_lock returns immediately with acquired =
// false when another connection (this same process's own concurrent
// reconcile of a different environment can't collide — the key is
// per-environment — but another replica's reconcile pass can) already
// holds id's lock, so the caller can just skip that environment for this
// pass rather than queue up behind it.
//
// The returned release func closes the dedicated connection this reserved
// from the pool — safe to call even when acquired is false (release is
// then just returning an untouched connection), and the caller must call
// it exactly once, typically via defer, whether or not acquired is true.
func (r *EnvironmentRepository) TryLockEnvironmentForReconcile(ctx context.Context, id uuid.UUID) (acquired bool, release func(), err error) {
	conn, err := r.db.Connx(ctx)
	if err != nil {
		return false, func() {}, fmt.Errorf("postgres: reserve connection for environment reconcile lock %s: %w", id, err)
	}

	lockKey := "environment-reconcile:" + id.String()
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1, 0))`, lockKey).Scan(&acquired); err != nil {
		_ = conn.Close()
		return false, func() {}, fmt.Errorf("postgres: try advisory lock for environment %s: %w", id, err)
	}
	if !acquired {
		_ = conn.Close()
		return false, func() {}, nil
	}

	return true, func() {
		// Best-effort: an explicit unlock on a connection about to be
		// closed is a courtesy, not a correctness requirement (Postgres
		// releases every session-level advisory lock automatically when
		// the backend's connection ends, which Close's eventual
		// termination will do regardless) — so a failure here is logged
		// nowhere and never propagated, same as this file's other
		// best-effort cleanup (e.g. removeEnvironmentDindSidecar's
		// counterpart in the docker backend).
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, lockKey)
		_ = conn.Close()
	}, nil
}

// AssignSSHPort picks the lowest currently-unused port in [rangeStart,
// rangeEnd] and persists it as id's ssh_port, in one statement — only when
// ssh_port is still NULL, so a caller can call this unconditionally and
// it's a no-op past the first successful call (see
// internal/acpbridge/environment_handlers.go's assignSSHPort). "Unused"
// means no other non-deleted environment already holds it
// (uq_environments_ssh_port is a partial unique index on exactly that
// condition — see migration 000042_add_environments.sql — so a
// deleted environment's old port is correctly treated as free here).
//
// This is a single UPDATE, not a SELECT-then-UPDATE: the pick and the
// write happen in the same statement specifically so Postgres's own
// unique-index enforcement is the actual race-safety net between two
// agent-runner replicas calling this concurrently for two different
// environments, not an application-level lock this package would
// otherwise need to invent. A genuine collision (both replicas' subqueries
// evaluate against the same pre-collision snapshot and pick the same
// port) surfaces as a unique_violation error from this call — retried up
// to maxPortAssignRetries times rather than given up on immediately: by
// the time of a retry, the other side's own UPDATE has already committed,
// so a fresh attempt's NOT EXISTS check correctly skips that port and
// picks the next free one instead. Without this, the caller (assignSSHPort
// in internal/acpbridge/environment_handlers.go) logged the error and
// carried on treating this as "no port assigned" — silently shipping a
// StatusRunning environment with no published SSH port at all, self-healed
// only if something later happened to call AssignSSHPort again (a
// restart-ports action), never surfaced to the user in the meantime.
func (r *EnvironmentRepository) AssignSSHPort(ctx context.Context, id uuid.UUID, rangeStart, rangeEnd int) (int, error) {
	for attempt := 0; ; attempt++ {
		var port int
		err := r.db.GetContext(ctx, &port, `
			UPDATE environments
			SET ssh_port = (
				SELECT s.port
				FROM generate_series($2::int, $3::int) AS s(port)
				WHERE NOT EXISTS (
					SELECT 1 FROM environments other
					WHERE other.ssh_port = s.port AND other.deleted_at IS NULL
				)
				ORDER BY s.port
				LIMIT 1
			),
			updated_at = now()
			WHERE id = $1 AND ssh_port IS NULL
			RETURNING ssh_port
		`, id, rangeStart, rangeEnd)
		if err == nil {
			return port, nil
		}
		if !isUniqueViolation(err) || attempt >= maxPortAssignRetries {
			return 0, fmt.Errorf("postgres: assign ssh port for environment %s: %w", id, err)
		}
	}
}
