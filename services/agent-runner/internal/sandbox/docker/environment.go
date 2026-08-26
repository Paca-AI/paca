// Environment support for the docker sandbox backend — implements
// sandbox.EnvironmentBackend on the same *Manager that already implements
// sandbox.Backend in manager.go, satisfying sandbox.FullBackend (see the
// var _ sandbox.FullBackend assertion in manager.go). See
// docs/ai-agent/environment-management.md's Docker section for the design
// this mirrors, and internal/sandbox/environment.go for the interface
// contract every method below implements.
package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

const (
	// labelEnvID parallels labelConvID (manager.go) — set on every static
	// environment's container instead of labelConvID, since an
	// environment's container isn't scoped to any single conversation; see
	// EnvironmentConfig's own doc comment on why one environment can be
	// attached to by many conversations over its life.
	labelEnvID = "paca.environment_id"

	// environmentVolumeMountPath is the fixed root a static environment's
	// named volume is mounted at inside its container. Deliberately NOT
	// sandboxWorkdir ("/home/goose", internal/executor/executor.go) — the
	// ephemeral per-conversation sandbox's cwd — so the two concepts never
	// collide even though they happen to share the same underlying image:
	// an environment container and an ephemeral conversation container are
	// always distinct containers, but keeping their working-directory
	// conventions disjoint means no code path can accidentally treat one
	// kind's fixed path as if it were the other's.
	environmentVolumeMountPath = "/home/paca/workspaces"
)

// publicHostAddr is the HostIP every user-facing port mapping
// (portBindingsFor below) binds to — unlike the internal ACP/goose-serve
// control port (bound to localhostAddr, manager.go), SSH and port-forward
// bindings must be reachable from outside the Docker host, not just from
// agent-runner's own local-dev process.
var publicHostAddr = netip.MustParseAddr("0.0.0.0")

// portBindingsFor builds the network.PortMap for cfg.PortMappings — shared
// by CreateEnvironment and RestartEnvironmentPorts, the only two places a
// container's port bindings are ever decided (see
// sandbox.EnvironmentConfig.PortMappings's own doc comment for why a plain
// StartEnvironment never touches this).
func portBindingsFor(mappings []sandbox.PortMapping) network.PortMap {
	if len(mappings) == 0 {
		return nil
	}
	bindings := make(network.PortMap, len(mappings))
	for _, m := range mappings {
		port := network.MustParsePort(fmt.Sprintf("%d/tcp", m.ContainerPort))
		bindings[port] = []network.PortBinding{{HostIP: publicHostAddr, HostPort: strconv.Itoa(m.HostPort)}}
	}
	return bindings
}

// environmentVolumeName is the Docker volume name backing environmentID's
// persistent storage — paca-env-<environmentID>, created once by
// CreateEnvironment and removed once, by DeleteEnvironment. Also used
// verbatim as the container's own name (environmentContainerName) — see
// that function's doc comment for why sharing the string is deliberate,
// not incidental.
func environmentVolumeName(environmentID string) string {
	return "paca-env-" + environmentID
}

// environmentContainerName names environmentID's container — deliberately
// the exact same string environmentVolumeName produces. Docker keeps
// container names and volume names in separate namespaces, so there's no
// collision risk, and giving both the same deterministic name buys two
// things: `docker ps`/`docker volume ls` visibly pair the two resources
// for the same environment, and — the part recreateGoneEnvironmentContainer
// below relies on — a removed container's name is freed back to Docker
// immediately, so recreating one under this same name always succeeds
// rather than racing whatever randomly-assigned name the old one had.
func environmentContainerName(environmentID string) string {
	return environmentVolumeName(environmentID)
}

// resolveEnvironmentImage returns the image reference CreateEnvironment
// should run: cfgImage if non-empty, otherwise managerDefault (this
// Manager's own AgentServerImage) — the "empty means use this backend's
// own config.AgentServerImage" contract EnvironmentConfig.Image's doc
// comment (internal/sandbox/environment.go) describes. An error only when
// neither is set, which means the integration layer never wired
// Manager.AgentServerImage up (see that field's own doc comment) — a
// deployment misconfiguration worth failing loudly on rather than passing
// an empty ref through to ensureImage/ContainerCreate for a confusing
// downstream failure. Pulled out as a pure function, the same way
// selectContainerIP was pulled out of containerIP, so this decision can be
// unit tested without a Docker daemon.
func resolveEnvironmentImage(cfgImage, managerDefault string) (string, error) {
	if cfgImage != "" {
		return cfgImage, nil
	}
	if managerDefault != "" {
		return managerDefault, nil
	}
	return "", errors.New("sandbox/docker: cfg.Image is empty and Manager.AgentServerImage is not configured — see Manager.AgentServerImage's doc comment")
}

// createAndStartEnvironmentContainer builds, starts, and waits for ready a
// static environment's container mounted on volumeName with cfg.PortMappings
// baked in as its published bindings — the body shared by CreateEnvironment
// (a freshly-created volume) and RestartEnvironmentPorts (an existing one,
// reattached unchanged). On any error, the new container (if one was
// created) is best-effort removed before returning; the caller is
// responsible for anything beyond that (CreateEnvironment additionally
// removes the volume it just created — see that method's own doc comment
// on why RestartEnvironmentPorts must not do the same to an existing one).
func (m *Manager) createAndStartEnvironmentContainer(ctx context.Context, image, volumeName string, cfg sandbox.EnvironmentConfig) (containerID, baseURL string, err error) {
	env := make([]string, 0, len(cfg.Env)+1)
	// User-configured env vars first, so GOOSE_SERVER__SECRET_KEY below
	// always wins on a name collision — same ordering rationale as Start.
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}
	env = append(env, "GOOSE_SERVER__SECRET_KEY="+cfg.SecretKey)
	if cfg.DockerEnabled {
		env = append(env, "DOCKER_HOST="+environmentDindDockerHost(cfg.EnvironmentID))
	}

	containerCfg := &container.Config{
		Image: image,
		Cmd:   []string{"serve", "--host", "0.0.0.0", "--port", fmt.Sprintf("%d", sandbox.GooseServePort)},
		Env:   env,
		Labels: map[string]string{
			labelEnvID:   cfg.EnvironmentID,
			labelManaged: "true",
		},
	}

	hostCfg := newHardenedHostConfig()
	// Environments persist explicitly across StopEnvironment/
	// StartEnvironment cycles — unlike Start's disposable-per-conversation
	// container, this one must not vanish out from under a stopped
	// environment.
	hostCfg.AutoRemove = false
	// newHardenedHostConfig's Resources are the ephemeral per-conversation
	// sandbox's own fixed defaults (2 cores / 4GiB) — every environment
	// container was silently getting exactly that, regardless of its own
	// configured cfg.CPULimit/MemoryLimit, until this override. Parsed with
	// the same resource.ParseQuantity the kubernetes backend's own
	// environmentResources (k8s/environment.go) already uses for the exact
	// same two fields, so "2"/"4Gi" mean the same thing on both backends.
	// Left alone (falls through to the hardcoded defaults above) only if
	// empty, which environment_service.go's own CreateEnvironment never
	// actually sends — defensive, not the expected path.
	if cfg.CPULimit != "" {
		q, err := resource.ParseQuantity(cfg.CPULimit)
		if err != nil {
			return "", "", fmt.Errorf("sandbox/docker: parse environment CPU limit %q: %w", cfg.CPULimit, err)
		}
		hostCfg.NanoCPUs = q.MilliValue() * 1_000_000
	}
	if cfg.MemoryLimit != "" {
		q, err := resource.ParseQuantity(cfg.MemoryLimit)
		if err != nil {
			return "", "", fmt.Errorf("sandbox/docker: parse environment memory limit %q: %w", cfg.MemoryLimit, err)
		}
		hostCfg.Memory = q.Value()
	}
	// SYS_CHROOT, on top of newHardenedHostConfig's own narrow list: only an
	// environment's container ever runs real sshd (see
	// sandbox.BootstrapEnvironmentSSH) — an ephemeral per-conversation
	// sandbox (Start, below) never does, so this is added here rather than
	// broadening newHardenedHostConfig's own shared CapAdd for every
	// sandbox. sshd's privilege-separated pre-auth child always chroot()s
	// into the Debian-patched privsep directory (/run/sshd, not upstream's
	// /var/empty) as an unconditional step of setting up its restricted
	// child, independent of which further sandboxing backend (seccomp,
	// rlimit, ...) it also layers on top — confirmed live, not guessed:
	// without this, every single incoming connection's pre-auth child hit
	// "chroot(\"/run/sshd\"): Operation not permitted [preauth]" and the
	// connection closed immediately after key exchange, before
	// authentication even began (so this was never about the user's own
	// key at all).
	// AUDIT_WRITE, same reasoning and scoping as SYS_CHROOT just above — a
	// second, independent capability gap in the exact same "environment
	// containers run real sshd" area, confirmed live: with SYS_CHROOT alone,
	// key exchange and publickey auth both succeeded, but the moment a real
	// interactive (PTY) session's post-auth child tried its own login/
	// session accounting, it printed "linux_audit_write_entry failed:
	// Operation not permitted" straight to the session's stderr (which is
	// why it showed up in the user's own terminal, not agent-runner's logs)
	// and the session was torn down before the shell ever ran — the exact
	// "Connection ... closed" a user sees with no further explanation.
	// UsePAM no (paca-environment.conf) rules out a PAM audit hook; this is
	// glibc/OpenSSH's own audit-on-login-accounting path, gated purely on
	// this one capability.
	hostCfg.CapAdd = append(hostCfg.CapAdd, "SYS_CHROOT", "AUDIT_WRITE")
	hostCfg.Mounts = []mount.Mount{
		{
			Type:   mount.TypeVolume,
			Source: volumeName,
			Target: environmentVolumeMountPath,
		},
	}
	// Dev-only, mirrors Start's own cfg.MCPDevSourceDir handling
	// (manager.go) — see sandbox.EnvironmentConfig.MCPDevSourceDir's doc
	// comment for why this needs its own copy here rather than just
	// reusing that one: an environment container's HostConfig is built
	// with the typed Mounts API (above), not Start's legacy Binds string,
	// and this function is the single shared creation path for every fresh
	// environment container (CreateEnvironment, recreateEnvironmentContainer,
	// recreateGoneEnvironmentContainer), so adding it here covers all three
	// without duplicating this block.
	if cfg.MCPDevSourceDir != "" {
		hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   cfg.MCPDevSourceDir,
			Target:   sandbox.MCPDevMountPath,
			ReadOnly: true,
		})
	}
	// User-facing bindings (SSH + port forwards) — independent of, and
	// merged with rather than replaced by, the insideDocker-gated
	// internal-control-port binding below.
	hostCfg.PortBindings = portBindingsFor(cfg.PortMappings)

	insideDocker := isInsideDocker()
	var netCfg *network.NetworkingConfig
	var hostPort int
	// See Start's own ownNetName doc comment for why this has to be
	// determined once here and threaded through to waitForEnvironmentReady
	// below rather than re-derived per candidate.
	var ownNetName string
	if insideDocker {
		var err error
		ownNetName, err = m.ownNetworkName(ctx)
		if err != nil {
			return "", "", err
		}
		netCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				ownNetName: {},
			},
		}
	} else {
		var err error
		hostPort, err = m.acquirePort()
		if err != nil {
			return "", "", err
		}
		if hostCfg.PortBindings == nil {
			hostCfg.PortBindings = network.PortMap{}
		}
		hostCfg.PortBindings[containerPort] = []network.PortBinding{{HostIP: localhostAddr, HostPort: fmt.Sprintf("%d", hostPort)}}
	}

	created, err := m.docker.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           containerCfg,
		HostConfig:       hostCfg,
		NetworkingConfig: netCfg,
		Name:             environmentContainerName(cfg.EnvironmentID),
	})
	if err != nil {
		if hostPort != 0 {
			m.releasePort(hostPort)
		}
		return "", "", fmt.Errorf("sandbox/docker: create environment container: %w", err)
	}

	// Best-effort teardown of just the container on any failure path below,
	// using a short-lived context rather than the caller's ctx (which just
	// failed) — mirrors Start's own cleanup rationale in manager.go.
	removeContainer := func() {
		removeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = m.docker.ContainerRemove(removeCtx, created.ID, client.ContainerRemoveOptions{Force: true})
		if hostPort != 0 {
			m.releasePort(hostPort)
		}
	}

	// Second network, alongside whatever netCfg above already attached at
	// create time — the one this environment's own dind sidecar is on and
	// nothing else is (see environment_dind.go's package doc comment).
	// Only attached when DockerEnabled. Mirrors Start's own NetworkConnect
	// in manager.go — see that call site's doc comment for why a separate
	// connect call, not a second NetworkingConfig.EndpointsConfig entry, is
	// what actually attaches more than one network at create time.
	if cfg.DockerEnabled {
		if _, err := m.docker.NetworkConnect(ctx, environmentDindNetworkName(cfg.EnvironmentID), client.NetworkConnectOptions{Container: created.ID}); err != nil {
			removeContainer()
			return "", "", fmt.Errorf("sandbox/docker: attach environment container to dind network: %w", err)
		}
	}

	if _, err := m.docker.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		removeContainer()
		return "", "", fmt.Errorf("sandbox/docker: start environment container: %w", err)
	}

	url, err := m.waitForEnvironmentReady(ctx, created.ID, cfg.SecretKey, insideDocker, ownNetName, hostPort)
	if err != nil {
		diagCtx, diagCancel := context.WithTimeout(context.Background(), 10*time.Second)
		diag := m.diagnoseUnready(diagCtx, created.ID)
		diagCancel()
		removeContainer()
		return "", "", fmt.Errorf("%w (%s)", err, diag)
	}

	m.setState(created.ID, handleState{hostPort: hostPort})
	return created.ID, url, nil
}

// CreateEnvironment provisions and starts a new static environment's
// backing container and its persistent named volume — called exactly once
// per environment, ever (see EnvironmentBackend.CreateEnvironment's doc
// comment). Structurally mirrors Start: same image-pull, network-attach,
// and WaitForReady logic (see createAndStartEnvironmentContainer, shared
// with RestartEnvironmentPorts below), with three deliberate differences:
// AutoRemove is false (this container must survive StopEnvironment, unlike
// an ephemeral conversation's), a named Docker volume is created and
// mounted at environmentVolumeMountPath, and cfg.SecretKey is used directly
// instead of a freshly generated one (see EnvironmentConfig.SecretKey's doc
// comment on why environments reuse the same key across restarts).
//
// When cfg.DockerEnabled, this also provisions this environment's own
// long-lived dind sidecar (environment_dind.go) before the environment's
// container itself — the sidecar's name must already exist for
// createAndStartEnvironmentContainer's DOCKER_HOST env var and
// NetworkConnect call to succeed, the same create-sidecar-before-sandbox
// ordering Start (manager.go) uses for the ephemeral per-conversation case.
// Named returns (err) so the deferred cleanups below can see whichever
// error path this function actually took.
func (m *Manager) CreateEnvironment(ctx context.Context, cfg sandbox.EnvironmentConfig) (handle *sandbox.EnvironmentHandle, err error) {
	image, err := resolveEnvironmentImage(cfg.Image, m.AgentServerImage)
	if err != nil {
		return nil, err
	}
	if err := m.ensureImage(ctx, image); err != nil {
		return nil, err
	}

	volumeName := environmentVolumeName(cfg.EnvironmentID)
	if _, err := m.docker.VolumeCreate(ctx, client.VolumeCreateOptions{
		Name: volumeName,
		Labels: map[string]string{
			labelEnvID:   cfg.EnvironmentID,
			labelManaged: "true",
		},
	}); err != nil {
		return nil, fmt.Errorf("sandbox/docker: create environment volume %s: %w", volumeName, err)
	}
	// Unlike RestartEnvironmentPorts, a failed CreateEnvironment must not
	// leave an orphaned volume behind for a future create attempt (with the
	// same cfg.EnvironmentID, hence the same deterministic volume name) to
	// collide with.
	defer func() {
		if err != nil {
			removeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = m.docker.VolumeRemove(removeCtx, volumeName, client.VolumeRemoveOptions{Force: true})
		}
	}()

	if cfg.DockerEnabled {
		if err = m.createEnvironmentDindSidecar(ctx, cfg.EnvironmentID); err != nil {
			return nil, fmt.Errorf("sandbox/docker: create environment dind sidecar: %w", err)
		}
		defer func() {
			if err != nil {
				removeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				m.removeEnvironmentDindSidecar(removeCtx, cfg.EnvironmentID)
			}
		}()
		if waitErr := m.waitForDindReady(ctx, environmentDindContainerName(cfg.EnvironmentID)); waitErr != nil {
			err = fmt.Errorf("sandbox/docker: environment dind sidecar not ready: %w", waitErr)
			return nil, err
		}
	}

	containerID, baseURL, createErr := m.createAndStartEnvironmentContainer(ctx, image, volumeName, cfg)
	if createErr != nil {
		err = createErr
		return nil, err
	}

	return &sandbox.EnvironmentHandle{BackendRef: containerID, BaseURL: baseURL, Backend: "docker", VolumeRef: volumeName}, nil
}

// recreateEnvironmentContainer stops, removes, and recreates backendRef
// against the same volumeRef with cfg fully re-applied — Docker's only way
// to change an existing container's config (port bindings, env — anything
// baked in at ContainerCreate time), shared by RestartEnvironmentPorts
// (changed port-forward set) and recreateEnvironmentIfMissingEnv below
// (backfilling GOOSE_PROVIDER onto a container that predates it) — the
// only two situations that need it.
func (m *Manager) recreateEnvironmentContainer(ctx context.Context, backendRef, volumeRef string, cfg sandbox.EnvironmentConfig) (*sandbox.EnvironmentHandle, error) {
	image, err := resolveEnvironmentImage(cfg.Image, m.AgentServerImage)
	if err != nil {
		return nil, err
	}
	if err := m.ensureImage(ctx, image); err != nil {
		return nil, err
	}

	timeoutSecs := int(sandbox.StopTimeout.Seconds())
	if _, err := m.docker.ContainerStop(ctx, backendRef, client.ContainerStopOptions{Timeout: &timeoutSecs}); err != nil && !cerrdefs.IsNotFound(err) {
		return nil, fmt.Errorf("sandbox/docker: stop environment container %s: %w", backendRef, err)
	}
	if state := m.popState(backendRef); state.hostPort != 0 {
		m.releasePort(state.hostPort)
	}
	if _, err := m.docker.ContainerRemove(ctx, backendRef, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		return nil, fmt.Errorf("sandbox/docker: remove environment container %s: %w", backendRef, err)
	}

	containerID, baseURL, err := m.createAndStartEnvironmentContainer(ctx, image, volumeRef, cfg)
	if err != nil {
		// One immediate retry: the old container is already gone but
		// volumeRef (the environment's actual data) is untouched, so a
		// transient create failure (an image-pull hiccup, a brief port
		// conflict) is worth retrying right away rather than leaving the
		// environment with zero containers until its next explicit Start
		// call happens to hit recreateGoneEnvironmentContainer's own
		// self-heal path.
		containerID, baseURL, err = m.createAndStartEnvironmentContainer(ctx, image, volumeRef, cfg)
		if err != nil {
			return nil, fmt.Errorf("sandbox/docker: recreate environment container against volume %s (data preserved — the next Start will retry): %w", volumeRef, err)
		}
	}
	return &sandbox.EnvironmentHandle{BackendRef: containerID, BaseURL: baseURL}, nil
}

// RestartEnvironmentPorts recreates backendRef with cfg.PortMappings baked
// in — see EnvironmentBackend.RestartEnvironmentPorts's own doc comment for
// why this, and not a plain StartEnvironment, is what a changed port-forward
// set requires on Docker: bindings are fixed at ContainerCreate time, so
// applying a new set means stopping and removing the existing container and
// creating a fresh one, reattached to the same volumeRef so no data is
// lost. The new container has none of the old one's authorized_keys pushed
// yet — the caller (internal/acpbridge/environment_handlers.go's
// handleRestartEnvironmentPorts) re-bootstraps SSH after a successful call,
// the same way handleCreateEnvironment already does after CreateEnvironment.
func (m *Manager) RestartEnvironmentPorts(ctx context.Context, backendRef, volumeRef string, cfg sandbox.EnvironmentConfig) (*sandbox.EnvironmentHandle, error) {
	return m.recreateEnvironmentContainer(ctx, backendRef, volumeRef, cfg)
}

// recreateEnvironmentIfMissingEnv backfills cfg.Env onto backendRef the one
// time it's actually needed: a container created by handleCreateEnvironment
// (which has no agent/LLM context at all — an environment isn't owned by
// one agent) never got GOOSE_PROVIDER/GOOSE_MODEL/the provider's API key
// baked in, so goose serve fails every session/new with "Configuration
// value not found: GOOSE_PROVIDER" until this runs once. GOOSE_PROVIDER's
// presence in the container's own Config.Env is the check — not a diff
// against cfg.Env — so this is a true one-time backfill: once applied, a
// static environment's LLM config is fixed for its whole life (whichever
// agent's conversation attaches first "wins"), never re-negotiated per
// conversation, exactly like ssh_port. Returns (nil, nil) when nothing
// needs to change (cfg.Env carries no provider, the container already has
// one, or backendRef can't be inspected — that last case is left for the
// caller's own ContainerStart to surface clearly via its existing
// not-found handling).
func (m *Manager) recreateEnvironmentIfMissingEnv(ctx context.Context, backendRef string, cfg sandbox.EnvironmentConfig) (*sandbox.EnvironmentHandle, error) {
	if cfg.Env["GOOSE_PROVIDER"] == "" {
		return nil, nil
	}
	inspect, err := m.docker.ContainerInspect(ctx, backendRef, client.ContainerInspectOptions{})
	if err != nil {
		return nil, nil
	}
	if inspect.Container.Config != nil {
		for _, kv := range inspect.Container.Config.Env {
			if strings.HasPrefix(kv, "GOOSE_PROVIDER=") {
				return nil, nil
			}
		}
	}

	var volumeName string
	for _, mnt := range inspect.Container.Mounts {
		if mnt.Destination == environmentVolumeMountPath {
			volumeName = mnt.Name
			break
		}
	}
	if volumeName == "" {
		return nil, fmt.Errorf("sandbox/docker: environment container %s has no %s volume mount to reattach while backfilling env", backendRef, environmentVolumeMountPath)
	}

	return m.recreateEnvironmentContainer(ctx, backendRef, volumeName, cfg)
}

// pacaInfraEnvKeys are env vars the built-in "paca" MCP server subprocess
// needs (see internal/executor/executor.go's buildMCPServers) that can
// legitimately differ between one StartEnvironment call and the next.
// PACA_API_KEY/PACA_API_URL/PACA_GATEWAY_URL are deployment-level — the
// same for every agent on this deployment, changing only on a key
// rotation or redeploy. PACA_WORKDIR/PACA_ACTOR_USER_ID/
// PACA_REPO_PLUGIN_IDS are conversation-scoped instead — trigger.Workdir,
// trigger.ActorUserID, and trigger.RepoPluginIDs, none of which describe
// this environment's own identity, unlike PACA_AGENT_ID/PACA_PROJECT_ID
// (deliberately excluded from this list — see below) — but they need the
// exact same treatment: Goose's EnvKeys lookup resolves each against the
// container's own OS env, which is fixed at ContainerCreate time, so a
// container created (or last recreated) while one conversation's trigger
// fields were in scope has no way to pick up a later conversation whose
// trigger fields differ. Unlike GOOSE_PROVIDER/GOOSE_MODEL/the LLM API key
// (frozen forever to whichever agent's conversation first attached — see
// recreateEnvironmentIfMissingEnv's doc comment), ensureEnvironmentInfraEnv
// keeps all of these in sync on every StartEnvironment call instead of
// baking them in once and never again — for any of these four that means a
// recreate each time a conversation's trigger fields differ from what the
// container currently has baked in, which beats the alternative: Goose
// refusing to load the whole "paca" extension with "Configuration value
// not found: <key>" (a real regression PACA_WORKDIR caused once it became
// a required EnvKeys entry, and PACA_REPO_PLUGIN_IDS repeated independently
// once a conversation with repo plugins configured attached to a container
// last recreated by one without any — same "Tool 'X' not found" symptom as
// a stale infra key, just triggered by a required key being newly added or
// newly populated rather than an existing one being rotated).
//
// PACA_AGENT_ID/PACA_PROJECT_ID are deliberately NOT in this list, even
// though buildMCPServers sets them per-trigger too: unlike workdir/actor/
// repo-plugins, they describe which agent and project this environment
// itself belongs to, and recreateEnvironmentIfMissingEnv's own doc comment
// already establishes that identity as frozen forever to whichever
// conversation first attached — silently resyncing it here on every
// StartEnvironment call would let a wrong environment_id/agent_id pairing
// on some later trigger quietly reassign a live environment's identity
// instead of surfacing as the caller bug it actually is.
var pacaInfraEnvKeys = []string{
	"PACA_API_KEY", "PACA_API_URL", "PACA_GATEWAY_URL",
	"PACA_WORKDIR", "PACA_ACTOR_USER_ID", "PACA_REPO_PLUGIN_IDS",
}

// ensureEnvironmentInfraEnv keeps pacaInfraEnvKeys in sync with this
// agent-runner process's own current config on every StartEnvironment call
// — a container/Pod baked before the platform's PACA_API_KEY was
// configured (or before it was rotated to its current value), or before a
// conversation targeting a different environment_folder set a new
// PACA_WORKDIR, would otherwise carry a stale or missing key forever,
// since Docker's env is only ever backfilled once by
// recreateEnvironmentIfMissingEnv's own GOOSE_PROVIDER-gated check
// (StartEnvironment's first check, above). Two distinct failure modes
// converge on the same user-visible symptom here: a stale/empty
// PACA_API_KEY doesn't fail session/new or session/load — Goose resolves
// an unmatched EnvKeys name to empty, not an error (see
// acp.GooseExtension's own doc comment) — it just leaves the "paca" MCP
// subprocess silently registering zero tools, since it hard-exits on
// startup when it sees an invalid/missing key (apps/mcp/src/index.ts). A
// PACA_WORKDIR entirely absent from the container's env is worse: Goose's
// own EnvKeys resolution fails hard ("Configuration value not found:
// PACA_WORKDIR") and refuses to load the "paca" extension at all, observed
// directly in a live environment's goose serve logs. Either way it's the
// same "Tool 'X' not found. Available tools: [...]" symptom this exists to
// prevent from recurring on a live environment.
//
// Only called after recreateEnvironmentIfMissingEnv has already run and
// found nothing to do — if that one recreated, the container already has
// the full, current cfg.Env baked in, infra keys included, so there's
// nothing left to reconcile on the same call.
//
// Recreating here preserves every other env var the container already
// has — GOOSE_PROVIDER, the agent's own PACA_AGENT_ID/PACA_PROJECT_ID,
// any user-configured env vars — only pacaInfraEnvKeys are overwritten, so
// this can never silently reassign this environment's frozen LLM/agent
// identity to whichever conversation happens to trigger the sync.
func (m *Manager) ensureEnvironmentInfraEnv(ctx context.Context, backendRef string, cfg sandbox.EnvironmentConfig) (*sandbox.EnvironmentHandle, error) {
	inspect, err := m.docker.ContainerInspect(ctx, backendRef, client.ContainerInspectOptions{})
	if err != nil || inspect.Container.Config == nil {
		return nil, nil
	}

	existing := make(map[string]string, len(inspect.Container.Config.Env))
	for _, kv := range inspect.Container.Config.Env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			existing[k] = v
		}
	}

	stale := false
	for _, key := range pacaInfraEnvKeys {
		if cfg.Env[key] != existing[key] {
			stale = true
			break
		}
	}
	if !stale {
		return nil, nil
	}

	var volumeName string
	for _, mnt := range inspect.Container.Mounts {
		if mnt.Destination == environmentVolumeMountPath {
			volumeName = mnt.Name
			break
		}
	}
	if volumeName == "" {
		return nil, fmt.Errorf("sandbox/docker: environment container %s has no %s volume mount to reattach while refreshing infra env", backendRef, environmentVolumeMountPath)
	}

	mergedEnv := make(map[string]string, len(existing)+len(pacaInfraEnvKeys))
	for k, v := range existing {
		mergedEnv[k] = v
	}
	// Both are recomputed fresh by createAndStartEnvironmentContainer
	// itself (GOOSE_SERVER__SECRET_KEY from cfg.SecretKey, DOCKER_HOST from
	// cfg.EnvironmentID when cfg.DockerEnabled) — dropped here so the
	// values read back from the container's own env, potentially stale,
	// never linger duplicated alongside the fresh ones it appends.
	delete(mergedEnv, "GOOSE_SERVER__SECRET_KEY")
	delete(mergedEnv, "DOCKER_HOST")
	for _, key := range pacaInfraEnvKeys {
		if v := cfg.Env[key]; v != "" {
			mergedEnv[key] = v
		} else {
			delete(mergedEnv, key)
		}
	}

	mergedCfg := cfg
	mergedCfg.Env = mergedEnv
	return m.recreateEnvironmentContainer(ctx, backendRef, volumeName, mergedCfg)
}

// recreateGoneEnvironmentContainer rebuilds environmentID's container from
// scratch after StartEnvironment discovers it's been removed outside of
// Paca — self-healing rather than forcing the "delete and recreate this
// environment" dead end ErrEnvironmentGone used to be the only option for.
// That old behavior was worse than it needed to be: DeleteEnvironment
// removes the volume along with the container, so a user who lost only the
// container (its data volume untouched) would have lost their files too,
// purely because agent-runner had no way to bring the container back on
// its own.
//
// Recovery only proceeds when cfg.EnvironmentID's named volume
// (environmentVolumeName) is still there: if it's gone too, the
// environment's data really is unrecoverable, and silently handing back a
// fresh empty volume under the same name (Docker auto-creates a named
// volume a mount references if it doesn't already exist) would misrepresent
// that loss as a successful resume — so this still returns
// sandbox.ErrEnvironmentGone in that case, same as before, keeping the
// caller's existing "delete and recreate" messaging honest.
//
// The new container is created under environmentContainerName(EnvironmentID)
// — the exact name every one of this environment's containers has always
// had (see that function's doc comment) — but Docker still assigns it a
// fresh container ID, so the returned handle's BackendRef differs from the
// one StartEnvironment was called with; the caller is responsible for
// persisting it (mirrors RestartEnvironmentPorts's own "backendRef may
// change" contract — see EnvironmentBackend.RestartEnvironmentPorts's doc
// comment).
func (m *Manager) recreateGoneEnvironmentContainer(ctx context.Context, cfg sandbox.EnvironmentConfig) (*sandbox.EnvironmentHandle, error) {
	volumeName := environmentVolumeName(cfg.EnvironmentID)
	if _, err := m.docker.VolumeInspect(ctx, volumeName, client.VolumeInspectOptions{}); err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil, fmt.Errorf("sandbox/docker: environment container %s no longer exists, and its volume %s is gone too — it was likely removed outside of Paca; delete and recreate this environment: %w",
				environmentContainerName(cfg.EnvironmentID), volumeName, sandbox.ErrEnvironmentGone)
		}
		return nil, fmt.Errorf("sandbox/docker: inspect environment volume %s: %w", volumeName, err)
	}

	image, err := resolveEnvironmentImage(cfg.Image, m.AgentServerImage)
	if err != nil {
		return nil, err
	}
	if err := m.ensureImage(ctx, image); err != nil {
		return nil, err
	}

	containerID, baseURL, err := m.createAndStartEnvironmentContainer(ctx, image, volumeName, cfg)
	if err != nil {
		return nil, fmt.Errorf("sandbox/docker: recreate environment container removed outside of Paca: %w", err)
	}
	return &sandbox.EnvironmentHandle{BackendRef: containerID, BaseURL: baseURL, Backend: "docker", VolumeRef: volumeName}, nil
}

// StartEnvironment restarts backendRef — a container CreateEnvironment
// already created — without creating a new one, unless that container has
// been removed outside of Paca (e.g. a manual `docker rm`, a `docker
// compose down` that doesn't know about it since it was never a
// Compose-managed service, or the whole Docker Desktop/Engine data
// directory getting reset across a host reboot), in which case it
// self-heals via recreateGoneEnvironmentContainer below rather than
// failing outright. First backfills cfg.Env via
// recreateEnvironmentIfMissingEnv if the container never got it (a
// one-time recreate, not a plain restart, since Docker has no other way to
// change an existing container's env — see that method's own doc
// comment); otherwise proceeds straight to ContainerStart below.
// ContainerStart against an already-running container is deliberately not
// special-cased as an error here: the Docker Engine API documents POST
// /containers/{id}/start as returning 304 Not Modified (not treated as an
// error status) when the container is already started, and this client's
// own checkResponseErr (github.com/moby/moby/client@v0.5.1's request.go)
// treats every status in [200,400) — 304 included — as success, returning
// a nil error. Verified by reading that source directly, not assumed. So
// callers may call this unconditionally on every conversation attach
// without first checking whether the environment's last known status was
// already "running".
func (m *Manager) StartEnvironment(ctx context.Context, backendRef string, cfg sandbox.EnvironmentConfig) (*sandbox.EnvironmentHandle, error) {
	if recreated, err := m.recreateEnvironmentIfMissingEnv(ctx, backendRef, cfg); err != nil {
		return nil, err
	} else if recreated != nil {
		// recreateEnvironmentContainer (which produced recreated) only
		// touches the environment's own container, not its separate,
		// already-existing dind sidecar — StopEnvironment can have left
		// that sidecar stopped, and a plain ContainerCreate+Start of a
		// fresh environment container never restarts it on its own, so
		// without this the environment comes back reporting success while
		// DOCKER_HOST still points at a stopped daemon. See
		// ensureEnvironmentDindStarted's own doc comment.
		if err := m.ensureEnvironmentDindStarted(ctx, cfg); err != nil {
			return nil, err
		}
		return recreated, nil
	}
	if recreated, err := m.ensureEnvironmentInfraEnv(ctx, backendRef, cfg); err != nil {
		return nil, err
	} else if recreated != nil {
		// Same gap as recreateEnvironmentIfMissingEnv's branch just above.
		if err := m.ensureEnvironmentDindStarted(ctx, cfg); err != nil {
			return nil, err
		}
		return recreated, nil
	}

	// Started before the environment's own container below, mirroring
	// CreateEnvironment's create-sidecar-first ordering — the container's
	// DOCKER_HOST env var already points at this sidecar's deterministic
	// name/network (baked in at ContainerCreate time), so nothing here
	// needs to wait on the other beyond the explicit waitForDindReady call
	// once both are up.
	if cfg.DockerEnabled {
		if err := m.startEnvironmentDindSidecar(ctx, cfg.EnvironmentID); err != nil {
			return nil, err
		}
	}

	if _, err := m.docker.ContainerStart(ctx, backendRef, client.ContainerStartOptions{}); err != nil {
		if cerrdefs.IsNotFound(err) {
			return m.recreateGoneEnvironmentContainer(ctx, cfg)
		}
		return nil, fmt.Errorf("sandbox/docker: start environment container %s: %w", backendRef, err)
	}

	insideDocker := isInsideDocker()
	var ownNetName string
	var hostPort int
	if insideDocker {
		var err error
		ownNetName, err = m.ownNetworkName(ctx)
		if err != nil {
			return nil, err
		}
		// No NetworkConnect here (unlike CreateEnvironment): a container's
		// network attachments are fixed at ContainerCreate time and persist
		// across stop/start on their own — ownNetName is only needed below
		// to pick the right one of backendRef's (possibly several)
		// addresses, exactly as in Start's own insideDocker branch.
	} else {
		var err error
		hostPort, err = m.environmentHostPort(ctx, backendRef)
		if err != nil {
			return nil, err
		}
	}

	baseURL, err := m.waitForEnvironmentReady(ctx, backendRef, cfg.SecretKey, insideDocker, ownNetName, hostPort)
	if err != nil {
		diagCtx, diagCancel := context.WithTimeout(context.Background(), 10*time.Second)
		diag := m.diagnoseUnready(diagCtx, backendRef)
		diagCancel()
		return nil, fmt.Errorf("%w (%s)", err, diag)
	}

	if cfg.DockerEnabled {
		if err := m.waitForDindReady(ctx, environmentDindContainerName(cfg.EnvironmentID)); err != nil {
			return nil, fmt.Errorf("sandbox/docker: environment dind sidecar not ready: %w", err)
		}
	}

	if hostPort != 0 {
		m.markPortInUse(hostPort)
	}
	m.setState(backendRef, handleState{hostPort: hostPort})
	return &sandbox.EnvironmentHandle{BackendRef: backendRef, BaseURL: baseURL}, nil
}

// ensureEnvironmentDindStarted starts (or confirms already-started) a
// DockerEnabled environment's dind sidecar and waits for it to answer —
// the same two steps StartEnvironment's own main flow does around
// createAndStartEnvironmentContainer's ContainerStart, needed here for a
// different reason: recreateEnvironmentIfMissingEnv/ensureEnvironmentInfraEnv
// each recreate only the environment's own container (via
// recreateEnvironmentContainer), never touching its separate, already-
// existing sidecar — so a sidecar left stopped by an earlier
// StopEnvironment (see stopEnvironmentDindSidecar) stays stopped straight
// through a recreate, and the environment reports a successful start with
// DOCKER_HOST still pointing at a dead daemon. A no-op when
// cfg.DockerEnabled is false. Unlike the main flow's split "start before
// ContainerStart, wait after waitForEnvironmentReady" ordering (deliberate
// overlap — see that comment), there's no equivalent container-start work
// to overlap with here, since createAndStartEnvironmentContainer has
// already fully completed by the time either caller reaches this.
func (m *Manager) ensureEnvironmentDindStarted(ctx context.Context, cfg sandbox.EnvironmentConfig) error {
	if !cfg.DockerEnabled {
		return nil
	}
	if err := m.startEnvironmentDindSidecar(ctx, cfg.EnvironmentID); err != nil {
		return err
	}
	if err := m.waitForDindReady(ctx, environmentDindContainerName(cfg.EnvironmentID)); err != nil {
		return fmt.Errorf("sandbox/docker: environment dind sidecar not ready: %w", err)
	}
	return nil
}

// StopEnvironment stops backendRef without removing it or releasing this
// Manager's own bookkeeping for it. Deliberately no popState/releasePort
// here, unlike Stop: in local-dev host-port mode, backendRef's published
// port is fixed at ContainerCreate time, not ContainerStart, so the same
// host port must still be free for THIS container to reclaim on the next
// StartEnvironment. Releasing it here would let an unrelated ephemeral
// Start hand it to a different container while this environment is merely
// paused, not deleted — see DeleteEnvironment for the actual release, and
// docs/ai-agent/environment-management.md's Open Risks on why this
// pool-held-indefinitely tradeoff is accepted, not solved, for Phase 1.
func (m *Manager) StopEnvironment(ctx context.Context, backendRef string) error {
	// Best-effort, and stopped before the environment's own container: see
	// stopEnvironmentDindSidecar's own doc comment for how it recovers
	// which sidecar (if any) belongs to backendRef with no EnvironmentID
	// passed here.
	m.stopEnvironmentDindSidecar(ctx, backendRef)

	timeoutSecs := int(sandbox.StopTimeout.Seconds())
	if _, err := m.docker.ContainerStop(ctx, backendRef, client.ContainerStopOptions{Timeout: &timeoutSecs}); err != nil {
		// A container that's already gone (removed outside of Paca) is, in
		// the sense StopEnvironment's caller cares about, already stopped —
		// treated as success rather than an error. This matters most for
		// the idle reaper (cmd/agent-runner/main.go): without this, an
		// environment whose container was manually removed would fail its
		// StopEnvironment call forever, on every reap cycle, instead of
		// settling into "stopped" and leaving the reaper alone.
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("sandbox/docker: stop environment container %s: %w", backendRef, err)
	}
	return nil
}

// DeleteEnvironment permanently removes backendRef's container and its
// volumeRef volume — the only irreversible teardown in EnvironmentBackend.
// popState's hostPort (if this Manager instance ever actually saw a
// Create/Start for backendRef) is released back to the local-dev port
// pool here; if this process never saw either call (e.g. a fresh
// agent-runner replica handling a direct delete), popState returns the
// zero value and there is nothing to release — a bounded, best-effort gap
// in the same already-accepted pool-bookkeeping tradeoff StopEnvironment's
// doc comment describes, not a new one.
//
// Idempotent by design: a container or volume that's already gone (a user
// ran `docker rm`/`docker volume rm` directly, or `docker compose down`,
// which has no way to know about a container this Manager created via the
// Engine API rather than as a Compose service) means DeleteEnvironment's
// actual goal — "this container/volume no longer exists" — is already
// achieved, not a failure. Without this, a manually-removed container left
// the *only* way to clear the environment's now-orphaned DB row blocked:
// the normal "delete environment" action would itself fail with a "no
// such container" error, forcing direct database surgery to recover.
func (m *Manager) DeleteEnvironment(ctx context.Context, backendRef, volumeRef string) error {
	state := m.popState(backendRef)
	if _, err := m.docker.ContainerRemove(ctx, backendRef, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("sandbox/docker: remove environment container %s: %w", backendRef, err)
	}
	if state.hostPort != 0 {
		m.releasePort(state.hostPort)
	}

	// After, not before, the environment's own container above: that
	// container is attached to the dind network too (createAndStart
	// EnvironmentContainer's own NetworkConnect, mirroring the sidecar's),
	// and Docker refuses to remove a network that still has any endpoint
	// attached. Removing the sidecar (and its network) first — the
	// original order here — left that NetworkRemove call failing every
	// time with the environment container still attached, silently
	// (removeEnvironmentDindSidecar is deliberately best-effort) leaving a
	// paca-env-dind-net-* network behind after every DockerEnabled
	// environment's deletion. Recovered from volumeRef, not backendRef:
	// environmentIDFromVolumeName works even when the container is already
	// gone (a legitimate case this method must tolerate — see its own doc
	// comment), unlike stopEnvironmentDindSidecar's label-inspection
	// approach, which needs a live container to read from. A no-op when
	// this environment was never DockerEnabled — removeEnvironmentDindSidecar's
	// own not-found tolerance handles that.
	m.removeEnvironmentDindSidecar(ctx, environmentIDFromVolumeName(volumeRef))

	if _, err := m.docker.VolumeRemove(ctx, volumeRef, client.VolumeRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("sandbox/docker: remove environment volume %s: %w", volumeRef, err)
	}
	return nil
}

// CopyToEnvironment uploads tarContent into backendRef at destPath. Docker's
// archive-copy API (PUT /containers/{id}/archive) has no notion of
// "ephemeral" vs "static" container, so this delegates to CopyToContainer
// directly rather than duplicating it.
func (m *Manager) CopyToEnvironment(ctx context.Context, backendRef, destPath string, tarContent io.Reader) error {
	return m.CopyToContainer(ctx, backendRef, destPath, tarContent)
}

// ExecEnvironment runs cmd inside backendRef and returns its combined
// stdout+stderr and exit code. Docker's own exec mechanism
// (ExecCreate/ExecAttach/ExecInspect, as Exec above already uses) has no
// notion of "ephemeral" vs "static" container either — it just runs a
// command inside whatever container ID it's given — so this delegates to
// Exec directly rather than duplicating its implementation. Confirmed
// against Exec's own implementation before assuming this, not guessed.
func (m *Manager) ExecEnvironment(ctx context.Context, backendRef string, cmd []string) (output string, exitCode int, err error) {
	return m.Exec(ctx, backendRef, cmd)
}

// StreamExecEnvironment runs cmd inside backendRef with a PTY, streaming
// stdin in and combined stdout+stderr out — the browser terminal's
// mechanism (see EnvironmentBackend.StreamExecEnvironment's doc comment
// for the full contract this implements). With Tty: true, the Engine API
// multiplexes stdout and stderr into the single stream ExecAttach hands
// back — unlike Exec's non-TTY path above, there is no stdcopy demuxing
// and no way to tell the two apart, so every byte is written straight to
// stdout and the stderr writer is never used. Blocks until backendRef's
// exec process exits, the hijacked connection breaks, or ctx is cancelled.
func (m *Manager) StreamExecEnvironment(ctx context.Context, backendRef string, cmd []string, stdin io.Reader, stdout, stderr io.Writer, resize <-chan sandbox.TermSize) error {
	created, err := m.docker.ExecCreate(ctx, backendRef, client.ExecCreateOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		TTY:          true,
		Cmd:          cmd,
	})
	if err != nil {
		return fmt.Errorf("sandbox/docker: exec create (tty) in %s: %w", backendRef, err)
	}

	attached, err := m.docker.ExecAttach(ctx, created.ID, client.ExecAttachOptions{TTY: true})
	if err != nil {
		return fmt.Errorf("sandbox/docker: exec attach (tty) in %s: %w", backendRef, err)
	}
	defer attached.Close()

	// done unblocks the two background goroutines below once this method
	// is about to return for any reason — the main stdout copy finishing
	// (the exec process exited, or attached.Close() below already ran
	// because ctx was cancelled), not just ctx being cancelled or resize
	// being closed, which are the only two events
	// EnvironmentBackend.StreamExecEnvironment's own doc comment requires
	// this to react to on its own.
	done := make(chan struct{})
	defer close(done)

	// ctx cancellation has no direct effect on a live net.Conn — force-close
	// it here so the io.Copy(stdout, ...) below actually unblocks instead of
	// running until the underlying connection breaks on its own.
	go func() {
		select {
		case <-ctx.Done():
			attached.Close()
		case <-done:
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case sz, ok := <-resize:
				if !ok {
					return
				}
				// Best-effort: a resize racing the exec process's own exit
				// is expected (the browser tab can still be open, still
				// firing resize events, a moment after the shell inside
				// exited) and shouldn't tear down the whole session.
				_, _ = m.docker.ExecResize(ctx, created.ID, client.ExecResizeOptions{
					Height: uint(sz.Rows),
					Width:  uint(sz.Cols),
				})
			}
		}
	}()

	// stdin -> container. Not joined below: it only returns once stdin
	// itself hits EOF/error or attached.Conn closes, and the caller (the
	// browser-terminal WebSocket handler this exists for) owns stdin's
	// lifetime, not this method — attached.Close() (deferred above) is
	// what ultimately unblocks it on every return path.
	go func() {
		_, _ = io.Copy(attached.Conn, stdin)
	}()

	_, copyErr := io.Copy(stdout, attached.Reader)
	if copyErr != nil && ctx.Err() == nil {
		return fmt.Errorf("sandbox/docker: exec stream (tty) in %s: %w", backendRef, copyErr)
	}
	// ctx.Err() is nil on a clean exit (the exec process exited and the
	// daemon closed the connection on its own) and non-nil when this
	// method's own ctx-cancellation goroutine above is what forced
	// copyErr — surfacing ctx.Err() in that case instead of a generic
	// "closed connection" error is the more useful signal to the caller.
	return ctx.Err()
}

// waitForEnvironmentReady builds the same candidate base-URL list Start
// builds inline for a freshly-created container (own-network IP inside
// Docker, or a host-port-mapped localhost address plus a bridge-IP
// fallback in local-dev mode — see Start's own candidates comment for the
// full history of why that fallback order exists) and polls until one
// answers /status. Shared by CreateEnvironment and StartEnvironment, which
// both reach a just-(re)started environment container the exact same way;
// only how insideDocker/ownNetName/hostPort were obtained differs between
// them. Not shared with Start itself — left untouched beyond this file's
// hardening change, to keep that diff minimal.
func (m *Manager) waitForEnvironmentReady(ctx context.Context, containerID, secretKey string, insideDocker bool, ownNetName string, hostPort int) (string, error) {
	var candidates []string
	if insideDocker {
		ip, err := m.containerIP(ctx, containerID, ownNetName)
		if err != nil {
			return "", err
		}
		candidates = []string{fmt.Sprintf("http://%s:%d", ip, sandbox.GooseServePort)}
	} else {
		candidates = []string{fmt.Sprintf("http://localhost:%d", hostPort)}
		if ip, err := m.containerIP(ctx, containerID, ""); err == nil {
			candidates = append([]string{fmt.Sprintf("http://%s:%d", ip, sandbox.GooseServePort)}, candidates...)
		}
	}
	return sandbox.WaitForReady(ctx, candidates, secretKey)
}

// environmentHostPort returns the local-dev host port backendRef's own
// goose-serve port (containerPort) is currently bound to, read fresh from
// the container's live NetworkSettings rather than this Manager's
// in-memory portsInUse/state maps. That's the only source of truth
// StartEnvironment can rely on: unlike an ephemeral conversation's Start,
// a static environment's container can already exist — already running or
// stopped — with a port binding chosen by a CreateEnvironment call from a
// prior, possibly since-restarted, agent-runner process.
func (m *Manager) environmentHostPort(ctx context.Context, containerID string) (int, error) {
	resp, err := m.docker.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return 0, fmt.Errorf("sandbox/docker: inspect environment container %s: %w", containerID, err)
	}
	if resp.Container.NetworkSettings == nil {
		return 0, fmt.Errorf("sandbox/docker: environment container %s has no network settings", containerID)
	}
	for _, binding := range resp.Container.NetworkSettings.Ports[containerPort] {
		if p, err := strconv.Atoi(binding.HostPort); err == nil {
			return p, nil
		}
	}
	return 0, fmt.Errorf("sandbox/docker: environment container %s has no host port bound for %s", containerID, containerPort)
}

// markPortInUse records p as taken in this Manager's local-dev port pool
// without searching for a free one the way acquirePort does — used when a
// port's owner (an environment's container) was discovered after the fact
// via environmentHostPort rather than freshly allocated here, so there is
// nothing to search for. Keeps this Manager's own bookkeeping from handing
// the same port to an unrelated ephemeral Start within this process's
// lifetime; see StopEnvironment's doc comment for why that bookkeeping is
// best-effort across an agent-runner restart regardless.
func (m *Manager) markPortInUse(p int) {
	m.portMu.Lock()
	defer m.portMu.Unlock()
	m.portsInUse[p] = true
}
