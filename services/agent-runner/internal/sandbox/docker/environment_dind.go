// environment_dind.go gives a static environment's container access to
// Docker when cfg.DockerEnabled — the long-lived counterpart to dind.go's
// per-conversation sidecar. Same isolation model (see dind.go's package doc
// comment): a dedicated docker:dind container on a private network only
// this one environment's container can reach.
//
// Unlike dind.go's sidecar — created fresh on every Start, destroyed on
// every Stop, since a conversation's own sandbox container is equally
// disposable — an environment's sidecar is created exactly once, by
// CreateEnvironment, and then stopped/started alongside the environment's
// own container by StopEnvironment/StartEnvironment for the rest of its
// life, mirroring how the environment's container itself persists (see
// environment.go's own package doc comment on why a static environment
// isn't disposable). Only DeleteEnvironment ever actually removes it.
//
// Every function here derives what it needs from cfg.EnvironmentID (or, for
// stopEnvironmentDindSidecar/removeEnvironmentDindSidecar below — called
// from StopEnvironment/DeleteEnvironment, neither of which receives
// EnvironmentID; see sandbox.EnvironmentBackend's own doc comment on that
// being deliberate) from whatever the caller already has: the environment
// container's own labelEnvID label, or volumeRef's deterministic naming.
// Nothing about this sidecar is cached in this Manager's own memory — the
// same "no new in-memory registry" design choice
// docs/ai-agent/environment-management.md makes for the environment feature
// as a whole.
package docker

import (
	"context"
	"fmt"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

// environmentDindContainerName and environmentDindNetworkName are
// deterministic (not Docker-assigned), exactly like environmentContainerName/
// environmentVolumeName (environment.go) and dindContainerName/
// conversationNetworkName (dind.go) — so every one of CreateEnvironment/
// StartEnvironment/RestartEnvironmentPorts can address this environment's
// own sidecar/network from cfg.EnvironmentID alone.
func environmentDindContainerName(environmentID string) string {
	return "paca-env-dind-" + environmentID
}

func environmentDindNetworkName(environmentID string) string {
	return "paca-env-dind-net-" + environmentID
}

// environmentDindDockerHost is the DOCKER_HOST value baked into an
// environment container's env whenever cfg.DockerEnabled — mirrors
// dindDockerHost (dind.go), parameterized by environmentID instead of
// conversationID. dindAPIPort is dind.go's own constant, shared as-is.
func environmentDindDockerHost(environmentID string) string {
	return fmt.Sprintf("tcp://%s:%d", environmentDindContainerName(environmentID), dindAPIPort)
}

// environmentIDFromVolumeName recovers environmentID from a volume name
// produced by environmentVolumeName — the inverse of that function. Lets
// DeleteEnvironment find this environment's dind sidecar (if any) from
// volumeRef alone, without inspecting a container that, per
// DeleteEnvironment's own idempotency contract, may already be gone.
func environmentIDFromVolumeName(volumeName string) string {
	return strings.TrimPrefix(volumeName, "paca-env-")
}

// createEnvironmentDindSidecar creates and starts environmentID's dedicated
// dind network+container. Originally called exactly once, by
// CreateEnvironment, the same "created once, cycled by Stop/Start for the
// rest of its life" contract the environment's own container follows (see
// this file's own doc comment) — now also called by startEnvironmentDindSidecar
// below to self-heal a sidecar removed outside of Paca, so this may run a
// second (or later) time against an environment whose dind network still
// exists from before. Idempotent with respect to that network because of
// it: NetworkInspect first, only NetworkCreate when it's actually missing,
// so a self-heal recreate never fails on a spurious "network already
// exists" the way blindly recreating it every time would. networkCreatedHere
// gates the failure-cleanup below to the network this call actually
// created — a self-heal that fails after finding (not creating) the
// network must never remove one that survived from before and might still
// be wanted.
//
// Unlike startDindSidecar (dind.go), AutoRemove is false: this container
// must survive a StopEnvironment the same way the environment's own
// container does. Resources are the same hardcoded 2 CPU/4GiB
// startDindSidecar already uses for the ephemeral sidecar, not
// cfg.CPULimit/MemoryLimit — a deliberate, documented deviation from how
// createAndStartEnvironmentContainer sizes the environment's own container:
// the sidecar's footprint is a fixed cost of opting into Docker access at
// all, independent of how the environment itself is sized. On any failure
// partway through, tears down whatever it already created before
// returning — mirrors startDindSidecar's own cleanup contract.
func (m *Manager) createEnvironmentDindSidecar(ctx context.Context, environmentID string) (err error) {
	if err := m.ensureImage(ctx, sandbox.DindImage); err != nil {
		return err
	}

	netName := environmentDindNetworkName(environmentID)
	networkCreatedHere := false
	if _, inspectErr := m.docker.NetworkInspect(ctx, netName, client.NetworkInspectOptions{}); inspectErr != nil {
		if !cerrdefs.IsNotFound(inspectErr) {
			return fmt.Errorf("sandbox/docker: inspect environment dind network %s: %w", netName, inspectErr)
		}
		if _, err := m.docker.NetworkCreate(ctx, netName, client.NetworkCreateOptions{
			Driver: "bridge",
			Labels: map[string]string{labelEnvID: environmentID, labelManaged: "true"},
		}); err != nil {
			return fmt.Errorf("sandbox/docker: create environment dind network: %w", err)
		}
		networkCreatedHere = true
	}
	defer func() {
		if err != nil && networkCreatedHere {
			removeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = m.docker.NetworkRemove(removeCtx, netName, client.NetworkRemoveOptions{})
		}
	}()

	created, err := m.docker.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: environmentDindContainerName(environmentID),
		Config: &container.Config{
			Image: sandbox.DindImage,
			Env:   []string{"DOCKER_TLS_CERTDIR="},
			Labels: map[string]string{
				labelEnvID:   environmentID,
				labelManaged: "true",
			},
		},
		HostConfig: &container.HostConfig{
			AutoRemove: false,
			Privileged: true,
			Resources: container.Resources{
				NanoCPUs: 2_000_000_000, // 2 cores — see doc comment above
				Memory:   4 << 30,       // 4 GiB
			},
		},
		NetworkingConfig: &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				netName: {},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("sandbox/docker: create environment dind sidecar: %w", err)
	}
	defer func() {
		if err != nil {
			removeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = m.docker.ContainerRemove(removeCtx, created.ID, client.ContainerRemoveOptions{Force: true})
		}
	}()

	if _, err = m.docker.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("sandbox/docker: start environment dind sidecar: %w", err)
	}
	return nil
}

// removeEnvironmentDindSidecar tears down environmentID's dind sidecar
// container and network, best-effort — called by DeleteEnvironment and by
// CreateEnvironment's own failure-cleanup path. Idempotent: a container or
// network that's already gone is silently treated as already-removed,
// matching this package's existing not-found tolerance elsewhere (e.g.
// StopEnvironment, DeleteEnvironment in environment.go).
func (m *Manager) removeEnvironmentDindSidecar(ctx context.Context, environmentID string) {
	_, _ = m.docker.ContainerRemove(ctx, environmentDindContainerName(environmentID), client.ContainerRemoveOptions{Force: true})
	_, _ = m.docker.NetworkRemove(ctx, environmentDindNetworkName(environmentID), client.NetworkRemoveOptions{})
}

// startEnvironmentDindSidecar restarts environmentID's existing dind
// sidecar container — StartEnvironment's counterpart to
// createEnvironmentDindSidecar, mirroring how StartEnvironment itself
// restarts the environment's own already-created container rather than
// making a new one, unless that container has been removed outside of
// Paca, in which case it self-heals via createEnvironmentDindSidecar
// instead of failing outright — the same treatment
// recreateGoneEnvironmentContainer gives the environment's own container
// (see that function's doc comment for the shared rationale). Safe to
// self-heal unconditionally here, with no equivalent volume-survives
// check: unlike the environment's own container, the sidecar carries no
// persistent state of its own at all (createEnvironmentDindSidecar mounts
// no volume into it — it's just an isolated Docker daemon), so there is
// nothing about a gone sidecar that could ever be data loss.
func (m *Manager) startEnvironmentDindSidecar(ctx context.Context, environmentID string) error {
	if _, err := m.docker.ContainerStart(ctx, environmentDindContainerName(environmentID), client.ContainerStartOptions{}); err != nil {
		if cerrdefs.IsNotFound(err) {
			return m.createEnvironmentDindSidecar(ctx, environmentID)
		}
		return fmt.Errorf("sandbox/docker: start environment dind sidecar: %w", err)
	}
	return nil
}

// stopEnvironmentDindSidecar stops backendRef's dind sidecar, if it has
// one — called by StopEnvironment, which (per
// sandbox.EnvironmentBackend.StopEnvironment's own doc comment) deliberately
// receives no EnvironmentID, only backendRef. Recovers it the same way
// stopping a k8s environment's sidecar needs no extra step at all (the
// whole Pod scales to zero together) — by reading environmentID back off
// the environment container's own labelEnvID label, set once at create time
// (createAndStartEnvironmentContainer), rather than adding an in-memory
// registry this Manager would need to keep in sync across restarts and
// replicas (see this file's own doc comment). A backendRef with no
// labelEnvID label, or whose sidecar was never created (docker_enabled was
// never set), is a silent no-op — StopEnvironment's own contract is
// best-effort, and most environments have no sidecar to stop at all.
func (m *Manager) stopEnvironmentDindSidecar(ctx context.Context, backendRef string) {
	inspect, err := m.docker.ContainerInspect(ctx, backendRef, client.ContainerInspectOptions{})
	if err != nil || inspect.Container.Config == nil {
		return
	}
	environmentID := inspect.Container.Config.Labels[labelEnvID]
	if environmentID == "" {
		return
	}
	timeoutSecs := int(sandbox.StopTimeout.Seconds())
	_, _ = m.docker.ContainerStop(ctx, environmentDindContainerName(environmentID), client.ContainerStopOptions{Timeout: &timeoutSecs})
}
