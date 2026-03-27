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

func TestPublish_MarshalError(t *testing.T) {
	p := &Publisher{}
	payload := map[string]any{
		"bad": func() {},
	}

	err := p.Publish(context.Background(), "route.key", payload)
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if !strings.Contains(err.Error(), "messaging: marshal") {
		t.Fatalf("expected marshal wrapper error, got %v", err)
	}
}

func TestClose_NilSafe(t *testing.T) {
	var p *Publisher
	p.Close()

	(&Publisher{}).Close()
}
