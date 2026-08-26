// Package environmentdom defines the Environment aggregate — a static,
// long-lived sandbox a user explicitly creates, which agent conversations
// can attach to instead of getting a fresh ephemeral sandbox every time.
// See docs/ai-agent/environment-management.md for the full design.
package environmentdom

import (
	"time"

	"github.com/google/uuid"
)

// Status values an Environment can be in. Mirrors the environments.status
// CHECK constraint (migration 000042_add_environments.sql) exactly.
const (
	StatusCreating  = "creating"
	StatusRunning   = "running"
	StatusStopping  = "stopping"
	StatusStopped   = "stopped"
	StatusSuspended = "suspended"
	StatusError     = "error"
	StatusDeleting  = "deleting"
)

// Backend values an Environment can run on. Mirrors
// services/agent-runner's SANDBOX_BACKEND values (config.Settings.SandboxBackend) —
// an environment always runs on whichever backend agent-runner itself is
// currently configured for, never a per-environment choice.
const (
	BackendDocker     = "docker"
	BackendKubernetes = "kubernetes"
)

// Environment is a named, long-lived sandbox a user explicitly creates and
// manages. Agents attach to it via EnvironmentFolder instead of getting a
// fresh disposable sandbox — see agent.AgentConversation.EnvironmentID.
type Environment struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
	// Slug is unique per project (uq_environments_project_slug) — the
	// user-facing identifier, editable-in-spirit like agents.handle even
	// though nothing currently exposes renaming it.
	Slug string
	// SSHPort is the dedicated external port a real `ssh` client connects
	// to reach this environment's own sshd — a native Docker -p binding or
	// a Kubernetes NodePort Service entry, published straight onto the
	// backing container/Pod (never relayed through agent-runner's own
	// process). Assigned once by agent-runner from its configured port
	// range and nil until the first successful CreateEnvironment call
	// returns. The one thing auto-created without any user action —
	// contrast with EnvironmentPortForward below, which a user adds
	// explicitly, one row per container port they want reachable
	// (replacing what used to be a single server-generated SubdomainSlug
	// field routed by Caddy — see docs/ai-agent/
	// environment-management.md's "Port Forwarding" section for why: some
	// self-hosted deployments can only forward one port through their
	// router/firewall at all, ruling out Caddy's shared-port-plus-
	// wildcard-DNS model the same way it already ruled one out for SSH).
	// SSHPort is written directly by agent-runner's own Postgres
	// connection (same precedent as it writing status/last_active_at
	// directly — see AgentRepository's doc comment on that
	// module-boundary convention), never by services/api itself — this
	// package only reads it back for the API response.
	SSHPort *int
	Status  string
	Backend string
	// BackendRef is the backend's own durable identity for this
	// environment's container/Pod — a Docker container ID, or a
	// Kubernetes Deployment name. Empty until the first successful
	// CreateEnvironment call returns.
	BackendRef *string
	// Image is nil unless the user explicitly opted into a custom image —
	// see sandbox.EnvironmentConfig.Image's doc comment for how a nil
	// value resolves to the platform default inside agent-runner.
	Image       *string
	CPULimit    string
	MemoryLimit string
	DiskLimitGB int
	// DockerEnabled opts this environment into a dedicated, long-lived
	// docker:dind sidecar (see services/agent-runner/internal/sandbox/
	// docker/environment_dind.go and .../k8s/dind.go) — the static-
	// environment counterpart to agent.Agent.DockerEnabled. Decided once at
	// CreateEnvironment time and never patched afterward (see
	// UpdateEnvironmentInput, which has no equivalent field): the sidecar's
	// network/container pairing is baked into the environment's container
	// at create time, the same way CPULimit/MemoryLimit are.
	DockerEnabled      bool
	VolumeRef          *string
	SecretKeyEncrypted string
	IdleTimeoutMinutes int
	LastActiveAt       time.Time
	ErrorMessage       *string
	// PortsPendingRestart is true whenever a port forward has been
	// added/removed since the environment's backing container/Pod last
	// had its full port-mapping set applied — set by AddPortForward/
	// DeletePortForward, cleared once StartEnvironment or the explicit
	// RestartEnvironment action successfully (re)applies the current set
	// (see docs/ai-agent/environment-management.md's "Port Forwarding"
	// section for why this can't just happen instantly on the docker
	// backend: a running container's port bindings can only be changed by
	// recreating it). Unlike SSHPort, this is services/api's own
	// bookkeeping, not agent-runner's — it's UI-facing state about
	// whether a restart is needed, not a routing fact.
	PortsPendingRestart bool
	CreatedBy           *uuid.UUID
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           *time.Time

	Folders []*EnvironmentFolder
}

// EnvironmentFolder is one working directory inside an Environment — what
// the folder-picker at chat-start reads, and what lets one environment
// host multiple working directories side by side. Identified purely by
// Path; a name/repo-clone-URL/branch used to exist here but were dropped
// before ever shipping (the repo-clone/branch machinery was never fully
// wired to a real credential source — see git history if reviving it).
type EnvironmentFolder struct {
	ID            uuid.UUID
	EnvironmentID uuid.UUID
	// Path is the absolute path inside the environment's container — e.g.
	// "/home/paca/workspaces/api". Unique per environment
	// (uq_environment_folders_path).
	Path      string
	CreatedBy *uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// BrowseEntry is one immediate child of a directory listed via
// FolderService.Browse — a live read against the environment's own
// running container/Pod filesystem, not a persisted row (nothing here is
// stored in Postgres).
type BrowseEntry struct {
	Name  string
	IsDir bool
}

// EnvironmentSSHKey is a user-registered public key, authenticated against
// by Fingerprint once the SSH bastion (Phase 3 —
// docs/ai-agent/environment-management.md's Terminal / SSH Access section)
// exists. No private key material is ever generated or held by Paca — the
// same trust model as a GitHub/GitLab deploy key.
type EnvironmentSSHKey struct {
	ID            uuid.UUID
	EnvironmentID uuid.UUID
	Label         string
	PublicKey     string
	Fingerprint   string
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
}

// EnvironmentPortForward is a user-added mapping exposing one container
// port on a dedicated external port — the general-purpose sibling of the
// single auto-created SSHPort above (see that field's own doc comment for
// the full "why this instead of subdomain routing" reasoning). A user adds
// one of these per container port they want reachable (their dev server's
// own port, not necessarily 80); HostPort is assigned once by
// agent-runner, the same "generated once, nil until assigned" lifecycle
// as Environment.SSHPort, and for the same reason never written by
// services/api itself. Adding/removing a row doesn't change what's
// actually published until the environment's Environment.PortsPendingRestart
// flag is cleared (see that field's own doc comment) — HostPort being
// non-nil only means "assigned," not "currently live."
type EnvironmentPortForward struct {
	ID            uuid.UUID
	EnvironmentID uuid.UUID
	Label         string
	ContainerPort int
	HostPort      *int
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
}
