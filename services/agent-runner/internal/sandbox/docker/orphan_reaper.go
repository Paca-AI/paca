package docker

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/moby/moby/client"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

var _ sandbox.AuthoritativeOrphanReaper = (*Manager)(nil)

// ReapAuthoritativeOrphans removes managed goose, privileged dind and network
// resources whose exact authoritative turn/run lease is no longer active.
// Legacy conversation sandboxes do not carry labelTurnID and are untouched.
func (m *Manager) ReapAuthoritativeOrphans(ctx context.Context, active sandbox.AuthoritativeRuntimeActive) (int, error) {
	filters := make(client.Filters).Add("label", labelManaged+"=true").Add("label", labelTurnID)
	containers, err := m.docker.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return 0, fmt.Errorf("sandbox/docker: list authoritative containers: %w", err)
	}
	type runtimeState struct {
		active bool
		err    error
	}
	states := make(map[string]runtimeState)
	resolve := func(labels map[string]string) (runtimeState, bool) {
		turnID, turnErr := uuid.Parse(labels[labelTurnID])
		runID, runErr := uuid.Parse(labels[labelConvID])
		if turnErr != nil || runErr != nil {
			return runtimeState{err: fmt.Errorf("sandbox/docker: invalid authoritative labels turn=%q run=%q",
				labels[labelTurnID], labels[labelConvID])}, false
		}
		key := turnID.String() + ":" + runID.String()
		if state, ok := states[key]; ok {
			return state, true
		}
		isActive, activeErr := active(ctx, turnID, runID)
		state := runtimeState{active: isActive, err: activeErr}
		states[key] = state
		return state, true
	}

	var errs []error
	reaped := 0
	for _, item := range containers.Items {
		state, valid := resolve(item.Labels)
		if !valid || state.err != nil {
			errs = append(errs, state.err)
			continue
		}
		if state.active {
			continue
		}
		if _, err := m.docker.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true}); err != nil {
			errs = append(errs, fmt.Errorf("sandbox/docker: reap authoritative container %s: %w", item.ID, err))
			continue
		}
		// Only the primary goose container has Manager state. popState is
		// intentionally harmless for dind containers; when it returns a host
		// port, release it so crash recovery cannot exhaust the in-process pool.
		handleState := m.popState(item.ID)
		if handleState.hostPort != 0 {
			m.releasePort(handleState.hostPort)
		}
		reaped++
	}

	networks, err := m.docker.NetworkList(ctx, client.NetworkListOptions{Filters: filters})
	if err != nil {
		errs = append(errs, fmt.Errorf("sandbox/docker: list authoritative networks: %w", err))
		return reaped, errors.Join(errs...)
	}
	for _, item := range networks.Items {
		state, valid := resolve(item.Labels)
		if !valid || state.err != nil {
			errs = append(errs, state.err)
			continue
		}
		if state.active {
			continue
		}
		if _, err := m.docker.NetworkRemove(ctx, item.ID, client.NetworkRemoveOptions{}); err != nil {
			errs = append(errs, fmt.Errorf("sandbox/docker: reap authoritative network %s: %w", item.ID, err))
			continue
		}
		reaped++
	}
	return reaped, errors.Join(errs...)
}
