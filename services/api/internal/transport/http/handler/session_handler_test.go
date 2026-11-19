package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
)

type fakeSessionSvc struct {
	list      []domainauth.Session
	revokeErr error
	gotUser   uuid.UUID
	gotCur    string
	gotID     string
	others    int
}

func (f *fakeSessionSvc) ListSessions(_ context.Context, u uuid.UUID, cur string) ([]domainauth.Session, error) {
	f.gotUser, f.gotCur = u, cur
	return f.list, nil
}
func (f *fakeSessionSvc) RevokeSession(_ context.Context, u uuid.UUID, id string) error {
	f.gotUser, f.gotID = u, id
	return f.revokeErr
}
func (f *fakeSessionSvc) RevokeOtherSessions(_ context.Context, u uuid.UUID, keep string) (int, error) {
	f.gotUser, f.gotCur = u, keep
	return f.others, nil
}

func sessionRouter(svc *fakeSessionSvc, uid uuid.UUID, family string) http.Handler {
	h := handler.NewSessionHandler(svc)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := middleware.WithActorID(r.Context(), uid)
			ctx = context.WithValue(ctx, middleware.ClaimsContextKey(), &domainauth.Claims{FamilyID: family})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Get("/sessions", h.List)
	r.Delete("/sessions", h.RevokeOthers)
	r.Delete("/sessions/{sessionId}", h.Revoke)
	return r
}

func doSession(h http.Handler, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), method, path, nil))
	return w
}

func TestSessionHandler_List(t *testing.T) {
	uid := uuid.New()
	svc := &fakeSessionSvc{list: []domainauth.Session{{
		ID: "fam-1", UserAgent: "Mozilla/5.0 (X11; Linux x86_64) Chrome/126.0 Safari/537.36",
		IP: "203.0.113.4", Current: true, CreatedAt: time.Unix(1, 0), LastActiveAt: time.Unix(2, 0),
	}}}
	w := doSession(sessionRouter(svc, uid, "fam-1"), http.MethodGet, "/sessions")

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if svc.gotUser != uid || svc.gotCur != "fam-1" {
		t.Fatalf("service got user %v current %q", svc.gotUser, svc.gotCur)
	}
	var env struct {
		Data []struct {
			ID, Browser, OS, IP string
			Current             bool
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data) != 1 || env.Data[0].Browser != "Chrome" || env.Data[0].OS != "Linux" || !env.Data[0].Current || env.Data[0].IP != "203.0.113.4" {
		t.Fatalf("unexpected body: %s", w.Body)
	}
}

func TestSessionHandler_ListEmptyIsArray(t *testing.T) {
	w := doSession(sessionRouter(&fakeSessionSvc{}, uuid.New(), "f"), http.MethodGet, "/sessions")
	var env struct {
		Data []any `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if env.Data == nil {
		t.Fatalf("data should be [], body: %s", w.Body)
	}
}

func TestSessionHandler_Revoke(t *testing.T) {
	uid := uuid.New()
	svc := &fakeSessionSvc{}
	w := doSession(sessionRouter(svc, uid, "cur"), http.MethodDelete, "/sessions/fam-9")
	if w.Code != http.StatusOK || svc.gotID != "fam-9" || svc.gotUser != uid {
		t.Fatalf("status %d id %q user %v", w.Code, svc.gotID, svc.gotUser)
	}
}

func TestSessionHandler_RevokeNotFound(t *testing.T) {
	svc := &fakeSessionSvc{revokeErr: domainauth.ErrSessionNotFound}
	w := doSession(sessionRouter(svc, uuid.New(), "cur"), http.MethodDelete, "/sessions/x")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", w.Code)
	}
}

func TestSessionHandler_RevokeOthersKeepsCaller(t *testing.T) {
	svc := &fakeSessionSvc{others: 3}
	w := doSession(sessionRouter(svc, uuid.New(), "cur"), http.MethodDelete, "/sessions")
	if w.Code != http.StatusOK || svc.gotCur != "cur" {
		t.Fatalf("status %d keep %q", w.Code, svc.gotCur)
	}
	var env struct {
		Data struct{ Revoked int } `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if env.Data.Revoked != 3 {
		t.Fatalf("body: %s", w.Body)
	}
}
