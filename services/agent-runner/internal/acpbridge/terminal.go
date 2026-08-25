// terminal.go implements the Phase 1 browser terminal from
// docs/ai-agent/environment-management.md's "Terminal / SSH Access"
// section: GET /environments/{id}/terminal/ws, a WebSocket a browser's
// xterm.js component connects to directly (through Caddy — see
// deploy/caddy/Caddyfile) for an interactive shell inside a running static
// environment's container.
//
// Unlike every other endpoint in this package, this one (and stats.go's
// live usage stream) is reached directly by the browser, not
// server-to-server, so it can't use requireInternalToken's
// X-Internal-Token header — see ticket.go for how it's authenticated
// instead.
package acpbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
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

// terminalPingInterval/terminalPingTimeout bound how long a browser
// terminal connection is allowed to sit silently before this endpoint
// decides it's dead and tears the session down. Without an active probe,
// a network-level drop that never sends a close frame (a laptop sleeping,
// a NAT timeout) leaves pumpTerminalInput's conn.Read blocked forever —
// which, since nothing else ever cancels touchCtx in that case, would
// keep bumping last_active_at indefinitely and permanently defeat the
// idle reaper for that environment (unlike SSH, a terminal session has no
// EnvironmentHasActiveSSHSession-style secondary liveness check; this ping
// loop is the only thing standing in for one).
const (
	terminalPingInterval = 20 * time.Second
	terminalPingTimeout  = 10 * time.Second
)

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

	if err := verifyTicket([]byte(s.InternalToken), ticketPurposeTerminal, r.URL.Query().Get("ticket"), environmentID); err != nil {
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
	go pingTerminalUntilDead(touchCtx, conn)

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

// pingTerminalUntilDead actively probes conn every terminalPingInterval
// and force-closes it the first time a probe doesn't get a pong back
// within terminalPingTimeout — see terminalPingInterval's own doc comment
// on why a passive-only connection (no probe) can hang forever. Closing
// conn here is what actually unblocks everything else in this session:
// pumpTerminalInput's blocked conn.Read returns an error, closing stdinW/
// resize, which unwinds StreamExecEnvironment and returns handleTerminalWS,
// which cancels touchCtx (and, transitively, this goroutine) via its own
// deferred cancelTouch.
func pingTerminalUntilDead(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(terminalPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, terminalPingTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				_ = conn.Close(websocket.StatusGoingAway, "ping timeout")
				return
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
