package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// SessionHandler lets an account see the devices it is signed in on and sign
// them out. Every route acts on the caller's own sessions only.
type SessionHandler struct {
	svc domainauth.SessionService
}

// NewSessionHandler returns a SessionHandler over svc.
func NewSessionHandler(svc domainauth.SessionService) *SessionHandler {
	return &SessionHandler{svc: svc}
}

// caller returns the signed-in account and the session the request belongs to.
func (h *SessionHandler) caller(w http.ResponseWriter, r *http.Request) (userID uuid.UUID, currentID string, ok bool) {
	id, found := middleware.ActorIDFromContext(r.Context())
	claims := middleware.ClaimsFrom(r)
	if !found || claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return userID, "", false
	}
	return id, claims.FamilyID, true
}

// List handles GET /users/me/sessions.
func (h *SessionHandler) List(w http.ResponseWriter, r *http.Request) {
	uid, current, ok := h.caller(w, r)
	if !ok {
		return
	}
	sessions, err := h.svc.ListSessions(r.Context(), uid, current)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]dto.SessionResponse, 0, len(sessions))
	for _, s := range sessions {
		browser, os := domainauth.ParseUserAgent(s.UserAgent)
		out = append(out, dto.SessionResponse{
			ID:           s.ID,
			Current:      s.Current,
			Browser:      browser,
			OS:           os,
			IP:           s.IP,
			RememberMe:   s.RememberMe,
			CreatedAt:    s.CreatedAt,
			LastActiveAt: s.LastActiveAt,
		})
	}
	presenter.OK(w, r, out)
}

// Revoke handles DELETE /users/me/sessions/{sessionId}: signs that device out.
// Ending the caller's own session this way works too, but does not clear the
// caller's cookies — the client should use POST /auth/logout for that.
func (h *SessionHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	uid, _, ok := h.caller(w, r)
	if !ok {
		return
	}
	if err := h.svc.RevokeSession(r.Context(), uid, chi.URLParam(r, "sessionId")); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "session revoked"})
}

// RevokeOthers handles DELETE /users/me/sessions: signs out every device but
// the one making the request.
func (h *SessionHandler) RevokeOthers(w http.ResponseWriter, r *http.Request) {
	uid, current, ok := h.caller(w, r)
	if !ok {
		return
	}
	n, err := h.svc.RevokeOtherSessions(r.Context(), uid, current)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"revoked": n})
}
