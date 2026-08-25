// Package config loads runtime configuration from environment variables and
// optional .env files.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Minimum lenths enforced across the entire stack — install.sh, the Go
// config loader, and the frontend auth-validation module all use these.
const (
	minUsernameLength = 3
	minPasswordLength = 8
)

// Load reads .env (if present) then environment variables and returns a
// validated Config.  Missing required keys cause a non-nil error that names
// every absent variable so operators see all gaps at once.
func Load() (*Config, error) {
	// .env is optional; ignore "file not found" error.
	_ = godotenv.Load()

	accessTTL, err := parseDuration(env("JWT_ACCESS_TTL", "15m"))
	if err != nil {
		return nil, fmt.Errorf("config: JWT_ACCESS_TTL: %w", err)
	}
	refreshTTL, err := parseDuration(env("JWT_REFRESH_TTL", "168h"))
	if err != nil {
		return nil, fmt.Errorf("config: JWT_REFRESH_TTL: %w", err)
	}
	refreshSessionTTL, err := parseDuration(env("JWT_REFRESH_SESSION_TTL", "24h"))
	if err != nil {
		return nil, fmt.Errorf("config: JWT_REFRESH_SESSION_TTL: %w", err)
	}

	cookieSecure, err := strconv.ParseBool(env("COOKIE_SECURE", "false"))
	if err != nil {
		return nil, fmt.Errorf("config: COOKIE_SECURE: %w", err)
	}

	// Collect all missing required keys before returning so the caller sees
	// every problem in a single error rather than one failure at a time.
	var errs []error

	secret, err := requireEnv("JWT_SECRET")
	if err != nil {
		errs = append(errs, err)
	}

	dsn, err := requireEnv("DATABASE_URL")
	if err != nil {
		errs = append(errs, err)
	}

	redisURL, err := requireEnv("REDIS_URL")
	if err != nil {
		errs = append(errs, err)
	}

	cacheProjectTTL, err := parseDuration(env("CACHE_PROJECT_TTL", "5m"))
	if err != nil {
		return nil, fmt.Errorf("config: CACHE_PROJECT_TTL: %w", err)
	}
	cacheConfigTTL, err := parseDuration(env("CACHE_CONFIG_TTL", "10m"))
	if err != nil {
		return nil, fmt.Errorf("config: CACHE_CONFIG_TTL: %w", err)
	}
	cacheSprintTTL, err := parseDuration(env("CACHE_SPRINT_TTL", "2m"))
	if err != nil {
		return nil, fmt.Errorf("config: CACHE_SPRINT_TTL: %w", err)
	}

	marketplaceTimeout, err := parseDuration(env("PLUGINS_MARKETPLACE_TIMEOUT", "20s"))
	if err != nil {
		return nil, fmt.Errorf("config: PLUGINS_MARKETPLACE_TIMEOUT: %w", err)
	}

	releaseCacheTTL, err := parseDuration(env("RELEASE_CHECK_CACHE_TTL", "1h"))
	if err != nil {
		return nil, fmt.Errorf("config: RELEASE_CHECK_CACHE_TTL: %w", err)
	}
	releaseTimeout, err := parseDuration(env("RELEASE_CHECK_TIMEOUT", "5s"))
	if err != nil {
		return nil, fmt.Errorf("config: RELEASE_CHECK_TIMEOUT: %w", err)
	}

	// Defaults here must match pluginrt.DefaultResourceLimits().
	pluginMaxCallDuration, err := parseDuration(env("PLUGINS_MAX_CALL_DURATION", "5s"))
	if err != nil {
		return nil, fmt.Errorf("config: PLUGINS_MAX_CALL_DURATION: %w", err)
	}
	pluginMaxMemoryPages, err := parseUint32(env("PLUGINS_MAX_MEMORY_PAGES", "1024"))
	if err != nil {
		return nil, fmt.Errorf("config: PLUGINS_MAX_MEMORY_PAGES: %w", err)
	}
	pluginMaxRequestBodyBytes, err := parseInt64(env("PLUGINS_MAX_REQUEST_BODY_BYTES", "1048576"))
	if err != nil {
		return nil, fmt.Errorf("config: PLUGINS_MAX_REQUEST_BODY_BYTES: %w", err)
	}

	adminUser, err := requireEnv("ADMIN_USERNAME")
	if err != nil {
		errs = append(errs, err)
	} else if len(strings.TrimSpace(adminUser)) < minUsernameLength {
		errs = append(errs, fmt.Errorf(
			"config: ADMIN_USERNAME must be at least %d characters", minUsernameLength))
	}

	adminPass, err := requireEnv("ADMIN_PASSWORD")
	if err != nil {
		errs = append(errs, err)
	} else if len(adminPass) < minPasswordLength {
		errs = append(errs, fmt.Errorf(
			"config: ADMIN_PASSWORD must be at least %d characters", minPasswordLength))
	}

	storageAccessKey, err := requireEnv("STORAGE_ACCESS_KEY_ID")
	if err != nil {
		errs = append(errs, err)
	}

	storageSecretKey, err := requireEnv("STORAGE_SECRET_ACCESS_KEY")
	if err != nil {
		errs = append(errs, err)
	}

	storageUseSSL, err := strconv.ParseBool(env("STORAGE_USE_SSL", "false"))
	if err != nil {
		return nil, fmt.Errorf("config: STORAGE_USE_SSL: %w", err)
	}

	// Required even though it authenticates only a handful of internal
	// ai-agent routes: ai-agent's own INTERNAL_API_KEY is a required,
	// non-empty pydantic field (see services/ai-agent/src/config.py), so an
	// empty key here would never actually authenticate anything — it would
	// just silently break ACP bridge status/disconnect calls instead of
	// failing loudly at startup.
	aiAgentInternalKey, err := requireEnv("AI_AGENT_INTERNAL_KEY")
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return &Config{
		Env: env("ENV", "development"),
		Server: ServerConfig{
			Port:               env("PORT", "8080"),
			CookieSecure:       cookieSecure,
			PublicURL:          env("PUBLIC_URL", ""),
			CORSAllowedOrigins: parseCORSOrigins(env("CORS_ORIGINS", "*")),
		},
		Database: DatabaseConfig{
			DSN: dsn,
		},
		Redis: RedisConfig{
			URL: redisURL,
		},
		Cache: CacheConfig{
			ProjectTTL: cacheProjectTTL,
			ConfigTTL:  cacheConfigTTL,
			SprintTTL:  cacheSprintTTL,
		},
		JWT: JWTConfig{
			Secret:            secret,
			AccessTTL:         accessTTL,
			RefreshTTL:        refreshTTL,
			RefreshSessionTTL: refreshSessionTTL,
		},
		Admin: AdminConfig{
			Username: adminUser,
			Password: adminPass,
		},
		Storage: StorageConfig{
			Provider:        env("STORAGE_PROVIDER", "minio"),
			Endpoint:        env("STORAGE_ENDPOINT", "minio:9000"),
			PublicURL:       env("STORAGE_PUBLIC_URL", ""),
			Region:          env("STORAGE_REGION", "us-east-1"),
			Bucket:          env("STORAGE_BUCKET", "paca"),
			AccessKeyID:     storageAccessKey,
			SecretAccessKey: storageSecretKey,
			UseSSL:          storageUseSSL,
		},
		Security: SecurityConfig{
			// ENCRYPTION_KEY should be a 64-character lowercase hex string
			// representing 32 bytes (AES-256).
			// Backward compatibility: fall back to legacy GITHUB_ENCRYPTION_KEY.
			EncryptionKey: env("ENCRYPTION_KEY", env("GITHUB_ENCRYPTION_KEY", "")),
			// AGENT_API_KEY is optional; when set the API accepts it as a
			// static service key for the AI agent without a DB lookup.
			AgentAPIKey: env("AGENT_API_KEY", ""),
		},
		Plugins: PluginsConfig{
			// PLUGINS_STORE controls where WASM binaries are loaded from.
			// "local" reads from the local filesystem; "s3" reads from the
			// object-storage bucket configured via STORAGE_* variables.
			Store:                 env("PLUGINS_STORE", "local"),
			WASMDir:               env("PLUGINS_WASM_DIR", "./plugins/local/backend"),
			FrontendDir:           env("PLUGINS_FRONTEND_DIR", "./plugins/local/frontend"),
			MCPDir:                env("PLUGINS_MCP_DIR", "./plugins/local/mcp"),
			SkillsDir:             env("PLUGINS_SKILLS_DIR", "./plugins/local/skills"),
			S3Prefix:              env("PLUGINS_S3_PREFIX", "plugins"),
			MarketplaceCatalogURL: env("PLUGINS_MARKETPLACE_CATALOG_URL", "https://raw.githubusercontent.com/Paca-AI/paca-plugins/master/catalog/plugins.json"),
			MarketplaceTimeout:    marketplaceTimeout,
			Limits: PluginLimitsConfig{
				MaxCallDuration:     pluginMaxCallDuration,
				MaxMemoryPages:      pluginMaxMemoryPages,
				MaxRequestBodyBytes: pluginMaxRequestBodyBytes,
			},
		},
		Release: ReleaseConfig{
			Version:      env("PACA_VERSION", "dev"),
			UpstreamRepo: env("RELEASE_UPSTREAM_REPO", "Paca-AI/paca"),
			ForkRepo:     env("RELEASE_FORK_REPO", ""),
			CacheTTL:     releaseCacheTTL,
			Timeout:      releaseTimeout,
		},
		AIAgentURL:         env("AI_AGENT_URL", "http://ai-agent:8080"),
		AIAgentInternalKey: aiAgentInternalKey,
		SSHBastionHost:     env("SSH_BASTION_HOST", ""),
		PortForwardHost:    env("PORT_FORWARD_HOST", ""),
	}, nil
}

// parseCORSOrigins splits a comma-separated CORS_ORIGINS value into an
// allow-list, trimming whitespace and dropping empty entries.
func parseCORSOrigins(v string) []string {
	parts := strings.Split(v, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			origins = append(origins, p)
		}
	}
	return origins
}

// env returns the environment variable value or a fallback default.
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// requireEnv returns the value of the named environment variable, or an error
// if the variable is unset or empty.
func requireEnv(key string) (string, error) {
	if v := os.Getenv(key); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("config: %s must be set", key)
}

func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	return d, nil
}

func parseUint32(s string) (uint32, error) {
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid uint32 %q: %w", s, err)
	}
	return uint32(v), nil
}

func parseInt64(s string) (int64, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid int64 %q: %w", s, err)
	}
	return v, nil
}
