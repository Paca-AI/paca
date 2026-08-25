// environmentssh.go bootstraps the real sshd
// services/agent-server/Dockerfile bakes into every environment image
// (see that file's own doc comment for the static sshd_config drop-in
// this pairs with) — the backend-agnostic half of real SSH access to a
// static environment (docs/ai-agent/environment-management.md's
// "Terminal / SSH Access" section; the other half is a dedicated external
// port published straight onto EnvironmentSSHPort inside whichever
// container/Pod backendRef names — a native Docker -p binding, or a
// Kubernetes NodePort Service entry, see EnvironmentConfig.PortMappings —
// never relayed through agent-runner's own process). Called identically
// from both internal/sandbox/docker/environment.go and
// internal/sandbox/k8s/environment.go — sshd is started entirely via the
// same ExecEnvironment/CopyToEnvironment primitives those two backends
// already implement for every other environment-provisioning step, not a
// new per-backend code path.
package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
)

// environmentWorkspaceRoot mirrors the docker/k8s packages' own identically-
// valued private constants (each backend already declares its own copy —
// this file adds a third rather than refactoring those, consistent with
// this codebase's existing "every package declares what it needs"
// convention). This is the one directory guaranteed to survive a
// stop/start cycle on both backends — Docker's ContainerStop preserves the
// whole container filesystem regardless, but Kubernetes recreates the Pod
// on every scale-up, wiping anything outside the PersistentVolumeClaim
// mounted here — so the host key and authorized_keys below must both live
// under it, not /etc/ssh, or a Kubernetes-backed environment would show
// every user a host-key-changed warning on every single stop/start.
const environmentWorkspaceRoot = "/home/paca/workspaces"

const (
	environmentSSHHostKeyDir  = environmentWorkspaceRoot + "/.ssh_host_keys"
	environmentSSHHostKeyPath = environmentSSHHostKeyDir + "/ssh_host_ed25519_key"
	// environmentAuthorizedKeysName must match services/agent-server/Dockerfile's
	// baked-in sshd_config drop-in AuthorizedKeysFile directive exactly —
	// tar-relative (SyncEnvironmentAuthorizedKeys extracts it under
	// environmentWorkspaceRoot below), not the absolute path sshd itself
	// is configured with.
	environmentAuthorizedKeysName = ".ssh_authorized_keys"
)

// execEnvironmentOK runs cmd via backend.ExecEnvironment and turns a
// non-zero exit code into an error too — ExecEnvironment's own err return
// is transport-level only (see EnvironmentBackend's doc comment), so a
// command that ran but failed (e.g. sshd refusing to start on a bad
// config) would otherwise look like success to a caller that only checks
// err.
func execEnvironmentOK(ctx context.Context, backend EnvironmentBackend, backendRef string, cmd []string) error {
	output, exitCode, err := backend.ExecEnvironment(ctx, backendRef, cmd)
	if err != nil {
		return fmt.Errorf("exec %q: %w", strings.Join(cmd, " "), err)
	}
	if exitCode != 0 {
		return fmt.Errorf("exec %q: exit code %d: %s", strings.Join(cmd, " "), exitCode, output)
	}
	return nil
}

// SyncEnvironmentAuthorizedKeys renders publicKeys (one per line, in
// standard authorized_keys format — the exact strings already stored in
// environment_ssh_keys.public_key) and pushes them to
// environmentAuthorizedKeysName. sshd re-reads AuthorizedKeysFile on every
// connection attempt, so this alone is enough to take effect immediately —
// no sshd restart needed. Called both from BootstrapEnvironmentSSH below
// (every Create/Start) and, on its own, whenever a key is added/removed
// from an already-running environment (see
// internal/acpbridge/environment_handlers.go's ssh-keys/sync endpoint).
func SyncEnvironmentAuthorizedKeys(ctx context.Context, backend EnvironmentBackend, backendRef string, publicKeys []string) error {
	content := strings.Join(publicKeys, "\n")
	if len(publicKeys) > 0 {
		content += "\n"
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: environmentAuthorizedKeysName,
		Mode: 0o600,
		Size: int64(len(content)),
	}); err != nil {
		return fmt.Errorf("sandbox: write authorized_keys tar: %w", err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		return fmt.Errorf("sandbox: write authorized_keys tar: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("sandbox: close authorized_keys tar: %w", err)
	}

	if err := backend.CopyToEnvironment(ctx, backendRef, environmentWorkspaceRoot, &buf); err != nil {
		return fmt.Errorf("sandbox: push authorized_keys: %w", err)
	}
	return nil
}

// BootstrapEnvironmentSSH brings up real SSH access inside a just-created
// or just-(re)started environment: generates a host key the first time
// only (idempotent — left untouched on every later call, so a client's
// known_hosts entry never goes stale across a stop/start), pushes the
// current authorized_keys, and starts sshd. Safe to call on every
// Create/StartEnvironment; a failure here is the caller's to decide how to
// treat (see internal/acpbridge/environment_handlers.go's syncSSHRoute,
// which logs and continues rather than failing the whole provisioning
// call — the same treatment its Caddy subdomain sync already gets).
//
// `/usr/sbin/sshd` with no `-D` daemonizes itself and returns immediately
// once listening — ExecEnvironment's one-shot, wait-for-completion
// contract is exactly what that needs, no different from any other
// provisioning command run here.
func BootstrapEnvironmentSSH(ctx context.Context, backend EnvironmentBackend, backendRef string, publicKeys []string) error {
	genHostKey := fmt.Sprintf(
		"mkdir -p %s && [ -f %s ] || ssh-keygen -q -t ed25519 -f %s -N ''",
		environmentSSHHostKeyDir, environmentSSHHostKeyPath, environmentSSHHostKeyPath,
	)
	if err := execEnvironmentOK(ctx, backend, backendRef, []string{"/bin/sh", "-c", genHostKey}); err != nil {
		return fmt.Errorf("sandbox: generate environment ssh host key: %w", err)
	}

	if err := SyncEnvironmentAuthorizedKeys(ctx, backend, backendRef, publicKeys); err != nil {
		return err
	}

	// /run/sshd is sshd's privilege-separation directory — it refuses to
	// start without it ("Missing privilege separation directory: /run/sshd",
	// confirmed live, not guessed). Unlike environmentSSHHostKeyDir above,
	// this can't live under environmentWorkspaceRoot: sshd expects it at
	// this fixed path. Neither services/agent-server/Dockerfile nor this
	// package ever created it before, and it isn't part of the image's
	// persisted layers in a way that survives here either — recreating it
	// on every call (mkdir -p is a no-op if it's already there) is the only
	// reliable fix, mirroring the host-key directory's own
	// create-if-missing treatment just above.
	if err := execEnvironmentOK(ctx, backend, backendRef, []string{"/bin/sh", "-c", "mkdir -p /run/sshd"}); err != nil {
		return fmt.Errorf("sandbox: create /run/sshd: %w", err)
	}

	if err := execEnvironmentOK(ctx, backend, backendRef, []string{"/usr/sbin/sshd"}); err != nil {
		return fmt.Errorf("sandbox: start environment sshd: %w", err)
	}
	return nil
}

// EnvironmentHasActiveSSHSession reports whether backendRef currently has
// at least one real, authenticated SSH session open — called by the idle
// reaper (cmd/agent-runner's reapOneIdleEnvironment) right before it would
// otherwise stop an environment whose last_active_at looks stale, so a
// live `ssh` connection is never cut out from under a user just because
// nothing else happened to touch that timestamp recently.
//
// This check exists because real SSH access is structurally invisible to
// every other activity signal environments.last_active_at already tracks
// (a conversation turn, the browser terminal): it reaches sshd directly on
// its own published port, never through agent-runner's own process, so
// there is no other way for this service to even learn a session exists —
// confirmed live: an environment idle-timed-out and was stopped mid-SSH-
// session, killing the connection with no warning ("Connection closed by
// remote host").
//
// Implemented via ps, not ss/netstat (neither exists in the environment
// image — confirmed against a real container, not assumed): sshd's own
// process title distinguishes an authenticated session ("sshd:
// <user>@<tty-or-notty>") from its listener ("sshd: /usr/sbin/sshd
// [listener] ...") and from a still-unauthenticated connection ("sshd:
// <user> [priv]", no "@") — matched here with the same pattern, verified
// live against both an idle container (0) and one with a real connected
// session (1).
func EnvironmentHasActiveSSHSession(ctx context.Context, backend EnvironmentBackend, backendRef string) (bool, error) {
	output, _, err := backend.ExecEnvironment(ctx, backendRef,
		[]string{"/bin/sh", "-c", "ps -eo args | grep -c '^sshd: .*@' || true"})
	if err != nil {
		return false, fmt.Errorf("sandbox: check active ssh sessions: %w", err)
	}
	count, convErr := strconv.Atoi(strings.TrimSpace(output))
	if convErr != nil {
		return false, fmt.Errorf("sandbox: parse active ssh session count %q: %w", output, convErr)
	}
	return count > 0, nil
}
