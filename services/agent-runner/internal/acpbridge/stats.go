// stats.go implements the live CPU/memory/disk usage stream: GET
// /environments/{id}/stats/ws, a WebSocket the environment detail page's
// Overview tab connects to directly (same browser-bypasses-services/api
// shape as terminal.go's browser terminal — see ticket.go for how this is
// authenticated instead of requireInternalToken's X-Internal-Token).
//
// Deliberately push, not the poll-a-REST-endpoint-on-an-interval design
// this replaced: a browser re-fetching GET .../stats every few seconds
// for as long as a tab is open is exactly the kind of "API called
// continuously" this exists to avoid — one WebSocket per open Overview
// tab instead, server push on its own ticker.
//
// Unlike the terminal, this stream is read-only in both directions in
// spirit: the server only ever sends, the client is only ever expected to
// receive. It still needs an active reader loop (drainStatsInput below)
// purely so coder/websocket can process control frames (the pong replies
// to this handler's own pings, and the close frame) — without one, this
// handler would never notice the browser tab closed.
package acpbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

// environmentStatsPushInterval matches ENVIRONMENT_STATS_POLL_INTERVAL_MS,
// the interval the REST-polling design this replaced used — kept the same
// so the rings feel exactly as live as before, just without the browser
// re-requesting anything.
const environmentStatsPushInterval = 5 * time.Second

// registerEnvironmentStatsRoute adds the public live-usage WebSocket to mux.
func (s *Server) registerEnvironmentStatsRoute(mux *http.ServeMux) {
	mux.HandleFunc("GET /environments/{id}/stats/ws", s.handleEnvironmentStatsWS)
}

// environmentStatsMessage is one push over the wire — the same shape the
// REST endpoint this replaced returned, so the frontend's own
// rate-derivation logic (comparing two CPUUsageUsec samples) didn't need
// to change, only its transport.
type environmentStatsMessage struct {
	CPUUsageUsec       int64 `json:"cpu_usage_usec"`
	CPULimitMillicores int64 `json:"cpu_limit_millicores"`
	MemoryUsedBytes    int64 `json:"memory_used_bytes"`
	MemoryLimitBytes   int64 `json:"memory_limit_bytes"`
	DiskUsedBytes      int64 `json:"disk_used_bytes"`
}

// handleEnvironmentStatsWS serves GET /environments/{id}/stats/ws — same
// accept-then-verify-ticket shape as handleTerminalWS (terminal.go), see
// that function's own doc comment for why the WebSocket is accepted
// before the ticket is checked. Purpose ticketPurposeStats: a ticket
// minted for the terminal must not also work here, and this one — unlike
// the terminal's, which requires agents.write — only ever needs
// agents.read (see services/api's StatsTicket handler).
func (s *Server) handleEnvironmentStatsWS(w http.ResponseWriter, r *http.Request) {
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

	if err := verifyTicket([]byte(s.InternalToken), ticketPurposeStats, r.URL.Query().Get("ticket"), environmentID); err != nil {
		s.Log.Warn("acpbridge: rejecting stats connection with an invalid ticket", "environment_id", environmentID, "error", err)
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
	if env.Status != "running" || env.BackendRef == nil || *env.BackendRef == "" {
		_ = conn.Close(4409, "environment not running")
		return
	}

	// Shared by the reader (below) and the push loop: either one ending
	// (a dead connection detected by drainStatsInput, or a write failure
	// in pushEnvironmentStats) should stop the other.
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go drainStatsInput(streamCtx, conn, cancel)
	go pingTerminalUntilDead(streamCtx, conn)

	s.Log.Info("acpbridge: stats stream starting", "environment_id", environmentID)
	pushEnvironmentStats(streamCtx, conn, s.SandboxMgr, *env.BackendRef, s.Log)
	s.Log.Info("acpbridge: stats stream ended", "environment_id", environmentID)
	_ = conn.Close(websocket.StatusNormalClosure, "stats stream ended")
}

// drainStatsInput loops conn.Read, discarding everything, until it closes
// or ctx is done — this connection carries no real client→server data,
// but coder/websocket still needs an active reader to process control
// frames (see this file's own doc comment), and this is the only thing
// that ever notices the browser side went away. Cancels cancel on any
// read error so pushEnvironmentStats's own loop stops promptly instead of
// writing into a dead connection until its next ticker tick happens to
// fail too.
func drainStatsInput(ctx context.Context, conn *websocket.Conn, cancel context.CancelFunc) {
	defer cancel()
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
}

// pushEnvironmentStats sends one reading immediately (so the rings aren't
// blank for up to environmentStatsPushInterval after the tab opens), then
// one more on every tick until ctx is done or a write fails. A single
// failed sandbox.ReadEnvironmentStats call is logged and skipped, not
// treated as connection-ending — a transient exec hiccup self-heals on
// the next tick rather than forcing the frontend to reconnect.
func pushEnvironmentStats(ctx context.Context, conn *websocket.Conn, backend sandbox.EnvironmentBackend, backendRef string, log Logger) {
	send := func() error {
		stats, err := sandbox.ReadEnvironmentStats(ctx, backend, backendRef)
		if err != nil {
			log.Warn("acpbridge: failed to read environment stats", "backend_ref", backendRef, "error", err)
			return nil
		}
		payload, err := json.Marshal(environmentStatsMessage{
			CPUUsageUsec:       stats.CPUUsageUsec,
			CPULimitMillicores: stats.CPULimitMillicores,
			MemoryUsedBytes:    stats.MemoryUsedBytes,
			MemoryLimitBytes:   stats.MemoryLimitBytes,
			DiskUsedBytes:      stats.DiskUsedBytes,
		})
		if err != nil {
			log.Warn("acpbridge: failed to marshal environment stats", "error", err)
			return nil
		}
		return conn.Write(ctx, websocket.MessageText, payload)
	}

	if err := send(); err != nil {
		return
	}

	ticker := time.NewTicker(environmentStatsPushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := send(); err != nil {
				return
			}
		}
	}
}
