package environmentdom

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the storage contract for the Environment aggregate.
type Repository interface {
	EnvironmentRepository
	FolderRepository
	SSHKeyRepository
	PortForwardRepository
}

// EnvironmentRepository defines storage operations for environments.
type EnvironmentRepository interface {
	ListEnvironments(ctx context.Context, projectID uuid.UUID) ([]*Environment, error)
	FindEnvironmentByID(ctx context.Context, id uuid.UUID) (*Environment, error)
	// FindVisibleEnvironmentInProject returns a single environment by ID,
	// but only if it belongs to projectID — the environment counterpart to
	// agentdom.AgentRepository.FindVisibleAgentInProject. Returns
	// ErrEnvironmentNotFound otherwise.
	FindVisibleEnvironmentInProject(ctx context.Context, projectID, environmentID uuid.UUID) (*Environment, error)
	CreateEnvironment(ctx context.Context, e *Environment) error
	UpdateEnvironment(ctx context.Context, e *Environment) error
	// UpdateEnvironmentStatus sets status (and, when non-nil, backendRef/
	// errMsg) in one call — the common transition agent-runner reports back
	// after Create/Start/Stop/Delete.
	UpdateEnvironmentStatus(ctx context.Context, id uuid.UUID, status string, backendRef, errMsg *string) error
	// UpdateEnvironmentProvisioning persists status, backend, backend_ref,
	// and volume_ref together in one write — the outcome of a *successful*
	// CreateEnvironment call to agent-runner. Additive beyond
	// UpdateEnvironmentStatus (added while wiring environment_service.go):
	// UpdateEnvironmentStatus's signature has no room for a backend change
	// or volume_ref, since every other status transition (Start/Stop/error)
	// never needs to report either — only the very first successful
	// provisioning call, which is also the only time backend (chosen by
	// agent-runner, not services/api — see Environment.Backend's doc
	// comment) is actually known.
	UpdateEnvironmentProvisioning(ctx context.Context, id uuid.UUID, status, backend, backendRef, volumeRef string) error
	// TouchEnvironment bumps last_active_at to now — called on conversation
	// attach/turn-end and on the browser terminal's periodic heartbeat.
	TouchEnvironment(ctx context.Context, id uuid.UUID) error
	SoftDeleteEnvironment(ctx context.Context, id uuid.UUID) error
	// SlugTaken reports whether slug is already used by a non-deleted
	// environment in projectID.
	SlugTaken(ctx context.Context, projectID uuid.UUID, slug string) (bool, error)
	// SetPortsPendingRestart writes Environment.PortsPendingRestart — see
	// that field's own doc comment.
	SetPortsPendingRestart(ctx context.Context, id uuid.UUID, pending bool) error
}

// FolderRepository defines storage for environment folders.
type FolderRepository interface {
	ListFolders(ctx context.Context, environmentID uuid.UUID) ([]*EnvironmentFolder, error)
	FindFolderByID(ctx context.Context, id uuid.UUID) (*EnvironmentFolder, error)
	CreateFolder(ctx context.Context, f *EnvironmentFolder) error
	DeleteFolder(ctx context.Context, id uuid.UUID) error
}

// SSHKeyRepository defines storage for environment SSH keys.
type SSHKeyRepository interface {
	ListSSHKeys(ctx context.Context, environmentID uuid.UUID) ([]*EnvironmentSSHKey, error)
	// FindSSHKeyByID returns a single SSH key by ID — mirrors
	// FolderRepository.FindFolderByID's own ownership-check-before-delete
	// use (Service.DeleteSSHKey), so deleting a key doesn't need to list
	// and scan every key on the environment just to check one exists.
	// Returns ErrSSHKeyNotFound otherwise.
	FindSSHKeyByID(ctx context.Context, id uuid.UUID) (*EnvironmentSSHKey, error)
	CreateSSHKey(ctx context.Context, k *EnvironmentSSHKey) error
	DeleteSSHKey(ctx context.Context, id uuid.UUID) error
	// FindSSHKeyByFingerprint resolves the environment an inbound SSH
	// connection is authorized for, by the public key fingerprint it
	// presented — the Phase 3 bastion's sole lookup path. Returns
	// ErrSSHKeyNotFound if no live key matches.
	FindSSHKeyByFingerprint(ctx context.Context, environmentID uuid.UUID, fingerprint string) (*EnvironmentSSHKey, error)
}

// PortForwardRepository defines storage for environment port forwards.
type PortForwardRepository interface {
	ListPortForwards(ctx context.Context, environmentID uuid.UUID) ([]*EnvironmentPortForward, error)
	CreatePortForward(ctx context.Context, pf *EnvironmentPortForward) error
	DeletePortForward(ctx context.Context, id uuid.UUID) error
	// FindPortForwardByID returns a single port forward by ID — used to
	// read its HostPort back before deleting, so the service layer can
	// tell agent-runner which port to immediately deregister.
	FindPortForwardByID(ctx context.Context, id uuid.UUID) (*EnvironmentPortForward, error)
}
