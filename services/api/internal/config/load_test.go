package config

import (
	"strings"
	"testing"
	"time"
)

func TestEnv(t *testing.T) {
	t.Setenv("TEST_ENV_KEY", "value")
	if got := env("TEST_ENV_KEY", "fallback"); got != "value" {
		t.Fatalf("expected %q, got %q", "value", got)
	}
	if got := env("MISSING_ENV_KEY", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback, got %q", got)
	}
}

func TestRequireEnv(t *testing.T) {
	t.Setenv("REQ_KEY", "ok")
	v, err := requireEnv("REQ_KEY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "ok" {
		t.Fatalf("expected %q, got %q", "ok", v)
	}

	t.Setenv("REQ_KEY_EMPTY", "")
	if _, err := requireEnv("REQ_KEY_EMPTY"); err == nil {
		t.Fatal("expected error for empty env")
	}
}

func TestParseDuration(t *testing.T) {
	d, err := parseDuration("15m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 15*time.Minute {
		t.Fatalf("expected %v, got %v", 15*time.Minute, d)
	}

	if _, err := parseDuration("not-a-duration"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoad_Success(t *testing.T) {
	t.Setenv("ENV", "test")
	t.Setenv("PORT", "9090")
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("JWT_ACCESS_TTL", "10m")
	t.Setenv("JWT_REFRESH_TTL", "48h")
	t.Setenv("JWT_REFRESH_SESSION_TTL", "12h")
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	t.Setenv("ADMIN_USERNAME", "admin")
	t.Setenv("ADMIN_PASSWORD", "password")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != "test" {
		t.Fatalf("expected env test, got %q", cfg.Env)
	}
	if cfg.Server.Port != "9090" {
		t.Fatalf("expected port 9090, got %q", cfg.Server.Port)
	}
	if !cfg.Server.CookieSecure {
		t.Fatal("expected CookieSecure true")
	}
	if cfg.JWT.AccessTTL != 10*time.Minute {
		t.Fatalf("unexpected AccessTTL: %v", cfg.JWT.AccessTTL)
	}
	if cfg.JWT.RefreshTTL != 48*time.Hour {
		t.Fatalf("unexpected RefreshTTL: %v", cfg.JWT.RefreshTTL)
	}
	if cfg.JWT.RefreshSessionTTL != 12*time.Hour {
		t.Fatalf("unexpected RefreshSessionTTL: %v", cfg.JWT.RefreshSessionTTL)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("RABBITMQ_URL", "")
	t.Setenv("ADMIN_USERNAME", "")
	t.Setenv("ADMIN_PASSWORD", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing required vars")
	}
	msg := err.Error()
	for _, key := range []string{"JWT_SECRET", "DATABASE_URL", "REDIS_URL", "RABBITMQ_URL", "ADMIN_USERNAME", "ADMIN_PASSWORD"} {
		if !strings.Contains(msg, key) {
			t.Fatalf("expected error to contain %s, got %q", key, msg)
		}
	}
}

func TestLoad_InvalidBoolOrDuration(t *testing.T) {
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	t.Setenv("ADMIN_USERNAME", "admin")
	t.Setenv("ADMIN_PASSWORD", "password")

	t.Setenv("COOKIE_SECURE", "definitely-not-bool")
	if _, err := Load(); err == nil {
		t.Fatal("expected bool parse error")
	}

	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("JWT_ACCESS_TTL", "invalid")
	if _, err := Load(); err == nil {
		t.Fatal("expected duration parse error")
	}
}
