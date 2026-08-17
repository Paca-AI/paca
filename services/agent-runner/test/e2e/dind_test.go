package e2e_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

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
