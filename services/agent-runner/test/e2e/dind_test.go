package e2e_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

// TestSandboxRunsAsRootWithDockerAccess runs sandbox.Manager against the
// real Docker daemon — not mocks — using the actual agent-server image
// (root user + docker CLI, see services/agent-server/Dockerfile) to verify
// the two things this whole feature exists for: a conversation's sandbox
// runs as root (no more "are you root?" package-manager failures), and it
// can drive Docker via the DOCKER_HOST env var sandbox.Start points at its
// dedicated per-conversation dind sidecar (see internal/sandbox/dind.go).
func TestSandboxRunsAsRootWithDockerAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Docker-heavy test in -short mode")
	}
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)

	mgr := newSandboxManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	handle, err := mgr.Start(ctx, sandbox.Config{
		ConversationID:    "e2e-dind-conv-root",
		Image:             agentServerImage(t),
		Env:               map[string]string{"GOOSE_PROVIDER": "openai", "GOOSE_MODEL": "fake-model"},
		GitCommitterName:  "paca-agent",
		GitCommitterEmail: "agent@example.com",
		DockerEnabled:     true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := mgr.Stop(context.Background(), handle); err != nil {
			t.Errorf("cleanup: stop sandbox: %v", err)
		}
	})

	out, exitCode, err := mgr.Exec(ctx, handle.ContainerID, []string{"id", "-u"})
	if err != nil {
		t.Fatalf("exec id -u: %v", err)
	}
	if exitCode != 0 || strings.TrimSpace(out) != "0" {
		t.Errorf("sandbox is not running as root: id -u = %q (exit %d), want \"0\" (exit 0)", out, exitCode)
	}

	// The exact scenario from the reported bug: a package-manager operation
	// that needs to write to /var/lib/dpkg, previously rejected with "are
	// you root?" — confirms the fix at the level the original failure was
	// actually observed at, not just id -u in isolation.
	out, exitCode, err = mgr.Exec(ctx, handle.ContainerID, []string{"sh", "-c", "apt-get update -qq && apt-get install -y -qq curl"})
	if err != nil {
		t.Fatalf("exec apt-get install: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("apt-get install failed as root: exit %d, output:\n%s", exitCode, out)
	}

	out, exitCode, err = mgr.Exec(ctx, handle.ContainerID, []string{"docker", "run", "--rm", "hello-world"})
	if err != nil {
		t.Fatalf("exec docker run hello-world: %v", err)
	}
	if exitCode != 0 || !strings.Contains(out, "Hello from Docker!") {
		t.Errorf("docker run hello-world inside the sandbox failed: exit %d, output:\n%s", exitCode, out)
	}
}

// TestSandboxDindSidecarsAreIsolatedPerConversation runs two separate
// conversations' sandboxes concurrently and confirms each one's Docker
// access is its own private environment: a container started inside one
// conversation's dind sidecar is invisible to `docker ps` run inside the
// other conversation's sandbox. This is the property the per-conversation
// (rather than one-shared-daemon) design exists to guarantee — see dind.go's
// package doc comment.
func TestSandboxDindSidecarsAreIsolatedPerConversation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Docker-heavy test in -short mode")
	}
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)

	mgr := newSandboxManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	image := agentServerImage(t)
	startSandbox := func(convID string) *sandbox.Handle {
		t.Helper()
		handle, err := mgr.Start(ctx, sandbox.Config{
			ConversationID:    convID,
			Image:             image,
			Env:               map[string]string{"GOOSE_PROVIDER": "openai", "GOOSE_MODEL": "fake-model"},
			GitCommitterName:  "paca-agent",
			GitCommitterEmail: "agent@example.com",
			DockerEnabled:     true,
		})
		if err != nil {
			t.Fatalf("Start(%s): %v", convID, err)
		}
		t.Cleanup(func() {
			if err := mgr.Stop(context.Background(), handle); err != nil {
				t.Errorf("cleanup: stop sandbox %s: %v", convID, err)
			}
		})
		return handle
	}

	handleA := startSandbox("e2e-dind-conv-isolation-a")
	handleB := startSandbox("e2e-dind-conv-isolation-b")

	const markerContainerName = "marker-from-conversation-a"
	if _, exitCode, err := mgr.Exec(ctx, handleA.ContainerID,
		[]string{"docker", "run", "-d", "--name", markerContainerName, "alpine", "sleep", "60"}); err != nil || exitCode != 0 {
		t.Fatalf("start marker container in conversation A's dind: exitCode=%d err=%v", exitCode, err)
	}

	out, exitCode, err := mgr.Exec(ctx, handleA.ContainerID, []string{"docker", "ps", "-a", "--format", "{{.Names}}"})
	if err != nil || exitCode != 0 {
		t.Fatalf("docker ps in conversation A: exitCode=%d err=%v", exitCode, err)
	}
	if !strings.Contains(out, markerContainerName) {
		t.Fatalf("conversation A can't see its own marker container — test setup is broken, not just isolation:\n%s", out)
	}

	out, exitCode, err = mgr.Exec(ctx, handleB.ContainerID, []string{"docker", "ps", "-a", "--format", "{{.Names}}"})
	if err != nil || exitCode != 0 {
		t.Fatalf("docker ps in conversation B: exitCode=%d err=%v", exitCode, err)
	}
	if strings.Contains(out, markerContainerName) {
		t.Errorf("conversation B can see conversation A's marker container — dind sidecars are not isolated:\n%s", out)
	}
}

// TestSandboxDindSidecarNotStartedWhenDisabled is the negative-path
// counterpart to the two tests above: both of those only ever exercise
// DockerEnabled: true, so a regression that started the privileged sidecar,
// created its private network, or set DOCKER_HOST even when the agent never
// opted in would pass unnoticed. Confirms none of that happens for a
// DockerEnabled: false (the default) conversation.
func TestSandboxDindSidecarNotStartedWhenDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Docker-heavy test in -short mode")
	}
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)

	mgr := newSandboxManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// rawUpstreamGooseImage, not agentServerImage: this test never touches
	// Docker from inside the sandbox, so it doesn't need the MCP-enabled
	// image agentServerImage requires building first (see that helper's doc
	// comment) — it only needs any goose serve container to inspect from
	// the outside.
	const convID = "e2e-dind-conv-disabled"
	handle, err := mgr.Start(ctx, sandbox.Config{
		ConversationID:    convID,
		Image:             rawUpstreamGooseImage(t),
		Env:               map[string]string{"GOOSE_PROVIDER": "openai", "GOOSE_MODEL": "fake-model"},
		GitCommitterName:  "paca-agent",
		GitCommitterEmail: "agent@example.com",
		// DockerEnabled intentionally omitted (false, the default) — the
		// whole point of this test.
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := mgr.Stop(context.Background(), handle); err != nil {
			t.Errorf("cleanup: stop sandbox: %v", err)
		}
	})

	docker, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("create docker client: %v", err)
	}
	defer func() { _ = docker.Close() }()

	// Names replicated from dind.go's dindContainerName/
	// conversationNetworkName formulas rather than imported — this package
	// is e2e_test, outside package sandbox, so it has no access to those
	// unexported functions and has to assert black-box, the same as any
	// other caller would.
	if _, err := docker.ContainerInspect(ctx, "paca-dind-"+convID, client.ContainerInspectOptions{}); !errdefs.IsNotFound(err) {
		t.Errorf("dind sidecar container exists for a DockerEnabled=false conversation (inspect err=%v, want not-found)", err)
	}
	if _, err := docker.NetworkInspect(ctx, "paca-sbx-net-"+convID, client.NetworkInspectOptions{}); !errdefs.IsNotFound(err) {
		t.Errorf("conversation network exists for a DockerEnabled=false conversation (inspect err=%v, want not-found)", err)
	}

	inspected, err := docker.ContainerInspect(ctx, handle.ContainerID, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("inspect sandbox container: %v", err)
	}
	if inspected.Container.Config == nil {
		t.Fatal("sandbox container inspect response has no Config")
	}
	for _, e := range inspected.Container.Config.Env {
		if strings.HasPrefix(e, "DOCKER_HOST=") {
			t.Errorf("sandbox container has %q set despite DockerEnabled=false", e)
		}
	}
}
