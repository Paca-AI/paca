package messaging

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func loggerForTests() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewPublisher_DialError(t *testing.T) {
	_, err := NewPublisher("not-a-valid-amqp-url", "events", loggerForTests())
	if err == nil {
		t.Fatal("expected dial error")
	}
	if !strings.Contains(err.Error(), "messaging: dial") {
		t.Fatalf("expected dial wrapper error, got %v", err)
	}
}

func TestPublish_NotInitialized(t *testing.T) {
	p := &Publisher{}

	err := p.Publish(context.Background(), "route.key", struct{}{})
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
