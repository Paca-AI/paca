package environmentsvc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
)

// mockRepo is a function-field-based mock of environmentdom.Repository —
// mirrors the mockAgentRepo pattern in service/agent/agent_service_test.go:
// each interface method is backed by an optional func field, falling back
// to a zero-value/nil-error default when unset.
type mockRepo struct {
	listEnvironments                func(ctx context.Context, projectID uuid.UUID) ([]*environmentdom.Environment, error)
	findEnvironmentByID             func(ctx context.Context, id uuid.UUID) (*environmentdom.Environment, error)
	findVisibleEnvironmentInProject func(ctx context.Context, projectID, environmentID uuid.UUID) (*environmentdom.Environment, error)
	createEnvironment               func(ctx context.Context, e *environmentdom.Environment) error
	updateEnvironment               func(ctx context.Context, e *environmentdom.Environment) error
	updateEnvironmentStatus         func(ctx context.Context, id uuid.UUID, status string, backendRef, errMsg *string) error
	updateEnvironmentProvisioning   func(ctx context.Context, id uuid.UUID, status, backend, backendRef, volumeRef string) error
	touchEnvironment                func(ctx context.Context, id uuid.UUID) error
	softDeleteEnvironment           func(ctx context.Context, id uuid.UUID) error
	slugTaken                       func(ctx context.Context, projectID uuid.UUID, slug string) (bool, error)
	setPortsPendingRestart          func(ctx context.Context, id uuid.UUID, pending bool) error

	listFolders    func(ctx context.Context, environmentID uuid.UUID) ([]*environmentdom.EnvironmentFolder, error)
	findFolderByID func(ctx context.Context, id uuid.UUID) (*environmentdom.EnvironmentFolder, error)
	createFolder   func(ctx context.Context, f *environmentdom.EnvironmentFolder) error
	deleteFolder   func(ctx context.Context, id uuid.UUID) error

	listSSHKeys             func(ctx context.Context, environmentID uuid.UUID) ([]*environmentdom.EnvironmentSSHKey, error)
	createSSHKey            func(ctx context.Context, k *environmentdom.EnvironmentSSHKey) error
	deleteSSHKey            func(ctx context.Context, id uuid.UUID) error
	findSSHKeyByFingerprint func(ctx context.Context, environmentID uuid.UUID, fingerprint string) (*environmentdom.EnvironmentSSHKey, error)

	listPortForwards    func(ctx context.Context, environmentID uuid.UUID) ([]*environmentdom.EnvironmentPortForward, error)
	createPortForward   func(ctx context.Context, pf *environmentdom.EnvironmentPortForward) error
	deletePortForward   func(ctx context.Context, id uuid.UUID) error
	findPortForwardByID func(ctx context.Context, id uuid.UUID) (*environmentdom.EnvironmentPortForward, error)
}

func (m *mockRepo) ListEnvironments(ctx context.Context, projectID uuid.UUID) ([]*environmentdom.Environment, error) {
	if m.listEnvironments != nil {
		return m.listEnvironments(ctx, projectID)
	}
	return nil, nil
}
func (m *mockRepo) FindEnvironmentByID(ctx context.Context, id uuid.UUID) (*environmentdom.Environment, error) {
	if m.findEnvironmentByID != nil {
		return m.findEnvironmentByID(ctx, id)
	}
	return nil, environmentdom.ErrEnvironmentNotFound
}
func (m *mockRepo) FindVisibleEnvironmentInProject(ctx context.Context, projectID, environmentID uuid.UUID) (*environmentdom.Environment, error) {
	if m.findVisibleEnvironmentInProject != nil {
		return m.findVisibleEnvironmentInProject(ctx, projectID, environmentID)
	}
	return nil, environmentdom.ErrEnvironmentNotFound
}
func (m *mockRepo) CreateEnvironment(ctx context.Context, e *environmentdom.Environment) error {
	if m.createEnvironment != nil {
		return m.createEnvironment(ctx, e)
	}
	return nil
}
func (m *mockRepo) UpdateEnvironment(ctx context.Context, e *environmentdom.Environment) error {
	if m.updateEnvironment != nil {
		return m.updateEnvironment(ctx, e)
	}
	return nil
}
func (m *mockRepo) UpdateEnvironmentStatus(ctx context.Context, id uuid.UUID, status string, backendRef, errMsg *string) error {
	if m.updateEnvironmentStatus != nil {
		return m.updateEnvironmentStatus(ctx, id, status, backendRef, errMsg)
	}
	return nil
}
func (m *mockRepo) UpdateEnvironmentProvisioning(ctx context.Context, id uuid.UUID, status, backend, backendRef, volumeRef string) error {
	if m.updateEnvironmentProvisioning != nil {
		return m.updateEnvironmentProvisioning(ctx, id, status, backend, backendRef, volumeRef)
	}
	return nil
}
func (m *mockRepo) TouchEnvironment(ctx context.Context, id uuid.UUID) error {
	if m.touchEnvironment != nil {
		return m.touchEnvironment(ctx, id)
	}
	return nil
}
func (m *mockRepo) SoftDeleteEnvironment(ctx context.Context, id uuid.UUID) error {
	if m.softDeleteEnvironment != nil {
		return m.softDeleteEnvironment(ctx, id)
	}
	return nil
}
func (m *mockRepo) SlugTaken(ctx context.Context, projectID uuid.UUID, slug string) (bool, error) {
	if m.slugTaken != nil {
		return m.slugTaken(ctx, projectID, slug)
	}
	return false, nil
}
func (m *mockRepo) SetPortsPendingRestart(ctx context.Context, id uuid.UUID, pending bool) error {
	if m.setPortsPendingRestart != nil {
		return m.setPortsPendingRestart(ctx, id, pending)
	}
	return nil
}
func (m *mockRepo) ListFolders(ctx context.Context, environmentID uuid.UUID) ([]*environmentdom.EnvironmentFolder, error) {
	if m.listFolders != nil {
		return m.listFolders(ctx, environmentID)
	}
	return nil, nil
}
func (m *mockRepo) FindFolderByID(ctx context.Context, id uuid.UUID) (*environmentdom.EnvironmentFolder, error) {
	if m.findFolderByID != nil {
		return m.findFolderByID(ctx, id)
	}
	return nil, environmentdom.ErrFolderNotFound
}
func (m *mockRepo) CreateFolder(ctx context.Context, f *environmentdom.EnvironmentFolder) error {
	if m.createFolder != nil {
		return m.createFolder(ctx, f)
	}
	return nil
}
func (m *mockRepo) DeleteFolder(ctx context.Context, id uuid.UUID) error {
	if m.deleteFolder != nil {
		return m.deleteFolder(ctx, id)
	}
	return nil
}
func (m *mockRepo) ListSSHKeys(ctx context.Context, environmentID uuid.UUID) ([]*environmentdom.EnvironmentSSHKey, error) {
	if m.listSSHKeys != nil {
		return m.listSSHKeys(ctx, environmentID)
	}
	return nil, nil
}
func (m *mockRepo) CreateSSHKey(ctx context.Context, k *environmentdom.EnvironmentSSHKey) error {
	if m.createSSHKey != nil {
		return m.createSSHKey(ctx, k)
	}
	return nil
}
func (m *mockRepo) DeleteSSHKey(ctx context.Context, id uuid.UUID) error {
	if m.deleteSSHKey != nil {
		return m.deleteSSHKey(ctx, id)
	}
	return nil
}
func (m *mockRepo) FindSSHKeyByFingerprint(ctx context.Context, environmentID uuid.UUID, fingerprint string) (*environmentdom.EnvironmentSSHKey, error) {
	if m.findSSHKeyByFingerprint != nil {
		return m.findSSHKeyByFingerprint(ctx, environmentID, fingerprint)
	}
	return nil, environmentdom.ErrSSHKeyNotFound
}
func (m *mockRepo) ListPortForwards(ctx context.Context, environmentID uuid.UUID) ([]*environmentdom.EnvironmentPortForward, error) {
	if m.listPortForwards != nil {
		return m.listPortForwards(ctx, environmentID)
	}
	return nil, nil
}
func (m *mockRepo) CreatePortForward(ctx context.Context, pf *environmentdom.EnvironmentPortForward) error {
	if m.createPortForward != nil {
		return m.createPortForward(ctx, pf)
	}
	return nil
}
func (m *mockRepo) DeletePortForward(ctx context.Context, id uuid.UUID) error {
	if m.deletePortForward != nil {
		return m.deletePortForward(ctx, id)
	}
	return nil
}
func (m *mockRepo) FindPortForwardByID(ctx context.Context, id uuid.UUID) (*environmentdom.EnvironmentPortForward, error) {
	if m.findPortForwardByID != nil {
		return m.findPortForwardByID(ctx, id)
	}
	return nil, environmentdom.ErrPortForwardNotFound
}

var _ environmentdom.Repository = (*mockRepo)(nil)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My Cool Env":    "my-cool-env",
		"  spaced out  ": "spaced-out",
		"a---b":          "a-b",
		"UPPER_CASE!!":   "upper-case",
		"":               "environment",
		"!!!":            "environment",
	}
	for in, want := range cases {
		assert.Equal(t, want, slugify(in), "slugify(%q)", in)
	}
}

// TestGenerateUniqueSlug_Collision verifies the "-2"/"-3" retry loop: the
// base slug and its first bump are both reported taken, so the third
// candidate is what should win.
func TestGenerateUniqueSlug_Collision(t *testing.T) {
	var seen []string
	repo := &mockRepo{
		slugTaken: func(_ context.Context, _ uuid.UUID, slug string) (bool, error) {
			seen = append(seen, slug)
			return slug == "my-env" || slug == "my-env-2", nil
		},
	}
	svc := New(repo, "", "")

	slug, err := svc.generateUniqueSlug(context.Background(), uuid.New(), "My Env")

	require.NoError(t, err)
	assert.Equal(t, "my-env-3", slug)
	assert.Equal(t, []string{"my-env", "my-env-2", "my-env-3"}, seen)
}

// TestResolveConversationWorkdir_NilEnvironment verifies the "no
// environment attached at all" input returns (nil, nil, nil) rather than
// erroring.
func TestResolveConversationWorkdir_NilEnvironment(t *testing.T) {
	svc := New(&mockRepo{}, "", "")

	env, folder, err := svc.ResolveConversationWorkdir(context.Background(), uuid.New(), nil, nil)

	assert.NoError(t, err)
	assert.Nil(t, env)
	assert.Nil(t, folder)
}

// TestResolveConversationWorkdir_AutoSelectsSoleFolder verifies that with
// no folderID given, an environment with exactly one folder auto-selects
// it.
func TestResolveConversationWorkdir_AutoSelectsSoleFolder(t *testing.T) {
	projectID := uuid.New()
	envID := uuid.New()
	folderID := uuid.New()
	env := &environmentdom.Environment{ID: envID, ProjectID: projectID}
	folder := &environmentdom.EnvironmentFolder{ID: folderID, EnvironmentID: envID, Path: "/home/paca/workspaces/api"}

	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, pID, eID uuid.UUID) (*environmentdom.Environment, error) {
			assert.Equal(t, projectID, pID)
			assert.Equal(t, envID, eID)
			return env, nil
		},
		listFolders: func(_ context.Context, eID uuid.UUID) ([]*environmentdom.EnvironmentFolder, error) {
			assert.Equal(t, envID, eID)
			return []*environmentdom.EnvironmentFolder{folder}, nil
		},
	}
	svc := New(repo, "", "")

	resolvedEnv, resolvedFolder, err := svc.ResolveConversationWorkdir(context.Background(), projectID, &envID, nil)

	require.NoError(t, err)
	assert.Equal(t, env, resolvedEnv)
	assert.Equal(t, folder, resolvedFolder)
}

// TestResolveConversationWorkdir_AmbiguousFolders verifies that with no
// folderID given and more than one folder, resolution fails with
// ErrFolderNotFound (ambiguous — the caller must ask the user to pick).
func TestResolveConversationWorkdir_AmbiguousFolders(t *testing.T) {
	envID := uuid.New()
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{ID: envID}, nil
		},
		listFolders: func(_ context.Context, _ uuid.UUID) ([]*environmentdom.EnvironmentFolder, error) {
			return []*environmentdom.EnvironmentFolder{
				{ID: uuid.New(), EnvironmentID: envID},
				{ID: uuid.New(), EnvironmentID: envID},
			}, nil
		},
	}
	svc := New(repo, "", "")

	_, _, err := svc.ResolveConversationWorkdir(context.Background(), uuid.New(), &envID, nil)

	assert.ErrorIs(t, err, environmentdom.ErrFolderNotFound)
}

// TestResolveConversationWorkdir_FolderBelongsToDifferentEnvironment
// verifies a folderID that resolves to a real row, but on a different
// environment, is rejected rather than silently accepted.
func TestResolveConversationWorkdir_FolderBelongsToDifferentEnvironment(t *testing.T) {
	envID := uuid.New()
	otherEnvID := uuid.New()
	folderID := uuid.New()
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{ID: envID}, nil
		},
		findFolderByID: func(_ context.Context, id uuid.UUID) (*environmentdom.EnvironmentFolder, error) {
			return &environmentdom.EnvironmentFolder{ID: id, EnvironmentID: otherEnvID}, nil
		},
	}
	svc := New(repo, "", "")

	_, _, err := svc.ResolveConversationWorkdir(context.Background(), uuid.New(), &envID, &folderID)

	assert.ErrorIs(t, err, environmentdom.ErrFolderNotFound)
}

// TestCreateEnvironment_Success exercises the full CreateEnvironment happy
// path against a stub agent-runner HTTP server, verifying the row is
// written StatusCreating first, then StatusRunning with the backend/
// backend_ref/volume_ref agent-runner reported.
func TestCreateEnvironment_Success(t *testing.T) {
	var created *environmentdom.Environment
	var provisioned struct {
		status, backend, backendRef, volumeRef string
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/internal/environments", r.URL.Path)
		assert.Equal(t, "test-internal-key", r.Header.Get("X-Internal-Token"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(internalCreateEnvironmentResponse{
			Backend:    "docker",
			BackendRef: "container-123",
			VolumeRef:  "paca-env-abc",
			BaseURL:    "http://sandbox:8080",
		})
	}))
	defer srv.Close()

	repo := &mockRepo{
		createEnvironment: func(_ context.Context, e *environmentdom.Environment) error {
			assert.Equal(t, environmentdom.StatusCreating, e.Status)
			created = e
			return nil
		},
		updateEnvironmentProvisioning: func(_ context.Context, id uuid.UUID, status, backend, backendRef, volumeRef string) error {
			assert.Equal(t, created.ID, id)
			provisioned.status, provisioned.backend, provisioned.backendRef, provisioned.volumeRef = status, backend, backendRef, volumeRef
			return nil
		},
	}
	svc := New(repo, srv.URL, "test-internal-key")

	projectID := uuid.New()
	env, err := svc.CreateEnvironment(context.Background(), projectID, environmentdom.CreateEnvironmentInput{Name: "My Env"})

	require.NoError(t, err)
	require.NotNil(t, env)
	assert.Equal(t, "my-env", env.Slug)
	assert.Equal(t, environmentdom.StatusRunning, env.Status)
	assert.Equal(t, "docker", env.Backend)
	require.NotNil(t, env.BackendRef)
	assert.Equal(t, "container-123", *env.BackendRef)
	assert.Equal(t, environmentdom.StatusRunning, provisioned.status)
	assert.Equal(t, "container-123", provisioned.backendRef)
	assert.Equal(t, "paca-env-abc", provisioned.volumeRef)
}

// TestCreateEnvironment_AgentRunnerFailure verifies a failing agent-runner
// call surfaces as an error (not swallowed) and marks the row StatusError.
func TestCreateEnvironment_AgentRunnerFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	var statusUpdates []string
	repo := &mockRepo{
		updateEnvironmentStatus: func(_ context.Context, _ uuid.UUID, status string, _, errMsg *string) error {
			statusUpdates = append(statusUpdates, status)
			assert.NotNil(t, errMsg)
			return nil
		},
	}
	svc := New(repo, srv.URL, "test-internal-key")

	env, err := svc.CreateEnvironment(context.Background(), uuid.New(), environmentdom.CreateEnvironmentInput{Name: "My Env"})

	assert.Error(t, err)
	assert.Nil(t, env)
	assert.Equal(t, []string{environmentdom.StatusError}, statusUpdates)
}

// TestAddSSHKey_InvalidKey verifies an unparseable public key is rejected
// with ErrSSHKeyInvalid rather than propagating the raw ssh parse error.
func TestAddSSHKey_InvalidKey(t *testing.T) {
	envID := uuid.New()
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{ID: envID}, nil
		},
	}
	svc := New(repo, "", "")

	_, err := svc.AddSSHKey(context.Background(), uuid.New(), envID, environmentdom.AddSSHKeyInput{
		Label:     "laptop",
		PublicKey: "not-a-real-key",
	})

	assert.ErrorIs(t, err, environmentdom.ErrSSHKeyInvalid)
}

// TestAddSSHKey_FingerprintTaken verifies a duplicate fingerprint (already
// registered on this environment) is rejected before ever reaching
// CreateSSHKey.
func TestAddSSHKey_FingerprintTaken(t *testing.T) {
	envID := uuid.New()
	// A real (throwaway) ed25519 authorized-key line.
	const pubKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJVQxxEsnQ36eAgW6DUJgWLNIQ9WxTVzYplzJ4slgU9c test@example"

	createCalled := false
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{ID: envID}, nil
		},
		findSSHKeyByFingerprint: func(_ context.Context, _ uuid.UUID, _ string) (*environmentdom.EnvironmentSSHKey, error) {
			return &environmentdom.EnvironmentSSHKey{ID: uuid.New()}, nil
		},
		createSSHKey: func(_ context.Context, _ *environmentdom.EnvironmentSSHKey) error {
			createCalled = true
			return nil
		},
	}
	svc := New(repo, "", "")

	_, err := svc.AddSSHKey(context.Background(), uuid.New(), envID, environmentdom.AddSSHKeyInput{
		Label:     "laptop",
		PublicKey: pubKey,
	})

	assert.ErrorIs(t, err, environmentdom.ErrSSHKeyFingerprintTaken)
	assert.False(t, createCalled)
}

// TestAddPortForward_InvalidContainerPort verifies an out-of-range
// container port is rejected before ever reaching CreatePortForward.
func TestAddPortForward_InvalidContainerPort(t *testing.T) {
	envID := uuid.New()
	createCalled := false
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{ID: envID}, nil
		},
		createPortForward: func(_ context.Context, _ *environmentdom.EnvironmentPortForward) error {
			createCalled = true
			return nil
		},
	}
	svc := New(repo, "", "")

	_, err := svc.AddPortForward(context.Background(), uuid.New(), envID, environmentdom.AddPortForwardInput{
		Label:         "dev server",
		ContainerPort: 70000,
	})

	assert.ErrorIs(t, err, environmentdom.ErrPortForwardContainerPortInvalid)
	assert.False(t, createCalled)
}

// TestDeletePortForward_NotOwnedByEnvironment verifies a port forward ID
// that resolves to a real row, but on a different environment, is rejected
// rather than silently deleted.
func TestDeletePortForward_NotOwnedByEnvironment(t *testing.T) {
	envID := uuid.New()
	otherEnvID := uuid.New()
	pfID := uuid.New()
	deleteCalled := false
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{ID: envID}, nil
		},
		findPortForwardByID: func(_ context.Context, id uuid.UUID) (*environmentdom.EnvironmentPortForward, error) {
			return &environmentdom.EnvironmentPortForward{ID: id, EnvironmentID: otherEnvID}, nil
		},
		deletePortForward: func(_ context.Context, _ uuid.UUID) error {
			deleteCalled = true
			return nil
		},
	}
	svc := New(repo, "", "")

	err := svc.DeletePortForward(context.Background(), uuid.New(), envID, pfID)

	assert.ErrorIs(t, err, environmentdom.ErrPortForwardNotFound)
	assert.False(t, deleteCalled)
}

// TestAddPortForward_MarksPortsPendingRestart verifies a successful add
// always sets PortsPendingRestart, even against a stopped environment
// (nothing to sync against agent-runner in that case) — the pending flag
// tracks "the current row set hasn't been applied yet," independent of
// whether the environment happens to be running right now.
func TestAddPortForward_MarksPortsPendingRestart(t *testing.T) {
	envID := uuid.New()
	var pendingSet *bool
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{ID: envID, Status: environmentdom.StatusStopped}, nil
		},
		createPortForward: func(_ context.Context, _ *environmentdom.EnvironmentPortForward) error {
			return nil
		},
		setPortsPendingRestart: func(_ context.Context, id uuid.UUID, pending bool) error {
			assert.Equal(t, envID, id)
			pendingSet = &pending
			return nil
		},
	}
	svc := New(repo, "", "")

	_, err := svc.AddPortForward(context.Background(), uuid.New(), envID, environmentdom.AddPortForwardInput{
		Label:         "dev server",
		ContainerPort: 3000,
	})

	require.NoError(t, err)
	require.NotNil(t, pendingSet)
	assert.True(t, *pendingSet)
}

// TestDeletePortForward_MarksPortsPendingRestart mirrors
// TestAddPortForward_MarksPortsPendingRestart for the delete path — and
// confirms no agent-runner call is made at all (nothing to deregister
// under the native-publish design; see DeletePortForward's own doc
// comment).
func TestDeletePortForward_MarksPortsPendingRestart(t *testing.T) {
	envID := uuid.New()
	pfID := uuid.New()
	backendRef := "container-abc"
	hostPort := 22001
	var pendingSet *bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected agent-runner call: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{ID: envID, Status: environmentdom.StatusRunning, BackendRef: &backendRef}, nil
		},
		findPortForwardByID: func(_ context.Context, id uuid.UUID) (*environmentdom.EnvironmentPortForward, error) {
			return &environmentdom.EnvironmentPortForward{ID: id, EnvironmentID: envID, HostPort: &hostPort}, nil
		},
		deletePortForward: func(_ context.Context, id uuid.UUID) error {
			assert.Equal(t, pfID, id)
			return nil
		},
		setPortsPendingRestart: func(_ context.Context, id uuid.UUID, pending bool) error {
			assert.Equal(t, envID, id)
			pendingSet = &pending
			return nil
		},
	}
	svc := New(repo, srv.URL, "test-internal-key")

	err := svc.DeletePortForward(context.Background(), uuid.New(), envID, pfID)

	require.NoError(t, err)
	require.NotNil(t, pendingSet)
	assert.True(t, *pendingSet)
}

// TestStartEnvironment_RestartsPortsWhenPending verifies a stopped
// environment with PortsPendingRestart set calls /restart-ports (not
// /start) and clears the flag on success — the "apply pending port
// changes transparently on the next Start" path.
func TestStartEnvironment_RestartsPortsWhenPending(t *testing.T) {
	envID := uuid.New()
	backendRef := "container-old"
	volumeRef := "paca-env-abc"
	var calledPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(internalRestartPortsResponse{
			BackendRef: "container-new",
			BaseURL:    "http://sandbox:8080",
			SSHPort:    22001,
		})
	}))
	defer srv.Close()

	var statusUpdates []string
	var pendingSet *bool
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{
				ID: envID, Status: environmentdom.StatusStopped,
				BackendRef: &backendRef, VolumeRef: &volumeRef,
				PortsPendingRestart: true,
			}, nil
		},
		updateEnvironmentStatus: func(_ context.Context, _ uuid.UUID, status string, backendRef, _ *string) error {
			statusUpdates = append(statusUpdates, status)
			require.NotNil(t, backendRef)
			assert.Equal(t, "container-new", *backendRef)
			return nil
		},
		setPortsPendingRestart: func(_ context.Context, id uuid.UUID, pending bool) error {
			assert.Equal(t, envID, id)
			pendingSet = &pending
			return nil
		},
	}
	svc := New(repo, srv.URL, "test-internal-key")

	env, err := svc.StartEnvironment(context.Background(), uuid.New(), envID)

	require.NoError(t, err)
	assert.Equal(t, "/internal/environments/"+envID.String()+"/restart-ports", calledPath)
	assert.Equal(t, []string{environmentdom.StatusRunning}, statusUpdates)
	require.NotNil(t, pendingSet)
	assert.False(t, *pendingSet)
	assert.Equal(t, environmentdom.StatusRunning, env.Status)
	require.NotNil(t, env.BackendRef)
	assert.Equal(t, "container-new", *env.BackendRef)
	require.NotNil(t, env.SSHPort)
	assert.Equal(t, 22001, *env.SSHPort)
}

// TestRestartEnvironment_RequiresRunning verifies the explicit restart
// action is rejected against a non-running environment — a stopped
// environment's pending changes are applied automatically on its next
// StartEnvironment instead (see TestStartEnvironment_RestartsPortsWhenPending).
func TestRestartEnvironment_RequiresRunning(t *testing.T) {
	envID := uuid.New()
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{ID: envID, Status: environmentdom.StatusStopped}, nil
		},
	}
	svc := New(repo, "", "")

	_, err := svc.RestartEnvironment(context.Background(), uuid.New(), envID)

	assert.ErrorIs(t, err, environmentdom.ErrEnvironmentBusy)
}
