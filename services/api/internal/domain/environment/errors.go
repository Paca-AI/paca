package environmentdom

import "errors"

// Environment errors
var (
	ErrEnvironmentNotFound    = errors.New("environment not found")
	ErrEnvironmentSlugTaken   = errors.New("environment slug already in use in this project")
	ErrEnvironmentNameInvalid = errors.New("environment name is empty or invalid")
	// ErrEnvironmentNotRunning is returned when an operation that requires a
	// live container (exec, terminal, heartbeat) targets an environment
	// that isn't currently running.
	ErrEnvironmentNotRunning = errors.New("environment is not running")
	// ErrEnvironmentBusy is returned when a start/stop/delete is requested
	// while the environment is already mid-transition (creating, stopping,
	// deleting).
	ErrEnvironmentBusy = errors.New("environment is busy — try again once its current operation finishes")
	// ErrEnvironmentCPULimitInvalid/ErrEnvironmentMemoryLimitInvalid guard
	// CreateEnvironment's optional cpu_limit/memory_limit overrides —
	// unparseable (see k8s.io/apimachinery/pkg/api/resource.ParseQuantity,
	// the same parser both sandbox backends use to interpret these exact
	// strings) or below a floor Docker/Kubernetes would accept but the
	// resulting container never could actually run in. Confirmed live: a
	// too-small memory value (e.g. "500m" — "m" means milli, not mega, so
	// this parses to 0.5 bytes) reaches Docker's own ContainerCreate and
	// fails there instead, surfacing a raw daemon error
	// ("Minimum memory limit allowed is 6MB") deep inside an "unhandled
	// error" log line instead of a clear validation message at request
	// time.
	ErrEnvironmentCPULimitInvalid    = errors.New("cpu limit is invalid or below the minimum (100m)")
	ErrEnvironmentMemoryLimitInvalid = errors.New("memory limit is invalid or below the minimum (256Mi)")
)

// Folder errors
var (
	ErrFolderNotFound    = errors.New("environment folder not found")
	ErrFolderPathTaken   = errors.New("a folder with this path already exists in this environment")
	ErrFolderPathInvalid = errors.New("folder path must be an absolute path")
)

// SSH key errors
var (
	ErrSSHKeyNotFound         = errors.New("SSH key not found")
	ErrSSHKeyInvalid          = errors.New("public key is not a valid SSH public key")
	ErrSSHKeyFingerprintTaken = errors.New("this public key is already registered on this environment")
)

// Port forward errors
var (
	ErrPortForwardNotFound             = errors.New("port forward not found")
	ErrPortForwardContainerPortInvalid = errors.New("container port must be between 1 and 65535")
	ErrPortForwardContainerPortTaken   = errors.New("a port forward for this container port already exists on this environment")
)
