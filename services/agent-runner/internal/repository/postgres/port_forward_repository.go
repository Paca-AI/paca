package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// PortForward is agent-runner's own minimal read/write view of one row in
// services/api's environment_port_forwards table (migration
// 000042_add_environments.sql, see docs/ai-agent/
// environment-management.md's "Port Forwarding" section) — not the full
// environmentdom.EnvironmentPortForward services/api's own domain package
// owns (a different Go module, unreachable from here — see
// EnvironmentRepository's own doc comment on the same module-boundary
// convention). created_by/created_at are never read here since nothing in
// agent-runner needs them. label IS read (unlike an earlier version of
// this struct): executor.buildEnvironmentContext includes it in the
// agent-facing note listing an environment's port forwards, so an agent
// can tell a listed port apart from another by more than a bare number.
type PortForward struct {
	ID            uuid.UUID `db:"id"`
	EnvironmentID uuid.UUID `db:"environment_id"`
	// Label is the user-supplied name for this forward (e.g. "frontend
	// dev server") — always non-empty (label TEXT NOT NULL in the
	// migration).
	Label         string `db:"label"`
	ContainerPort int    `db:"container_port"`
	// HostPort is the dedicated external port published straight to
	// ContainerPort inside this row's environment — a native Docker -p
	// binding or a Kubernetes NodePort Service entry (see
	// sandbox.EnvironmentConfig.PortMappings), never relayed through
	// agent-runner's own process. Assigned once by AssignHostPort and nil
	// until then — mirrors Environment.SSHPort's own "generated once,
	// agent-runner-owned" lifecycle exactly (see that field's doc comment).
	HostPort *int `db:"host_port"`
}

// portForwardColumns is shared by every SELECT in this file so the
// various list/find methods can't drift out of sync with each other's
// column list.
const portForwardColumns = `id, environment_id, label, container_port, host_port`

// PortForwardRepository reads and writes agent-runner's own minimal slice
// of the environment_port_forwards table services/api owns the canonical
// row for — the full row lifecycle (INSERT on user add, DELETE on user
// remove) stays there; agent-runner only ever reads a row and assigns its
// own host_port.
//
// A concrete sqlx-backed struct, mirroring EnvironmentRepository/
// SSHKeyRepository's own convention.
type PortForwardRepository struct {
	db *sqlx.DB
}

// NewPortForwardRepository builds a PortForwardRepository backed by db.
func NewPortForwardRepository(db *sqlx.DB) *PortForwardRepository {
	return &PortForwardRepository{db: db}
}

// ListForEnvironment returns every port forward row for environmentID,
// regardless of assignment state — read by
// internal/acpbridge/environment_handlers.go's handlePortForwardsAssign
// (to assign a host_port to any row that doesn't have one yet) and
// buildPortMappings (to assemble the full set a container/Service should
// publish).
func (r *PortForwardRepository) ListForEnvironment(ctx context.Context, environmentID uuid.UUID) ([]*PortForward, error) {
	var pfs []*PortForward
	err := r.db.SelectContext(ctx, &pfs, `
		SELECT `+portForwardColumns+`
		FROM environment_port_forwards
		WHERE environment_id = $1
		ORDER BY created_at ASC
	`, environmentID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list port forwards for environment %s: %w", environmentID, err)
	}
	return pfs, nil
}

// AssignHostPort picks the lowest currently-unused port in [rangeStart,
// rangeEnd] and persists it as id's host_port, in one statement — only
// when host_port is still NULL, so a caller can call this unconditionally
// and it's a no-op past the first successful call. Mirrors
// EnvironmentRepository.AssignSSHPort's single-UPDATE race-safety
// reasoning exactly, retry included (see that method's own doc comment for
// why a unique_violation collision is retried rather than given up on
// immediately) — "unused" means no other row already holds it
// (uq_environment_port_forwards_host_port is a plain unique index over the
// whole table, not partial like uq_environments_ssh_port, since this table
// is hard-deleted so a removed row's port is never left behind to
// reclaim).
func (r *PortForwardRepository) AssignHostPort(ctx context.Context, id uuid.UUID, rangeStart, rangeEnd int) (int, error) {
	for attempt := 0; ; attempt++ {
		var port int
		err := r.db.GetContext(ctx, &port, `
			UPDATE environment_port_forwards
			SET host_port = (
				SELECT s.port
				FROM generate_series($2::int, $3::int) AS s(port)
				WHERE NOT EXISTS (
					SELECT 1 FROM environment_port_forwards other
					WHERE other.host_port = s.port
				)
				ORDER BY s.port
				LIMIT 1
			)
			WHERE id = $1 AND host_port IS NULL
			RETURNING host_port
		`, id, rangeStart, rangeEnd)
		if err == nil {
			return port, nil
		}
		if !isUniqueViolation(err) || attempt >= maxPortAssignRetries {
			return 0, fmt.Errorf("postgres: assign host port for port forward %s: %w", id, err)
		}
	}
}
