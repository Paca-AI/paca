package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

type fakePushSvc struct {
	subscribed   []notificationdom.PushSubscribeInput
	unsubscribed []string
	prefs        map[notificationdom.NotificationType]bool
	subscribeErr error
	setErr       error
	set          map[notificationdom.NotificationType]bool
	dnd          notificationdom.DoNotDisturb
	dndErr       error
	dndSetUntil  *time.Time
	dndSetIndef  bool
}

func (f *fakePushSvc) PublicKey(context.Context) (string, error) { return "PUBKEY", nil }
func (f *fakePushSvc) Subscribe(_ context.Context, _ uuid.UUID, in notificationdom.PushSubscribeInput) error {
	if f.subscribeErr != nil {
		return f.subscribeErr
	}
	f.subscribed = append(f.subscribed, in)
	return nil
}
func (f *fakePushSvc) Unsubscribe(_ context.Context, _ uuid.UUID, endpoint string) error {
	f.unsubscribed = append(f.unsubscribed, endpoint)
	return nil
}
func (f *fakePushSvc) SubscriptionCount(context.Context, uuid.UUID) (int, error) {
	return len(f.subscribed), nil
}
func (f *fakePushSvc) Preferences(context.Context, uuid.UUID) (map[notificationdom.NotificationType]bool, error) {
	return f.prefs, nil
}
func (f *fakePushSvc) SetPreference(_ context.Context, _ uuid.UUID, t notificationdom.NotificationType, enabled bool) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.set == nil {
		f.set = map[notificationdom.NotificationType]bool{}
	}
	f.set[t] = enabled
	return nil
}

func (f *fakePushSvc) DoNotDisturb(context.Context, uuid.UUID) (notificationdom.DoNotDisturb, error) {
	return f.dnd, nil
}
func (f *fakePushSvc) SetDoNotDisturb(_ context.Context, _ uuid.UUID, until *time.Time, indefinite bool) error {
	if f.dndErr != nil {
		return f.dndErr
	}
	f.dndSetUntil, f.dndSetIndef = until, indefinite
	f.dnd = notificationdom.DoNotDisturb{Active: until != nil || indefinite, Until: until, Indefinite: indefinite}
	return nil
}

func pushRouter(svc notificationdom.PushService) chi.Router {
	h := handler.NewPushHandler(svc)
	r := chi.NewRouter()
	r.Get("/users/me/push", h.Settings)
	r.Post("/users/me/push/subscriptions", h.Subscribe)
	r.Post("/users/me/push/subscriptions/delete", h.Unsubscribe)
	r.Put("/users/me/push/preferences", h.SetPreference)
	r.Put("/users/me/push/do-not-disturb", h.SetDoNotDisturb)
	return r
}

func TestPushHandler_RequiresAnActor(t *testing.T) {
	r := pushRouter(&fakePushSvc{})
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/users/me/push"},
		{http.MethodPost, "/users/me/push/subscriptions"},
		{http.MethodPost, "/users/me/push/subscriptions/delete"},
		{http.MethodPut, "/users/me/push/preferences"},
		{http.MethodPut, "/users/me/push/do-not-disturb"},
	} {
		if w := doNotifRequest(r, c.method, c.path, map[string]any{}); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without an actor = %d, want 401", c.method, c.path, w.Code)
		}
	}
}

func TestPushHandler_Settings(t *testing.T) {
	svc := &fakePushSvc{prefs: map[notificationdom.NotificationType]bool{
		notificationdom.NotificationTypeAssigned: true, notificationdom.NotificationTypeMentioned: false,
		notificationdom.NotificationTypeQuestion: true, notificationdom.NotificationTypeAutomation: true,
	}}
	w := doNotifRequestWithActor(pushRouter(svc), http.MethodGet, "/users/me/push", nil, uuid.New())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	_, _, data := decodeNotificationEnvelope(t, w.Body.Bytes())
	var got struct {
		PublicKey   string `json:"public_key"`
		Preferences []struct {
			Type    string `json:"type"`
			Enabled bool   `json:"enabled"`
		} `json:"preferences"`
	}
	mustUnmarshal(t, data, &got)
	if got.PublicKey != "PUBKEY" || len(got.Preferences) != 4 {
		t.Fatalf("unexpected settings: %+v", got)
	}
	for _, p := range got.Preferences {
		if want := p.Type != "mentioned"; p.Enabled != want {
			t.Errorf("preference %s enabled=%v, want %v", p.Type, p.Enabled, want)
		}
	}
}

func TestPushHandler_Subscribe(t *testing.T) {
	svc := &fakePushSvc{}
	body := map[string]any{"endpoint": "https://push.example/a", "keys": map[string]string{"p256dh": "pk", "auth": "au"}}
	w := doNotifRequestWithActor(pushRouter(svc), http.MethodPost, "/users/me/push/subscriptions", body, uuid.New())
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	if len(svc.subscribed) != 1 || svc.subscribed[0].Endpoint != "https://push.example/a" || svc.subscribed[0].P256dh != "pk" || svc.subscribed[0].Auth != "au" {
		t.Fatalf("subscription not passed through: %+v", svc.subscribed)
	}
}

func TestPushHandler_InvalidSubscriptionIs400(t *testing.T) {
	svc := &fakePushSvc{subscribeErr: notificationdom.ErrInvalidSubscription}
	w := doNotifRequestWithActor(pushRouter(svc), http.MethodPost, "/users/me/push/subscriptions", map[string]any{"endpoint": "http://x"}, uuid.New())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPushHandler_Unsubscribe(t *testing.T) {
	svc := &fakePushSvc{}
	r := pushRouter(svc)
	if w := doNotifRequestWithActor(r, http.MethodPost, "/users/me/push/subscriptions/delete", map[string]any{}, uuid.New()); w.Code != http.StatusBadRequest {
		t.Fatalf("a missing endpoint = %d, want 400", w.Code)
	}
	w := doNotifRequestWithActor(r, http.MethodPost, "/users/me/push/subscriptions/delete", map[string]any{"endpoint": "https://push.example/a"}, uuid.New())
	if w.Code != http.StatusNoContent || len(svc.unsubscribed) != 1 {
		t.Fatalf("status = %d, unsubscribed = %v", w.Code, svc.unsubscribed)
	}
}

func TestPushHandler_SetPreference(t *testing.T) {
	svc := &fakePushSvc{}
	r := pushRouter(svc)
	w := doNotifRequestWithActor(r, http.MethodPut, "/users/me/push/preferences", map[string]any{"type": "assigned", "enabled": false}, uuid.New())
	if w.Code != http.StatusNoContent || svc.set[notificationdom.NotificationTypeAssigned] != false || len(svc.set) != 1 {
		t.Fatalf("status = %d, set = %v", w.Code, svc.set)
	}

	svc.setErr = notificationdom.ErrInvalidPreference
	if w := doNotifRequestWithActor(r, http.MethodPut, "/users/me/push/preferences", map[string]any{"type": "nope", "enabled": true}, uuid.New()); w.Code != http.StatusBadRequest {
		t.Fatalf("an unknown type = %d, want 400", w.Code)
	}
}

func mustUnmarshal(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

func TestPushHandler_SettingsCarryDoNotDisturb(t *testing.T) {
	until := time.Date(2026, 11, 23, 20, 0, 0, 0, time.UTC)
	svc := &fakePushSvc{prefs: map[notificationdom.NotificationType]bool{}, dnd: notificationdom.DoNotDisturb{Active: true, Until: &until}}
	w := doNotifRequestWithActor(pushRouter(svc), http.MethodGet, "/users/me/push", nil, uuid.New())
	_, _, data := decodeNotificationEnvelope(t, w.Body.Bytes())
	var got struct {
		DND struct {
			Active     bool       `json:"active"`
			Until      *time.Time `json:"until"`
			Indefinite bool       `json:"indefinite"`
		} `json:"do_not_disturb"`
	}
	mustUnmarshal(t, data, &got)
	if !got.DND.Active || got.DND.Until == nil || !got.DND.Until.Equal(until) || got.DND.Indefinite {
		t.Fatalf("do_not_disturb = %+v", got.DND)
	}
}

func TestPushHandler_SetDoNotDisturb(t *testing.T) {
	svc := &fakePushSvc{}
	r := pushRouter(svc)
	until := time.Date(2026, 11, 23, 20, 0, 0, 0, time.UTC)

	w := doNotifRequestWithActor(r, http.MethodPut, "/users/me/push/do-not-disturb", map[string]any{"until": until}, uuid.New())
	if w.Code != http.StatusOK || svc.dndSetUntil == nil || !svc.dndSetUntil.Equal(until) || svc.dndSetIndef {
		t.Fatalf("timed: status=%d until=%v indefinite=%v", w.Code, svc.dndSetUntil, svc.dndSetIndef)
	}
	if w = doNotifRequestWithActor(r, http.MethodPut, "/users/me/push/do-not-disturb", map[string]any{"indefinite": true}, uuid.New()); w.Code != http.StatusOK || !svc.dndSetIndef {
		t.Fatalf("indefinite: status=%d indefinite=%v", w.Code, svc.dndSetIndef)
	}
	if w = doNotifRequestWithActor(r, http.MethodPut, "/users/me/push/do-not-disturb", map[string]any{"until": nil}, uuid.New()); w.Code != http.StatusOK || svc.dndSetUntil != nil || svc.dndSetIndef {
		t.Fatalf("off: status=%d until=%v indefinite=%v", w.Code, svc.dndSetUntil, svc.dndSetIndef)
	}

	svc.dndErr = notificationdom.ErrInvalidDND
	if w = doNotifRequestWithActor(r, http.MethodPut, "/users/me/push/do-not-disturb", map[string]any{"until": until}, uuid.New()); w.Code != http.StatusBadRequest {
		t.Fatalf("an invalid time = %d, want 400", w.Code)
	}
}
