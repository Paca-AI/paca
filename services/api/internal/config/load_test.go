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

func TestParseCORSOrigins(t *testing.T) {
	if got := parseCORSOrigins("*"); len(got) != 1 || got[0] != "*" {
		t.Fatalf("expected [\"*\"], got %v", got)
	}
	got := parseCORSOrigins("https://a.example.com, https://b.example.com ,,")
	want := []string{"https://a.example.com", "https://b.example.com"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
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

func TestParseUint32(t *testing.T) {
	v, err := parseUint32("1024")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 1024 {
		t.Fatalf("expected 1024, got %d", v)
	}

	if _, err := parseUint32("not-a-uint"); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := parseUint32("-1"); err == nil {
		t.Fatal("expected parse error for negative value")
	}
}

func TestParseInt64(t *testing.T) {
	v, err := parseInt64("10485760")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 10485760 {
		t.Fatalf("expected 10485760, got %d", v)
	}

	if _, err := parseInt64("not-an-int"); err == nil {
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
	t.Setenv("ADMIN_USERNAME", "admin")
	t.Setenv("ADMIN_PASSWORD", "password")
	t.Setenv("STORAGE_ACCESS_KEY_ID", "access-key")
	t.Setenv("STORAGE_SECRET_ACCESS_KEY", "secret-key")
	t.Setenv("AI_AGENT_INTERNAL_KEY", "internal-key")

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
	t.Setenv("ADMIN_USERNAME", "")
	t.Setenv("ADMIN_PASSWORD", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing required vars")
	}
	msg := err.Error()
	for _, key := range []string{"JWT_SECRET", "DATABASE_URL", "REDIS_URL", "ADMIN_USERNAME", "ADMIN_PASSWORD"} {
		if !strings.Contains(msg, key) {
			t.Fatalf("expected error to contain %s, got %q", key, msg)
		}
	}
}

func TestLoad_InvalidBoolOrDuration(t *testing.T) {
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
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

func TestLoad_PluginLimits_Defaults(t *testing.T) {
	setLoadDefaults(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Plugins.Limits.MaxCallDuration != 5*time.Second {
		t.Fatalf("expected default MaxCallDuration 5s, got %v", cfg.Plugins.Limits.MaxCallDuration)
	}
	if cfg.Plugins.Limits.MaxMemoryPages != 1024 {
		t.Fatalf("expected default MaxMemoryPages 1024, got %d", cfg.Plugins.Limits.MaxMemoryPages)
	}
	if cfg.Plugins.Limits.MaxRequestBodyBytes != 10*1024*1024 {
		t.Fatalf("expected default MaxRequestBodyBytes 10MiB, got %d", cfg.Plugins.Limits.MaxRequestBodyBytes)
	}
}

func TestLoad_PluginLimits_Custom(t *testing.T) {
	setLoadDefaults(t)
	t.Setenv("PLUGINS_MAX_CALL_DURATION", "30s")
	t.Setenv("PLUGINS_MAX_MEMORY_PAGES", "2048")
	t.Setenv("PLUGINS_MAX_REQUEST_BODY_BYTES", "1048576")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Plugins.Limits.MaxCallDuration != 30*time.Second {
		t.Fatalf("expected MaxCallDuration 30s, got %v", cfg.Plugins.Limits.MaxCallDuration)
	}
	if cfg.Plugins.Limits.MaxMemoryPages != 2048 {
		t.Fatalf("expected MaxMemoryPages 2048, got %d", cfg.Plugins.Limits.MaxMemoryPages)
	}
	if cfg.Plugins.Limits.MaxRequestBodyBytes != 1048576 {
		t.Fatalf("expected MaxRequestBodyBytes 1048576, got %d", cfg.Plugins.Limits.MaxRequestBodyBytes)
	}
}

func TestLoad_PluginLimits_InvalidValues(t *testing.T) {
	setLoadDefaults(t)
	t.Setenv("PLUGINS_MAX_CALL_DURATION", "not-a-duration")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid PLUGINS_MAX_CALL_DURATION")
	}

	setLoadDefaults(t)
	t.Setenv("PLUGINS_MAX_MEMORY_PAGES", "not-a-uint")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid PLUGINS_MAX_MEMORY_PAGES")
	}

	setLoadDefaults(t)
	t.Setenv("PLUGINS_MAX_REQUEST_BODY_BYTES", "not-an-int")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid PLUGINS_MAX_REQUEST_BODY_BYTES")
	}
}

func TestLoad_AdminUsernameTooShort(t *testing.T) {
	setLoadDefaults(t)
	t.Setenv("ADMIN_USERNAME", "ab")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for short admin username")
	}
	if !strings.Contains(err.Error(), "ADMIN_USERNAME") || !strings.Contains(err.Error(), "3") {
		t.Fatalf("expected ADMIN_USERNAME length error, got %q", err.Error())
	}
}

func TestLoad_AdminPasswordTooShort(t *testing.T) {
	setLoadDefaults(t)
	t.Setenv("ADMIN_PASSWORD", "short")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for short admin password")
	}
	if !strings.Contains(err.Error(), "ADMIN_PASSWORD") || !strings.Contains(err.Error(), "8") {
		t.Fatalf("expected ADMIN_PASSWORD length error, got %q", err.Error())
	}
}

// setLoadDefaults is a helper that seeds the minimum valid env vars so that
// individual driver tests only need to set the vars they are exercising.
func setLoadDefaults(t *testing.T) {
	t.Helper()
	t.Setenv("ENV", "test")
	t.Setenv("PORT", "8080")
	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("JWT_ACCESS_TTL", "15m")
	t.Setenv("JWT_REFRESH_TTL", "168h")
	t.Setenv("JWT_REFRESH_SESSION_TTL", "24h")
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("ADMIN_USERNAME", "admin")
	t.Setenv("ADMIN_PASSWORD", "password")
	t.Setenv("STORAGE_ACCESS_KEY_ID", "access-key")
	t.Setenv("STORAGE_SECRET_ACCESS_KEY", "secret-key")
	t.Setenv("AI_AGENT_INTERNAL_KEY", "internal-key")
}
