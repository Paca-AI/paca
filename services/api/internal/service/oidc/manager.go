package oidc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/config"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	settingsdom "github.com/Paca-AI/api/internal/domain/settings"
	"github.com/Paca-AI/api/internal/platform/secret"
)

// LoginService is the runtime portion of an OIDC provider. *Service and the
// Manager both implement it; the latter delegates each request to its active
// immutable snapshot.
type LoginService interface {
	BeginLogin(ctx context.Context) (authURL string, state string, err error)
	Callback(ctx context.Context, code, state string) (*domainauth.TokenPair, error)
}

// ConfigSource reports which durable source produced the active snapshot.
type ConfigSource string

const (
	ConfigSourceEnvironment ConfigSource = "environment"
	ConfigSourceDatabase    ConfigSource = "database"
)

var (
	ErrDisabled              = errors.New("oidc: disabled")
	ErrInvalidConfig         = errors.New("oidc: invalid configuration")
	ErrEncryptionUnavailable = errors.New("oidc: encrypted secret storage unavailable")
	ErrProviderValidation    = errors.New("oidc: provider validation failed")
	ErrSSOAdminRequired      = settingsdom.ErrSSOAdminRequired
)

// ServiceFactory performs provider construction and Discovery for a candidate
// configuration. Bootstrap supplies the production factory; tests inject a
// deterministic implementation.
type ServiceFactory func(ctx context.Context, cfg config.OIDCConfig) (LoginService, error)

// AdminGuard checks whether SSO-only mode would retain an administrator who
// can actually authenticate through the configured issuer.
type AdminGuard interface {
	HasSSOUserWithRole(ctx context.Context, issuer string, roleNames []string) (bool, error)
}

type ManagerDeps struct {
	EnvironmentConfig config.OIDCConfig
	EnvironmentName   string
	PublicURL         string
	Settings          settingsdom.Repository
	Encryptor         *secret.Encryptor
	Factory           ServiceFactory
	AdminGuard        AdminGuard
}

// AdminConfig is the non-secret effective configuration returned to the admin
// UI. ClientSecretConfigured is the only information exposed about the secret.
type AdminConfig struct {
	Source                          ConfigSource
	Enabled                         bool
	IssuerURL                       string
	ClientID                        string
	ClientSecretConfigured          bool
	Scopes                          []string
	RedirectURL                     string
	DisplayName                     string
	UsernameClaim                   string
	LocalLoginEnabled               bool
	EncryptedSecretStorageAvailable bool
}

type UpdateConfig struct {
	Enabled           bool
	IssuerURL         string
	ClientID          string
	ClientSecret      string
	Scopes            []string
	RedirectURL       string
	DisplayName       string
	UsernameClaim     string
	LocalLoginEnabled bool
}

// LoginOptions is the small public subset consumed by /auth/config.
type LoginOptions struct {
	LocalLoginEnabled bool
	OIDCEnabled       bool
	DisplayName       string
}

type runtimeSnapshot struct {
	config  config.OIDCConfig
	source  ConfigSource
	service LoginService
}

// Manager owns the current OIDC runtime snapshot. Updates are serialized in
// one process; reads are lock-free and always observe a complete snapshot.
type Manager struct {
	updateMu        sync.Mutex
	active          atomic.Pointer[runtimeSnapshot]
	environmentName string
	publicURL       string
	settings        settingsdom.Repository
	encryptor       *secret.Encryptor
	factory         ServiceFactory
	adminGuard      AdminGuard
}

func NewManager(ctx context.Context, deps ManagerDeps) (*Manager, error) {
	if deps.Settings == nil {
		return nil, errors.New("oidc: settings repository required")
	}
	m := &Manager{
		environmentName: deps.EnvironmentName,
		publicURL:       deps.PublicURL,
		settings:        deps.Settings,
		encryptor:       deps.Encryptor,
		factory:         deps.Factory,
		adminGuard:      deps.AdminGuard,
	}

	row, err := deps.Settings.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("oidc: load settings: %w", err)
	}
	cfg := deps.EnvironmentConfig
	source := ConfigSourceEnvironment
	if row.OIDCConfigured {
		cfg, err = m.configFromRow(row)
		if err != nil {
			return nil, err
		}
		source = ConfigSourceDatabase
	}
	cfg, err = config.NormalizeOIDCConfig(cfg, deps.EnvironmentName, deps.PublicURL)
	if err != nil {
		return nil, err
	}
	snapshot, err := m.prepareSnapshot(ctx, cfg, source)
	if err != nil {
		return nil, err
	}
	m.active.Store(snapshot)
	return m, nil
}

func (m *Manager) configFromRow(row *settingsdom.WorkspaceSettings) (config.OIDCConfig, error) {
	cfg := config.OIDCConfig{
		Enabled:           row.OIDCEnabled,
		IssuerURL:         value(row.OIDCIssuerURL),
		ClientID:          value(row.OIDCClientID),
		Scopes:            splitScopes(value(row.OIDCScopes)),
		RedirectURL:       value(row.OIDCRedirectURL),
		DisplayName:       value(row.OIDCDisplayName),
		UsernameClaim:     value(row.OIDCUsernameClaim),
		LocalLoginEnabled: row.LocalLoginEnabled,
	}
	if row.OIDCClientSecretEnc != nil && *row.OIDCClientSecretEnc != "" {
		if m.encryptor == nil {
			return cfg, ErrEncryptionUnavailable
		}
		plain, err := m.encryptor.Decrypt(*row.OIDCClientSecretEnc)
		if err != nil {
			return cfg, fmt.Errorf("oidc: decrypt client secret: %w", err)
		}
		cfg.ClientSecret = plain
	}
	return cfg, nil
}

func (m *Manager) prepareSnapshot(ctx context.Context, cfg config.OIDCConfig, source ConfigSource) (*runtimeSnapshot, error) {
	snapshot := &runtimeSnapshot{config: cfg, source: source}
	if !cfg.Enabled {
		return snapshot, nil
	}
	if m.factory == nil {
		return nil, errors.New("oidc: service factory required")
	}
	service, err := m.factory(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderValidation, err)
	}
	if !cfg.LocalLoginEnabled {
		if m.adminGuard == nil {
			return nil, errors.New("oidc: administrator guard required")
		}
		allowed, err := m.adminGuard.HasSSOUserWithRole(ctx, cfg.IssuerURL, []string{"ADMIN", "SUPER_ADMIN"})
		if err != nil {
			return nil, fmt.Errorf("oidc: sso-only admin check: %w", err)
		}
		if !allowed {
			return nil, ErrSSOAdminRequired
		}
	}
	snapshot.service = service
	return snapshot, nil
}

func (m *Manager) AdminConfig() AdminConfig {
	snapshot := m.active.Load()
	if snapshot == nil {
		return AdminConfig{EncryptedSecretStorageAvailable: m.encryptor != nil}
	}
	cfg := snapshot.config
	return AdminConfig{
		Source:                          snapshot.source,
		Enabled:                         cfg.Enabled,
		IssuerURL:                       cfg.IssuerURL,
		ClientID:                        cfg.ClientID,
		ClientSecretConfigured:          cfg.ClientSecret != "",
		Scopes:                          append([]string(nil), cfg.Scopes...),
		RedirectURL:                     cfg.RedirectURL,
		DisplayName:                     cfg.DisplayName,
		UsernameClaim:                   cfg.UsernameClaim,
		LocalLoginEnabled:               cfg.LocalLoginEnabled,
		EncryptedSecretStorageAvailable: m.encryptor != nil,
	}
}

func (m *Manager) Update(ctx context.Context, in UpdateConfig, actor uuid.UUID) (AdminConfig, error) {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()

	current := m.active.Load()
	secretValue := in.ClientSecret
	if secretValue == "" && current != nil {
		secretValue = current.config.ClientSecret
	}
	cfg, err := config.NormalizeOIDCConfig(config.OIDCConfig{
		Enabled:           in.Enabled,
		IssuerURL:         in.IssuerURL,
		ClientID:          in.ClientID,
		ClientSecret:      secretValue,
		Scopes:            append([]string(nil), in.Scopes...),
		RedirectURL:       in.RedirectURL,
		DisplayName:       in.DisplayName,
		UsernameClaim:     in.UsernameClaim,
		LocalLoginEnabled: in.LocalLoginEnabled,
	}, m.environmentName, m.publicURL)
	if err != nil {
		return AdminConfig{}, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if m.encryptor == nil {
		return AdminConfig{}, ErrEncryptionUnavailable
	}

	candidate, err := m.prepareSnapshot(ctx, cfg, ConfigSourceDatabase)
	if err != nil {
		return AdminConfig{}, err
	}
	var encryptedSecret *string
	if cfg.ClientSecret != "" {
		ciphertext, err := m.encryptor.Encrypt(cfg.ClientSecret)
		if err != nil {
			return AdminConfig{}, fmt.Errorf("oidc: encrypt client secret: %w", err)
		}
		encryptedSecret = &ciphertext
	}

	_, err = m.settings.WithLock(ctx, func(row *settingsdom.WorkspaceSettings) (*settingsdom.WorkspaceSettings, error) {
		row.OIDCConfigured = true
		row.OIDCEnabled = cfg.Enabled
		row.OIDCIssuerURL = stringPtr(cfg.IssuerURL)
		row.OIDCClientID = stringPtr(cfg.ClientID)
		row.OIDCClientSecretEnc = encryptedSecret
		row.OIDCScopes = stringPtr(strings.Join(cfg.Scopes, ","))
		row.OIDCRedirectURL = stringPtr(cfg.RedirectURL)
		row.OIDCDisplayName = stringPtr(cfg.DisplayName)
		row.OIDCUsernameClaim = stringPtr(cfg.UsernameClaim)
		row.LocalLoginEnabled = cfg.LocalLoginEnabled
		row.UpdatedAt = time.Now().UTC()
		row.UpdatedBy = &actor
		return row, nil
	})
	if err != nil {
		return AdminConfig{}, err
	}

	m.active.Store(candidate)
	return m.AdminConfig(), nil
}

func (m *Manager) LocalLoginEnabled() bool {
	snapshot := m.active.Load()
	return snapshot == nil || snapshot.config.LocalLoginEnabled
}

func (m *Manager) LoginOptions() LoginOptions {
	snapshot := m.active.Load()
	if snapshot == nil {
		return LoginOptions{LocalLoginEnabled: true}
	}
	return LoginOptions{
		LocalLoginEnabled: snapshot.config.LocalLoginEnabled,
		OIDCEnabled:       snapshot.config.Enabled,
		DisplayName:       snapshot.config.DisplayName,
	}
}

func (m *Manager) BeginLogin(ctx context.Context) (string, string, error) {
	snapshot := m.active.Load()
	if snapshot == nil || snapshot.service == nil {
		return "", "", ErrDisabled
	}
	return snapshot.service.BeginLogin(ctx)
}

func (m *Manager) Callback(ctx context.Context, code, state string) (*domainauth.TokenPair, error) {
	snapshot := m.active.Load()
	if snapshot == nil || snapshot.service == nil {
		return nil, ErrDisabled
	}
	return snapshot.service.Callback(ctx, code, state)
}

func stringPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func value(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func splitScopes(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}
