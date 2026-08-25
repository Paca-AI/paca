package dto

import (
	"time"

	"github.com/google/uuid"

	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
)

// =========================================================================
// Environment DTOs
// =========================================================================

// EnvironmentResponse is the public view of a static environment.
// backend_ref/volume_ref/secret_key_encrypted are never serialized — those
// are internal implementation details of how agent-runner reaches this
// environment's container/Pod, not something a client needs or should see.
type EnvironmentResponse struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	// SSHPort is nil until agent-runner's own Postgres write assigns one
	// (see environmentdom.Environment.SSHPort's doc comment) — either the
	// SSH-routing feature isn't configured on this deployment, or
	// CreateEnvironment hasn't completed yet.
	SSHPort            *int      `json:"ssh_port"`
	Status             string    `json:"status"`
	Backend            string    `json:"backend"`
	Image              *string   `json:"image"`
	CPULimit           string    `json:"cpu_limit"`
	MemoryLimit        string    `json:"memory_limit"`
	DiskLimitGB        int       `json:"disk_limit_gb"`
	IdleTimeoutMinutes int       `json:"idle_timeout_minutes"`
	LastActiveAt       time.Time `json:"last_active_at"`
	ErrorMessage       *string   `json:"error_message"`
	// PortsPendingRestart is true whenever a port forward has been
	// added/removed since the environment's backing container/Pod last
	// had its full port-mapping set applied — see
	// environmentdom.Environment.PortsPendingRestart's doc comment. The
	// frontend uses this to show a "restart required" prompt.
	PortsPendingRestart bool                        `json:"ports_pending_restart"`
	CreatedAt           time.Time                   `json:"created_at"`
	UpdatedAt           time.Time                   `json:"updated_at"`
	Folders             []EnvironmentFolderResponse `json:"folders,omitempty"`
}

// CreateEnvironmentRequest is the body for POST
// /projects/:projectId/environments. Image is optional — when omitted,
// services/api passes an empty string through to agent-runner's
// CreateEnvironment, which resolves the platform default itself (see
// environmentdom.CreateEnvironmentInput.Image's doc comment).
type CreateEnvironmentRequest struct {
	Name        string  `json:"name" binding:"required"`
	Image       *string `json:"image"`
	CPULimit    *string `json:"cpu_limit"`
	MemoryLimit *string `json:"memory_limit"`
	DiskLimitGB *int    `json:"disk_limit_gb"`
}

// UpdateEnvironmentRequest is the body for PATCH
// /projects/:projectId/environments/:environmentId.
type UpdateEnvironmentRequest struct {
	Name               *string `json:"name"`
	IdleTimeoutMinutes *int    `json:"idle_timeout_minutes"`
}

// EnvironmentFromEntity maps an Environment entity to EnvironmentResponse.
func EnvironmentFromEntity(e *environmentdom.Environment) EnvironmentResponse {
	resp := EnvironmentResponse{
		ID:                  e.ID,
		ProjectID:           e.ProjectID,
		Name:                e.Name,
		Slug:                e.Slug,
		SSHPort:             e.SSHPort,
		Status:              e.Status,
		Backend:             e.Backend,
		Image:               e.Image,
		CPULimit:            e.CPULimit,
		MemoryLimit:         e.MemoryLimit,
		DiskLimitGB:         e.DiskLimitGB,
		IdleTimeoutMinutes:  e.IdleTimeoutMinutes,
		LastActiveAt:        e.LastActiveAt,
		ErrorMessage:        e.ErrorMessage,
		PortsPendingRestart: e.PortsPendingRestart,
		CreatedAt:           e.CreatedAt,
		UpdatedAt:           e.UpdatedAt,
	}
	if len(e.Folders) > 0 {
		resp.Folders = make([]EnvironmentFolderResponse, 0, len(e.Folders))
		for _, f := range e.Folders {
			resp.Folders = append(resp.Folders, EnvironmentFolderFromEntity(f))
		}
	}
	return resp
}

// =========================================================================
// Environment Folder DTOs
// =========================================================================

// EnvironmentFolderResponse is the public view of a folder within an
// environment — identified purely by Path (no name/repo-clone/branch; see
// environmentdom.EnvironmentFolder's doc comment).
type EnvironmentFolderResponse struct {
	ID        uuid.UUID `json:"id"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}

// AddFolderRequest is the body for POST
// /projects/:projectId/environments/:environmentId/folders.
type AddFolderRequest struct {
	Path string `json:"path" binding:"required"`
}

// EnvironmentFolderFromEntity maps an EnvironmentFolder entity to its DTO.
func EnvironmentFolderFromEntity(f *environmentdom.EnvironmentFolder) EnvironmentFolderResponse {
	return EnvironmentFolderResponse{
		ID:        f.ID,
		Path:      f.Path,
		CreatedAt: f.CreatedAt,
	}
}

// =========================================================================
// Environment folder browse DTOs
// =========================================================================

// BrowseEntryResponse is one immediate child of a browsed directory.
type BrowseEntryResponse struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
}

// BrowseFolderResponse is the body returned for GET
// /projects/:projectId/environments/:environmentId/browse.
type BrowseFolderResponse struct {
	Path    string                `json:"path"`
	Entries []BrowseEntryResponse `json:"entries"`
}

// =========================================================================
// Environment SSH Key DTOs
// =========================================================================

// EnvironmentSSHKeyResponse is the public view of a registered SSH key.
// PublicKey is included (it's not secret — the point of a public key is
// that it's shareable); only the never-collected private key stays off
// this server entirely (see EnvironmentSSHKey's doc comment).
type EnvironmentSSHKeyResponse struct {
	ID          uuid.UUID `json:"id"`
	Label       string    `json:"label"`
	PublicKey   string    `json:"public_key"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

// AddSSHKeyRequest is the body for POST
// /projects/:projectId/environments/:environmentId/ssh-keys.
type AddSSHKeyRequest struct {
	Label     string `json:"label" binding:"required"`
	PublicKey string `json:"public_key" binding:"required"`
}

// EnvironmentSSHKeyFromEntity maps an EnvironmentSSHKey entity to its DTO.
func EnvironmentSSHKeyFromEntity(k *environmentdom.EnvironmentSSHKey) EnvironmentSSHKeyResponse {
	return EnvironmentSSHKeyResponse{
		ID:          k.ID,
		Label:       k.Label,
		PublicKey:   k.PublicKey,
		Fingerprint: k.Fingerprint,
		CreatedAt:   k.CreatedAt,
	}
}

// =========================================================================
// Environment Port Forward DTOs
// =========================================================================

// EnvironmentPortForwardResponse is the public view of a port forward.
// HostPort is nil until agent-runner assigns one (see
// environmentdom.EnvironmentPortForward.HostPort's doc comment) — either
// the environment isn't currently running yet, or port forwarding isn't
// configured on this deployment.
type EnvironmentPortForwardResponse struct {
	ID            uuid.UUID `json:"id"`
	Label         string    `json:"label"`
	ContainerPort int       `json:"container_port"`
	HostPort      *int      `json:"host_port"`
	CreatedAt     time.Time `json:"created_at"`
}

// AddPortForwardRequest is the body for POST
// /projects/:projectId/environments/:environmentId/port-forwards.
type AddPortForwardRequest struct {
	Label         string `json:"label"`
	ContainerPort int    `json:"container_port" binding:"required"`
}

// EnvironmentPortForwardFromEntity maps an EnvironmentPortForward entity to its DTO.
func EnvironmentPortForwardFromEntity(pf *environmentdom.EnvironmentPortForward) EnvironmentPortForwardResponse {
	return EnvironmentPortForwardResponse{
		ID:            pf.ID,
		Label:         pf.Label,
		ContainerPort: pf.ContainerPort,
		HostPort:      pf.HostPort,
		CreatedAt:     pf.CreatedAt,
	}
}

// =========================================================================
// Terminal ticket DTO
// =========================================================================

// TerminalTicketResponse is the body returned for POST
// /projects/:projectId/environments/:environmentId/terminal-ticket. Ticket
// is a short-lived (60s) signed token agent-runner verifies on the other
// end of WSURL — see EnvironmentHandler.TerminalTicket's doc comment for
// the exact format.
type TerminalTicketResponse struct {
	Ticket string `json:"ticket"`
	WSURL  string `json:"ws_url"`
}

// =========================================================================
// Deployment config DTO
// =========================================================================

// EnvironmentDeploymentConfigResponse is the body returned for GET
// /environments/config — see EnvironmentHandler.GetConfig's doc comment.
// Either field is "" when that feature isn't configured on this
// deployment.
type EnvironmentDeploymentConfigResponse struct {
	// SSHBastionHost is SSH_BASTION_HOST — the host part of the real `ssh
	// -p <port> root@<host>` command a client actually runs.
	SSHBastionHost string `json:"ssh_bastion_host"`
	// PortForwardHost is PORT_FORWARD_HOST — the host part of the address a
	// client uses to reach any of an environment's user-added port
	// forwards, e.g. "<port_forward_host>:<host_port>".
	PortForwardHost string `json:"port_forward_host"`
}
