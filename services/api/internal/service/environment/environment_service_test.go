package environmentsvc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
	"github.com/Paca-AI/api/internal/events"
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
	findSSHKeyByID          func(ctx context.Context, id uuid.UUID) (*environmentdom.EnvironmentSSHKey, error)
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
func (m *mockRepo) FindSSHKeyByID(ctx context.Context, id uuid.UUID) (*environmentdom.EnvironmentSSHKey, error) {
	if m.findSSHKeyByID != nil {
		return m.findSSHKeyByID(ctx, id)
	}
	return nil, environmentdom.ErrSSHKeyNotFound
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

// mockPublisher is a function-field-based mock of environmentPublisher,
// mirroring mockRepo's own style.
type mockPublisher struct {
	appendFn func(ctx context.Context, stream, eventType string, payload any) error
}

func (m *mockPublisher) Append(ctx context.Context, stream, eventType string, payload any) error {
	if m.appendFn != nil {
		return m.appendFn(ctx, stream, eventType, payload)
	}
	return nil
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
// TestCreateEnvironment_QueuesCommand verifies the request path
// CreateEnvironment now takes: it writes the row (StatusCreating) and
// queues a command onto StreamEnvironmentCommands, making no agent-runner
// call at all (no HTTP test server is even set up for this test). The
// actual agent-runner provisioning call moved to ExecuteCreate — see
// TestExecuteCreate_Success and TestExecuteCreate_AgentRunnerFailure below
// — which worker.EnvironmentCommandConsumer invokes once it reads the
// queued command.
func TestCreateEnvironment_QueuesCommand(t *testing.T) {
	var created *environmentdom.Environment
	var publishedStream, publishedType string
	var publishedPayload any
	repo := &mockRepo{
		createEnvironment: func(_ context.Context, e *environmentdom.Environment) error {
			assert.Equal(t, environmentdom.StatusCreating, e.Status)
			created = e
			return nil
		},
	}
	pub := &mockPublisher{
		appendFn: func(_ context.Context, stream, eventType string, payload any) error {
			publishedStream, publishedType, publishedPayload = stream, eventType, payload
			return nil
		},
	}
	svc := New(repo, "", "").WithPublisher(pub)

	projectID := uuid.New()
	env, err := svc.CreateEnvironment(context.Background(), projectID, environmentdom.CreateEnvironmentInput{Name: "My Env"})

	require.NoError(t, err)
	require.NotNil(t, env)
	assert.Equal(t, "my-env", env.Slug)
	assert.Equal(t, environmentdom.StatusCreating, env.Status)
	require.NotNil(t, created)
	assert.Equal(t, events.StreamEnvironmentCommands, publishedStream)
	assert.Equal(t, events.TopicEnvironmentCreate, publishedType)
	assert.Equal(t, map[string]any{"environment_id": created.ID.String()}, publishedPayload)
}

// TestCreateEnvironment_ReturnsErrorWhenPublishFails verifies a failure to
// queue the command is reported to the caller and marks the
// already-written row StatusError — nothing will ever provision it now,
// so leaving it StatusCreating would strand it there forever.
func TestCreateEnvironment_ReturnsErrorWhenPublishFails(t *testing.T) {
	var created *environmentdom.Environment
	var statusUpdates []string
	repo := &mockRepo{
		createEnvironment: func(_ context.Context, e *environmentdom.Environment) error {
			created = e
			return nil
		},
		updateEnvironmentStatus: func(_ context.Context, id uuid.UUID, status string, _, _ *string) error {
			assert.Equal(t, created.ID, id)
			statusUpdates = append(statusUpdates, status)
			return nil
		},
	}
	pub := &mockPublisher{
		appendFn: func(context.Context, string, string, any) error {
			return errors.New("valkey unavailable")
		},
	}
	svc := New(repo, "", "").WithPublisher(pub)

	env, err := svc.CreateEnvironment(context.Background(), uuid.New(), environmentdom.CreateEnvironmentInput{Name: "My Env"})

	require.Error(t, err)
	assert.Nil(t, env)
	assert.Equal(t, []string{environmentdom.StatusError}, statusUpdates)
}

// TestExecuteCreate_Success verifies ExecuteCreate —
// worker.EnvironmentCommandConsumer's entry point once it reads a queued
// create command — provisions via agent-runner using fields already
// persisted on the row (not threaded through the queued command itself;
// see ExecuteCreate's own doc comment for why, including the secret key)
// and persists the resulting backend/backend_ref/volume_ref.
func TestExecuteCreate_Success(t *testing.T) {
	envID := uuid.New()
	projectID := uuid.New()
	var gotReq internalCreateEnvironmentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/internal/environments", r.URL.Path)
		assert.Equal(t, "test-internal-key", r.Header.Get("X-Internal-Token"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotReq))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(internalCreateEnvironmentResponse{
			Backend:    "docker",
			BackendRef: "container-123",
			VolumeRef:  "paca-env-abc",
			BaseURL:    "http://sandbox:8080",
		})
	}))
	defer srv.Close()

	var provisioned struct {
		status, backend, backendRef, volumeRef string
	}
	repo := &mockRepo{
		findEnvironmentByID: func(_ context.Context, id uuid.UUID) (*environmentdom.Environment, error) {
			assert.Equal(t, envID, id)
			return &environmentdom.Environment{
				ID: envID, ProjectID: projectID, Status: environmentdom.StatusCreating,
				CPULimit: "2", MemoryLimit: "4Gi", DiskLimitGB: 20,
				SecretKeyEncrypted: "plaintext-secret",
			}, nil
		},
		updateEnvironmentProvisioning: func(_ context.Context, id uuid.UUID, status, backend, backendRef, volumeRef string) error {
			assert.Equal(t, envID, id)
			provisioned.status, provisioned.backend, provisioned.backendRef, provisioned.volumeRef = status, backend, backendRef, volumeRef
			return nil
		},
	}
	svc := New(repo, srv.URL, "test-internal-key")

	err := svc.ExecuteCreate(context.Background(), envID)

	require.NoError(t, err)
	assert.Equal(t, envID.String(), gotReq.EnvironmentID)
	assert.Equal(t, projectID.String(), gotReq.ProjectID)
	assert.Equal(t, "plaintext-secret", gotReq.SecretKey)
	assert.Equal(t, environmentdom.StatusRunning, provisioned.status)
	assert.Equal(t, "docker", provisioned.backend)
	assert.Equal(t, "container-123", provisioned.backendRef)
	assert.Equal(t, "paca-env-abc", provisioned.volumeRef)
}

// TestExecuteCreate_AgentRunnerFailure verifies a failing agent-runner call
// surfaces as an error (not swallowed) and marks the row StatusError.
func TestExecuteCreate_AgentRunnerFailure(t *testing.T) {
	envID := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	var statusUpdates []string
	repo := &mockRepo{
		findEnvironmentByID: func(_ context.Context, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{
				ID: envID, ProjectID: uuid.New(), Status: environmentdom.StatusCreating,
				CPULimit: "2", MemoryLimit: "4Gi", DiskLimitGB: 20,
			}, nil
		},
		updateEnvironmentStatus: func(_ context.Context, _ uuid.UUID, status string, _, errMsg *string) error {
			statusUpdates = append(statusUpdates, status)
			assert.NotNil(t, errMsg)
			return nil
		},
	}
	svc := New(repo, srv.URL, "test-internal-key")

	err := svc.ExecuteCreate(context.Background(), envID)

	assert.Error(t, err)
	assert.Equal(t, []string{environmentdom.StatusError}, statusUpdates)
}

// TestCreateEnvironment_CPULimitTooSmall verifies a cpu_limit override that
// parses but falls below minCPULimitMillicores is rejected before ever
// reaching agent-runner — no repo write, no HTTP call — rather than
// surfacing as a raw Docker daemon error at container-create time (see
// errors.go's ErrEnvironmentCPULimitInvalid doc comment for the incident
// this guards against).
func TestCreateEnvironment_CPULimitTooSmall(t *testing.T) {
	repo := &mockRepo{
		createEnvironment: func(_ context.Context, _ *environmentdom.Environment) error {
			t.Fatal("CreateEnvironment repo call should not be reached for an invalid cpu_limit")
			return nil
		},
	}
	svc := New(repo, "http://unused.invalid", "test-internal-key")

	tiny := "1m"
	_, err := svc.CreateEnvironment(context.Background(), uuid.New(), environmentdom.CreateEnvironmentInput{
		Name:     "My Env",
		CPULimit: &tiny,
	})

	assert.ErrorIs(t, err, environmentdom.ErrEnvironmentCPULimitInvalid)
}

// TestCreateEnvironment_MemoryLimitTooSmall mirrors
// TestCreateEnvironment_CPULimitTooSmall for memory_limit — "500m" is valid
// Kubernetes quantity syntax (500 *milli*-bytes, not 500 megabytes), which
// is exactly the mistake that reached Docker's ContainerCreate as a raw
// "Minimum memory limit allowed is 6MB" daemon error before this
// validation existed.
func TestCreateEnvironment_MemoryLimitTooSmall(t *testing.T) {
	repo := &mockRepo{
		createEnvironment: func(_ context.Context, _ *environmentdom.Environment) error {
			t.Fatal("CreateEnvironment repo call should not be reached for an invalid memory_limit")
			return nil
		},
	}
	svc := New(repo, "http://unused.invalid", "test-internal-key")

	tiny := "500m"
	_, err := svc.CreateEnvironment(context.Background(), uuid.New(), environmentdom.CreateEnvironmentInput{
		Name:        "My Env",
		MemoryLimit: &tiny,
	})

	assert.ErrorIs(t, err, environmentdom.ErrEnvironmentMemoryLimitInvalid)
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
// TestStartEnvironment_QueuesCommandAndSetsStarting verifies the request
// path StartEnvironment now takes: it makes no agent-runner call at all
// (no HTTP test server is even set up for this test), just queues a
// command onto StreamEnvironmentCommands and marks the row "starting".
// The actual agent-runner call moved to ExecuteStart — see
// TestExecuteStart_PlainStart and friends below — which
// worker.EnvironmentCommandConsumer invokes once it reads the queued
// command.
func TestStartEnvironment_QueuesCommandAndSetsStarting(t *testing.T) {
	envID := uuid.New()
	backendRef := "container-1"

	var statusUpdates []string
	var publishedStream, publishedType string
	var publishedPayload any
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{
				ID: envID, Status: environmentdom.StatusStopped,
				BackendRef: &backendRef,
			}, nil
		},
		updateEnvironmentStatus: func(_ context.Context, _ uuid.UUID, status string, _, _ *string) error {
			statusUpdates = append(statusUpdates, status)
			return nil
		},
	}
	pub := &mockPublisher{
		appendFn: func(_ context.Context, stream, eventType string, payload any) error {
			publishedStream, publishedType, publishedPayload = stream, eventType, payload
			return nil
		},
	}
	svc := New(repo, "", "").WithPublisher(pub)

	env, err := svc.StartEnvironment(context.Background(), uuid.New(), envID)

	require.NoError(t, err)
	assert.Equal(t, events.StreamEnvironmentCommands, publishedStream)
	assert.Equal(t, events.TopicEnvironmentStart, publishedType)
	assert.Equal(t, map[string]any{"environment_id": envID.String()}, publishedPayload)
	assert.Equal(t, []string{environmentdom.StatusStarting}, statusUpdates)
	assert.Equal(t, environmentdom.StatusStarting, env.Status)
}

// TestStartEnvironment_ReturnsErrorWhenPublishFails verifies a failure to
// queue the command is reported to the caller, and never marks the
// environment "starting" — if nothing is going to execute the start,
// telling the user it's starting would be a lie.
func TestStartEnvironment_ReturnsErrorWhenPublishFails(t *testing.T) {
	envID := uuid.New()
	backendRef := "container-1"

	var statusUpdates []string
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{
				ID: envID, Status: environmentdom.StatusStopped,
				BackendRef: &backendRef,
			}, nil
		},
		updateEnvironmentStatus: func(_ context.Context, _ uuid.UUID, status string, _, _ *string) error {
			statusUpdates = append(statusUpdates, status)
			return nil
		},
	}
	pub := &mockPublisher{
		appendFn: func(context.Context, string, string, any) error {
			return errors.New("valkey unavailable")
		},
	}
	svc := New(repo, "", "").WithPublisher(pub)

	_, err := svc.StartEnvironment(context.Background(), uuid.New(), envID)

	require.Error(t, err)
	assert.Empty(t, statusUpdates)
}

// TestStartEnvironment_RejectsPendingRestartWithNoVolumeRef verifies the
// one precondition check StartEnvironment still does synchronously — it
// costs no extra I/O (env is already loaded) unlike the agent-runner call
// this same check used to gate, which moved to ExecuteStart.
func TestStartEnvironment_RejectsPendingRestartWithNoVolumeRef(t *testing.T) {
	envID := uuid.New()
	backendRef := "container-1"
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{
				ID: envID, Status: environmentdom.StatusStopped,
				BackendRef: &backendRef, VolumeRef: nil,
				PortsPendingRestart: true,
			}, nil
		},
	}
	svc := New(repo, "", "").WithPublisher(&mockPublisher{})

	_, err := svc.StartEnvironment(context.Background(), uuid.New(), envID)

	assert.Error(t, err)
}

// TestExecuteStart_RestartsPortsWhenPending verifies ExecuteStart —
// worker.EnvironmentCommandConsumer's entry point once it reads a queued
// start command — takes the restart-ports branch when the environment has
// a pending port change, the same branch StartEnvironment used to take
// synchronously before this became asynchronous.
func TestExecuteStart_RestartsPortsWhenPending(t *testing.T) {
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
	var touched []uuid.UUID
	repo := &mockRepo{
		findEnvironmentByID: func(_ context.Context, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{
				ID: envID, Status: environmentdom.StatusStarting,
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
		touchEnvironment: func(_ context.Context, id uuid.UUID) error {
			touched = append(touched, id)
			return nil
		},
		setPortsPendingRestart: func(_ context.Context, id uuid.UUID, pending bool) error {
			assert.Equal(t, envID, id)
			pendingSet = &pending
			return nil
		},
	}
	svc := New(repo, srv.URL, "test-internal-key")

	err := svc.ExecuteStart(context.Background(), envID)

	require.NoError(t, err)
	assert.Equal(t, "/internal/environments/"+envID.String()+"/restart-ports", calledPath)
	assert.Equal(t, []string{environmentdom.StatusRunning}, statusUpdates)
	assert.Equal(t, []uuid.UUID{envID}, touched)
	require.NotNil(t, pendingSet)
	assert.False(t, *pendingSet)
}

// TestExecuteStart_PlainStart verifies the plain (no pending port changes)
// path bumps last_active_at, not just status — without this, a freshly
// started environment keeps whatever last_active_at it had from before it
// was stopped, and the idle reaper (agent-runner's reapIdleEnvironments,
// which only reads the DB column) stops it again within its next tick,
// seconds after the user started it.
func TestExecuteStart_PlainStart(t *testing.T) {
	envID := uuid.New()
	backendRef := "container-1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(internalStartEnvironmentResponse{
			BaseURL: "http://sandbox:8080",
		})
	}))
	defer srv.Close()

	var statusUpdates []string
	var touched []uuid.UUID
	repo := &mockRepo{
		findEnvironmentByID: func(_ context.Context, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{
				ID: envID, Status: environmentdom.StatusStarting,
				BackendRef: &backendRef,
			}, nil
		},
		updateEnvironmentStatus: func(_ context.Context, _ uuid.UUID, status string, _, _ *string) error {
			statusUpdates = append(statusUpdates, status)
			return nil
		},
		touchEnvironment: func(_ context.Context, id uuid.UUID) error {
			touched = append(touched, id)
			return nil
		},
	}
	svc := New(repo, srv.URL, "test-internal-key")

	err := svc.ExecuteStart(context.Background(), envID)

	require.NoError(t, err)
	assert.Equal(t, []string{environmentdom.StatusRunning}, statusUpdates)
	assert.Equal(t, []uuid.UUID{envID}, touched)
}

// TestExecuteStart_SucceedsWhenTouchFails verifies a TouchEnvironment
// failure doesn't turn an otherwise-successful start into a reported
// error: by the time it's called, the backend is already started and
// status is already persisted as running, so failing the whole call here
// would report a start that, in fact, succeeded (and leave the row with a
// stale last_active_at on top).
func TestExecuteStart_SucceedsWhenTouchFails(t *testing.T) {
	envID := uuid.New()
	backendRef := "container-1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(internalStartEnvironmentResponse{
			BaseURL: "http://sandbox:8080",
		})
	}))
	defer srv.Close()

	var statusUpdates []string
	repo := &mockRepo{
		findEnvironmentByID: func(_ context.Context, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{
				ID: envID, Status: environmentdom.StatusStarting,
				BackendRef: &backendRef,
			}, nil
		},
		updateEnvironmentStatus: func(_ context.Context, _ uuid.UUID, status string, _, _ *string) error {
			statusUpdates = append(statusUpdates, status)
			return nil
		},
		touchEnvironment: func(_ context.Context, _ uuid.UUID) error {
			return errors.New("valkey unavailable")
		},
	}
	svc := New(repo, srv.URL, "test-internal-key")

	err := svc.ExecuteStart(context.Background(), envID)

	require.NoError(t, err)
	assert.Equal(t, []string{environmentdom.StatusRunning}, statusUpdates)
}

// TestExecuteStart_RestartsPortsWhenPending_ClearsPendingWhenTouchFails is
// the restart-ports-path counterpart to
// TestExecuteStart_SucceedsWhenTouchFails: a TouchEnvironment failure must
// not skip clearing ports_pending_restart, since the new port bindings are
// already live on the backend by the time TouchEnvironment is called.
func TestExecuteStart_RestartsPortsWhenPending_ClearsPendingWhenTouchFails(t *testing.T) {
	envID := uuid.New()
	backendRef := "container-old"
	volumeRef := "paca-env-abc"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(internalRestartPortsResponse{
			BackendRef: "container-new",
			BaseURL:    "http://sandbox:8080",
		})
	}))
	defer srv.Close()

	var pendingSet *bool
	repo := &mockRepo{
		findEnvironmentByID: func(_ context.Context, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{
				ID: envID, Status: environmentdom.StatusStarting,
				BackendRef: &backendRef, VolumeRef: &volumeRef,
				PortsPendingRestart: true,
			}, nil
		},
		touchEnvironment: func(_ context.Context, _ uuid.UUID) error {
			return errors.New("valkey unavailable")
		},
		setPortsPendingRestart: func(_ context.Context, _ uuid.UUID, pending bool) error {
			pendingSet = &pending
			return nil
		},
	}
	svc := New(repo, srv.URL, "test-internal-key")

	err := svc.ExecuteStart(context.Background(), envID)

	require.NoError(t, err)
	require.NotNil(t, pendingSet)
	assert.False(t, *pendingSet)
}

// TestStopEnvironment_QueuesCommandAndSetsStopping mirrors
// TestStartEnvironment_QueuesCommandAndSetsStarting: StopEnvironment makes
// no agent-runner call at all, just queues a command onto
// StreamEnvironmentCommands and marks the row "stopping". The actual
// agent-runner call moved to ExecuteStop — see
// TestExecuteStop_Success/TestExecuteStop_AgentRunnerFailure below.
func TestStopEnvironment_QueuesCommandAndSetsStopping(t *testing.T) {
	envID := uuid.New()
	backendRef := "container-1"

	var statusUpdates []string
	var publishedStream, publishedType string
	var publishedPayload any
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{
				ID: envID, Status: environmentdom.StatusRunning,
				BackendRef: &backendRef,
			}, nil
		},
		updateEnvironmentStatus: func(_ context.Context, _ uuid.UUID, status string, _, _ *string) error {
			statusUpdates = append(statusUpdates, status)
			return nil
		},
	}
	pub := &mockPublisher{
		appendFn: func(_ context.Context, stream, eventType string, payload any) error {
			publishedStream, publishedType, publishedPayload = stream, eventType, payload
			return nil
		},
	}
	svc := New(repo, "", "").WithPublisher(pub)

	env, err := svc.StopEnvironment(context.Background(), uuid.New(), envID)

	require.NoError(t, err)
	assert.Equal(t, events.StreamEnvironmentCommands, publishedStream)
	assert.Equal(t, events.TopicEnvironmentStop, publishedType)
	assert.Equal(t, map[string]any{"environment_id": envID.String()}, publishedPayload)
	assert.Equal(t, []string{environmentdom.StatusStopping}, statusUpdates)
	assert.Equal(t, environmentdom.StatusStopping, env.Status)
}

// TestStopEnvironment_ReturnsErrorWhenPublishFails mirrors
// TestStartEnvironment_ReturnsErrorWhenPublishFails.
func TestStopEnvironment_ReturnsErrorWhenPublishFails(t *testing.T) {
	envID := uuid.New()
	backendRef := "container-1"

	var statusUpdates []string
	repo := &mockRepo{
		findVisibleEnvironmentInProject: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{
				ID: envID, Status: environmentdom.StatusRunning,
				BackendRef: &backendRef,
			}, nil
		},
		updateEnvironmentStatus: func(_ context.Context, _ uuid.UUID, status string, _, _ *string) error {
			statusUpdates = append(statusUpdates, status)
			return nil
		},
	}
	pub := &mockPublisher{
		appendFn: func(context.Context, string, string, any) error {
			return errors.New("valkey unavailable")
		},
	}
	svc := New(repo, "", "").WithPublisher(pub)

	_, err := svc.StopEnvironment(context.Background(), uuid.New(), envID)

	require.Error(t, err)
	assert.Empty(t, statusUpdates)
}

// TestExecuteStop_Success verifies ExecuteStop —
// worker.EnvironmentCommandConsumer's entry point once it reads a queued
// stop command — calls agent-runner's /stop endpoint and marks the row
// stopped.
func TestExecuteStop_Success(t *testing.T) {
	envID := uuid.New()
	backendRef := "container-1"
	var calledPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	var statusUpdates []string
	repo := &mockRepo{
		findEnvironmentByID: func(_ context.Context, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{
				ID: envID, Status: environmentdom.StatusStopping,
				BackendRef: &backendRef,
			}, nil
		},
		updateEnvironmentStatus: func(_ context.Context, _ uuid.UUID, status string, _, _ *string) error {
			statusUpdates = append(statusUpdates, status)
			return nil
		},
	}
	svc := New(repo, srv.URL, "test-internal-key")

	err := svc.ExecuteStop(context.Background(), envID)

	require.NoError(t, err)
	assert.Equal(t, "/internal/environments/"+envID.String()+"/stop", calledPath)
	assert.Equal(t, []string{environmentdom.StatusStopped}, statusUpdates)
}

// TestExecuteStop_AgentRunnerFailure verifies a failed agent-runner call
// records StatusError with the failure reason, rather than leaving the row
// stuck "stopping" forever.
func TestExecuteStop_AgentRunnerFailure(t *testing.T) {
	envID := uuid.New()
	backendRef := "container-1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	var statusUpdates []string
	var errMsgs []*string
	repo := &mockRepo{
		findEnvironmentByID: func(_ context.Context, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{
				ID: envID, Status: environmentdom.StatusStopping,
				BackendRef: &backendRef,
			}, nil
		},
		updateEnvironmentStatus: func(_ context.Context, _ uuid.UUID, status string, _, errMsg *string) error {
			statusUpdates = append(statusUpdates, status)
			errMsgs = append(errMsgs, errMsg)
			return nil
		},
	}
	svc := New(repo, srv.URL, "test-internal-key")

	err := svc.ExecuteStop(context.Background(), envID)

	require.Error(t, err)
	assert.Equal(t, []string{environmentdom.StatusError}, statusUpdates)
	require.Len(t, errMsgs, 1)
	require.NotNil(t, errMsgs[0])
	assert.Contains(t, *errMsgs[0], "boom")
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
