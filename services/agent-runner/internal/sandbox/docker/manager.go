// Package docker implements sandbox.Backend by running one Docker
// container per active conversation, each running `goose serve` — the
// original sandbox backend, reached via a mounted /var/run/docker.sock
// (sibling-container/DooD, not Docker-in-Docker). Go analog of
// services/ai-agent/src/agent/docker_workspace.py, adapted for Goose:
//   - runs the Goose image (services/agent-server/Dockerfile, built on
//     ghcr.io/aaif-goose/goose — see that Dockerfile's doc comment) instead
//     of the OpenHands agent-server image
//   - no repo_tools injection (that mechanism is being replaced by exposing
//     list_repositories/clone_repository as more Paca MCP server tools)
//   - health-checks goose serve's /status endpoint instead of OpenHands'
//     /health
//
// See ../k8s for the Kubernetes-Job alternative, selected instead when
// this process itself runs inside a cluster with no Docker socket to
// mount (config.Settings.SandboxBackend).
package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

const (
	labelConvID  = "paca.conversation_id"
	labelManaged = "paca.managed"
)

// containerPort is the port `goose serve` listens on inside every sandbox
// container — a var, not a const, since network.Port is a struct type
// (MustParsePort can't be evaluated at compile time).
var containerPort = network.MustParsePort(fmt.Sprintf("%d/tcp", sandbox.GooseServePort))

var localhostAddr = netip.MustParseAddr("127.0.0.1")

var _ sandbox.Backend = (*Manager)(nil)
var _ sandbox.FullBackend = (*Manager)(nil)

// handleState is Manager's own per-Handle bookkeeping — the host-port
// mapping and dind-sidecar pairing a Handle's ContainerID needs again at
// Stop time. Kept here, keyed by ContainerID, rather than on
// sandbox.Handle itself — see that struct's own doc comment on why: it's
// a plain, backend-opaque value type shared with every other
// sandbox.Backend implementation, so this package can't (and shouldn't)
// smuggle Docker-specific fields onto it.
type handleState struct {
	hostPort int          // 0 unless this handle used host-port-mapping mode
	dind     *dindSidecar // this container's paired Docker-access sidecar — see dind.go
}

// Manager owns the Docker client and (in local-dev, non-networked mode) a
// pool of host ports to map container ports onto — mirrors the module-level
// port pool in docker_workspace.py, scoped to a Manager instance instead of
// package globals so tests don't share state across Manager instances.
type Manager struct {
	docker        *client.Client
	portPoolStart int
	portPoolSize  int
	portMu        sync.Mutex
	portsInUse    map[int]bool

	// AgentServerImage is the platform's pinned goose image reference
	// (config.Settings.AgentServerImage) — read only by CreateEnvironment,
	// as its fallback when EnvironmentConfig.Image is empty (see that
	// field's own doc comment in internal/sandbox/environment.go). Start
	// never reads this: an ephemeral conversation's cfg.Image is already
	// always non-empty, resolved upstream by executor.Options.Image.
	//
	// Deliberately an exported field, not a NewManager constructor
	// parameter or a setter method: every existing NewManager call site
	// (cmd/agent-runner/main.go, test/e2e, this package's own tests) stays
	// source-compatible, and the one caller that actually resolves
	// environments — main.go's newSandboxBackend, already being widened to
	// return sandbox.FullBackend for this feature — can set this directly
	// on the *Manager it gets back in one line instead.
	AgentServerImage string

	// confirmedImages caches which image refs ensureImage has already
	// confirmed present on this Docker host (via ImageList or a successful
	// ImagePull) — see ensureImage's doc comment.
	imageMu         sync.Mutex
	confirmedImages map[string]bool

	stateMu sync.Mutex
	state   map[string]handleState
}

// NewManager builds a Manager from the standard Docker environment
// variables (DOCKER_HOST, etc. — see client.FromEnv).
func NewManager(portPoolStart, portPoolSize int) (*Manager, error) {
	// WithAPIVersionNegotiation dropped — the client library's own
	// deprecation notice says API-version negotiation is enabled by
	// default now, making the option a no-op.
	dockerClient, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("sandbox/docker: create docker client: %w", err)
	}
	return &Manager{
		docker:          dockerClient,
		portPoolStart:   portPoolStart,
		portPoolSize:    portPoolSize,
		portsInUse:      make(map[int]bool),
		confirmedImages: make(map[string]bool),
		state:           make(map[string]handleState),
	}, nil
}

func isInsideDocker() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

const (
	// defaultSandboxNanoCPUs/defaultSandboxMemoryBytes are the CPU/memory
	// limits every sandbox container gets — 2 cores, 4 GiB. Hardcoded
	// rather than sourced from a config knob: unlike the kubernetes
	// backend's SandboxCPULimit/SandboxMemoryLimit (see
	// internal/config.Settings' own doc comment, which explicitly calls
	// these two values out as the defaults it matches), the docker backend
	// has never taken a Settings value for this, and there's no
	// deployment-observed need to change that now — pulled out as named
	// constants here only because newHardenedHostConfig below gives them a
	// second call site (CreateEnvironment) to share with Start.
	defaultSandboxNanoCPUs    = 2_000_000_000 // 2 cores
	defaultSandboxMemoryBytes = 4 << 30       // 4 GiB

	// defaultPidsLimit bounds how many processes/threads a single sandbox's
	// cgroup may fork — a cheap, fixed backstop against a runaway fork bomb
	// (see docs/ai-agent/environment-management.md's Hardening section).
	// Kept as a hardcoded constant, not a new env var, for the same reason
	// the two limits above are: the docker backend has no existing
	// per-limit config-knob pattern to extend (that pattern —
	// SandboxCPULimit/SandboxMemoryLimit — belongs to the kubernetes
	// backend only), and 512 is generous enough for any legitimate
	// build/dev/test workload that there's no deployment-observed need to
	// make it tunable yet.
	defaultPidsLimit = 512
)

// newHardenedHostConfig returns the container.HostConfig fields shared by
// every sandbox this Manager starts — ephemeral conversation (Start) or
// static environment (CreateEnvironment) alike — before the caller fills
// in whatever differs between the two (AutoRemove, PortBindings, Mounts,
// Binds). Root stays the container's default user either way (needed for
// mid-task `apt-get install` — see services/agent-server/Dockerfile's own
// justification, and docs/ai-agent/environment-management.md's Hardening
// section on why a blanket non-root flip isn't the fix here), but
// CapDrop/CapAdd narrows what a root process can actually do to a short
// allow-list (file-ownership and package-install operations), and
// PidsLimit bounds fork-bomb-style resource exhaustion. Each call returns
// a fresh *container.HostConfig — safe for the caller to mutate further
// without aliasing another call's struct.
func newHardenedHostConfig() *container.HostConfig {
	pidsLimit := int64(defaultPidsLimit)
	return &container.HostConfig{
		CapDrop: []string{"ALL"},
		CapAdd:  []string{"CHOWN", "SETUID", "SETGID", "DAC_OVERRIDE", "FOWNER"},
		Resources: container.Resources{
			NanoCPUs:  defaultSandboxNanoCPUs,
			Memory:    defaultSandboxMemoryBytes,
			PidsLimit: &pidsLimit,
		},
	}
}

// Start spins up a goose-serve container for one conversation and waits for
// it to become reachable. Does not stop the container on its own — see
// sandbox.Handle's doc comment.
func (m *Manager) Start(ctx context.Context, cfg sandbox.Config) (*sandbox.Handle, error) {
	secretKey, err := sandbox.RandomHex(32)
	if err != nil {
		return nil, fmt.Errorf("sandbox/docker: generate secret key: %w", err)
	}

	// Only started when this conversation's agent opted in (cfg.DockerEnabled
	// — see sandbox.Config's doc comment): most agents never run a Docker
	// command, so most conversations skip this container/network pair, and
	// the DOCKER_HOST env line below, entirely. sidecarOK flips true only
	// once the sandbox container this pairs with has actually started;
	// every return between here and there — this function's own early
	// errors and every cleanup() call below alike — leaves it false, so
	// this defer tears the sidecar back down instead of leaking a
	// privileged container on any failure path.
	//
	// startDindSidecar itself only creates the network and starts the
	// sidecar container — it does NOT wait for dockerd inside it to answer
	// `docker info` (that used to be folded into the same call, blocking
	// the sandbox container below from even starting until it finished).
	// That wait (waitForDindReady) instead runs concurrently, in the
	// dindReady goroutine below, alongside this function's own sandbox
	// container boot: the two are independent of each other (the sandbox
	// container only needs sidecar.networkID, available immediately after
	// NetworkCreate, to attach via NetworkConnect further down — not a live
	// dockerd), so there is nothing to gain by making one wait on the
	// other's full readiness before even starting. dindReady is joined
	// just before this function returns a Handle, so callers still never
	// see a Handle whose DOCKER_HOST isn't actually live yet.
	//
	// The goroutine runs against dindCtx, a child of ctx cancelled by the
	// deferred cancelDindWait() below on every return path — not the
	// happy-path join alone. Without that, a sandbox-container failure
	// after this goroutine starts (any cleanup() call further down) would
	// abandon it mid-poll: waitForDindReady would keep Exec'ing `docker
	// info` against a container that's about to be force-removed by this
	// function's own sidecarOK defer, for up to dindReadyTimeout (90s),
	// with nothing left reading its result. Cancelling here instead makes
	// it exit on its own next poll tick (or mid-Exec, since the Docker
	// client's calls respect ctx) as soon as this function is done with it.
	dindCtx, cancelDindWait := context.WithCancel(ctx)
	defer cancelDindWait()

	var sidecar *dindSidecar
	var dindReady chan error
	if cfg.DockerEnabled {
		var err error
		sidecar, err = m.startDindSidecar(ctx, cfg.ConversationID)
		if err != nil {
			return nil, fmt.Errorf("sandbox/docker: start dind sidecar: %w", err)
		}
		dindReady = make(chan error, 1)
		go func() {
			dindReady <- m.waitForDindReady(dindCtx, sidecar.containerID)
		}()
	}
	sidecarOK := false
	defer func() {
		if !sidecarOK && sidecar != nil {
			m.stopDindSidecar(context.Background(), sidecar)
		}
	}()

	env := make([]string, 0, len(cfg.Env)+8)
	// User-configured env vars first, so the hardcoded infra vars below
	// always win on a name collision — matches docker_workspace.py's
	// "hardcoded infra keys always win" ordering rationale.
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}
	env = append(env,
		"GOOSE_SERVER__SECRET_KEY="+secretKey,
		"GIT_AUTHOR_NAME="+cfg.GitCommitterName,
		"GIT_AUTHOR_EMAIL="+cfg.GitCommitterEmail,
		"GIT_COMMITTER_NAME="+cfg.GitCommitterName,
		"GIT_COMMITTER_EMAIL="+cfg.GitCommitterEmail,
	)
	if cfg.DockerEnabled {
		env = append(env, "DOCKER_HOST="+dindDockerHost(cfg.ConversationID))
	}

	if err := m.ensureImage(ctx, cfg.Image); err != nil {
		return nil, err
	}

	containerCfg := &container.Config{
		Image: cfg.Image,
		Cmd:   []string{"serve", "--host", "0.0.0.0", "--port", fmt.Sprintf("%d", sandbox.GooseServePort)},
		Env:   env,
		Labels: map[string]string{
			labelConvID:  cfg.ConversationID,
			labelManaged: "true",
		},
	}
	hostCfg := newHardenedHostConfig()
	hostCfg.AutoRemove = true
	if cfg.MCPDevSourceDir != "" {
		hostCfg.Binds = []string{cfg.MCPDevSourceDir + ":" + sandbox.MCPDevMountPath + ":ro"}
	}

	insideDocker := isInsideDocker()
	var netCfg *network.NetworkingConfig
	var hostPort int
	// Set only in the insideDocker branch below, and only read in the
	// matching insideDocker branch of the candidates block further down —
	// declared out here, not inside that first branch, specifically so
	// containerIP there can be told which of the sandbox's now-multiple
	// networks (see the NetworkConnect call below) this process's own
	// container can actually route to. Getting this wrong doesn't fail
	// loudly: Go's randomized map iteration order means containerIP would
	// pick the right network roughly half the time, so the other half
	// silently burns the full readyTimeout and reports a plausible-looking
	// "never got healthy" instead of what actually happened.
	var ownNetName string

	if insideDocker {
		var err error
		ownNetName, err = m.ownNetworkName(ctx)
		if err != nil {
			return nil, err
		}
		netCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				ownNetName: {},
			},
		}
	} else {
		hostPort, err = m.acquirePort()
		if err != nil {
			return nil, err
		}
		hostCfg.PortBindings = network.PortMap{
			containerPort: []network.PortBinding{{HostIP: localhostAddr, HostPort: fmt.Sprintf("%d", hostPort)}},
		}
	}

	created, err := m.docker.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           containerCfg,
		HostConfig:       hostCfg,
		NetworkingConfig: netCfg,
		// Platform left nil: ensureImage above already pulled the
		// host-architecture variant, so there's nothing left to select here.
	})
	if err != nil {
		if hostPort != 0 {
			m.releasePort(hostPort)
		}
		return nil, fmt.Errorf("sandbox/docker: create container: %w", err)
	}

	// cleanup uses its own short-lived context, never the caller's ctx —
	// every call site below invokes cleanup() precisely because ctx just
	// failed (expired or was cancelled), so removing the container "with
	// ctx" would immediately no-op against an already-Done context. Since
	// ContainerRemove's error is discarded (best-effort teardown of a
	// container we're abandoning anyway), that failure was previously
	// invisible: the container kept running — still holding its 2 CPU/4GiB
	// cgroup allowance — for the rest of the process's lifetime instead of
	// being force-removed, silently starving whatever sandbox this Manager
	// starts next.
	cleanup := func() {
		removeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = m.docker.ContainerRemove(removeCtx, created.ID, client.ContainerRemoveOptions{Force: true})
		if hostPort != 0 {
			m.releasePort(hostPort)
		}
	}

	// Second network, alongside whatever netCfg above already attached at
	// create time (ownNetworkName for api/gateway reachability, or nothing
	// — the implicit default bridge — in host-port-mapped mode): the one
	// this container's own dind sidecar is on and nothing else is (see
	// dind.go's package doc comment). Only attached when DockerEnabled —
	// with no sidecar, there's no second network to join. Attached by
	// NetworkConnect rather than a second NetworkingConfig.EndpointsConfig
	// entry at create time — confirmed directly (not assumed) that Docker's
	// own container-create API only actually attaches one network from that
	// map even when given several; a separate connect call is the verified
	// way to add more.
	//
	// containerIP below now has to be told which network to prefer
	// (ownNetName, insideDocker branch only) rather than picking whichever
	// of this container's now-multiple networks Go's randomized map
	// iteration happens to return first: an earlier version of this
	// comment claimed that didn't matter since goose serve binds
	// 0.0.0.0:3284 and answers on every interface the container has — true
	// as far as it goes, but irrelevant to whether the *caller* (this
	// process, running inside its own container when insideDocker) can
	// route to whichever address it got handed. This process is only ever
	// attached to ownNetName, never the private per-conversation network
	// docker creates — Docker does not route between distinct user-defined
	// bridge networks — so a wrong pick there isn't harmless, it's a
	// coin-flip-odds full readyTimeout followed by a failed Start.
	if sidecar != nil {
		if _, err := m.docker.NetworkConnect(ctx, sidecar.networkID, client.NetworkConnectOptions{Container: created.ID}); err != nil {
			cleanup()
			return nil, fmt.Errorf("sandbox/docker: attach to conversation network: %w", err)
		}
	}

	if _, err := m.docker.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		cleanup()
		return nil, fmt.Errorf("sandbox/docker: start container: %w", err)
	}

	var candidates []string
	if insideDocker {
		ip, err := m.containerIP(ctx, created.ID, ownNetName)
		if err != nil {
			cleanup()
			return nil, err
		}
		candidates = []string{fmt.Sprintf("http://%s:%d", ip, sandbox.GooseServePort)}
	} else {
		// Two candidate addresses, tried every poll tick, first one to
		// answer wins:
		//
		//  1. The container's own bridge-network IP, reachable directly
		//     with no NAT/port-publish involved — Docker still auto-attaches
		//     every container to the default bridge network even when this
		//     branch's hostCfg carries no NetworkingConfig, so containerIP
		//     (already used for the insideDocker branch above) works here
		//     too. On a native Linux Docker host — every CI runner, plus any
		//     dev machine that isn't running Docker Desktop — the host can
		//     route to that bridge subnet directly, same as any other local
		//     interface.
		//  2. The existing host-port-mapped "localhost:<hostPort>" address,
		//     kept as a fallback for Docker Desktop (macOS/Windows), whose
		//     split host/VM architecture makes the bridge IP unreachable
		//     from the host — published ports are the only thing that
		//     crosses that boundary there.
		//
		// Added after every Docker-backed e2e test started hanging until
		// its own context deadline on GitHub Actions' ubuntu-latest runners
		// waiting on the localhost:<hostPort> address specifically, with
		// the container itself reporting status=running the whole
		// time — i.e. candidate 2 alone, which is all this branch used to
		// try, was somehow unreachable on that runner even though the
		// container was healthy. Not reproducible on any local machine
		// tried (candidate 2 alone always worked), so the exact mechanism
		// on the runner side (iptables/nftables port-publish quirk, most
		// likely) was never directly confirmed — candidate 1 sidesteps
		// whatever it is rather than working around it blindly.
		candidates = []string{fmt.Sprintf("http://localhost:%d", hostPort)}
		// No preferred network here (unlike the insideDocker branch above):
		// this process isn't itself containerized in this branch, so it can
		// route to any of the sandbox's networks' bridge subnets directly —
		// which one containerIP picks doesn't affect reachability, only
		// which literal IP shows up in the candidate list. localhost above
		// remains a guaranteed-reachable fallback regardless either way.
		if ip, err := m.containerIP(ctx, created.ID, ""); err == nil {
			candidates = append([]string{fmt.Sprintf("http://%s:%d", ip, sandbox.GooseServePort)}, candidates...)
		}
	}

	baseURL, err := sandbox.WaitForReady(ctx, candidates, secretKey)
	if err != nil {
		// Captured before cleanup() removes the container — a container
		// that never answers /status could be crash-looping, OOM-killed, or
		// simply still starting under load, and "context deadline exceeded"
		// alone can't distinguish those. Uses its own context for the same
		// reason cleanup() does: ctx is already Done() here.
		diagCtx, diagCancel := context.WithTimeout(context.Background(), 10*time.Second)
		diag := m.diagnoseUnready(diagCtx, created.ID)
		diagCancel()
		cleanup()
		return nil, fmt.Errorf("%w (%s)", err, diag)
	}

	// Joined here, not left running past this function's return, so a
	// caller never gets back a Handle whose DOCKER_HOST isn't actually live
	// yet even though it was started concurrently with the sandbox
	// container above — see the dindReady goroutine's own comment for why
	// that concurrency is safe to begin with.
	if dindReady != nil {
		if err := <-dindReady; err != nil {
			cleanup()
			return nil, fmt.Errorf("sandbox/docker: dind sidecar not ready: %w", err)
		}
	}

	m.setState(created.ID, handleState{hostPort: hostPort, dind: sidecar})
	sidecarOK = true
	return &sandbox.Handle{
		ContainerID: created.ID,
		BaseURL:     baseURL,
		SecretKey:   secretKey,
	}, nil
}

// Stop tears down a sandbox previously returned by Start. Best-effort: a
// failure to stop is logged by the caller, not treated as fatal — the
// container is AutoRemove'd, so a failed explicit Stop still gets cleaned
// up by the daemon once it exits on its own (or is reaped by Docker after
// the process it's attached to goes away).
func (m *Manager) Stop(ctx context.Context, h *sandbox.Handle) error {
	state := m.popState(h.ContainerID)

	timeoutSecs := int(sandbox.StopTimeout.Seconds())
	_, err := m.docker.ContainerStop(ctx, h.ContainerID, client.ContainerStopOptions{Timeout: &timeoutSecs})
	if state.hostPort != 0 {
		m.releasePort(state.hostPort)
	}

	if state.dind != nil {
		teardownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		// Force-disconnect first, not left to AutoRemove's own timing: the
		// conversation network can't be removed while any container is
		// still attached to it, and there's no guarantee AutoRemove has
		// actually finished detaching the main container by the moment
		// ContainerStop above returns — an unremoved network would
		// otherwise silently leak, since stopDindSidecar's own
		// NetworkRemove call is best-effort (matching this whole method's
		// contract) and won't retry a failure or surface it.
		_, _ = m.docker.NetworkDisconnect(teardownCtx, state.dind.networkID, client.NetworkDisconnectOptions{
			Container: h.ContainerID,
			Force:     true,
		})
		m.stopDindSidecar(teardownCtx, state.dind)
		cancel()
	}

	if err != nil {
		return fmt.Errorf("sandbox/docker: stop container %s: %w", h.ContainerID, err)
	}
	return nil
}

// CopyToContainer uploads tarContent (a tar archive stream) into containerID
// at destPath — a thin, domain-agnostic wrapper around the Docker
// archive-copy API (PUT /containers/{id}/archive). Used by the executor
// package to place Goose skill files (SKILL.md, under destPath's
// .agents/skills/<name>/ subdirectory) on the sandbox's real filesystem
// before the ACP session starts, since Goose only discovers skills already
// present on disk under its session's own working directory — there is no
// ACP-level "load these skills" parameter, only cwd (see
// executor.buildInitialMessage's doc comment on session/new's params).
func (m *Manager) CopyToContainer(ctx context.Context, containerID, destPath string, tarContent io.Reader) error {
	if _, err := m.docker.CopyToContainer(ctx, containerID, client.CopyToContainerOptions{
		DestinationPath: destPath,
		Content:         tarContent,
	}); err != nil {
		return fmt.Errorf("sandbox/docker: copy to container %s: %w", containerID, err)
	}
	return nil
}

// Exec runs cmd inside containerID (as the container's own default user —
// "goose" on the pinned image, same as the process that owns whatever files
// cmd inspects) and returns its combined stdout+stderr and exit code. Used
// by the executor package to compute post-edit git diffs directly against
// the sandbox's real filesystem — see internal/executor/diff.go — since
// Goose's ACP-over-HTTP implementation has no fs/read_text_file-style
// callback into the client the way some other ACP agents do (confirmed
// against the agent-client-protocol project's own goosed-over-ACP tracking
// issue: the only server-initiated request type it implements is
// request_permission), so there is no protocol-level way to recover a
// diff — this reaches into the container directly instead.
func (m *Manager) Exec(ctx context.Context, containerID string, cmd []string) (output string, exitCode int, err error) {
	created, err := m.docker.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	})
	if err != nil {
		return "", 0, fmt.Errorf("sandbox/docker: exec create: %w", err)
	}

	attached, err := m.docker.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return "", 0, fmt.Errorf("sandbox/docker: exec attach: %w", err)
	}
	defer attached.Close()

	// Not a TTY (ExecAttachOptions.TTY left false above), so stdout/stderr
	// arrive multiplexed on the one stream — stdcopy demultiplexes both into
	// the same buffer since callers here only care about combined output,
	// not which stream a line came from.
	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, attached.Reader); err != nil {
		return "", 0, fmt.Errorf("sandbox/docker: exec read output: %w", err)
	}

	inspected, err := m.docker.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return buf.String(), 0, fmt.Errorf("sandbox/docker: exec inspect: %w", err)
	}
	return buf.String(), inspected.ExitCode, nil
}

// ensureImage confirms ref is present on this Docker host, pulling it if
// not — called on every sandbox Start, the hottest path in this service.
// The image ref is pinned for the process's lifetime (it comes from
// Executor's own startup Options, not anything per-conversation), so once
// ref has been confirmed present here — whether by finding it in
// ImageList or by a successful ImagePull — every later Start for the same
// ref skips both calls entirely instead of re-listing every image on the
// host each time.
func (m *Manager) ensureImage(ctx context.Context, ref string) error {
	if m.imageConfirmed(ref) {
		return nil
	}

	list, err := m.docker.ImageList(ctx, client.ImageListOptions{})
	if err == nil {
		for _, img := range list.Items {
			for _, tag := range img.RepoTags {
				if tag == ref {
					m.confirmImage(ref)
					return nil
				}
			}
			for _, digest := range img.RepoDigests {
				if digest == ref {
					m.confirmImage(ref)
					return nil
				}
			}
		}
	}

	arch := runtime.GOARCH
	rc, err := m.docker.ImagePull(ctx, ref, client.ImagePullOptions{
		Platforms: []ocispec.Platform{{OS: "linux", Architecture: arch}},
	})
	if err != nil {
		return fmt.Errorf("sandbox/docker: pull image %s: %w", ref, err)
	}
	defer func() { _ = rc.Close() }()
	_, _ = io.Copy(io.Discard, rc)
	m.confirmImage(ref)
	return nil
}

func (m *Manager) imageConfirmed(ref string) bool {
	m.imageMu.Lock()
	defer m.imageMu.Unlock()
	return m.confirmedImages[ref]
}

func (m *Manager) confirmImage(ref string) {
	m.imageMu.Lock()
	defer m.imageMu.Unlock()
	m.confirmedImages[ref] = true
}

// ownNetworkName returns the first Docker network this process's own
// container is attached to, so the spawned sandbox can join the same
// network and reach `api`/`gateway` by their Compose service hostnames —
// mirrors docker_workspace.py's _get_current_networks.
func (m *Manager) ownNetworkName(ctx context.Context) (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("sandbox/docker: get own hostname: %w", err)
	}
	resp, err := m.docker.ContainerInspect(ctx, hostname, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("sandbox/docker: inspect own container %s: %w", hostname, err)
	}
	if resp.Container.NetworkSettings == nil {
		return "bridge", nil
	}
	for name := range resp.Container.NetworkSettings.Networks {
		return name, nil
	}
	return "bridge", nil
}

// containerIP returns containerID's address on preferredNetwork if it's
// attached to one by that name, falling back to the first valid address
// found otherwise (preferredNetwork == "", or the container isn't actually
// on it). The fallback is fine when the caller can reach the container on
// any of its networks — e.g. the non-insideDocker branch below, which
// always has a host-port-mapped candidate as a backstop regardless of
// which address this returns — but is NOT fine for a caller that can only
// route to one specific network itself; that caller must pass its own
// network by name. See Start's insideDocker branch's own doc comment on
// why this container ended up multi-homed and what went wrong here before
// this parameter existed.
func (m *Manager) containerIP(ctx context.Context, containerID, preferredNetwork string) (string, error) {
	resp, err := m.docker.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("sandbox/docker: inspect container %s: %w", containerID, err)
	}
	if resp.Container.NetworkSettings == nil {
		return "", fmt.Errorf("sandbox/docker: container %s has no network settings", containerID)
	}
	ip, ok := selectContainerIP(resp.Container.NetworkSettings.Networks, preferredNetwork)
	if !ok {
		return "", fmt.Errorf("sandbox/docker: container %s has no assigned IP yet", containerID)
	}
	return ip, nil
}

// selectContainerIP is containerIP's own network-selection rule, pulled
// out as a pure function of a NetworkSettings.Networks map so the
// deterministic-preference behavior can be unit tested directly — no
// Docker daemon, no ContainerInspect call, no mocking required — instead
// of only being exercisable through a real container's live network
// state. See containerIP's doc comment for what preferredNetwork is for.
func selectContainerIP(networks map[string]*network.EndpointSettings, preferredNetwork string) (string, bool) {
	if preferredNetwork != "" {
		if ep, ok := networks[preferredNetwork]; ok && ep.IPAddress.IsValid() {
			return ep.IPAddress.String(), true
		}
	}
	for _, ep := range networks {
		if ep.IPAddress.IsValid() {
			return ep.IPAddress.String(), true
		}
	}
	return "", false
}

// diagnoseUnready summarizes a container's runtime state and recent output
// for the error path when it never answers /status — a state string alone
// ("running"/"exited"/"dead", exit code, OOM flag) plus its last few log
// lines is usually enough to tell a slow-starting container apart from one
// that's crash-looping (bad env, missing dependency inside the image) from
// one that started fine but never bound the port, none of which "context
// deadline exceeded" on its own can distinguish. Best-effort: inspect/logs
// failures are folded into the returned string rather than propagated,
// since this only ever augments an error the caller is already returning.
func (m *Manager) diagnoseUnready(ctx context.Context, containerID string) string {
	state := "inspect failed"
	if resp, err := m.docker.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{}); err == nil && resp.Container.State != nil {
		s := resp.Container.State
		state = fmt.Sprintf("status=%s exitCode=%d oomKilled=%v error=%q", s.Status, s.ExitCode, s.OOMKilled, s.Error)
	} else if err != nil {
		state = fmt.Sprintf("inspect failed: %v", err)
	}

	var logs string
	if rc, err := m.docker.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Tail: "40"}); err == nil {
		var buf bytes.Buffer
		_, _ = stdcopy.StdCopy(&buf, &buf, rc)
		_ = rc.Close()
		logs = buf.String()
		if logs == "" {
			logs = "(empty)"
		}
	} else {
		logs = fmt.Sprintf("fetch failed: %v", err)
	}

	return fmt.Sprintf("container %s: %s; last logs: %s", containerID, state, logs)
}

func (m *Manager) acquirePort() (int, error) {
	m.portMu.Lock()
	defer m.portMu.Unlock()
	for p := m.portPoolStart; p < m.portPoolStart+m.portPoolSize; p++ {
		if !m.portsInUse[p] {
			m.portsInUse[p] = true
			return p, nil
		}
	}
	return 0, errors.New("sandbox/docker: no ports available in the agent port pool")
}

func (m *Manager) releasePort(p int) {
	m.portMu.Lock()
	defer m.portMu.Unlock()
	delete(m.portsInUse, p)
}

func (m *Manager) setState(containerID string, s handleState) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	m.state[containerID] = s
}

// popState returns and forgets containerID's bookkeeping — called exactly
// once, from Stop, so a stopped container's entry doesn't linger in this
// map forever.
func (m *Manager) popState(containerID string) handleState {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	s := m.state[containerID]
	delete(m.state, containerID)
	return s
}
