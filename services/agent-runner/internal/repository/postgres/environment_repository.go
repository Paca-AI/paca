package postgres

import (
	"context"
	"fmt"

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
	VolumeRef   *string `db:"volume_ref"`
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
	disk_limit_gb, volume_ref, secret_key_encrypted, idle_timeout_minutes
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
