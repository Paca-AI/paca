package environmentdom

import (
	"context"

	"github.com/google/uuid"
)

// Service is the combined Environment service contract.
type Service interface {
	EnvironmentService
	FolderService
	SSHKeyService
	PortForwardService
}

// EnvironmentService defines environment CRUD and lifecycle use cases.
type EnvironmentService interface {
	ListEnvironments(ctx context.Context, projectID uuid.UUID) ([]*Environment, error)
	GetEnvironment(ctx context.Context, projectID, environmentID uuid.UUID) (*Environment, error)
	// CreateEnvironment writes the environment row (status StatusCreating),
	// asks agent-runner to provision its backing container/Pod and volume,
	// then updates status to StatusRunning (or StatusError, with
	// ErrorMessage set, if provisioning failed) before returning.
	CreateEnvironment(ctx context.Context, projectID uuid.UUID, in CreateEnvironmentInput) (*Environment, error)
	UpdateEnvironment(ctx context.Context, projectID, environmentID uuid.UUID, in UpdateEnvironmentInput) (*Environment, error)
	// StartEnvironment asks agent-runner to restart a stopped/suspended
	// environment's existing container/Pod. No-op (returns the environment
	// unchanged) if it's already running.
	StartEnvironment(ctx context.Context, projectID, environmentID uuid.UUID) (*Environment, error)
	// StopEnvironment asks agent-runner to stop the environment's
	// container/Pod without deleting it or its volume.
	StopEnvironment(ctx context.Context, projectID, environmentID uuid.UUID) (*Environment, error)
	// RestartEnvironment applies any pending port-forward changes to a
	// currently-running environment's backing container/Pod — the
	// explicit user-facing counterpart to StartEnvironment's own implicit
	// "recreate instead of plain-start when dirty" behavior, for a user
	// clicking "Restart" after adding/removing a forward without wanting
	// to stop the environment first. Requires the environment to already
	// be StatusRunning (ErrEnvironmentBusy otherwise) — a stopped
	// environment's pending changes are applied automatically on its next
	// StartEnvironment instead, with no separate action needed.
	RestartEnvironment(ctx context.Context, projectID, environmentID uuid.UUID) (*Environment, error)
	// DeleteEnvironment asks agent-runner to permanently remove the
	// environment's container/Pod and volume, then soft-deletes the row.
	DeleteEnvironment(ctx context.Context, projectID, environmentID uuid.UUID) error
	// Heartbeat bumps last_active_at — called periodically by the browser
	// terminal while it's open, and by agent-runner on every conversation
	// attach/turn-end, so the idle reaper never stops an environment with
	// something actively using it.
	Heartbeat(ctx context.Context, projectID, environmentID uuid.UUID) error
	// ResolveConversationWorkdir validates that environmentID (when set)
	// belongs to projectID and, together with folderID, resolves the exact
	// folder a new conversation should work in: folderID is used directly
	// if set; otherwise the environment's sole folder is auto-selected,
	// mirroring the existing single-agent auto-select convention in the
	// frontend's useAgentPicker. Returns ErrFolderNotFound if folderID
	// doesn't resolve, or if none is given and the environment has more
	// than one folder (ambiguous — the caller must ask the user to pick).
	// environmentID == nil is a valid input (no environment attached to
	// this conversation at all) and returns (nil, nil, nil).
	ResolveConversationWorkdir(ctx context.Context, projectID uuid.UUID, environmentID, folderID *uuid.UUID) (env *Environment, folder *EnvironmentFolder, err error)
}

// FolderService defines folder CRUD use cases.
type FolderService interface {
	ListFolders(ctx context.Context, projectID, environmentID uuid.UUID) ([]*EnvironmentFolder, error)
	// AddFolder creates the folder row and asks agent-runner to mkdir it
	// into place inside the environment's container.
	AddFolder(ctx context.Context, projectID, environmentID uuid.UUID, in AddFolderInput) (*EnvironmentFolder, error)
	DeleteFolder(ctx context.Context, projectID, environmentID, folderID uuid.UUID) error
	// Browse lists the immediate children of path inside environmentID's
	// running container/Pod — used by the folder-creation UI to let a
	// user navigate to an existing directory instead of typing its path
	// blind. path defaults to agent-runner's fixed folders root when
	// empty. Requires the environment to be StatusRunning
	// (ErrEnvironmentNotRunning otherwise) since there's no live
	// filesystem to read from a stopped container/Pod.
	Browse(ctx context.Context, projectID, environmentID uuid.UUID, path string) (resolvedPath string, entries []BrowseEntry, err error)
}

// SSHKeyService defines SSH key CRUD use cases. Pure CRUD against
// environment_ssh_keys — no agent-runner round-trip, since nothing
// consumes these keys until the Phase 3 bastion exists.
type SSHKeyService interface {
	ListSSHKeys(ctx context.Context, projectID, environmentID uuid.UUID) ([]*EnvironmentSSHKey, error)
	AddSSHKey(ctx context.Context, projectID, environmentID uuid.UUID, in AddSSHKeyInput) (*EnvironmentSSHKey, error)
	DeleteSSHKey(ctx context.Context, projectID, environmentID, keyID uuid.UUID) error
}

// PortForwardService defines port-forward CRUD use cases — a user-managed
// sibling to the environment's own auto-created SSH port (see
// Environment.SSHPort's doc comment), one row per container port they want
// reachable from outside.
type PortForwardService interface {
	ListPortForwards(ctx context.Context, projectID, environmentID uuid.UUID) ([]*EnvironmentPortForward, error)
	// AddPortForward creates the port-forward row and, if the environment
	// is currently running, asks agent-runner to assign it a host port and
	// start relaying immediately rather than waiting for the environment's
	// next Start.
	AddPortForward(ctx context.Context, projectID, environmentID uuid.UUID, in AddPortForwardInput) (*EnvironmentPortForward, error)
	// DeletePortForward removes the row and, if it had already been
	// assigned a host port, asks agent-runner to stop relaying it
	// immediately rather than waiting for the next reconcile tick.
	DeletePortForward(ctx context.Context, projectID, environmentID, portForwardID uuid.UUID) error
}

// --- Input types ---

// CreateEnvironmentInput carries fields required to create an environment.
// CPULimit/MemoryLimit/DiskLimitGB are optional advanced overrides — nil
// means "use the same defaults migration 000042 gives the column" and the
// service is responsible for applying them explicitly rather than relying
// on the DB default, since every column is written on insert.
type CreateEnvironmentInput struct {
	Name string
	// Image is nil unless the user explicitly opted into a custom image —
	// see Environment.Image's doc comment.
	Image       *string
	CPULimit    *string
	MemoryLimit *string
	DiskLimitGB *int
	CreatedBy   *uuid.UUID
}

// UpdateEnvironmentInput carries mutable environment fields.
type UpdateEnvironmentInput struct {
	Name               *string
	IdleTimeoutMinutes *int
}

// AddFolderInput carries fields to add a folder to an environment.
type AddFolderInput struct {
	Path      string
	CreatedBy *uuid.UUID
}

// AddSSHKeyInput carries fields to register an SSH public key. PublicKey is
// the raw "ssh-ed25519 AAAA... comment" line — the service parses it and
// derives Fingerprint, rather than trusting a client-supplied fingerprint.
type AddSSHKeyInput struct {
	Label     string
	PublicKey string
	CreatedBy *uuid.UUID
}

// AddPortForwardInput carries fields to add a port forward to an
// environment.
type AddPortForwardInput struct {
	Label         string
	ContainerPort int
	CreatedBy     *uuid.UUID
}
