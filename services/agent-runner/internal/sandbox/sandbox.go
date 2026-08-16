// Package sandbox manages the lifecycle of one Docker container per active
// conversation, each running `goose serve`. Go analog of
// services/ai-agent/src/agent/docker_workspace.py, adapted for Goose:
//   - runs `ghcr.io/block/goose` instead of the OpenHands agent-server image
//   - no repo_tools injection (that mechanism is being replaced by exposing
//     list_repositories/clone_repository as more Paca MCP server tools)
//   - health-checks goose serve's /status endpoint instead of OpenHands'
//     /health
package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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
)

const (
	labelConvID  = "paca.conversation_id"
	labelManaged = "paca.managed"
	readyTimeout = 120 * time.Second
	// readyPollEvery was 1 full second — meaning up to ~1s of pure dead
	// waiting on every single cold start past the moment goose serve
	// actually became ready, for no benefit (the poll itself is a cheap,
	// local HTTP GET against a container this process already owns, not
	// something worth rate-limiting). 100ms caps that worst case at a
	// tenth of the cost, at the price of a handful of extra fast local
	// requests while waiting — negligible next to the seconds a cold start
	// otherwise takes.
	readyPollEvery = 100 * time.Millisecond
	// stopTimeout is how long ContainerStop waits after SIGTERM before
	// force-killing (SIGKILL) the sandbox. Confirmed directly, not
	// guessed: a plain `docker stop` against a real goose serve container
	// (no custom grace period, so Docker's own 10s CLI default) took the
	// full 10s every time — goose serve does not appear to handle SIGTERM
	// for a fast graceful exit. An earlier version of this constant was
	// 30s, chosen without that data; that made HandleControl's stop path
	// (see internal/handler) take ~30s end to end in practice, since
	// Handle doesn't return until this deferred call does. There's
	// nothing worth preserving inside the container by that point anyway
	// — the in-flight ACP turn has already been aborted via context
	// cancellation before Stop is ever called — so a short grace period
	// costs nothing and a long one only delays a user-facing "stop".
	stopTimeout = 3 * time.Second
)

// MCPDevMountPath is where Config.MCPDevSourceDir (when set) is bind-mounted
// inside the sandbox container. executor.buildMCPServers points the Paca MCP
// server's stdio command at "<MCPDevMountPath>/build/index.js" via the
// container's own /usr/bin/node when this override is active, instead of the
// image's globally npm-installed @paca-ai/paca-mcp — see Config's doc
// comment.
const MCPDevMountPath = "/opt/paca-mcp-dev"

// containerPort is the port `goose serve` listens on inside every sandbox
// container — a var, not a const, since network.Port is a struct type
// (MustParsePort can't be evaluated at compile time).
var containerPort = network.MustParsePort("3284/tcp")

var localhostAddr = netip.MustParseAddr("127.0.0.1")

// Config describes one conversation's sandbox. Env is the full set of
// environment variables to inject beyond the ones this package always sets
// itself (GOOSE_SERVER__SECRET_KEY) — the caller (executor) is responsible
// for deciding GOOSE_PROVIDER/GOOSE_MODEL/the provider's own API key env
// var/PACA_* MCP vars, since that mapping is agent-config logic, not
// container-lifecycle logic.
type Config struct {
	ConversationID    string
	Image             string
	Env               map[string]string
	GitCommitterName  string
	GitCommitterEmail string

	// MCPDevSourceDir, when non-empty, is bind-mounted read-only into the
	// container at MCPDevMountPath. Must be a path on the Docker daemon
	// host's own filesystem, not a path inside this process's own
	// container: this process reaches the daemon over a mounted
	// /var/run/docker.sock (sibling-container/DooD, not Docker-in-Docker),
	// so a bind mount's Source is always resolved by the daemon against its
	// own host filesystem, regardless of what this process itself can see
	// at that path. Dev-only — see config.Settings.MCPDevSourceDir.
	MCPDevSourceDir string
}

// Handle is a live sandbox container. Returned by Manager.Start and
// consumed by Manager.Stop — mirrors SandboxHandle in docker_workspace.py,
// including the same "does not stop on its own" contract: callers that want
// scoped auto-teardown wrap Start/Stop themselves (see executor.Run).
type Handle struct {
	ContainerID string
	// BaseURL is the container's goose serve endpoint, already health-
	// checked and ready — e.g. "http://172.18.0.5:3284" (same-Docker-
	// network mode) or "http://localhost:32941" (local-dev host-port mode).
	BaseURL   string
	SecretKey string

	hostPort int // 0 unless this handle used host-port-mapping mode
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

	// confirmedImages caches which image refs ensureImage has already
	// confirmed present on this Docker host (via ImageList or a successful
	// ImagePull) — see ensureImage's doc comment.
	imageMu         sync.Mutex
	confirmedImages map[string]bool
}

// NewManager builds a Manager from the standard Docker environment
// variables (DOCKER_HOST, etc. — see client.FromEnv).
func NewManager(portPoolStart, portPoolSize int) (*Manager, error) {
	// WithAPIVersionNegotiation dropped — the client library's own
	// deprecation notice says API-version negotiation is enabled by
	// default now, making the option a no-op.
	docker, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("sandbox: create docker client: %w", err)
	}
	return &Manager{
		docker:          docker,
		portPoolStart:   portPoolStart,
		portPoolSize:    portPoolSize,
		portsInUse:      make(map[int]bool),
		confirmedImages: make(map[string]bool),
	}, nil
}

func isInsideDocker() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

// Start spins up a goose-serve container for one conversation and waits for
// it to become reachable. Does not stop the container on its own — see
// Handle's doc comment.
func (m *Manager) Start(ctx context.Context, cfg Config) (*Handle, error) {
	secretKey, err := randomHex(32)
	if err != nil {
		return nil, fmt.Errorf("sandbox: generate secret key: %w", err)
	}

	env := make([]string, 0, len(cfg.Env)+7)
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

	if err := m.ensureImage(ctx, cfg.Image); err != nil {
		return nil, err
	}

	containerCfg := &container.Config{
		Image: cfg.Image,
		Cmd:   []string{"serve", "--host", "0.0.0.0", "--port", "3284"},
		Env:   env,
		Labels: map[string]string{
			labelConvID:  cfg.ConversationID,
			labelManaged: "true",
		},
	}
	hostCfg := &container.HostConfig{
		AutoRemove: true,
		Resources: container.Resources{
			NanoCPUs: 2_000_000_000, // 2 cores
			Memory:   4 << 30,       // 4 GiB
		},
	}
	if cfg.MCPDevSourceDir != "" {
		hostCfg.Binds = []string{cfg.MCPDevSourceDir + ":" + MCPDevMountPath + ":ro"}
	}

	insideDocker := isInsideDocker()
	var netCfg *network.NetworkingConfig
	var hostPort int

	if insideDocker {
		netName, err := m.ownNetworkName(ctx)
		if err != nil {
			return nil, err
		}
		netCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				netName: {},
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
		return nil, fmt.Errorf("sandbox: create container: %w", err)
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

	if _, err := m.docker.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		cleanup()
		return nil, fmt.Errorf("sandbox: start container: %w", err)
	}

	var candidates []string
	if insideDocker {
		ip, err := m.containerIP(ctx, created.ID)
		if err != nil {
			cleanup()
			return nil, err
		}
		candidates = []string{fmt.Sprintf("http://%s:3284", ip)}
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
		if ip, err := m.containerIP(ctx, created.ID); err == nil {
			candidates = append([]string{fmt.Sprintf("http://%s:3284", ip)}, candidates...)
		}
	}

	baseURL, err := waitForReady(ctx, candidates, secretKey)
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

	return &Handle{
		ContainerID: created.ID,
		BaseURL:     baseURL,
		SecretKey:   secretKey,
		hostPort:    hostPort,
	}, nil
}

// Stop tears down a sandbox previously returned by Start. Best-effort: a
// failure to stop is logged by the caller, not treated as fatal — the
// container is AutoRemove'd, so a failed explicit Stop still gets cleaned
// up by the daemon once it exits on its own (or is reaped by Docker after
// the process it's attached to goes away).
func (m *Manager) Stop(ctx context.Context, h *Handle) error {
	timeoutSecs := int(stopTimeout.Seconds())
	_, err := m.docker.ContainerStop(ctx, h.ContainerID, client.ContainerStopOptions{Timeout: &timeoutSecs})
	if h.hostPort != 0 {
		m.releasePort(h.hostPort)
	}
	if err != nil {
		return fmt.Errorf("sandbox: stop container %s: %w", h.ContainerID, err)
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
		return "", 0, fmt.Errorf("sandbox: exec create: %w", err)
	}

	attached, err := m.docker.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return "", 0, fmt.Errorf("sandbox: exec attach: %w", err)
	}
	defer attached.Close()

	// Not a TTY (ExecAttachOptions.TTY left false above), so stdout/stderr
	// arrive multiplexed on the one stream — stdcopy demultiplexes both into
	// the same buffer since callers here only care about combined output,
	// not which stream a line came from.
	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, attached.Reader); err != nil {
		return "", 0, fmt.Errorf("sandbox: exec read output: %w", err)
	}

	inspected, err := m.docker.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return buf.String(), 0, fmt.Errorf("sandbox: exec inspect: %w", err)
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
		return fmt.Errorf("sandbox: pull image %s: %w", ref, err)
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
		return "", fmt.Errorf("sandbox: get own hostname: %w", err)
	}
	resp, err := m.docker.ContainerInspect(ctx, hostname, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("sandbox: inspect own container %s: %w", hostname, err)
	}
	if resp.Container.NetworkSettings == nil {
		return "bridge", nil
	}
	for name := range resp.Container.NetworkSettings.Networks {
		return name, nil
	}
	return "bridge", nil
}

func (m *Manager) containerIP(ctx context.Context, containerID string) (string, error) {
	resp, err := m.docker.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("sandbox: inspect container %s: %w", containerID, err)
	}
	if resp.Container.NetworkSettings == nil {
		return "", fmt.Errorf("sandbox: container %s has no network settings", containerID)
	}
	for _, ep := range resp.Container.NetworkSettings.Networks {
		if ep.IPAddress.IsValid() {
			return ep.IPAddress.String(), nil
		}
	}
	return "", fmt.Errorf("sandbox: container %s has no assigned IP yet", containerID)
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

// waitForReady polls every candidate base URL's /status endpoint each tick
// and returns the first one to answer with a non-5xx response — see the
// call site's comment on why Start ever passes more than one candidate.
// Trying every candidate on every tick (rather than exhausting one before
// moving to the next) means a candidate that's simply unreachable (instant
// connection-refused/no-route, the expected shape when e.g. the bridge-IP
// candidate doesn't apply on this host) costs at most one fast failed
// dial per tick, not a share of the overall timeout budget.
func waitForReady(ctx context.Context, candidates []string, secretKey string) (string, error) {
	deadline := time.Now().Add(readyTimeout)
	httpClient := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		for _, baseURL := range candidates {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/status", nil)
			req.Header.Set("X-Secret-Key", secretKey)
			resp, err := httpClient.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode < 500 {
					return baseURL, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(readyPollEvery):
		}
	}
	return "", fmt.Errorf("sandbox: none of %v/status became ready after %s", candidates, readyTimeout)
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
	return 0, errors.New("sandbox: no ports available in the agent port pool")
}

func (m *Manager) releasePort(p int) {
	m.portMu.Lock()
	defer m.portMu.Unlock()
	delete(m.portsInUse, p)
}
