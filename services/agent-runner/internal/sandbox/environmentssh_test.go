package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

// fakeEnvironmentBackend records ExecEnvironment/CopyToEnvironment calls —
// the only two methods BootstrapEnvironmentSSH/SyncEnvironmentAuthorizedKeys
// actually use — and stubs the rest of EnvironmentBackend so this type
// satisfies the interface those functions take.
type fakeEnvironmentBackend struct {
	execCmds    [][]string
	execResults map[string]struct {
		output   string
		exitCode int
		err      error
	}
	copies []struct {
		destPath string
		files    map[string]string
	}
}

func (f *fakeEnvironmentBackend) CreateEnvironment(ctx context.Context, cfg EnvironmentConfig) (*EnvironmentHandle, error) {
	return nil, nil
}
func (f *fakeEnvironmentBackend) StartEnvironment(ctx context.Context, backendRef string, cfg EnvironmentConfig) (*EnvironmentHandle, error) {
	return nil, nil
}
func (f *fakeEnvironmentBackend) StopEnvironment(ctx context.Context, backendRef string) error {
	return nil
}
func (f *fakeEnvironmentBackend) RestartEnvironmentPorts(ctx context.Context, backendRef, volumeRef string, cfg EnvironmentConfig) (*EnvironmentHandle, error) {
	return nil, nil
}
func (f *fakeEnvironmentBackend) DeleteEnvironment(ctx context.Context, backendRef, volumeRef string) error {
	return nil
}
func (f *fakeEnvironmentBackend) StreamExecEnvironment(ctx context.Context, backendRef string, cmd []string, stdin io.Reader, stdout, stderr io.Writer, resize <-chan TermSize) error {
	return nil
}

func (f *fakeEnvironmentBackend) ExecEnvironment(ctx context.Context, backendRef string, cmd []string) (string, int, error) {
	f.execCmds = append(f.execCmds, cmd)
	if res, ok := f.execResults[strings.Join(cmd, " ")]; ok {
		return res.output, res.exitCode, res.err
	}
	return "", 0, nil
}

func (f *fakeEnvironmentBackend) CopyToEnvironment(ctx context.Context, backendRef, destPath string, tarContent io.Reader) error {
	files := make(map[string]string)
	tr := tar.NewReader(tarContent)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, tr); err != nil {
			return err
		}
		files[hdr.Name] = buf.String()
	}
	f.copies = append(f.copies, struct {
		destPath string
		files    map[string]string
	}{destPath, files})
	return nil
}

func TestSyncEnvironmentAuthorizedKeys_PushesAllKeys(t *testing.T) {
	backend := &fakeEnvironmentBackend{}
	keys := []string{"ssh-ed25519 AAAA1 alice", "ssh-ed25519 AAAA2 bob"}

	if err := SyncEnvironmentAuthorizedKeys(context.Background(), backend, "ref", keys); err != nil {
		t.Fatalf("SyncEnvironmentAuthorizedKeys: %v", err)
	}

	if len(backend.copies) != 1 {
		t.Fatalf("copies = %d, want 1", len(backend.copies))
	}
	cp := backend.copies[0]
	if cp.destPath != environmentWorkspaceRoot {
		t.Errorf("destPath = %q, want %q", cp.destPath, environmentWorkspaceRoot)
	}
	got, ok := cp.files[environmentAuthorizedKeysName]
	if !ok {
		t.Fatalf("tar has no %q entry, got %v", environmentAuthorizedKeysName, cp.files)
	}
	want := "ssh-ed25519 AAAA1 alice\nssh-ed25519 AAAA2 bob\n"
	if got != want {
		t.Errorf("authorized_keys content = %q, want %q", got, want)
	}
}

func TestSyncEnvironmentAuthorizedKeys_EmptyKeysWritesEmptyFile(t *testing.T) {
	backend := &fakeEnvironmentBackend{}
	if err := SyncEnvironmentAuthorizedKeys(context.Background(), backend, "ref", nil); err != nil {
		t.Fatalf("SyncEnvironmentAuthorizedKeys: %v", err)
	}
	got := backend.copies[0].files[environmentAuthorizedKeysName]
	if got != "" {
		t.Errorf("authorized_keys content = %q, want empty (every key revoked)", got)
	}
}

func TestBootstrapEnvironmentSSH_GeneratesHostKeyPushesKeysAndStartsSSHD(t *testing.T) {
	backend := &fakeEnvironmentBackend{}
	keys := []string{"ssh-ed25519 AAAA1 alice"}

	if err := BootstrapEnvironmentSSH(context.Background(), backend, "ref", keys); err != nil {
		t.Fatalf("BootstrapEnvironmentSSH: %v", err)
	}

	if len(backend.execCmds) != 3 {
		t.Fatalf("execCmds = %d, want 3 (host key generation, /run/sshd creation, sshd start): %v", len(backend.execCmds), backend.execCmds)
	}
	hostKeyCmd := strings.Join(backend.execCmds[0], " ")
	if !strings.Contains(hostKeyCmd, "ssh-keygen") || !strings.Contains(hostKeyCmd, environmentSSHHostKeyPath) {
		t.Errorf("first exec = %q, want it to generate the host key at %q", hostKeyCmd, environmentSSHHostKeyPath)
	}
	// Regression guard for the live "Missing privilege separation directory:
	// /run/sshd" failure this step exists to prevent — sshd refuses to start
	// at all without it, and nothing else (not the image, not this package
	// before this fix) ever created it.
	runSSHDCmd := strings.Join(backend.execCmds[1], " ")
	if !strings.Contains(runSSHDCmd, "mkdir -p /run/sshd") {
		t.Errorf("second exec = %q, want it to create /run/sshd before sshd starts", runSSHDCmd)
	}
	sshdCmd := backend.execCmds[2]
	if len(sshdCmd) == 0 || sshdCmd[len(sshdCmd)-1] != "/usr/sbin/sshd" {
		t.Errorf("third exec = %v, want it to start /usr/sbin/sshd", sshdCmd)
	}

	if len(backend.copies) != 1 {
		t.Fatalf("copies = %d, want 1 (authorized_keys)", len(backend.copies))
	}
	if got := backend.copies[0].files[environmentAuthorizedKeysName]; got != "ssh-ed25519 AAAA1 alice\n" {
		t.Errorf("authorized_keys content = %q", got)
	}
}

const activeSSHSessionCheckCmd = "/bin/sh -c ps -eo args | grep -c '^sshd: .*@' || true"

// TestEnvironmentHasActiveSSHSession_NoneOpen is a regression test for the
// idle reaper's "don't stop an environment mid-ssh-session" fix (see
// cmd/agent-runner's reapOneIdleEnvironment): with no matching sshd
// process, this must report false so a genuinely idle environment still
// gets reaped normally.
func TestEnvironmentHasActiveSSHSession_NoneOpen(t *testing.T) {
	backend := &fakeEnvironmentBackend{
		execResults: map[string]struct {
			output   string
			exitCode int
			err      error
		}{
			activeSSHSessionCheckCmd: {output: "0\n"},
		},
	}

	active, err := EnvironmentHasActiveSSHSession(context.Background(), backend, "ref")
	if err != nil {
		t.Fatalf("EnvironmentHasActiveSSHSession: %v", err)
	}
	if active {
		t.Error("active = true, want false when no sshd session process is present")
	}
}

// TestEnvironmentHasActiveSSHSession_OneOpen confirms a real connected
// session (verified live against a real container to get this exact
// "sshd: root@notty"-shaped count — see the function's own doc comment) is
// detected.
func TestEnvironmentHasActiveSSHSession_OneOpen(t *testing.T) {
	backend := &fakeEnvironmentBackend{
		execResults: map[string]struct {
			output   string
			exitCode int
			err      error
		}{
			activeSSHSessionCheckCmd: {output: "1\n"},
		},
	}

	active, err := EnvironmentHasActiveSSHSession(context.Background(), backend, "ref")
	if err != nil {
		t.Fatalf("EnvironmentHasActiveSSHSession: %v", err)
	}
	if !active {
		t.Error("active = false, want true when a real sshd session process is present")
	}
}

// TestEnvironmentHasActiveSSHSession_UnparsableOutputErrors guards against
// silently treating a malformed/unexpected count as "no session" — a false
// negative here is exactly the bug this whole check exists to prevent, so
// an output this function doesn't understand must surface as an error
// (the caller then fails safe by reaping as already decided — see
// reapOneIdleEnvironment's own handling), not silently become false.
func TestEnvironmentHasActiveSSHSession_UnparsableOutputErrors(t *testing.T) {
	backend := &fakeEnvironmentBackend{
		execResults: map[string]struct {
			output   string
			exitCode int
			err      error
		}{
			activeSSHSessionCheckCmd: {output: "not-a-number"},
		},
	}

	if _, err := EnvironmentHasActiveSSHSession(context.Background(), backend, "ref"); err == nil {
		t.Fatal("EnvironmentHasActiveSSHSession: want an error for unparsable output, got nil")
	}
}

func TestBootstrapEnvironmentSSH_RunSSHDCreationFailurePropagates(t *testing.T) {
	backend := &fakeEnvironmentBackend{
		execResults: map[string]struct {
			output   string
			exitCode int
			err      error
		}{
			"/bin/sh -c mkdir -p /run/sshd": {output: "permission denied", exitCode: 1},
		},
	}

	err := BootstrapEnvironmentSSH(context.Background(), backend, "ref", nil)
	if err == nil {
		t.Fatal("expected an error when /run/sshd creation fails, got nil")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error = %v, want it to include the failing command's own output", err)
	}
	for _, cmd := range backend.execCmds {
		if len(cmd) > 0 && cmd[len(cmd)-1] == "/usr/sbin/sshd" {
			t.Error("sshd was started despite /run/sshd creation failing — it would only fail there too, more confusingly")
		}
	}
}

func TestBootstrapEnvironmentSSH_SSHDStartFailurePropagates(t *testing.T) {
	backend := &fakeEnvironmentBackend{
		execResults: map[string]struct {
			output   string
			exitCode int
			err      error
		}{
			"/usr/sbin/sshd": {output: "bad config", exitCode: 1},
		},
	}

	err := BootstrapEnvironmentSSH(context.Background(), backend, "ref", nil)
	if err == nil {
		t.Fatal("expected an error when sshd exits non-zero, got nil")
	}
	if !strings.Contains(err.Error(), "bad config") {
		t.Errorf("error = %v, want it to include sshd's own output", err)
	}
}
