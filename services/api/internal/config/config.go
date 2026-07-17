// Package config defines the typed configuration model for the API service.
package config

import "time"

// Config holds all runtime configuration for the service.
type Config struct {
	Server     ServerConfig
	Database   DatabaseConfig
	Redis      RedisConfig
	Cache      CacheConfig
	JWT        JWTConfig
	Admin      AdminConfig
	Storage    StorageConfig
	Security   SecurityConfig
	Plugins    PluginsConfig
	OIDC       OIDCConfig
	AIAgentURL string // base URL of the ai-agent service, e.g. http://ai-agent:8080
	Env        string // development | production
}

// OIDCConfig holds settings for OIDC SSO login against the Vortex identity
// provider (ADR-038).  The feature is disabled unless Issuer is set.
type OIDCConfig struct {
	// Issuer is the OIDC issuer base URL (OIDC_ISSUER), e.g.
	// https://ai.skyplatform.net/api/identity.  Discovery metadata is read
	// from {Issuer}/.well-known/openid-configuration.
	Issuer string
	// ClientID / ClientSecret identify this API as an OAuth client
	// (OIDC_CLIENT_ID / OIDC_CLIENT_SECRET, client_secret_post).
	ClientID     string
	ClientSecret string
	// RedirectURL is the externally reachable callback URL
	// (OIDC_REDIRECT_URL).  Defaults to
	// {PUBLIC_URL}/api/v1/auth/oidc/callback when PUBLIC_URL is set.
	RedirectURL string
	// Scopes is the space-separated scope list (OIDC_SCOPES,
	// default "openid profile email").
	Scopes string
	// AutoCreateUsers enables JIT user provisioning on first SSO login
	// (OIDC_AUTO_CREATE_USERS, default true).
	AutoCreateUsers bool
	// DefaultRole is the global role name assigned to JIT-created users
	// (OIDC_DEFAULT_ROLE, default "USER"), resolved against global_roles.
	DefaultRole string
	// ButtonLabel is the label the SPA shows on the SSO button
	// (OIDC_BUTTON_LABEL, default "Sign in with Vortex").
	ButtonLabel string
}

// Enabled reports whether OIDC SSO login is configured.
func (c OIDCConfig) Enabled() bool { return c.Issuer != "" }

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port         string
	CookieSecure bool   // set Secure flag on auth cookies; enable when behind an SSL-terminating proxy
	PublicURL    string // externally reachable base URL (for example, https://paca.example.com)
}

// AdminConfig holds the default administrator credentials seeded on first startup.
type AdminConfig struct {
	Username string
	Password string
}

// DatabaseConfig holds the primary database connection settings.
type DatabaseConfig struct {
	DSN string
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	URL string
}

// CacheConfig holds TTL settings for the different cache categories.
//
// Each TTL controls how long the corresponding data is served from Valkey/Redis
// before a fresh database read is made.  Set a TTL to zero to disable caching
// for that category entirely.
//
// Environment variables (loaded by config.Load):
//
//	CACHE_PROJECT_TTL  – project + member data          (default: 5m)
//	CACHE_CONFIG_TTL   – task types, statuses, custom
//	                     field definitions, and roles    (default: 10m)
//	CACHE_SPRINT_TTL   – sprints and views               (default: 2m)
type CacheConfig struct {
	// ProjectTTL is the cache duration for project detail and member list data.
	ProjectTTL time.Duration
	// ConfigTTL is the cache duration for infrequently-changing project
	// configuration: task types, task statuses, custom field definitions, and
	// project roles.  Global roles also use this TTL.
	ConfigTTL time.Duration
	// SprintTTL is the cache duration for sprint and view configuration data.
	SprintTTL time.Duration
}

// JWTConfig holds JWT signing and expiry settings.
type JWTConfig struct {
	Secret            string
	AccessTTL         time.Duration
	RefreshTTL        time.Duration // persistent session (remember me = true)
	RefreshSessionTTL time.Duration // ephemeral session (remember me = false)
}

// StorageConfig holds object-storage settings.
// When Provider is "s3" the service connects to AWS S3 using the Region field.
// When Provider is "minio" (default) it targets the Endpoint URL.
// The bucket is created automatically on startup if it does not exist.
type StorageConfig struct {
	Provider        string // "s3" | "minio"  (default: "minio")
	Endpoint        string // MinIO URL, e.g. "minio:9000"; ignored for AWS S3
	PublicURL       string // public-facing base URL for presigned URLs, e.g. "http://localhost/storage"
	Region          string // AWS region; used for S3; also supplied to MinIO (can be any value)
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool // set true when Endpoint is HTTPS
}

// PluginsConfig holds runtime settings for the plugin subsystem.
type PluginsConfig struct {
	// Store selects where WASM plugin binaries are loaded from.
	// Accepted values: "local" (default) or "s3".
	// When "local", WASMDir is the root directory on the local filesystem.
	// When "s3", the Storage bucket and prefix are reused (STORAGE_BUCKET /
	// PLUGINS_S3_PREFIX).
	Store string

	// WASMDir is the local filesystem directory that contains plugin WASM
	// binaries.  Each plugin is expected at {WASMDir}/{pluginName}/backend.wasm.
	// Only used when Store is "local".  Defaults to "./plugins/local/backend".
	WASMDir string

	// FrontendDir is the local filesystem directory that contains extracted
	// frontend assets for installed plugins.
	// Each plugin is expected at {FrontendDir}/{pluginName}/assets/remoteEntry.js.
	FrontendDir string

	// MCPDir is the local filesystem directory that contains extracted MCP
	// bundles for installed plugins.  Served at /plugins-mcp/<pluginName>/.
	// Each plugin is expected at {MCPDir}/{pluginName}/mcp.js.
	MCPDir string

	// S3Prefix is the S3 key prefix used when Store is "s3".
	// Plugin WASM binaries are fetched from {S3Prefix}/{pluginName}/backend.wasm.
	S3Prefix string

	// MarketplaceCatalogURL points to a public JSON catalog in a GitHub repository
	// (for example, the raw URL of paca-plugins/catalog/plugins.json).
	MarketplaceCatalogURL string

	// MarketplaceTimeout is the HTTP timeout used when fetching marketplace
	// metadata and artifacts.
	MarketplaceTimeout time.Duration

	// Limits holds resource limits enforced on plugin WASM execution.
	Limits PluginLimitsConfig
}

// PluginLimitsConfig holds resource limits enforced on plugin WASM module
// execution. Mirrors platform/plugin.ResourceLimits; see that type's field
// docs for why each limit exists.
type PluginLimitsConfig struct {
	// MaxCallDuration is the maximum time allowed for a single plugin
	// function call.
	MaxCallDuration time.Duration
	// MaxMemoryPages is the maximum number of 64-KiB WASM linear-memory pages
	// a plugin module may allocate.
	MaxMemoryPages uint32
	// MaxRequestBodyBytes is the maximum size of an inbound HTTP request body
	// that may be proxied to a plugin route, and of an event payload
	// dispatched to a plugin. 0 means "no limit".
	MaxRequestBodyBytes int64
}

// SecurityConfig holds secrets used by first-party and plugin features.
type SecurityConfig struct {
	// EncryptionKey is a 32-byte AES-256 key (hex-encoded) used to encrypt
	// sensitive data at rest.
	EncryptionKey string

	// AgentAPIKey is a pre-shared secret that the AI agent service uses to
	// authenticate against the Paca API.  When set, the API accepts this key
	// via the X-API-Key header and authenticates the request as the built-in
	// agent bot user — no database lookup is required.
	// Configure via the AGENT_API_KEY environment variable.
	AgentAPIKey string
}
