package notificationhealthsvc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
)

type fakeStats struct {
	stats []notificationdom.ChannelStats
	err   error
}

func (f fakeStats) DeliveryStats(context.Context, time.Time) ([]notificationdom.ChannelStats, error) {
	return f.stats, f.err
}

type fakeStreams struct {
	streams []notificationdom.StreamHealth
	err     error
}

func (f fakeStreams) StreamHealth(context.Context, time.Time) ([]notificationdom.StreamHealth, error) {
	return f.streams, f.err
}

var now = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

func health(t *testing.T, stats []notificationdom.ChannelStats, streams []notificationdom.StreamHealth) *notificationdom.Health {
	t.Helper()
	s := New(fakeStats{stats: stats}, fakeStreams{streams: streams})
	s.now = func() time.Time { return now }
	h, err := s.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func ago(d time.Duration) *time.Time { t := now.Add(-d); return &t }
func f(v float64) *float64           { return &v }

func hasReason(h *notificationdom.Health, sub string) bool {
	for _, r := range h.Reasons {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

func TestHealth_OKWhenNothingIsWrong(t *testing.T) {
	h := health(t,
		[]notificationdom.ChannelStats{{Channel: "realtime", Sent: 10, SentLastHour: 4, LatencyP95: f(0.4)}},
		[]notificationdom.StreamHealth{{Stream: "paca.task_assignments", Group: "g", Pending: 2, OldestPendingIdle: 20 * time.Second, DeadTotal: 3}})
	if h.Status != notificationdom.HealthOK || len(h.Reasons) != 0 {
		t.Fatalf("status = %s reasons = %v; old dead letters and a short wait are not a problem", h.Status, h.Reasons)
	}
	if !h.CheckedAt.Equal(now) {
		t.Fatalf("CheckedAt = %v", h.CheckedAt)
	}
}

func TestHealth_FailuresInTheLastHourDegrade(t *testing.T) {
	h := health(t, []notificationdom.ChannelStats{{Channel: "push", FailedLastHour: 3}}, nil)
	if h.Status != notificationdom.HealthDegraded || !hasReason(h, "push: 3 deliveries failed") {
		t.Fatalf("status = %s reasons = %v", h.Status, h.Reasons)
	}
}

func TestHealth_OverdueIsLateThenStalled(t *testing.T) {
	late := health(t, []notificationdom.ChannelStats{{Channel: "realtime", Overdue: 4, OldestOverdue: ago(3 * time.Minute)}}, nil)
	if late.Status != notificationdom.HealthDegraded {
		t.Fatalf("a few minutes overdue = %s, want degraded", late.Status)
	}
	stalled := health(t, []notificationdom.ChannelStats{{Channel: "realtime", Overdue: 4, OldestOverdue: ago(11 * time.Minute)}}, nil)
	if stalled.Status != notificationdom.HealthFailing || !hasReason(stalled, "looks stalled") {
		t.Fatalf("ten-plus minutes overdue = %s %v, want failing", stalled.Status, stalled.Reasons)
	}
}

func TestHealth_StuckConsumerAndDeadLetters(t *testing.T) {
	slow := health(t, nil, []notificationdom.StreamHealth{{Stream: "s", OldestPendingIdle: 6 * time.Minute}})
	if slow.Status != notificationdom.HealthDegraded {
		t.Fatalf("6 minutes unacknowledged = %s, want degraded", slow.Status)
	}
	stuck := health(t, nil, []notificationdom.StreamHealth{{Stream: "s", OldestPendingIdle: 16 * time.Minute}})
	if stuck.Status != notificationdom.HealthFailing || !hasReason(stuck, "consumer looks stuck") {
		t.Fatalf("16 minutes unacknowledged = %s %v, want failing", stuck.Status, stuck.Reasons)
	}
	dead := health(t, nil, []notificationdom.StreamHealth{{Stream: "paca.plugin_events", DeadLastHour: 2}})
	if dead.Status != notificationdom.HealthDegraded || !hasReason(dead, "paca.plugin_events.dead") {
		t.Fatalf("recent dead letters = %s %v", dead.Status, dead.Reasons)
	}
}

func TestHealth_SlowDeliveryDegrades(t *testing.T) {
	h := health(t, []notificationdom.ChannelStats{{Channel: "realtime", LatencyP95: f(95)}}, nil)
	if h.Status != notificationdom.HealthDegraded || !hasReason(h, "took over 95s") {
		t.Fatalf("status = %s reasons = %v", h.Status, h.Reasons)
	}
}

func TestHealth_FailingWinsOverDegradedRegardlessOfOrder(t *testing.T) {
	h := health(t,
		[]notificationdom.ChannelStats{{Channel: "push", FailedLastHour: 1}},
		[]notificationdom.StreamHealth{{Stream: "s", OldestPendingIdle: time.Hour}})
	if h.Status != notificationdom.HealthFailing || len(h.Reasons) != 2 {
		t.Fatalf("status = %s reasons = %v; want failing with both reasons", h.Status, h.Reasons)
	}
}

func TestHealth_ErrorsAndMissingValkey(t *testing.T) {
	boom := errors.New("db down")
	if _, err := New(fakeStats{err: boom}, nil).Health(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("a failing stats query must be reported, got %v", err)
	}
	if _, err := New(fakeStats{}, fakeStreams{err: boom}).Health(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("a failing stream query must be reported, got %v", err)
	}
	h, err := New(fakeStats{}, nil).Health(context.Background())
	if err != nil || h.Status != notificationdom.HealthOK || h.Streams != nil {
		t.Fatalf("no Valkey: %+v %v; stream health is simply not reported", h, err)
	}
}
