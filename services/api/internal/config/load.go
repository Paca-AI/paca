// Package config loads runtime configuration from environment variables and
// optional .env files.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
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
	pluginMaxRequestBodyBytes, err := parseInt64(env("PLUGINS_MAX_REQUEST_BODY_BYTES", "10485760"))
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

	environment := env("ENV", "development")
	publicURL := env("PUBLIC_URL", "")

	oidc, oidcErr := loadOIDCConfig(environment, publicURL)
	if oidcErr != nil {
		errs = append(errs, oidcErr)
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return &Config{
		Env: environment,
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
		OIDC:               oidc,
		AIAgentURL:         env("AI_AGENT_URL", "http://ai-agent:8080"),
		AIAgentInternalKey: aiAgentInternalKey,
	}, nil
}

// loadOIDCConfig reads and validates the OIDC_* and LOCAL_LOGIN_ENABLED
// variables. All OIDC requirements fail fast at startup rather than at first
// login: an SSO-only deployment that cannot reach its IdP must not come up
// half-configured.
func loadOIDCConfig(environment, publicURL string) (OIDCConfig, error) {
	cfg := OIDCConfig{}

	enabled, err := strconv.ParseBool(env("OIDC_ENABLED", "false"))
	if err != nil {
		return cfg, fmt.Errorf("config: OIDC_ENABLED: %w", err)
	}
	cfg.Enabled = enabled

	localLogin, err := strconv.ParseBool(env("LOCAL_LOGIN_ENABLED", "true"))
	if err != nil {
		return cfg, fmt.Errorf("config: LOCAL_LOGIN_ENABLED: %w", err)
	}
	cfg.LocalLoginEnabled = localLogin

	if !enabled {
		// Password login is the only entry point when SSO is off — a stray
		// LOCAL_LOGIN_ENABLED=false would otherwise lock every human out.
		cfg.LocalLoginEnabled = true
		return cfg, nil
	}

	// The issuer is an identifier, not a normalizable URL: discovery metadata
	// and ID-token iss claims must match it exactly, and some providers issue
	// identifiers with a trailing slash. Only trim surrounding whitespace.
	cfg.IssuerURL = strings.TrimSpace(env("OIDC_ISSUER_URL", ""))
	cfg.ClientID = strings.TrimSpace(env("OIDC_CLIENT_ID", ""))
	cfg.ClientSecret = env("OIDC_CLIENT_SECRET", "")
	cfg.DisplayName = env("OIDC_DISPLAY_NAME", "Single Sign-On")

	cfg.RedirectURL = strings.TrimSpace(env("OIDC_REDIRECT_URL", ""))
	if cfg.RedirectURL == "" {
		if publicURL == "" {
			return cfg, errors.New("config: OIDC_REDIRECT_URL or PUBLIC_URL must be set when OIDC is enabled")
		}
		cfg.RedirectURL = strings.TrimRight(publicURL, "/") + "/api/v1/auth/oidc/callback"
	}

	if cfg.IssuerURL == "" {
		return cfg, errors.New("config: OIDC_ISSUER_URL must be set when OIDC is enabled")
	}
	if !strings.HasPrefix(cfg.IssuerURL, "https://") && !isLoopbackIssuer(cfg.IssuerURL) {
		return cfg, errors.New("config: OIDC_ISSUER_URL must use https (http allowed only for localhost/127.0.0.1 dev issuers)")
	}
	if cfg.ClientID == "" {
		return cfg, errors.New("config: OIDC_CLIENT_ID must be set when OIDC is enabled")
	}
	// Paca is a confidential web client: the code exchange happens
	// server-side, so a client secret is always required.
	if cfg.ClientSecret == "" {
		return cfg, errors.New("config: OIDC_CLIENT_SECRET must be set when OIDC is enabled")
	}
	if !strings.HasPrefix(cfg.RedirectURL, "https://") && environment == "production" {
		return cfg, errors.New("config: OIDC_REDIRECT_URL must be https in production")
	}

	cfg.Scopes = parseScopes(env("OIDC_SCOPES", "openid,profile,email"))
	if !slices.Contains(cfg.Scopes, "openid") {
		// "openid" is what turns an OAuth2 flow into an OIDC one — without
		// it there is no ID token to validate.
		cfg.Scopes = append([]string{"openid"}, cfg.Scopes...)
	}

	// Note: JIT provisioning is intentionally not configurable — it is
	// always on. The provisioning flow is the only supported write path for
	// user_external_identities, so a "no JIT" mode would leave first logins
	// with no way to succeed. Revisit when an admin linking API exists.
	cfg.DefaultRole = strings.TrimSpace(env("OIDC_DEFAULT_ROLE", "USER"))
	if cfg.DefaultRole == "" {
		cfg.DefaultRole = "USER"
	}
	if cfg.DefaultRole == "ADMIN" || cfg.DefaultRole == "SUPER_ADMIN" {
		// JIT users get whatever global role the IdP proves they are entitled
		// to — which is none. Elevated roles must be granted manually in
		// Paca, never handed out automatically at first login.
		return cfg, fmt.Errorf("config: OIDC_DEFAULT_ROLE must not be %s — grant elevated roles manually in Paca", cfg.DefaultRole)
	}

	cfg.UsernameClaim = env("OIDC_USERNAME_CLAIM", "preferred_username")

	return cfg, nil
}

// parseScopes splits a comma-separated scope list, trimming whitespace and
// dropping empty entries.
func parseScopes(v string) []string {
	parts := strings.Split(v, ",")
	scopes := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			scopes = append(scopes, p)
		}
	}
	return scopes
}

// isLoopbackIssuer reports whether an http:// issuer URL points at the local
// machine — the only case where plain http is acceptable (local development
// with a containerized Keycloak/Authentik).
func isLoopbackIssuer(issuer string) bool {
	u, err := url.Parse(issuer)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
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
