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
