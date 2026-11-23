package handler_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

type fakeHealthSvc struct {
	h   *notificationdom.Health
	err error
}

func (f fakeHealthSvc) Health(context.Context) (*notificationdom.Health, error) { return f.h, f.err }

func healthRouter(svc notificationdom.HealthService) chi.Router {
	r := chi.NewRouter()
	r.Get("/admin/notifications/health", handler.NewNotificationHealthHandler(svc).Get)
	return r
}

func TestNotificationHealthHandler_ReportsStatusChannelsAndStreams(t *testing.T) {
	p95 := 1.5
	oldest := time.Date(2026, 11, 23, 11, 0, 0, 0, time.UTC)
	svc := fakeHealthSvc{h: &notificationdom.Health{
		Status: notificationdom.HealthDegraded, Reasons: []string{"push: 2 deliveries failed in the last hour"},
		CheckedAt: time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC),
		Channels: []notificationdom.ChannelStats{{
			Channel: "push", Pending: 3, Retrying: 1, Overdue: 2, OldestOverdue: &oldest, Sent: 40, Failed: 2,
			SentLastHour: 9, FailedLastHour: 2, LatencyP95: &p95,
			TopErrors: []notificationdom.ErrorCount{{Error: "push service returned 503", Count: 2}},
		}},
		Streams: []notificationdom.StreamHealth{{Stream: "paca.plugin_events", Group: "api.plugin_dispatcher", Pending: 4, OldestPendingIdle: 90 * time.Second, DeadLastHour: 1, DeadTotal: 5}},
	}}
	w := doNotifRequestWithActor(healthRouter(svc), http.MethodGet, "/admin/notifications/health", nil, uuid.New())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	_, _, data := decodeNotificationEnvelope(t, w.Body.Bytes())
	var got struct {
		Status   string   `json:"status"`
		Reasons  []string `json:"reasons"`
		Channels []struct {
			Channel        string   `json:"channel"`
			Overdue        int64    `json:"overdue"`
			FailedLastHour int64    `json:"failed_last_hour"`
			P95            *float64 `json:"latency_p95_seconds"`
			P50            *float64 `json:"latency_p50_seconds"`
			TopErrors      []struct {
				Error string `json:"error"`
				Count int64  `json:"count"`
			} `json:"top_errors"`
		} `json:"channels"`
		Streams []struct {
			Stream string `json:"stream"`
			Idle   int64  `json:"oldest_pending_idle_seconds"`
			Dead   int64  `json:"dead_last_hour"`
		} `json:"streams"`
	}
	mustUnmarshal(t, data, &got)
	if got.Status != "degraded" || len(got.Reasons) != 1 || len(got.Channels) != 1 || len(got.Streams) != 1 {
		t.Fatalf("response = %+v", got)
	}
	c := got.Channels[0]
	if c.Channel != "push" || c.Overdue != 2 || c.FailedLastHour != 2 || c.P95 == nil || *c.P95 != 1.5 || c.P50 != nil {
		t.Fatalf("channel = %+v; an unmeasured latency must be null, not zero", c)
	}
	if len(c.TopErrors) != 1 || c.TopErrors[0].Count != 2 {
		t.Fatalf("top errors = %+v", c.TopErrors)
	}
	if got.Streams[0].Idle != 90 || got.Streams[0].Dead != 1 {
		t.Fatalf("stream = %+v", got.Streams[0])
	}
}

func TestNotificationHealthHandler_ReportsServiceErrors(t *testing.T) {
	w := doNotifRequestWithActor(healthRouter(fakeHealthSvc{err: errors.New("db down")}), http.MethodGet, "/admin/notifications/health", nil, uuid.New())
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}
