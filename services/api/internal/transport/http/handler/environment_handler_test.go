package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMintTerminalTicket_Format locks down the exact wire format agent-runner's
// verifier expects (confirmed against its own round-trip tests — see
// TerminalTicket's doc comment):
//
//	base64url_nopad( environment_id + "|" + expires_unix_ts + "|" +
//	  hex(hmac_sha256(key, environment_id + "|" + expires_unix_ts)) )
//
// This test independently recomputes the expected ticket byte-for-byte and
// compares, rather than merely checking mintTerminalTicket doesn't panic —
// a regression in field order, separator, digest case, or padding would
// otherwise slip through unnoticed until it broke against the real
// agent-runner verifier.
func TestMintTerminalTicket_Format(t *testing.T) {
	key := "test-internal-key"
	envID := uuid.New()
	ttl := 60 * time.Second

	before := time.Now().Add(ttl).Unix()
	ticket := mintTerminalTicket(key, envID, ttl)
	after := time.Now().Add(ttl).Unix()

	// Decode with RawURLEncoding (unpadded base64url) — the format must not
	// use standard padded base64 or padded base64url.
	decoded, err := base64.RawURLEncoding.DecodeString(ticket)
	require.NoError(t, err, "ticket must be valid unpadded base64url")

	parts := strings.Split(string(decoded), "|")
	require.Len(t, parts, 3, "decoded ticket must be exactly environment_id|expires|hexsig")

	assert.Equal(t, envID.String(), parts[0])
	assert.Equal(t, strings.ToLower(envID.String()), parts[0], "environment_id must be lowercase")

	var expires int64
	_, err = fmt.Sscanf(parts[1], "%d", &expires)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, expires, before)
	assert.LessOrEqual(t, expires, after)

	// Recompute the HMAC independently over exactly "<env_id>|<expires>"
	// and confirm it matches the ticket's trailing hex digest.
	payload := fmt.Sprintf("%s|%d", envID.String(), expires)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(payload))
	wantSig := hex.EncodeToString(mac.Sum(nil))

	assert.Equal(t, wantSig, parts[2])
	assert.Equal(t, strings.ToLower(wantSig), parts[2], "hex digest must be lowercase")

	// No padding characters anywhere in the encoded ticket.
	assert.NotContains(t, ticket, "=")
}

// TestMintTerminalTicket_DifferentKeysProduceDifferentTickets is a basic
// sanity check that the HMAC key actually participates in the signature.
func TestMintTerminalTicket_DifferentKeysProduceDifferentTickets(t *testing.T) {
	envID := uuid.New()
	ttl := 60 * time.Second

	t1 := mintTerminalTicket("key-one", envID, ttl)
	t2 := mintTerminalTicket("key-two", envID, ttl)

	assert.NotEqual(t, t1, t2)
}
