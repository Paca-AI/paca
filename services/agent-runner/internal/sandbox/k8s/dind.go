// dind.go gives a conversation's sandbox Pod access to Docker when
// cfg.DockerEnabled — the Kubernetes analog of ../dind.go, simplified by
// Kubernetes' own Pod network model: every container in a Pod already
// shares one network namespace, so the dind sidecar added here needs no
// dedicated per-conversation network the way ../dind.go's Docker backend
// does to pair a sandbox container with its sidecar — DOCKER_HOST=tcp://
// localhost:2375 (set on the primary container in Start) already reaches
// it directly.
//
// Privileged, same as ../dind.go's own sidecar and for the same reason:
// dockerd needs to manage its own cgroups/mounts/iptables, and nothing
// less than a privileged container grants that. See ../dind.go's package
// doc comment for the full isolation rationale — it carries over
// unchanged, since this privilege is still scoped to one disposable
// sidecar container inside one disposable, per-conversation Pod, never the
// cluster's own nodes or any other conversation's Pod. Whether a cluster's
// Pod Security admission configuration actually allows a privileged Pod at
// all is a deployment-time concern for the Helm chart, not something this
// package can control — see deploy/helm/paca's own docs on DockerEnabled.
package k8s

import (
	"bytes"
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

const (
	dindContainerName = "dind"

	// dindReadyTimeout/dindReadyPollEvery mirror ../dind.go's own
	// constants — the same dockerd startup-time reasoning applies
	// unchanged; only the mechanism used to poll it (pods/exec here vs.
	// the Docker Exec API there) differs.
	dindReadyTimeout   = 90 * time.Second
	dindReadyPollEvery = 500 * time.Millisecond
)

// dindContainer returns the docker:dind sidecar container spec appended to
// a sandbox Pod's Containers when cfg.DockerEnabled. resources is the same
// ResourceRequirements Start already computed for the primary container —
// reused here rather than left unset, matching ../docker/dind.go's own
// sidecar (which has always requested/limited its dind container, 2
// CPU/4GiB hardcoded); without a limit here specifically, this privileged
// sidecar could consume unbounded CPU/memory on whatever node it lands on,
// a bigger blast radius on Kubernetes' typical multi-pod-per-node
// bin-packing than the docker backend's one-sidecar-per-dedicated-host
// model.
func dindContainer(resources corev1.ResourceRequirements) corev1.Container {
	return corev1.Container{
		Name:      dindContainerName,
		Image:     sandbox.DindImage,
		Env:       []corev1.EnvVar{{Name: "DOCKER_TLS_CERTDIR", Value: ""}},
		Resources: resources,
		SecurityContext: &corev1.SecurityContext{
			// Privileged implies AllowPrivilegeEscalation and root
			// regardless of what else is set here — dockerd needs to
			// manage its own cgroups/mounts/iptables, nothing less grants
			// that (see the package doc comment). No capabilities/seccomp
			// hardening applies on top of Privileged: true; it overrides
			// them.
			Privileged: ptr.To(true),
		},
	}
}

// waitForDindReady polls by exec'ing `docker info` inside the dind
// container until it answers — mirrors ../dind.go's waitForDindReady,
// adapted to the pods/exec subresource instead of the Docker Exec API. At
// this point in Start, the sandbox container that will actually use this
// sidecar over "localhost" is already up (this is the last readiness check
// before Start returns), so — unlike ../dind.go, which runs this
// concurrently with its own sandbox container's boot since Docker's
// per-conversation network makes the two independent — there is nothing
// to overlap this with here: both containers were created together as
// part of the same Pod spec and are already running by the time this is
// called.
func (m *Manager) waitForDindReady(ctx context.Context, podName string) error {
	deadline := time.Now().Add(dindReadyTimeout)
	for time.Now().Before(deadline) {
		var out bytes.Buffer
		if err := m.streamExec(ctx, podName, dindContainerName, []string{"docker", "info"}, nil, &out, &out); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(dindReadyPollEvery):
		}
	}
	return fmt.Errorf("sandbox/k8s: dind sidecar in pod %s never became ready after %s", podName, dindReadyTimeout)
}
