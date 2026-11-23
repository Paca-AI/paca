package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// maxPushBody bounds a subscription request; real ones are well under 1 KiB.
const maxPushBody = 8 << 10

// PushHandler serves Web Push subscription and notification-preference
// endpoints. Every route acts on the caller's own account.
type PushHandler struct {
	svc notificationdom.PushService
}

// NewPushHandler returns a PushHandler wired to svc.
func NewPushHandler(svc notificationdom.PushService) *PushHandler {
	return &PushHandler{svc: svc}
}

func pushActor(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := middleware.ActorIDFromContext(r.Context())
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
	}
	return id, ok
}

func pushError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, notificationdom.ErrInvalidSubscription) || errors.Is(err, notificationdom.ErrInvalidPreference) || errors.Is(err, notificationdom.ErrInvalidDND) {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, err.Error()))
		return
	}
	presenter.Error(w, r, err)
}

// Settings handles GET /users/me/push: the VAPID public key a browser
// subscribes against, how many browsers the caller has subscribed, and the
// caller's per-type push preferences.
func (h *PushHandler) Settings(w http.ResponseWriter, r *http.Request) {
	userID, ok := pushActor(w, r)
	if !ok {
		return
	}
	key, err := h.svc.PublicKey(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	count, err := h.svc.SubscriptionCount(r.Context(), userID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	prefs, err := h.svc.Preferences(r.Context(), userID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	dnd, err := h.svc.DoNotDisturb(r.Context(), userID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := dto.PushSettingsResponse{
		DoNotDisturb:      dto.DoNotDisturb{Active: dnd.Active, Until: dnd.Until, Indefinite: dnd.Indefinite},
		PublicKey:         key,
		SubscriptionCount: count,
		Preferences:       make([]dto.PushPreference, 0, len(notificationdom.AllNotificationTypes)),
	}
	for _, t := range notificationdom.AllNotificationTypes {
		resp.Preferences = append(resp.Preferences, dto.PushPreference{Type: string(t), Enabled: prefs[t]})
	}
	presenter.OK(w, r, resp)
}

// Subscribe handles POST /users/me/push/subscriptions.
func (h *PushHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := pushActor(w, r)
	if !ok {
		return
	}
	var req dto.PushSubscribeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPushBody)).Decode(&req); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid request body"))
		return
	}
	if err := h.svc.Subscribe(r.Context(), userID, notificationdom.PushSubscribeInput{
		Endpoint: req.Endpoint, P256dh: req.Keys.P256dh, Auth: req.Keys.Auth, UserAgent: r.UserAgent(),
	}); err != nil {
		pushError(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// Unsubscribe handles POST /users/me/push/subscriptions/delete. A POST rather
// than a DELETE because the endpoint is a long URL that does not belong in a
// path or query string.
func (h *PushHandler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := pushActor(w, r)
	if !ok {
		return
	}
	var req dto.PushUnsubscribeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPushBody)).Decode(&req); err != nil || req.Endpoint == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid request body"))
		return
	}
	if err := h.svc.Unsubscribe(r.Context(), userID, req.Endpoint); err != nil {
		pushError(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// SetPreference handles PUT /users/me/push/preferences.
func (h *PushHandler) SetPreference(w http.ResponseWriter, r *http.Request) {
	userID, ok := pushActor(w, r)
	if !ok {
		return
	}
	var req dto.PushPreference
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPushBody)).Decode(&req); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid request body"))
		return
	}
	if err := h.svc.SetPreference(r.Context(), userID, notificationdom.NotificationType(req.Type), req.Enabled); err != nil {
		pushError(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// SetDoNotDisturb handles PUT /users/me/push/do-not-disturb.
func (h *PushHandler) SetDoNotDisturb(w http.ResponseWriter, r *http.Request) {
	userID, ok := pushActor(w, r)
	if !ok {
		return
	}
	var req dto.DoNotDisturbRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPushBody)).Decode(&req); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid request body"))
		return
	}
	if err := h.svc.SetDoNotDisturb(r.Context(), userID, req.Until, req.Indefinite); err != nil {
		pushError(w, r, err)
		return
	}
	dnd, err := h.svc.DoNotDisturb(r.Context(), userID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.DoNotDisturb{Active: dnd.Active, Until: dnd.Until, Indefinite: dnd.Indefinite})
}
