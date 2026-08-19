// dind.go gives every conversation's sandbox container access to Docker —
// not by mounting this process's own /var/run/docker.sock the way this
// process reaches the Docker host that runs sandboxes in the first place
// (see Config's doc comment on that being sibling-container/DooD, not
// Docker-in-Docker): doing that inside the sandbox too would hand a
// conversation root-equivalent control over the very same Docker host that
// runs every other conversation's sandbox, plus this process itself.
// Instead, every conversation gets its own dedicated `docker:dind` sidecar
// on a private network only that one sandbox container can reach.
//
// Confirmed directly against the pinned digest below, not assumed: a plain
// container on that kind of network, with DOCKER_HOST pointed at the
// sidecar by name over plaintext port 2375, ran `docker run hello-world`
// end to end — image pull, container run, log streaming, all the way
// through — and a second, separately-networked sidecar had no visibility
// into the first's containers at all.
package sandbox

import (
	"context"
	"fmt"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const (
	// dindImage is pinned by digest for the same full-immutability reason
	// services/agent-server/Dockerfile pins the goose base image (see that
	// file's doc comment) — a tag can be force-moved upstream, a digest
	// can't. Resolved directly from docker:27-dind at the time this was
	// written (`docker pull docker:27-dind && docker inspect ... {{index
	// .RepoDigests 0}}`); rotate the same way, not by editing this by hand.
	dindImage = "docker:27-dind@sha256:aa3df78ecf320f5fafdce71c659f1629e96e9de0968305fe1de670e0ca9176ce"

	// dindAPIPort is where dockerd listens for the Engine API inside the
	// sidecar once DOCKER_TLS_CERTDIR is cleared (see startDindSidecar) —
	// 2375 is the plaintext port; 2376 (TLS) is left unused since network
	// isolation, not TLS, is this design's security boundary (see the
	// package doc comment) and generating/rotating client certs for a
	// pairing that's torn down with the conversation anyway would add real
	// complexity for no attacker that isolation doesn't already keep out.
	dindAPIPort = 2375

	// dindReadyTimeout is generous on purpose: dockerd itself is typically
	// listening within a couple of seconds (confirmed in its own logs —
	// "API listen on [::]:2375"), but polling it via Exec (see
	// waitForDindReady's doc comment on why Exec, not the network) goes
	// through this process's own Docker daemon's exec machinery, which was
	// directly observed taking upwards of 15s to first succeed under
	// concurrent host load in testing, well past dockerd's own actual
	// readiness. A short timeout tuned to the common case would spuriously
	// fail exactly when the host is busiest — the worst time for that.
	dindReadyTimeout   = 90 * time.Second
	dindReadyPollEvery = 500 * time.Millisecond
)

// dindContainerName and conversationNetworkName are deterministic (not
// Docker-assigned) specifically so the sandbox container's DOCKER_HOST env
// var can be set before the sidecar even exists — sibling-name DNS
// resolution on a user-defined bridge network (unlike the default "bridge"
// network, which does none) resolves it once the sidecar actually starts,
// with no IP lookup or startup-ordering dependency needed.
func dindContainerName(conversationID string) string {
	return "paca-dind-" + conversationID
}

func conversationNetworkName(conversationID string) string {
	return "paca-sbx-net-" + conversationID
}

// dindDockerHost is the DOCKER_HOST value the sandbox container's own
// environment gets pointed at — see startDindSidecar.
func dindDockerHost(conversationID string) string {
	return fmt.Sprintf("tcp://%s:%d", dindContainerName(conversationID), dindAPIPort)
}

// dindSidecar is what startDindSidecar hands back to Start — everything
// stopDindSidecar needs to tear the pairing down again.
type dindSidecar struct {
	containerID string
	networkID   string
}

// startDindSidecar creates a private, per-conversation Docker network and a
// `docker:dind` container on it — privileged (required for dockerd to
// manage its own cgroups/mounts/iptables; nothing less works), but that
// privilege is scoped to this one disposable sidecar, never the agent's own
// sandbox container (see the package doc comment). Nothing else is ever
// attached to the network this creates: not this process, not any other
// conversation's sandbox or sidecar — so the sidecar's unauthenticated
// Engine API is reachable only by the one sandbox container Start's caller
// attaches to the same network afterward.
//
// Does NOT wait for dockerd inside the sidecar to actually answer `docker
// info` — that used to be blocking here, but Start now runs that wait
// (waitForDindReady) concurrently with its own sandbox container's boot
// instead, since the two don't depend on each other (see Start's doc
// comment on the sidecar/dindReady goroutine). Callers that need a live
// dockerd before returning must call waitForDindReady themselves.
//
// On any failure partway through, tears down whatever it already created
// before returning — callers don't need their own partial-failure cleanup
// for this pairing the way Start's own cleanup() already doesn't need to
// know about it on success.
func (m *Manager) startDindSidecar(ctx context.Context, conversationID string) (sidecar *dindSidecar, err error) {
	if err := m.ensureImage(ctx, dindImage); err != nil {
		return nil, err
	}

	netResult, err := m.docker.NetworkCreate(ctx, conversationNetworkName(conversationID), client.NetworkCreateOptions{
		Driver: "bridge",
		Labels: map[string]string{labelConvID: conversationID, labelManaged: "true"},
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox: create conversation network: %w", err)
	}
	sidecar = &dindSidecar{networkID: netResult.ID}
	defer func() {
		if err != nil {
			m.stopDindSidecar(context.Background(), sidecar)
		}
	}()

	created, err := m.docker.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: dindContainerName(conversationID),
		Config: &container.Config{
			Image: dindImage,
			Env:   []string{"DOCKER_TLS_CERTDIR="},
			Labels: map[string]string{
				labelConvID:  conversationID,
				labelManaged: "true",
			},
		},
		HostConfig: &container.HostConfig{
			AutoRemove: true,
			Privileged: true,
			Resources: container.Resources{
				NanoCPUs: 2_000_000_000, // 2 cores
				Memory:   4 << 30,       // 4 GiB
			},
		},
		NetworkingConfig: &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				conversationNetworkName(conversationID): {},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox: create dind sidecar: %w", err)
	}
	sidecar.containerID = created.ID

	if _, err = m.docker.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return nil, fmt.Errorf("sandbox: start dind sidecar: %w", err)
	}

	return sidecar, nil
}

// waitForDindReady polls by exec'ing `docker info` *inside* the sidecar
// container instead of reaching it over the network the way waitForReady
// checks the main sandbox's /status: at this point in Start, nothing else
// exists to check it from yet — the sandbox container that will actually
// talk to this sidecar over the network isn't created until after this
// returns — but Docker's Exec API reaches any running container directly
// through this process's own daemon connection regardless of container
// networking, so it needs no network path to exist at all, only the
// container to be running.
func (m *Manager) waitForDindReady(ctx context.Context, containerID string) error {
	deadline := time.Now().Add(dindReadyTimeout)
	for time.Now().Before(deadline) {
		if _, exitCode, err := m.Exec(ctx, containerID, []string{"docker", "info"}); err == nil && exitCode == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(dindReadyPollEvery):
		}
	}
	return fmt.Errorf("sandbox: dind sidecar %s never became ready after %s", containerID, dindReadyTimeout)
}

// stopDindSidecar tears down what startDindSidecar created, best-effort
// (mirrors Stop's own contract) — the sidecar container first (AutoRemove
// handles it on a clean exit; the explicit ContainerRemove here is what
// matters when startDindSidecar is unwinding a partial failure instead),
// then the network, which Docker refuses to remove while any container is
// still attached to it.
func (m *Manager) stopDindSidecar(ctx context.Context, sidecar *dindSidecar) {
	if sidecar == nil {
		return
	}
	if sidecar.containerID != "" {
		_, _ = m.docker.ContainerRemove(ctx, sidecar.containerID, client.ContainerRemoveOptions{Force: true})
	}
	if sidecar.networkID != "" {
		_, _ = m.docker.NetworkRemove(ctx, sidecar.networkID, client.NetworkRemoveOptions{})
	}
}
