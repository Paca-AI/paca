// Package sandbox manages the lifecycle of one Docker container per active
// conversation, each running `goose serve`. Go analog of
// services/ai-agent/src/agent/docker_workspace.py, adapted for Goose:
//   - runs `ghcr.io/block/goose` instead of the OpenHands agent-server image
//   - no repo_tools injection (that mechanism is being replaced by exposing
//     list_repositories/clone_repository as more Paca MCP server tools —
//     see docs/ai-agent/goose-migration.md's component mapping table)
//   - health-checks goose serve's /status endpoint instead of OpenHands'
//     /health
package sandbox

import (
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
	// otherwise takes. See docs/ai-agent/goose-migration.md.
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
		docker:        docker,
		portPoolStart: portPoolStart,
		portPoolSize:  portPoolSize,
		portsInUse:    make(map[int]bool),
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

	cleanup := func() {
		_, _ = m.docker.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
		if hostPort != 0 {
			m.releasePort(hostPort)
		}
	}

	if _, err := m.docker.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		cleanup()
		return nil, fmt.Errorf("sandbox: start container: %w", err)
	}

	var baseURL string
	if insideDocker {
		ip, err := m.containerIP(ctx, created.ID)
		if err != nil {
			cleanup()
			return nil, err
		}
		baseURL = fmt.Sprintf("http://%s:3284", ip)
	} else {
		baseURL = fmt.Sprintf("http://localhost:%d", hostPort)
	}

	if err := waitForReady(ctx, baseURL, secretKey); err != nil {
		cleanup()
		return nil, err
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

func (m *Manager) ensureImage(ctx context.Context, ref string) error {
	list, err := m.docker.ImageList(ctx, client.ImageListOptions{})
	if err == nil {
		for _, img := range list.Items {
			for _, tag := range img.RepoTags {
				if tag == ref {
					return nil
				}
			}
			for _, digest := range img.RepoDigests {
				if digest == ref {
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
	return nil
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

func waitForReady(ctx context.Context, baseURL, secretKey string) error {
	deadline := time.Now().Add(readyTimeout)
	httpClient := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/status", nil)
		req.Header.Set("X-Secret-Key", secretKey)
		resp, err := httpClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readyPollEvery):
		}
	}
	return fmt.Errorf("sandbox: %s/status not ready after %s", baseURL, readyTimeout)
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
