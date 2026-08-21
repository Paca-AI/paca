// Package sandbox defines the shared contract every sandbox backend
// implements: one disposable workload per active conversation, running
// `goose serve` over ACP. This package itself holds no backend logic —
// see internal/sandbox/docker (a Docker container, reached via a mounted
// /var/run/docker.sock) and internal/sandbox/k8s (a Kubernetes Job) for
// the two implementations, and Backend's own doc comment for how
// executor.Executor drives either one interchangeably.
package sandbox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	readyTimeout = 120 * time.Second
	// readyPollEvery was 1 full second — meaning up to ~1s of pure dead
	// waiting on every single cold start past the moment goose serve
	// actually became ready, for no benefit (the poll itself is a cheap,
	// local HTTP GET against a sandbox this process already owns, not
	// something worth rate-limiting). 100ms caps that worst case at a
	// tenth of the cost, at the price of a handful of extra fast local
	// requests while waiting — negligible next to the seconds a cold start
	// otherwise takes.
	readyPollEvery = 100 * time.Millisecond

	// StopTimeout is how long a sandbox's own stop path waits after
	// SIGTERM before force-killing (SIGKILL) it. Confirmed directly, not
	// guessed: a plain `docker stop` against a real goose serve container
	// (no custom grace period, so Docker's own 10s CLI default) took the
	// full 10s every time — goose serve does not appear to handle SIGTERM
	// for a fast graceful exit. An earlier version of this constant was
	// 30s, chosen without that data; that made HandleControl's stop path
	// (see internal/handler) take ~30s end to end in practice, since
	// Handle doesn't return until this deferred call does. There's
	// nothing worth preserving inside the sandbox by that point anyway —
	// the in-flight ACP turn has already been aborted via context
	// cancellation before Stop is ever called — so a short grace period
	// costs nothing and a long one only delays a user-facing "stop".
	//
	// Shared so internal/sandbox/k8s can apply the same grace period to a
	// sandbox Pod's TerminationGracePeriodSeconds — same underlying
	// process (goose serve), same reasoning applies unchanged.
	StopTimeout = 3 * time.Second

	// GooseServePort is the port `goose serve` listens on inside every
	// sandbox, Docker or Kubernetes alike.
	GooseServePort = 3284
)

// DindImage is the pinned docker:dind sidecar image both backends' own
// DockerEnabled support runs — see internal/sandbox/docker/dind.go and
// internal/sandbox/k8s/dind.go. Pinned by digest for the same
// full-immutability reason services/agent-server/Dockerfile pins the goose
// base image (see that file's doc comment) — a tag can be force-moved
// upstream, a digest can't. Resolved directly from docker:27-dind at the
// time this was written (`docker pull docker:27-dind && docker inspect
// ... {{index .RepoDigests 0}}`); rotate the same way, not by editing this
// by hand.
const DindImage = "docker:27-dind@sha256:aa3df78ecf320f5fafdce71c659f1629e96e9de0968305fe1de670e0ca9176ce"

// MCPDevMountPath is where Config.MCPDevSourceDir (when set) is bind-mounted
// inside the sandbox container by internal/sandbox/docker — a Docker
// Compose local-dev convention specifically (internal/sandbox/k8s rejects
// MCPDevSourceDir outright; see that package's Start). Kept here rather
// than in the docker package so executor.go's buildMCPServers, which is
// otherwise backend-agnostic, doesn't need to import a specific backend
// package just for this one path string. executor.buildMCPServers points
// the Paca MCP server's stdio command at "<MCPDevMountPath>/build/index.js"
// via the container's own /usr/bin/node when this override is active,
// instead of the image's globally npm-installed @paca-ai/paca-mcp — see
// Config's doc comment.
const MCPDevMountPath = "/opt/paca-mcp-dev"

// Config describes one conversation's sandbox. Env is the full set of
// environment variables to inject beyond the ones a backend always sets
// itself (GOOSE_SERVER__SECRET_KEY) — the caller (executor) is responsible
// for deciding GOOSE_PROVIDER/GOOSE_MODEL/the provider's own API key env
// var/PACA_* MCP vars, since that mapping is agent-config logic, not
// sandbox-lifecycle logic.
type Config struct {
	ConversationID string
	// TurnID is populated only for authoritative project-chat runs. Backends
	// persist it as resource metadata so the lease-aware orphan reaper can
	// distinguish these workloads from legacy conversation sandboxes.
	TurnID            string
	Image             string
	Env               map[string]string
	GitCommitterName  string
	GitCommitterEmail string

	// MCPDevSourceDir, when non-empty, is bind-mounted read-only into the
	// sandbox at MCPDevMountPath — internal/sandbox/docker only; see that
	// constant's own doc comment. Dev-only — see config.Settings.MCPDevSourceDir.
	MCPDevSourceDir string

	// DockerEnabled opts this conversation into a per-conversation
	// Docker-in-Docker sidecar (see internal/sandbox/docker/dind.go and
	// internal/sandbox/k8s/dind.go) — off by default (agent.Config.
	// DockerEnabled, sourced from agents.docker_enabled, defaults to false).
	// Most agents never run a Docker command, and the sidecar is a real
	// per-session cost (a privileged container, plus — for the docker
	// backend — a private network) to pay on every cold start regardless
	// — this flag lets an agent that actually needs Docker opt in,
	// without taxing every other agent.
	DockerEnabled bool
}

// Backend runs one conversation's sandbox somewhere — internal/sandbox/docker
// (a Docker container) or internal/sandbox/k8s (a Kubernetes Job) — so
// executor.Executor can drive either without knowing which; main.go picks
// the concrete implementation to wire up from config.Settings.SandboxBackend.
//
// ContainerID in Start/Stop/CopyToContainer/Exec's signatures is a Handle's
// own identifier, opaque to callers — for the docker backend that's a real
// Docker container ID; for the kubernetes backend it's the Job name (see
// that package's own doc comment on why a Job name, not a Pod name, is
// what Handle.ContainerID holds there).
type Backend interface {
	// Start spins up a fresh sandbox for cfg.ConversationID and waits for
	// it to become reachable. Does not stop the sandbox on its own — see
	// Handle's doc comment.
	Start(ctx context.Context, cfg Config) (*Handle, error)
	// Stop tears down a sandbox previously returned by Start. Best-effort.
	Stop(ctx context.Context, h *Handle) error
	// CopyToContainer uploads tarContent into containerID at destPath.
	CopyToContainer(ctx context.Context, containerID, destPath string, tarContent io.Reader) error
	// Exec runs cmd inside containerID and returns its combined
	// stdout+stderr and exit code.
	Exec(ctx context.Context, containerID string, cmd []string) (output string, exitCode int, err error)
}

// Handle is what Start returns and Stop consumes for one conversation's
// sandbox — mirrors SandboxHandle in docker_workspace.py, including the
// same "does not stop on its own" contract: callers that want scoped
// auto-teardown wrap Start/Stop themselves (see executor.Run).
//
// Deliberately a plain, backend-opaque value: every Backend implementation
// returns/consumes the exact same shape, and this package has no
// knowledge of what either backend needs to remember about one internally
// between Start and Stop — the docker package keeps its own host-port and
// dind-sidecar bookkeeping in a map on its Manager, keyed by ContainerID,
// rather than smuggling it onto this struct (which would mean only that
// one backend's package could construct a Handle at all, since Go doesn't
// allow setting unexported fields from another package — a real
// constraint that only became visible once the docker backend moved out
// of this package into its own). The k8s package similarly keeps nothing
// on Handle, resolving its current Pod name fresh from the Job name in
// ContainerID on every call instead of caching it here.
type Handle struct {
	ContainerID string
	// BaseURL is the sandbox's goose serve endpoint, already health-checked
	// and ready — e.g. "http://172.18.0.5:3284" (docker, same-network
	// mode), "http://localhost:32941" (docker, local-dev host-port mode),
	// or "http://10.0.4.12:3284" (kubernetes, Pod IP).
	BaseURL   string
	SecretKey string
}

// WaitForReady polls every candidate base URL's /status endpoint each tick
// and returns the first one to answer with a non-5xx response. Trying
// every candidate on every tick (rather than exhausting one before moving
// to the next) means a candidate that's simply unreachable (instant
// connection-refused/no-route) costs at most one fast failed dial per
// tick, not a share of the overall timeout budget.
//
// Shared between backends — internal/sandbox/docker and
// internal/sandbox/k8s both drive the exact same readiness polling against
// their own candidate address(es) instead of each hand-rolling a copy of
// this logic — the goose serve /status contract and secret-key auth are
// identical between them, only how a candidate address is obtained
// differs.
func WaitForReady(ctx context.Context, candidates []string, secretKey string) (string, error) {
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
