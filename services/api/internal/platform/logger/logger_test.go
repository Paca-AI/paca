package logger

import (
	"log/slog"
	"testing"
)

func TestNew_ProductionUsesJSONHandler(t *testing.T) {
	l := New("production")
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
	if _, ok := l.Handler().(*slog.JSONHandler); !ok {
		t.Fatalf("expected JSON handler in production, got %T", l.Handler())
	}
}

func TestNew_NonProductionUsesTextHandler(t *testing.T) {
	l := New("development")
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
	if _, ok := l.Handler().(*slog.TextHandler); !ok {
		t.Fatalf("expected text handler outside production, got %T", l.Handler())
	}
}
