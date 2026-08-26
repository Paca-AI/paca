package sandbox

import (
	"context"
	"errors"
	"io"
)

// ErrEnvironmentGone is wrapped into the error StartEnvironment returns
// when the environment's backing container/Pod no longer exists — e.g. a
// user removed it directly (`docker rm`, `kubectl delete`, or a `docker
// compose down` that doesn't know about it, since these containers/Pods
// are created directly via the Docker/Kubernetes API, never as a
// Compose-declared service) instead of through StopEnvironment/
// DeleteEnvironment. Exists so a caller (executor.coldStartEnvironment)
// could, in principle, distinguish "this environment needs to be deleted
// and recreated" from a transient/unexpected failure via errors.Is, though
// today both are already handled the same way — the wrapping error's own
// message is already clear and actionable, and coldStartEnvironment
// persists it verbatim to environments.error_message either way.
var ErrEnvironmentGone = errors.New("sandbox: environment's backing container/pod no longer exists")

// EnvironmentConfig describes one static environment's sandbox — the
// long-lived counterpart to Config. Unlike a conversation's sandbox, an
// environment's container/Pod is created once and then started/stopped an
// unbounded number of times over its life, so EnvironmentConfig is passed
// to every one of CreateEnvironment/StartEnvironment, not just an initial
// call — whatever it carries must be safe to re-apply on every start.
type EnvironmentConfig struct {
	EnvironmentID string
	// Image is the pinned image reference to run. Empty means "use this
	// backend's own config.AgentServerImage" — the same image ephemeral
	// conversation sandboxes already run — so CreateEnvironment resolves
	// the platform default itself rather than requiring every caller
	// (services/api, tests, ...) to know what that default is.
	Image string
	Env   map[string]string

	CPULimit, MemoryLimit string
	// DiskLimitGB is enforced natively via the backing PersistentVolumeClaim
	// on the kubernetes backend. The docker backend has no first-class
	// per-named-volume quota primitive and treats this as advisory only —
	// see docs/ai-agent/environment-management.md's Open Risks.
	DiskLimitGB int

	// DockerEnabled opts this environment into a dedicated, long-lived
	// Docker-in-Docker sidecar — the static-environment counterpart to
	// Config.DockerEnabled (see that field's own doc comment for the shared
	// isolation model). Off by default (environments.docker_enabled,
	// decided once at CreateEnvironment time and never patched afterward —
	// see services/api's UpdateEnvironmentRequest, which has no equivalent
	// field). Unlike Config.DockerEnabled's per-conversation sidecar
	// (created fresh on every Start, torn down on every Stop),
	// EnvironmentBackend.CreateEnvironment creates this environment's
	// sidecar exactly once; StartEnvironment/StopEnvironment then cycle it
	// alongside the environment's own container/Pod for the rest of its
	// life — see internal/sandbox/docker/environment_dind.go's own doc
	// comment for why that lifecycle can't just mirror the ephemeral one's
	// create-and-destroy-every-cycle shape.
	DockerEnabled bool

	// SecretKey authenticates goose serve's /status and ACP endpoints
	// (X-Secret-Key). Generated once by the caller at environment creation
	// and persisted (services/api's environments.secret_key_encrypted) —
	// reused on every subsequent Start, unlike an ephemeral conversation's
	// sandbox secret key, which is regenerated fresh on every Start because
	// that container never survives a restart.
	SecretKey string

	// PortMappings is the full desired set of container-port -> host-port
	// bindings this environment should publish externally — one entry for
	// the environment's own auto-assigned SSH port (EnvironmentSSHPort),
	// plus one per user-added port forward (services/api's
	// environment_port_forwards). Realized as native Docker -p bindings or
	// a Kubernetes NodePort Service entry, never relayed through
	// agent-runner's own process — see EnvironmentBackend.
	// RestartEnvironmentPorts's own doc comment for why this can only be
	// (re)applied by CreateEnvironment or RestartEnvironmentPorts, never a
	// plain StartEnvironment: bindings are fixed at container-create time
	// on Docker, and even on Kubernetes (where they're just a Service, no
	// Pod involved) a plain Start has no new information to apply — always
	// empty on an ordinary StartEnvironment call.
	PortMappings []PortMapping

	// MCPDevSourceDir mirrors Config.MCPDevSourceDir for a static
	// environment's container/Pod — see that field's own doc comment and
	// MCPDevMountPath's. docker only: internal/sandbox/docker/
	// environment.go bind-mounts it read-only at MCPDevMountPath on every
	// fresh container create (initial CreateEnvironment and any later
	// recreate); the kubernetes backend rejects a non-empty value outright,
	// the same as its ephemeral counterpart.
	MCPDevSourceDir string
}

// PortMapping is one container-port -> host-port binding — see
// EnvironmentConfig.PortMappings's own doc comment.
type PortMapping struct {
	ContainerPort int
	HostPort      int
}

// EnvironmentHandle is what CreateEnvironment/StartEnvironment return — the
// long-lived counterpart to Handle. BackendRef is this environment's
// durable identity (a Docker container ID, or a Kubernetes Deployment
// name) and is what the caller persists and passes back into every later
// Start/Stop/Delete/exec call; BaseURL is re-resolved fresh on every
// StartEnvironment call, since the underlying address (a Pod IP, a
// container's network address) is not guaranteed stable across a
// stop/start cycle.
//
// Backend and VolumeRef are populated by CreateEnvironment only (the one
// call whose caller — agent-runner's internal HTTP handler — has to
// persist them for later Delete/reporting calls; StartEnvironment's
// callers already have both from the first CreateEnvironment response, so
// its implementations may leave these two fields zero-valued). Backend
// names which concrete implementation produced this handle ("docker" or
// "kubernetes" — the caller has no other reliable way to learn this, since
// EnvironmentBackend itself is implementation-agnostic). VolumeRef is the
// real, authoritative name of the volume/PersistentVolumeClaim actually
// provisioned — round-tripped from the backend rather than reconstructed
// by a caller guessing at a naming convention, so a future change to
// either backend's internal naming can never silently break a later
// DeleteEnvironment call.
type EnvironmentHandle struct {
	BackendRef string
	BaseURL    string
	Backend    string
	VolumeRef  string
}

// TermSize is a PTY's row/column dimensions, sent on Resize whenever a
// browser terminal's viewport changes size. Mirrors the shape both
// backends' real resize primitives already take (Docker's
// ContainerExecResize, Kubernetes' remotecommand.TerminalSize).
type TermSize struct {
	Rows, Cols uint16
}

// EnvironmentBackend runs a static, long-lived sandbox — one a user
// explicitly creates, that outlives any single conversation. It is
// additive to Backend, not a variant of it: Backend's four methods keep
// their existing disposable-per-conversation contract completely
// unchanged (see that interface's own doc comment), and every concrete
// sandbox backend (internal/sandbox/docker, internal/sandbox/k8s)
// implements both on the same *Manager, satisfying FullBackend below.
//
// Unlike Backend.Start, which both creates and starts a sandbox in one
// call because an ephemeral sandbox only ever runs once, EnvironmentBackend
// splits creation from starting: CreateEnvironment runs exactly once for a
// given environment, ever; StartEnvironment/StopEnvironment then cycle
// that same backing container/Pod (and its volume) an unbounded number of
// times across the environment's life, preserving disk state across each
// cycle — see StopEnvironment's doc comment.
type EnvironmentBackend interface {
	// CreateEnvironment provisions a new environment's backing
	// container/Pod and persistent volume, and starts it — the returned
	// Handle is already reachable, the same "started and ready" contract
	// Backend.Start has. Called exactly once per environment, ever.
	CreateEnvironment(ctx context.Context, cfg EnvironmentConfig) (*EnvironmentHandle, error)
	// StartEnvironment restarts a previously-created environment's
	// existing container/Pod (backendRef) — it does not create a new one.
	// cfg is passed again for SecretKey (used on every call, for
	// WaitForReady's auth header) and Env: neither backend can live-patch
	// an already-configured container/Pod's env on every start without
	// disrupting whatever the environment's own process tree is doing, so
	// Env is instead backfilled at most once — only when the container/Pod
	// doesn't already have it (checked via GOOSE_PROVIDER's presence; see
	// docker.Manager.recreateEnvironmentIfMissingEnv /
	// k8s.Manager.ensureEnvironmentEnv) — never diffed or re-applied on
	// every call.
	StartEnvironment(ctx context.Context, backendRef string, cfg EnvironmentConfig) (*EnvironmentHandle, error)
	// StopEnvironment stops backendRef's container/Pod without deleting it
	// or its volume — unlike Backend.Stop, this is not a teardown.
	// Whatever the environment's process tree was doing (a dev server left
	// running, files on disk) is preserved for the next StartEnvironment.
	StopEnvironment(ctx context.Context, backendRef string) error
	// RestartEnvironmentPorts applies cfg.PortMappings to backendRef's
	// backing container/Pod — called whenever the desired port-mapping set
	// has changed since it was last applied (a port forward was added or
	// removed), which a plain StartEnvironment cannot pick up (see
	// EnvironmentConfig.PortMappings's own doc comment for why). On the
	// docker backend this recreates the container (bindings are fixed at
	// create time), returning a handle whose BackendRef is a new container
	// ID; on the kubernetes backend this only patches a Service's port
	// list, the Deployment/Pod is never touched, and the returned
	// BackendRef is unchanged. volumeRef is reattached unchanged either
	// way — data never moves.
	RestartEnvironmentPorts(ctx context.Context, backendRef, volumeRef string, cfg EnvironmentConfig) (*EnvironmentHandle, error)
	// DeleteEnvironment permanently removes backendRef's container/Pod and
	// its backing volume (volumeRef) — the only irreversible teardown in
	// this interface.
	DeleteEnvironment(ctx context.Context, backendRef, volumeRef string) error
	// CopyToEnvironment uploads tarContent into backendRef at destPath —
	// the environment counterpart to Backend.CopyToContainer.
	CopyToEnvironment(ctx context.Context, backendRef, destPath string, tarContent io.Reader) error
	// ExecEnvironment runs cmd inside backendRef and returns its combined
	// stdout+stderr and exit code — the environment counterpart to
	// Backend.Exec. Used for one-shot folder provisioning (mkdir/git
	// clone), not interactive sessions — see StreamExecEnvironment for
	// those.
	ExecEnvironment(ctx context.Context, backendRef string, cmd []string) (output string, exitCode int, err error)
	// StreamExecEnvironment runs an interactive command (a shell, for the
	// browser terminal) inside backendRef with a PTY, streaming stdin in
	// and stdout/stderr out for the duration of the connection. resize
	// carries PTY dimension changes for the lifetime of the call — the
	// caller closes it (or lets it go unused) once the session ends;
	// StreamExecEnvironment stops watching it when ctx is done or the
	// command exits. Blocks until the command exits, the connection
	// breaks, or ctx is cancelled.
	StreamExecEnvironment(ctx context.Context, backendRef string, cmd []string, stdin io.Reader, stdout, stderr io.Writer, resize <-chan TermSize) error
}

// FullBackend is a sandbox backend that supports both the disposable
// per-conversation model (Backend) and the long-lived static-environment
// model (EnvironmentBackend) — what main.go actually wires up; both
// internal/sandbox/docker.Manager and internal/sandbox/k8s.Manager satisfy
// it on the same struct.
type FullBackend interface {
	Backend
	EnvironmentBackend
}
