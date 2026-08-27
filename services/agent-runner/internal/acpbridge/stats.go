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
	"sync"
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
	// HasActiveSSHSession lets the frontend stop showing a misleading
	// "sleeps in Xm" countdown while a real SSH session is open: the idle
	// reaper (cmd/agent-runner/main.go's reapOneIdleEnvironment) already
	// defers stopping an environment for exactly this reason — direct SSH
	// access never touches last_active_at at all, unlike a conversation
	// turn or the browser terminal's heartbeat, so an environment can look
	// idle by that column while someone is, right now, typing in a real
	// terminal over it. Computed with the same cheap check the reaper
	// itself uses (sandbox.EnvironmentHasActiveSSHSession — a single `ps`
	// exec, not a scan), and folded into this already-shared per-
	// environment tick rather than costing its own poll loop.
	HasActiveSSHSession bool `json:"has_active_ssh_session"`
}

// handleEnvironmentStatsWS serves GET /environments/{id}/stats/ws — same
// accept-then-verify-ticket shape as handleTerminalWS (terminal.go), see
// that function's own doc comment for why the WebSocket is accepted
// before the ticket is checked. Purpose ticketPurposeStats: a ticket
// minted for the terminal must not also work here, and this one — unlike
// the terminal's, which requires environments.connect — only ever needs
// environments.read (see services/api's StatsTicket handler).
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

	// Subscribes to (starting, if this is the first tab open on this
	// environment) one shared poll loop per environment rather than
	// running sandbox.ReadEnvironmentStats's own exec — including its
	// recursive `du -sb` of the whole workspace — once per connection. See
	// environmentStatsHub's doc comment for why that matters.
	ch, unsubscribe := subscribeEnvironmentStats(environmentID, s.SandboxMgr, *env.BackendRef, s.Log)
	defer unsubscribe()

	s.Log.Info("acpbridge: stats stream starting", "environment_id", environmentID)
	pushEnvironmentStats(streamCtx, conn, ch)
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

// pushEnvironmentStats relays whatever environmentStatsHub publishes on ch
// to conn until ctx is done, the hub closes ch, or a write fails. All the
// actual sampling (and its cadence) lives in the hub now — this is just
// the per-connection fan-out leg.
func pushEnvironmentStats(ctx context.Context, conn *websocket.Conn, ch <-chan environmentStatsMessage) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
				return
			}
		}
	}
}

// ── Shared per-environment stats polling ─────────────────────────────────
//
// Every open Overview tab watching the same environment used to run its
// own copy of pushEnvironmentStats's ticker, each independently calling
// sandbox.ReadEnvironmentStats — including environmentstats.go's `du -sb`
// of the entire workspace — every environmentStatsPushInterval. N tabs on
// one environment meant N concurrent full recursive disk scans on every
// tick, multiplying container IO by open-tab count instead of environment
// count. environmentStatsHub fixes that: one poll loop per environment
// (per agent-runner replica — see subscribeEnvironmentStats), shared by
// every connection subscribed to it, started on the first subscriber and
// stopped once the last one disconnects.

// environmentStatsHub is the single poll loop backing every stats
// WebSocket currently open on one environment.
type environmentStatsHub struct {
	mu   sync.Mutex
	subs map[chan environmentStatsMessage]struct{}
	// last is the most recently published reading, handed to a new
	// subscriber immediately on join so a tab opened after the hub's
	// first tick doesn't sit blank until the next one — mirrors the
	// "send one reading immediately" behavior the old per-connection loop
	// gave every connection.
	last *environmentStatsMessage
	stop context.CancelFunc
}

var (
	statsHubsMu sync.Mutex
	statsHubs   = map[uuid.UUID]*environmentStatsHub{}
)

// subscribeEnvironmentStats registers a new subscriber for environmentID,
// starting the shared poll loop if none is running yet, and returns a
// channel that receives every future reading plus an unsubscribe func the
// caller must call exactly once (typically via defer) to leave the hub —
// stopping the loop once the last subscriber has left. backend/backendRef/
// log are only consulted when this call starts a fresh loop; a subscriber
// joining an already-running hub can pass the same values (they're
// intrinsic to the environment, not the connection) and they're ignored.
func subscribeEnvironmentStats(environmentID uuid.UUID, backend sandbox.EnvironmentBackend, backendRef string, log Logger) (<-chan environmentStatsMessage, func()) {
	statsHubsMu.Lock()
	hub, ok := statsHubs[environmentID]
	if !ok {
		hub = &environmentStatsHub{subs: map[chan environmentStatsMessage]struct{}{}}
		statsHubs[environmentID] = hub
	}
	ch := make(chan environmentStatsMessage, 1)
	hub.mu.Lock()
	hub.subs[ch] = struct{}{}
	if hub.last != nil {
		ch <- *hub.last
	}
	startLoop := hub.stop == nil
	if startLoop {
		loopCtx, cancel := context.WithCancel(context.Background())
		hub.stop = cancel
		go hub.run(loopCtx, backend, backendRef, log)
	}
	hub.mu.Unlock()
	statsHubsMu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			statsHubsMu.Lock()
			defer statsHubsMu.Unlock()
			hub.mu.Lock()
			delete(hub.subs, ch)
			empty := len(hub.subs) == 0
			if empty && hub.stop != nil {
				hub.stop()
				hub.stop = nil
			}
			hub.mu.Unlock()
			if empty {
				delete(statsHubs, environmentID)
			}
		})
	}
	return ch, unsubscribe
}

// run is the shared poll loop: one sandbox.ReadEnvironmentStats call per
// tick (immediately on start, then every environmentStatsPushInterval),
// fanned out to every currently-registered subscriber. A failed read is
// logged and skipped for that tick — the same "transient hiccup self-heals
// next tick" treatment the original per-connection loop gave it — rather
// than stopping the loop out from under every subscriber over one blip.
func (h *environmentStatsHub) run(ctx context.Context, backend sandbox.EnvironmentBackend, backendRef string, log Logger) {
	send := func() {
		stats, err := sandbox.ReadEnvironmentStats(ctx, backend, backendRef)
		if err != nil {
			log.Warn("acpbridge: failed to read environment stats", "backend_ref", backendRef, "error", err)
			return
		}
		// Best-effort, same tolerance reapOneIdleEnvironment gives this
		// exact check: a transient failure just means this tick reports
		// no active session rather than blocking the CPU/memory/disk
		// numbers on it.
		hasActiveSSHSession, err := sandbox.EnvironmentHasActiveSSHSession(ctx, backend, backendRef)
		if err != nil {
			log.Warn("acpbridge: failed to check for an active ssh session", "backend_ref", backendRef, "error", err)
		}
		msg := environmentStatsMessage{
			CPUUsageUsec:        stats.CPUUsageUsec,
			CPULimitMillicores:  stats.CPULimitMillicores,
			MemoryUsedBytes:     stats.MemoryUsedBytes,
			MemoryLimitBytes:    stats.MemoryLimitBytes,
			DiskUsedBytes:       stats.DiskUsedBytes,
			HasActiveSSHSession: hasActiveSSHSession,
		}
		h.mu.Lock()
		h.last = &msg
		for ch := range h.subs {
			select {
			case ch <- msg:
			default:
				// Subscriber hasn't drained its previous reading yet —
				// drop this tick for it rather than block the whole
				// hub; the next tick supersedes it anyway.
			}
		}
		h.mu.Unlock()
	}

	send()
	ticker := time.NewTicker(environmentStatsPushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}
