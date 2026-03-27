package cache

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewClient_InvalidURL(t *testing.T) {
	_, err := NewClient(":://bad-url", testLogger())
	if err == nil {
		t.Fatal("expected error for invalid redis url")
	}
	if !strings.Contains(err.Error(), "cache: parse url") {
		t.Fatalf("expected parse url error, got %v", err)
	}
}

func TestNewClient_PingFailure(t *testing.T) {
	_, err := NewClient("redis://127.0.0.1:1/0", testLogger())
	if err == nil {
		t.Fatal("expected ping error for unreachable redis")
	}
	if !strings.Contains(err.Error(), "cache: ping") {
		t.Fatalf("expected ping error, got %v", err)
	}
}
