package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// SSHKeyRepository is agent-runner's own minimal, read-only view of
// services/api's environment_ssh_keys table (migration
// 000042_add_environments.sql, see docs/ai-agent/environment-management.md).
// The table plus its full CRUD live entirely in services/api — agent-runner
// only ever reads it to render a running environment's authorized_keys
// (ListPublicKeys below); a real sshd inside the environment's own
// container/Pod is what actually authenticates a connection, not this
// process (see docs/ai-agent/environment-management.md's "Terminal / SSH
// Access" section) — there is no fingerprint-lookup call this package needs
// to make on agent-runner's own side at all.
//
// A concrete sqlx-backed struct, mirroring EnvironmentRepository's own
// convention (this codebase has no precedent for repositories-as-
// interfaces).
type SSHKeyRepository struct {
	db *sqlx.DB
}

// NewSSHKeyRepository builds an SSHKeyRepository backed by db.
func NewSSHKeyRepository(db *sqlx.DB) *SSHKeyRepository {
	return &SSHKeyRepository{db: db}
}

// ListPublicKeys returns every public_key registered on environmentID, in
// the exact authorized_keys-line form services/api's AddSSHKey already
// validated and stored — read by internal/sandbox.SyncEnvironmentAuthorizedKeys
// to render the real sshd's AuthorizedKeysFile.
func (r *SSHKeyRepository) ListPublicKeys(ctx context.Context, environmentID uuid.UUID) ([]string, error) {
	var keys []string
	err := r.db.SelectContext(ctx, &keys, `
		SELECT public_key FROM environment_ssh_keys
		WHERE environment_id = $1
		ORDER BY created_at ASC
	`, environmentID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list public keys for environment %s: %w", environmentID, err)
	}
	return keys, nil
}
