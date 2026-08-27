// ticket.go implements the short-lived, HMAC-signed tickets that
// authenticate a browser connecting *directly* to one of this package's
// public WebSocket endpoints (terminal.go's browser terminal, stats.go's
// live usage stream) — every other endpoint in this package is
// server-to-server, guarded by requireInternalToken's X-Internal-Token
// header, but a browser tab has no way to hold that secret without
// exposing it to the page. Minted by services/api (a parallel change,
// using the exact same shared secret — Server.InternalToken, i.e.
// INTERNAL_API_KEY/AI_AGENT_INTERNAL_KEY — reused here as an HMAC signing
// key, not as a bearer token) using the byte-for-byte reference
// implementation this file's own mintTicket is (used directly only by
// this package's tests; verifyTicket is what every real connection runs
// through).
//
// # Ticket format
//
//		base64url_nopad( purpose + "|" + environment_id + "|" + expires_unix_ts + "|" + hex(hmac_sha256(secret, purpose + "|" + environment_id + "|" + expires_unix_ts)) )
//
//	  - purpose: which endpoint this ticket is good for — "terminal" or
//	    "stats" today. Binds a ticket to the one endpoint it was minted
//	    for: terminal tickets require environments.connect (a shell is a
//	    live interactive session), stats tickets only need
//	    environments.read (viewing usage numbers isn't), so without this a
//	    lower-privilege stats ticket could never grant terminal access,
//	    but a terminal ticket could otherwise be replayed against the
//	    stats endpoint (or any future ticket-authenticated endpoint) even
//	    though it was only ever meant to unlock the one it was requested
//	    for.
//	  - environment_id: the environment's UUID, lowercase canonical string
//	    form (uuid.UUID.String()).
//	  - expires_unix_ts: decimal seconds-since-epoch (int64, base 10, no
//	    leading zeros/sign), e.g. "1755878400".
//	  - The HMAC-SHA256 is computed over the ASCII bytes of
//	    "<purpose>|<environment_id>|<expires_unix_ts>" (that exact
//	    three-field payload, pipe-separated — the signature does NOT cover
//	    a fourth field), keyed by the raw bytes of the shared secret
//	    string, and hex-lowercase encoded (64 hex characters).
//	  - The four fields above are joined with "|" into one string and the
//	    whole thing is base64url-encoded WITHOUT padding (Go:
//	    base64.RawURLEncoding; Node: Buffer.from(str).toString('base64url'),
//	    which is unpadded by default).
//
// Known, accepted limitation: a ticket is neither single-use nor
// re-checked against the connecting user's current authorization at
// connect time — only its own purpose, expiry, and signature are
// verified. The blast radius is bounded by each minting endpoint's own
// short TTL (60s for the terminal — see
// services/api/internal/transport/http/handler/environment_handler.go —
// and the same for stats), short enough that closing this fully (a shared
// consumed-ticket store, or a live authorization re-check per connection)
// isn't worth the added state for this phase.
package acpbridge

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ticketPurposeTerminal/ticketPurposeStats are the only two values
// purpose can take — see this file's own doc comment on why a ticket is
// scoped to exactly one.
const (
	ticketPurposeTerminal = "terminal"
	ticketPurposeStats    = "stats"
)

// mintTicket builds a ticket for purpose/environmentID, expiring at
// expiresAt — the reference implementation of the format documented in
// this file's package doc comment. Exported-in-spirit only via its doc
// comment: kept unexported since agent-runner never mints its own tickets
// in production (services/api does), used directly only by this
// package's own tests to generate a valid ticket to verify against.
func mintTicket(secret []byte, purpose string, environmentID uuid.UUID, expiresAt time.Time) string {
	payload := ticketPayload(purpose, environmentID.String(), expiresAt.Unix())
	sig := ticketSignature(secret, payload)
	raw := payload + "|" + sig
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func ticketPayload(purpose, environmentID string, expiresUnix int64) string {
	return purpose + "|" + environmentID + "|" + strconv.FormatInt(expiresUnix, 10)
}

func ticketSignature(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// verifyTicket checks ticket against secret, wantPurpose, and
// wantEnvironmentID — see this file's package doc comment for the exact
// wire format. Returns a non-nil error for any of: malformed
// encoding/shape, a signature mismatch, a purpose that doesn't match
// wantPurpose, an environment_id that doesn't match wantEnvironmentID, or
// an expiry in the past.
func verifyTicket(secret []byte, wantPurpose string, ticket string, wantEnvironmentID uuid.UUID) error {
	if ticket == "" {
		return fmt.Errorf("acpbridge: missing ticket")
	}
	raw, err := base64.RawURLEncoding.DecodeString(ticket)
	if err != nil {
		return fmt.Errorf("acpbridge: malformed ticket encoding: %w", err)
	}
	parts := strings.SplitN(string(raw), "|", 4)
	if len(parts) != 4 {
		return fmt.Errorf("acpbridge: malformed ticket shape")
	}
	purpose, environmentIDStr, expiresStr, gotSig := parts[0], parts[1], parts[2], parts[3]

	expectedSig := ticketSignature(secret, purpose+"|"+environmentIDStr+"|"+expiresStr)
	if !hmac.Equal([]byte(gotSig), []byte(expectedSig)) {
		return fmt.Errorf("acpbridge: invalid ticket signature")
	}

	if purpose != wantPurpose {
		return fmt.Errorf("acpbridge: ticket purpose mismatch")
	}

	environmentID, err := uuid.Parse(environmentIDStr)
	if err != nil || environmentID != wantEnvironmentID {
		return fmt.Errorf("acpbridge: ticket environment_id mismatch")
	}

	expiresUnix, err := strconv.ParseInt(expiresStr, 10, 64)
	if err != nil {
		return fmt.Errorf("acpbridge: malformed ticket expiry")
	}
	if time.Now().Unix() > expiresUnix {
		return fmt.Errorf("acpbridge: ticket expired")
	}
	return nil
}
