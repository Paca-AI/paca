package docker

import (
	"context"
	"testing"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

// TestEnvironmentVolumeName_IsDeterministicPerEnvironmentID is a regression
// test for the paca-env-<id> naming CreateEnvironment/DeleteEnvironment
// both rely on to agree on the same volume across calls.
func TestEnvironmentVolumeName_IsDeterministicPerEnvironmentID(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	want := "paca-env-" + id

	if got := environmentVolumeName(id); got != want {
		t.Errorf("environmentVolumeName(%q) = %q, want %q", id, got, want)
	}
	if got := environmentVolumeName(id); got != want {
		t.Errorf("environmentVolumeName(%q) called again = %q, want the same %q", id, got, want)
	}
}

// TestEnvironmentContainerName_MatchesEnvironmentVolumeName is a regression
// test for the naming this environment's container and its volume
// deliberately share (see environmentContainerName's own doc comment): a
// user should be able to visually pair the two in `docker ps`/`docker
// volume ls`, and recreateGoneEnvironmentContainer relies on the container
// name always resolving to the same paca-env-<id> volume.
func TestEnvironmentContainerName_MatchesEnvironmentVolumeName(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"

	if got, want := environmentContainerName(id), environmentVolumeName(id); got != want {
		t.Errorf("environmentContainerName(%q) = %q, want the same as environmentVolumeName's %q", id, got, want)
	}
}

// TestResolveEnvironmentImage_PrefersCfgImageOverManagerDefault exercises
// EnvironmentConfig.Image's own doc comment contract: a caller-supplied
// image always wins over this Manager's own AgentServerImage default.
func TestResolveEnvironmentImage_PrefersCfgImageOverManagerDefault(t *testing.T) {
	got, err := resolveEnvironmentImage("ghcr.io/paca/custom:v1", "ghcr.io/paca/agent-server:pinned")
	if err != nil {
		t.Fatalf("resolveEnvironmentImage: unexpected error: %v", err)
	}
	if got != "ghcr.io/paca/custom:v1" {
		t.Errorf("resolveEnvironmentImage = %q, want the caller-supplied image", got)
	}
}

// TestResolveEnvironmentImage_FallsBackToManagerDefaultWhenCfgImageEmpty is
// the "empty means use this backend's own config.AgentServerImage" path
// EnvironmentConfig.Image's doc comment describes — the platform default a
// bare create-environment call (no image override) should resolve to.
func TestResolveEnvironmentImage_FallsBackToManagerDefaultWhenCfgImageEmpty(t *testing.T) {
	got, err := resolveEnvironmentImage("", "ghcr.io/paca/agent-server:pinned")
	if err != nil {
		t.Fatalf("resolveEnvironmentImage: unexpected error: %v", err)
	}
	if got != "ghcr.io/paca/agent-server:pinned" {
		t.Errorf("resolveEnvironmentImage = %q, want the manager default", got)
	}
}

// TestResolveEnvironmentImage_ErrorsWhenNeitherIsSet is the deployment-
// misconfiguration case: CreateEnvironment was called with no image
// override, and this Manager's own AgentServerImage was never wired up by
// main.go — see Manager.AgentServerImage's doc comment on why that's an
// exported field a caller sets directly rather than a constructor param.
func TestResolveEnvironmentImage_ErrorsWhenNeitherIsSet(t *testing.T) {
	if _, err := resolveEnvironmentImage("", ""); err == nil {
		t.Error("resolveEnvironmentImage(\"\", \"\") = nil error, want an error")
	}
}

// TestNewHardenedHostConfig_DropsAllCapabilitiesAndAddsBackOnlyTheNarrowList
// is a regression test for the Hardening section of
// docs/ai-agent/environment-management.md: every sandbox (ephemeral
// conversation or static environment) drops every capability and re-adds
// back only the file-ownership/package-install set root actually needs.
func TestNewHardenedHostConfig_DropsAllCapabilitiesAndAddsBackOnlyTheNarrowList(t *testing.T) {
	hc := newHardenedHostConfig()

	if len(hc.CapDrop) != 1 || hc.CapDrop[0] != "ALL" {
		t.Errorf("CapDrop = %v, want [\"ALL\"]", hc.CapDrop)
	}

	wantAdd := map[string]bool{"CHOWN": true, "SETUID": true, "SETGID": true, "DAC_OVERRIDE": true, "FOWNER": true}
	if len(hc.CapAdd) != len(wantAdd) {
		t.Errorf("CapAdd = %v, want exactly %v", hc.CapAdd, wantAdd)
	}
	for _, c := range hc.CapAdd {
		if !wantAdd[c] {
			t.Errorf("CapAdd contains unexpected capability %q", c)
		}
	}
}

// TestNewHardenedHostConfig_SetsThePidsLimit is a regression test for the
// fork-bomb backstop the Hardening section also calls for — PidsLimit must
// actually be set (a nil pointer means "no limit" to the Engine API, the
// opposite of what this is for).
func TestNewHardenedHostConfig_SetsThePidsLimit(t *testing.T) {
	hc := newHardenedHostConfig()

	if hc.PidsLimit == nil {
		t.Fatal("PidsLimit = nil, want a set limit")
	}
	if *hc.PidsLimit != defaultPidsLimit {
		t.Errorf("PidsLimit = %d, want %d", *hc.PidsLimit, defaultPidsLimit)
	}
}

// TestNewHardenedHostConfig_ReturnsAFreshStructEveryCall guards against a
// package-level shared HostConfig (or a shared slice/pointer inside one)
// being handed to two different callers (Start and CreateEnvironment) that
// each go on to mutate their own copy — see newHardenedHostConfig's own
// doc comment on why each call must be independent.
func TestNewHardenedHostConfig_ReturnsAFreshStructEveryCall(t *testing.T) {
	a := newHardenedHostConfig()
	b := newHardenedHostConfig()

	if a == b {
		t.Fatal("newHardenedHostConfig returned the same *HostConfig pointer twice")
	}
	if a.PidsLimit == b.PidsLimit {
		t.Error("newHardenedHostConfig's two calls share the same PidsLimit pointer")
	}

	a.AutoRemove = true
	if b.AutoRemove {
		t.Error("mutating one newHardenedHostConfig result's AutoRemove affected the other's")
	}
}

// TestMarkPortInUse_MakesThePortUnavailableToAcquirePort is a regression
// test for markPortInUse's whole purpose: a port discovered after the fact
// (environmentHostPort, for a pre-existing environment container) must be
// just as unavailable to a later acquirePort call as one acquirePort
// itself handed out — otherwise a concurrent ephemeral Start could be
// handed a port a running environment container already has bound.
func TestMarkPortInUse_MakesThePortUnavailableToAcquirePort(t *testing.T) {
	m := &Manager{portPoolStart: 40000, portPoolSize: 1, portsInUse: make(map[int]bool)}

	m.markPortInUse(40000)

	if _, err := m.acquirePort(); err == nil {
		t.Error("acquirePort succeeded after markPortInUse claimed the pool's only port")
	}
}

// TestPortBindingsFor_EmptyMappingsReturnsNil verifies an environment with
// no user-facing ports at all (SSH not configured, no forwards yet) leaves
// hostCfg.PortBindings nil rather than an empty-but-non-nil map, so
// CreateEnvironment's own insideDocker branch can freely check for nil
// before lazily allocating it.
func TestPortBindingsFor_EmptyMappingsReturnsNil(t *testing.T) {
	if got := portBindingsFor(nil); got != nil {
		t.Errorf("portBindingsFor(nil) = %v, want nil", got)
	}
}

// TestPortBindingsFor_BuildsOnePublicBindingPerMapping verifies every
// mapping gets its own network.PortBinding, bound to publicHostAddr
// (0.0.0.0) rather than localhostAddr — the whole point being these must
// be reachable from outside the Docker host, unlike the internal
// ACP/goose-serve control port's own binding.
func TestPortBindingsFor_BuildsOnePublicBindingPerMapping(t *testing.T) {
	got := portBindingsFor([]sandbox.PortMapping{
		{ContainerPort: sandbox.EnvironmentSSHPort, HostPort: 22001},
		{ContainerPort: 3000, HostPort: 30001},
	})

	if len(got) != 2 {
		t.Fatalf("portBindingsFor returned %d entries, want 2", len(got))
	}
	for containerPort, bindings := range got {
		if len(bindings) != 1 {
			t.Fatalf("bindings for %v = %v, want exactly 1", containerPort, bindings)
		}
		if bindings[0].HostIP != publicHostAddr {
			t.Errorf("bindings for %v HostIP = %v, want publicHostAddr (0.0.0.0)", containerPort, bindings[0].HostIP)
		}
	}
}

// TestRecreateEnvironmentIfMissingEnv_NoopWhenCfgHasNoProvider covers the
// one branch of the GOOSE_PROVIDER backfill (see that method's own doc
// comment) that's testable without a real Docker daemon: nothing to
// backfill, so it must return before ever touching the Docker client — a
// zero-value *Manager (nil docker client) would panic if it didn't.
// Full CreateEnvironment/StartEnvironment coverage needs a real daemon,
// same scoping precedent as this package's other tests (see
// manager_test.go).
func TestRecreateEnvironmentIfMissingEnv_NoopWhenCfgHasNoProvider(t *testing.T) {
	m := &Manager{}

	handle, err := m.recreateEnvironmentIfMissingEnv(context.Background(), "some-container-id", sandbox.EnvironmentConfig{})
	if err != nil {
		t.Fatalf("recreateEnvironmentIfMissingEnv: %v", err)
	}
	if handle != nil {
		t.Errorf("recreateEnvironmentIfMissingEnv with no cfg.Env = %+v, want nil handle", handle)
	}
}

// TestEnsureEnvironmentInfraEnv_NoopWhenCfgHasNoEnv guards against
// handleStartEnvironment's plain restart (whose EnvironmentConfig never
// sets Env at all — see its own doc comment) silently wiping
// pacaInfraEnvKeys (PACA_API_KEY, GOOSE_PATH_ROOT, etc.) from an
// already-configured container: a nil/empty cfg.Env must be treated as
// "nothing to reconcile", not "clear every key". Same zero-value-Manager
// technique as TestRecreateEnvironmentIfMissingEnv_NoopWhenCfgHasNoProvider
// just above — the guard must return before ever touching the Docker
// client, which a nil client would panic on otherwise.
func TestEnsureEnvironmentInfraEnv_NoopWhenCfgHasNoEnv(t *testing.T) {
	m := &Manager{}

	handle, err := m.ensureEnvironmentInfraEnv(context.Background(), "some-container-id", sandbox.EnvironmentConfig{})
	if err != nil {
		t.Fatalf("ensureEnvironmentInfraEnv: %v", err)
	}
	if handle != nil {
		t.Errorf("ensureEnvironmentInfraEnv with no cfg.Env = %+v, want nil handle", handle)
	}
}
