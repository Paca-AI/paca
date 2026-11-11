package middleware_test

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

type headReader struct {
	seq, epoch int64
	ok         bool
	asked      []string
}

func (h *headReader) ChangeHead(_ context.Context, project string) (int64, int64, bool) {
	h.asked = append(h.asked, project)
	return h.seq, h.epoch, h.ok
}

func serve(reader httpmw.ChangeHeadReader, method string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Route("/projects/{projectId}", func(r chi.Router) {
		r.Use(httpmw.ChangeHead(reader))
		r.HandleFunc("/x", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), method, "/projects/p1/x", nil))
	return rec
}

func TestChangeHead_StampsReadsOfAProject(t *testing.T) {
	h := &headReader{seq: 42, epoch: 7, ok: true}
	rec := serve(h, http.MethodGet)
	if rec.Header().Get(httpmw.ChangeSeqHeader) != "42" || rec.Header().Get(httpmw.ChangeEpochHeader) != "7" {
		t.Errorf("headers = %v", rec.Header())
	}
	if len(h.asked) != 1 || h.asked[0] != "p1" {
		t.Errorf("asked = %v", h.asked)
	}
}

func TestChangeHead_LeavesWritesAndUnknownHeadsAlone(t *testing.T) {
	h := &headReader{seq: 1, epoch: 1, ok: true}
	if rec := serve(h, http.MethodPatch); rec.Header().Get(httpmw.ChangeSeqHeader) != "" || len(h.asked) != 0 {
		t.Error("a write must not be stamped or cost a read")
	}
	if rec := serve(&headReader{ok: false}, http.MethodGet); rec.Header().Get(httpmw.ChangeSeqHeader) != "" {
		t.Error("a project with no head must not be stamped")
	}
	if rec := serve(nil, http.MethodGet); rec.Code != http.StatusOK {
		t.Errorf("a nil reader disables the middleware, got %d", rec.Code)
	}
}

func TestChangeHead_IsNotRevealedToARefusedRequest(t *testing.T) {
	h := &headReader{seq: 9, epoch: 9, ok: true}
	r := chi.NewRouter()
	r.Route("/projects/{projectId}", func(r chi.Router) {
		r.Use(httpmw.ChangeHead(h))
		r.Get("/denied", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
		r.Get("/implicit", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	})
	denied := httptest.NewRecorder()
	r.ServeHTTP(denied, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/p1/denied", nil))
	if denied.Header().Get(httpmw.ChangeSeqHeader) != "" {
		t.Error("a 403 must not carry the change head")
	}
	implicit := httptest.NewRecorder()
	r.ServeHTTP(implicit, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/p1/implicit", nil))
	if implicit.Header().Get(httpmw.ChangeSeqHeader) != "9" {
		t.Errorf("an implicit 200 must carry it, headers = %v", implicit.Header())
	}
}

type flushHijackRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (f *flushHijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	f.hijacked = true
	return nil, nil, nil
}

func TestChangeHead_DoesNotBreakStreamingOrUpgrades(t *testing.T) {
	h := &headReader{seq: 3, epoch: 3, ok: true}
	r := chi.NewRouter()
	r.Route("/projects/{projectId}", func(r chi.Router) {
		r.Use(httpmw.ChangeHead(h))
		r.Get("/stream", func(w http.ResponseWriter, _ *http.Request) {
			fl, ok := w.(http.Flusher)
			if !ok {
				t.Error("the wrapped writer must still be a Flusher")
				return
			}
			_, _ = w.Write([]byte("data: 1\n\n"))
			fl.Flush()
		})
		r.Get("/ws", func(w http.ResponseWriter, _ *http.Request) {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("the wrapped writer must still be a Hijacker")
				return
			}
			_, _, _ = hj.Hijack()
		})
	})
	rec := &flushHijackRecorder{ResponseRecorder: httptest.NewRecorder()}
	r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/p1/stream", nil))
	if rec.Header().Get(httpmw.ChangeSeqHeader) != "3" || !rec.Flushed {
		t.Errorf("a stream must carry the head and be flushed: %v flushed=%v", rec.Header(), rec.Flushed)
	}
	r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/p1/ws", nil))
	if !rec.hijacked {
		t.Error("an upgrade must reach the underlying Hijacker")
	}
}
