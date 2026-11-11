package middleware

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// Response headers carrying the project's change-log head at the time a read
// began (see ChangeHead).
const (
	ChangeSeqHeader   = "X-Change-Seq"
	ChangeEpochHeader = "X-Change-Epoch"
)

// ChangeHeadReader reports a project's change-log head. *messaging.Publisher
// implements it.
type ChangeHeadReader interface {
	ChangeHead(ctx context.Context, projectID string) (seq, epoch int64, ok bool)
}

// ChangeHead stamps every read under /projects/{projectId} with the project's
// change-log head as it was *before* the handler ran, so the data the response
// holds is at least as new as the head it names. A client that joins the
// realtime feed after loading that data starts from this head, which closes the
// window between its first requests and its join being acknowledged: whatever
// changed in between is replayed instead of missed. A change the data already
// reflects is replayed too, which is harmless, as applying a change is
// idempotent. Writes are not stamped, and a nil reader disables it.
func ChangeHead(reader ChangeHeadReader) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if reader == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}
			id := chi.URLParam(r, "projectId")
			if id == "" {
				next.ServeHTTP(w, r)
				return
			}
			// Read before the handler runs, but only reveal it on success: a
			// refused request (not a member, no permission) must not learn how
			// active somebody else's project is.
			seq, epoch, ok := reader.ChangeHead(r.Context(), id)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(&headStamper{ResponseWriter: w, seq: seq, epoch: epoch}, r)
		})
	}
}

// headStamper adds the head headers to a successful response.
type headStamper struct {
	http.ResponseWriter
	seq, epoch int64
	wrote      bool
}

func (h *headStamper) stamp(status int) {
	if h.wrote {
		return
	}
	h.wrote = true
	if status >= 200 && status < 300 {
		h.Header().Set(ChangeSeqHeader, strconv.FormatInt(h.seq, 10))
		h.Header().Set(ChangeEpochHeader, strconv.FormatInt(h.epoch, 10))
	}
}

func (h *headStamper) WriteHeader(status int) {
	h.stamp(status)
	h.ResponseWriter.WriteHeader(status)
}

func (h *headStamper) Write(b []byte) (int, error) {
	h.stamp(http.StatusOK)
	return h.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (h *headStamper) Unwrap() http.ResponseWriter { return h.ResponseWriter }

// Flush keeps streaming responses (server-sent events) working: the head is
// stamped as the headers go out, then the stream flows through unchanged.
func (h *headStamper) Flush() {
	h.stamp(http.StatusOK)
	if f, ok := h.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack keeps WebSocket upgrades under /projects/{id} working.
func (h *headStamper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := h.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hj.Hijack()
}
