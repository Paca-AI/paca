package environmentsvc

import (
	"fmt"

	"golang.org/x/crypto/ssh"
)

// sshFingerprint parses an "ssh-ed25519 AAAA... comment"-style authorized-key
// line and returns its SHA256 fingerprint (the same "SHA256:base64..." form
// `ssh-keygen -lf` prints) — never trusting a client-supplied fingerprint,
// per AddSSHKeyInput's doc comment. Returns an error for anything that
// isn't a well-formed SSH public key.
func sshFingerprint(publicKey string) (string, error) {
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return "", fmt.Errorf("parse SSH public key: %w", err)
	}
	return ssh.FingerprintSHA256(parsed), nil
}
