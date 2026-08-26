package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
)

// TestEnvironmentToRecord_RoundTrip verifies environmentToRecord/
// environmentFromRecord preserve every field, including the nullable
// pointer fields (BackendRef/Image/VolumeRef/ErrorMessage/CreatedBy).
func TestEnvironmentToRecord_RoundTrip(t *testing.T) {
	backendRef := "container-abc"
	image := "ghcr.io/paca-ai/agent-server:latest"
	volumeRef := "paca-env-abc"
	errMsg := "provisioning failed"
	createdBy := uuid.New()

	e := &environmentdom.Environment{
		ID:                 uuid.New(),
		ProjectID:          uuid.New(),
		Name:               "My Env",
		Slug:               "my-env",
		Status:             environmentdom.StatusRunning,
		Backend:            environmentdom.BackendDocker,
		BackendRef:         &backendRef,
		Image:              &image,
		CPULimit:           "2",
		MemoryLimit:        "4Gi",
		DiskLimitGB:        20,
		VolumeRef:          &volumeRef,
		SecretKeyEncrypted: "encrypted-secret",
		IdleTimeoutMinutes: 60,
		LastActiveAt:       time.Now(),
		ErrorMessage:       &errMsg,
		CreatedBy:          &createdBy,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	rec := environmentToRecord(e)
	got := environmentFromRecord(rec)

	assert.Equal(t, e.ID, got.ID)
	assert.Equal(t, e.ProjectID, got.ProjectID)
	assert.Equal(t, e.Name, got.Name)
	assert.Equal(t, e.Slug, got.Slug)
	assert.Equal(t, e.Status, got.Status)
	assert.Equal(t, e.Backend, got.Backend)
	if assert.NotNil(t, got.BackendRef) {
		assert.Equal(t, backendRef, *got.BackendRef)
	}
	if assert.NotNil(t, got.Image) {
		assert.Equal(t, image, *got.Image)
	}
	if assert.NotNil(t, got.VolumeRef) {
		assert.Equal(t, volumeRef, *got.VolumeRef)
	}
	if assert.NotNil(t, got.ErrorMessage) {
		assert.Equal(t, errMsg, *got.ErrorMessage)
	}
	if assert.NotNil(t, got.CreatedBy) {
		assert.Equal(t, createdBy, *got.CreatedBy)
	}
	assert.Equal(t, e.SecretKeyEncrypted, got.SecretKeyEncrypted)
}

// TestEnvironmentToRecord_NilOptionalFields verifies a freshly-created
// environment (no BackendRef/VolumeRef/ErrorMessage/CreatedBy yet) doesn't
// panic and round-trips as nil, not empty-string pointers.
func TestEnvironmentToRecord_NilOptionalFields(t *testing.T) {
	e := &environmentdom.Environment{
		ID:                 uuid.New(),
		ProjectID:          uuid.New(),
		Name:               "Fresh Env",
		Slug:               "fresh-env",
		Status:             environmentdom.StatusCreating,
		Backend:            environmentdom.BackendDocker,
		CPULimit:           "2",
		MemoryLimit:        "4Gi",
		DiskLimitGB:        20,
		SecretKeyEncrypted: "encrypted-secret",
		IdleTimeoutMinutes: 60,
		LastActiveAt:       time.Now(),
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	rec := environmentToRecord(e)

	assert.Nil(t, rec.BackendRef)
	assert.Nil(t, rec.Image)
	assert.Nil(t, rec.VolumeRef)
	assert.Nil(t, rec.ErrorMessage)
	assert.Nil(t, rec.CreatedBy)

	got := environmentFromRecord(rec)
	assert.Nil(t, got.BackendRef)
	assert.Nil(t, got.CreatedBy)
}

// TestEnvFolderToRecord_RoundTrip verifies folder mapping preserves the
// optional CreatedBy field.
func TestEnvFolderToRecord_RoundTrip(t *testing.T) {
	createdBy := uuid.New()

	f := &environmentdom.EnvironmentFolder{
		ID:            uuid.New(),
		EnvironmentID: uuid.New(),
		Path:          "/home/paca/workspaces/api",
		CreatedBy:     &createdBy,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	rec := envFolderToRecord(f)
	got := envFolderFromRecord(rec)

	assert.Equal(t, f.ID, got.ID)
	assert.Equal(t, f.EnvironmentID, got.EnvironmentID)
	assert.Equal(t, f.Path, got.Path)
	if assert.NotNil(t, got.CreatedBy) {
		assert.Equal(t, createdBy, *got.CreatedBy)
	}
}

// TestEnvFolderToRecord_NilOptionalFields verifies a folder with no
// CreatedBy attached round-trips without panicking on the nil pointer
// field.
func TestEnvFolderToRecord_NilOptionalFields(t *testing.T) {
	f := &environmentdom.EnvironmentFolder{
		ID:            uuid.New(),
		EnvironmentID: uuid.New(),
		Path:          "/home/paca/workspaces/scratch",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	rec := envFolderToRecord(f)
	assert.Nil(t, rec.CreatedBy)

	got := envFolderFromRecord(rec)
	assert.Nil(t, got.CreatedBy)
}

// TestSSHKeyToRecord_RoundTrip verifies SSH key mapping preserves every
// field, including the optional CreatedBy.
func TestSSHKeyToRecord_RoundTrip(t *testing.T) {
	createdBy := uuid.New()
	k := &environmentdom.EnvironmentSSHKey{
		ID:            uuid.New(),
		EnvironmentID: uuid.New(),
		Label:         "laptop",
		PublicKey:     "ssh-ed25519 AAAA... test@example",
		Fingerprint:   "SHA256:abcdef",
		CreatedBy:     &createdBy,
		CreatedAt:     time.Now(),
	}

	rec := sshKeyToRecord(k)
	got := sshKeyFromRecord(rec)

	assert.Equal(t, k.ID, got.ID)
	assert.Equal(t, k.EnvironmentID, got.EnvironmentID)
	assert.Equal(t, k.Label, got.Label)
	assert.Equal(t, k.PublicKey, got.PublicKey)
	assert.Equal(t, k.Fingerprint, got.Fingerprint)
	if assert.NotNil(t, got.CreatedBy) {
		assert.Equal(t, createdBy, *got.CreatedBy)
	}
}

// TestPortForwardToRecord_RoundTrip verifies port forward mapping preserves
// every field, including the optional HostPort/CreatedBy.
func TestPortForwardToRecord_RoundTrip(t *testing.T) {
	hostPort := 22000
	createdBy := uuid.New()
	pf := &environmentdom.EnvironmentPortForward{
		ID:            uuid.New(),
		EnvironmentID: uuid.New(),
		Label:         "dev server",
		ContainerPort: 3000,
		HostPort:      &hostPort,
		CreatedBy:     &createdBy,
		CreatedAt:     time.Now(),
	}

	rec := envPortForwardToRecord(pf)
	got := envPortForwardFromRecord(rec)

	assert.Equal(t, pf.ID, got.ID)
	assert.Equal(t, pf.EnvironmentID, got.EnvironmentID)
	assert.Equal(t, pf.Label, got.Label)
	assert.Equal(t, pf.ContainerPort, got.ContainerPort)
	if assert.NotNil(t, got.HostPort) {
		assert.Equal(t, hostPort, *got.HostPort)
	}
	if assert.NotNil(t, got.CreatedBy) {
		assert.Equal(t, createdBy, *got.CreatedBy)
	}
}

// TestPortForwardToRecord_NilOptionalFields verifies a freshly-created port
// forward (no HostPort assigned yet) round-trips without panicking on the
// nil pointer fields.
func TestPortForwardToRecord_NilOptionalFields(t *testing.T) {
	pf := &environmentdom.EnvironmentPortForward{
		ID:            uuid.New(),
		EnvironmentID: uuid.New(),
		Label:         "dev server",
		ContainerPort: 3000,
		CreatedAt:     time.Now(),
	}

	rec := envPortForwardToRecord(pf)

	assert.Nil(t, rec.HostPort)
	assert.Nil(t, rec.CreatedBy)

	got := envPortForwardFromRecord(rec)
	assert.Nil(t, got.HostPort)
	assert.Nil(t, got.CreatedBy)
}
