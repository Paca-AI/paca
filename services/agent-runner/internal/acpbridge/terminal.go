// terminal.go implements the Phase 1 browser terminal from
// docs/ai-agent/environment-management.md's "Terminal / SSH Access"
// section: GET /environments/{id}/terminal/ws, a WebSocket a browser's
// xterm.js component connects to directly (through Caddy — see
// deploy/caddy/Caddyfile) for an interactive shell inside a running static
// environment's container.
//
// Unlike every other endpoint in this package, this one is reached
// directly by the browser, not server-to-server, so it can't use
// requireInternalToken's X-Internal-Token header — there is no way for a
// browser tab to hold that secret without exposing it to the page. Instead
// the connection carries a short-lived, HMAC-signed `ticket` query param,
// minted by services/api (a parallel change) using the exact same shared
// secret (Server.InternalToken, i.e. INTERNAL_API_KEY/AI_AGENT_INTERNAL_KEY)
// this package already uses to guard its internal endpoints — reused as an
// HMAC signing key here, not as a bearer token.
//
// # Ticket format
//
// A ticket is:
//
//		base64url_nopad( environment_id + "|" + expires_unix_ts + "|" + hex(hmac_sha256(secret, environment_id + "|" + expires_unix_ts)) )
//
//	  - environment_id: the environment's UUID, lowercase canonical string form
//	    (uuid.UUID.String()).
//	  - expires_unix_ts: decimal seconds-since-epoch (int64, base 10, no
//	    leading zeros/sign), e.g. "1755878400".
//	  - The HMAC-SHA256 is computed over the ASCII bytes of
//	    "<environment_id>|<expires_unix_ts>" (that exact two-field payload,
//	    pipe-separated — the signature does NOT cover a third field), keyed
//	    by the raw bytes of the shared secret string, and hex-lowercase
//	    encoded (64 hex characters).
//	  - The three fields above are joined with "|" into one string
//	    ("<environment_id>|<expires_unix_ts>|<hex_hmac>") and the whole thing
//	    is base64url-encoded WITHOUT padding (Go: base64.RawURLEncoding; Node:
//	    Buffer.from(str).toString('base64url'), which is unpadded by
//	    default).
//
// verifyTicket below is the byte-for-byte reference implementation of both
// directions (minting and verifying) — see mintTicket, used only by this
// package's own tests, and verifyTicket, used on every real connection.
package acpbridge

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

// terminalTouchInterval is how often an open terminal session bumps its
// environment's last_active_at — frequent enough that the idle reaper
// (cmd/agent-runner/main.go's reapIdleEnvironments, which polls once a
// minute) never observes a stale timestamp while a real terminal session
// is active.
const terminalTouchInterval = 30 * time.Second

// Binary WebSocket frame protocol between the browser and this endpoint —
// first byte is a type tag, exactly as
// docs/ai-agent/environment-management.md's Terminal section specifies.
// Client→server: frameData (stdin bytes) or frameResize (a PTY size
// change). Server→client: frameData only (stdout+stderr bytes — a
// PTY-backed shell has no meaningful separation between the two once
// xterm.js just renders both as one continuous stream). The two directions
// share the same numeric tag for "data" since they're independent
// namespaces, one per direction, never mixed on the wire.
const (
	frameData   byte = 0x01
	frameResize byte = 0x02
)

// registerTerminalRoute adds the public browser-terminal WebSocket to mux.
func (s *Server) registerTerminalRoute(mux *http.ServeMux) {
	mux.HandleFunc("GET /environments/{id}/terminal/ws", s.handleTerminalWS)
}

// mintTicket builds a ticket for environmentID, expiring at expiresAt —
// the reference implementation of the format documented in this file's
// package doc comment. Exported-in-spirit only via its doc comment: kept
// unexported since agent-runner never mints its own tickets in production
// (services/api does), used directly only by this package's tests to
// generate a valid ticket to verify against.
func mintTicket(secret []byte, environmentID uuid.UUID, expiresAt time.Time) string {
	payload := ticketPayload(environmentID.String(), expiresAt.Unix())
	sig := ticketSignature(secret, payload)
	raw := payload + "|" + sig
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func ticketPayload(environmentID string, expiresUnix int64) string {
	return environmentID + "|" + strconv.FormatInt(expiresUnix, 10)
}

func ticketSignature(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// verifyTicket checks ticket against secret and wantEnvironmentID — see
// this file's package doc comment for the exact wire format. Returns a
// non-nil error for any of: malformed encoding/shape, a signature
// mismatch, an environment_id that doesn't match wantEnvironmentID, or an
// expiry in the past.
func verifyTicket(secret []byte, ticket string, wantEnvironmentID uuid.UUID) error {
	if ticket == "" {
		return fmt.Errorf("acpbridge: missing ticket")
	}
	raw, err := base64.RawURLEncoding.DecodeString(ticket)
	if err != nil {
		return fmt.Errorf("acpbridge: malformed ticket encoding: %w", err)
	}
	parts := strings.SplitN(string(raw), "|", 3)
	if len(parts) != 3 {
		return fmt.Errorf("acpbridge: malformed ticket shape")
	}
	environmentIDStr, expiresStr, gotSig := parts[0], parts[1], parts[2]

	expectedSig := ticketSignature(secret, environmentIDStr+"|"+expiresStr)
	if !hmac.Equal([]byte(gotSig), []byte(expectedSig)) {
		return fmt.Errorf("acpbridge: invalid ticket signature")
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

// handleTerminalWS serves GET /environments/{id}/terminal/ws — see this
// file's package doc comment for the full auth/protocol contract. Accepts
// the WebSocket unconditionally first (mirroring handleWS's own "accept,
// then validate the first thing sent" shape), so every rejection reason
// (bad ticket, unknown/non-running environment) is reported via a WS close
// frame rather than a pre-accept HTTP error — consistent with how
// handleWS's own hello-frame validation failures are reported.
func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	environmentID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid environment id", http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(bridgeMessageReadLimit)
	ctx := r.Context()

	if err := verifyTicket([]byte(s.InternalToken), r.URL.Query().Get("ticket"), environmentID); err != nil {
		s.Log.Warn("acpbridge: rejecting terminal connection with an invalid ticket", "environment_id", environmentID, "error", err)
		_ = conn.Close(4401, "invalid or expired ticket")
		return
	}

	if s.EnvironmentRepo == nil || s.SandboxMgr == nil {
		_ = conn.Close(websocket.StatusInternalError, "environments not configured")
		return
	}

	env, err := s.EnvironmentRepo.FindEnvironmentByID(ctx, environmentID)
	if err != nil {
		_ = conn.Close(4404, "environment not found")
		return
	}

	writer := &wsTerminalWriter{conn: conn, ctx: ctx}

	if env.Status != "running" || env.BackendRef == nil || *env.BackendRef == "" {
		// A clear error message frame first, then close — the environment
		// exists (the ticket/lookup above succeeded) but isn't in a state
		// this endpoint can attach a shell to right now.
		_, _ = fmt.Fprintf(writer, "environment is not running (status=%s)\r\n", env.Status)
		_ = conn.Close(4409, "environment not running")
		return
	}

	stdinR, stdinW := io.Pipe()
	resize := make(chan sandbox.TermSize)
	go pumpTerminalInput(ctx, conn, stdinW, resize)

	touchCtx, cancelTouch := context.WithCancel(ctx)
	defer cancelTouch()
	go s.touchEnvironmentPeriodically(touchCtx, environmentID)

	s.Log.Info("acpbridge: terminal session starting", "environment_id", environmentID)
	err = s.SandboxMgr.StreamExecEnvironment(ctx, *env.BackendRef, []string{"/bin/bash"}, stdinR, writer, writer, resize)
	_ = stdinR.Close()
	if err != nil {
		s.Log.Warn("acpbridge: terminal session ended with an error", "environment_id", environmentID, "error", err)
		_ = conn.Close(websocket.StatusInternalError, "terminal session error")
		return
	}
	s.Log.Info("acpbridge: terminal session ended", "environment_id", environmentID)
	_ = conn.Close(websocket.StatusNormalClosure, "terminal session ended")
}

// touchEnvironmentPeriodically bumps environmentID's last_active_at every
// terminalTouchInterval until ctx is done — see that constant's doc
// comment.
func (s *Server) touchEnvironmentPeriodically(ctx context.Context, environmentID uuid.UUID) {
	ticker := time.NewTicker(terminalTouchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.EnvironmentRepo.TouchEnvironment(ctx, environmentID); err != nil {
				s.Log.Warn("acpbridge: failed to touch environment during terminal session",
					"environment_id", environmentID, "error", err)
			}
		}
	}
}

// pumpTerminalInput reads client→server binary frames from conn until it
// closes or ctx is done, dispatching each to stdinW (frameData) or resize
// (frameResize) per this file's frame protocol. Always closes stdinW and
// resize on return so StreamExecEnvironment's blocked reads/range loop
// unblock cleanly once the browser disconnects — mirrors
// sandbox.EnvironmentBackend.StreamExecEnvironment's own doc comment on
// resize: "the caller closes it ... once the session ends".
func pumpTerminalInput(ctx context.Context, conn *websocket.Conn, stdinW *io.PipeWriter, resize chan<- sandbox.TermSize) {
	defer close(resize)
	defer func() { _ = stdinW.Close() }()
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary || len(data) == 0 {
			continue
		}
		switch data[0] {
		case frameData:
			if _, err := stdinW.Write(data[1:]); err != nil {
				return
			}
		case frameResize:
			if len(data) < 5 {
				continue
			}
			size := sandbox.TermSize{
				Rows: binary.BigEndian.Uint16(data[1:3]),
				Cols: binary.BigEndian.Uint16(data[3:5]),
			}
			select {
			case resize <- size:
			case <-ctx.Done():
				return
			}
		}
	}
}

// wsTerminalWriter adapts conn into an io.Writer, wrapping every Write in
// the server→client frameData tag. Passed as both the stdout and stderr
// argument to StreamExecEnvironment (see this file's package doc comment
// on why one writer covers both).
type wsTerminalWriter struct {
	conn *websocket.Conn
	ctx  context.Context
}

func (w *wsTerminalWriter) Write(p []byte) (int, error) {
	frame := make([]byte, len(p)+1)
	frame[0] = frameData
	copy(frame[1:], p)
	if err := w.conn.Write(w.ctx, websocket.MessageBinary, frame); err != nil {
		return 0, err
	}
	return len(p), nil
}
