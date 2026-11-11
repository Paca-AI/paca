package messaging

import (
	"context"
	"strings"
	"testing"
)

func TestPublish_NotInitialized(t *testing.T) {
	p := &Publisher{}

	err := p.Publish(context.Background(), "paca.events", struct{}{})
	if err == nil {
		t.Fatal("expected not-initialized error")
	}
	if !strings.Contains(err.Error(), "messaging: publisher not initialized") {
		t.Fatalf("expected not-initialized error, got %v", err)
	}
}

func TestAppend_NotInitialized(t *testing.T) {
	p := &Publisher{}

	err := p.Append(context.Background(), "paca.analytics", "user.created", struct{}{})
	if err == nil {
		t.Fatal("expected not-initialized error")
	}
	if !strings.Contains(err.Error(), "messaging: publisher not initialized") {
		t.Fatalf("expected not-initialized error, got %v", err)
	}
}

func TestClose_NilSafe(_ *testing.T) {
	var p *Publisher
	p.Close()

	(&Publisher{}).Close()
}

func TestIncrCounter_CountsUpAndRejectsANilPublisher(t *testing.T) {
	client, cleanup := newTestRedisClient(t)
	defer cleanup()
	p := NewPublisher(client, testLogger())
	for want := int64(1); want <= 3; want++ {
		got, err := p.IncrCounter(context.Background(), "k")
		if err != nil || got != want {
			t.Fatalf("IncrCounter = %d, %v; want %d", got, err, want)
		}
	}
	var nilPub *Publisher
	if _, err := nilPub.IncrCounter(context.Background(), "k"); err == nil {
		t.Error("a nil publisher must report an error")
	}
}
