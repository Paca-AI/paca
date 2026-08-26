package sandbox

import (
	"crypto/rand"
	"encoding/hex"
)

// RandomHex returns n random bytes hex-encoded — used for each container's
// one-off GOOSE_SERVER__SECRET_KEY. Mirrors docker_workspace.py's
// secrets.token_hex(32): a fresh key per ephemeral container is fine, since
// nothing needs to decrypt anything this key protected after the container
// is gone. Exported so internal/sandbox/k8s generates the same kind of key
// the same way, instead of a second hand-copied version.
func RandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
