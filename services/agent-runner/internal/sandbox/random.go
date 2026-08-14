package sandbox

import (
	"crypto/rand"
	"encoding/hex"
)

// randomHex returns n random bytes hex-encoded — used for each container's
// one-off GOOSE_SERVER__SECRET_KEY. Mirrors docker_workspace.py's
// secrets.token_hex(32): a fresh key per ephemeral container is fine, since
// nothing needs to decrypt anything this key protected after the container
// is gone.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
