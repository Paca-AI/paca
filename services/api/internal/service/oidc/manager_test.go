package oidc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/config"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	settingsdom "github.com/Paca-AI/api/internal/domain/settings"
	"github.com/Paca-AI/api/internal/platform/secret"
)

type managerSettingsRepo struct {
	mu          sync.Mutex
	row         *settingsdom.WorkspaceSettings
	withLockErr error
	writes      int
}

func (r *managerSettingsRepo) Get(context.Context) (*settingsdom.WorkspaceSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *r.row
	return &cp, nil
}

func (r *managerSettingsRepo) WithLock(_ context.Context, fn func(*settingsdom.WorkspaceSettings) (*settingsdom.WorkspaceSettings, error)) (*settingsdom.WorkspaceSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.withLockErr != nil {
		return nil, r.withLockErr
	}
	cp := *r.row
	updated, err := fn(&cp)
	if err != nil {
		return nil, err
	}
	if updated != nil {
		stored := *updated
		r.row = &stored
		r.writes++
		return updated, nil
	}
	return &cp, nil
}

type managerLoginService struct {
	name string
}

func (s *managerLoginService) BeginLogin(context.Context) (string, string, error) {
	return "https://id.example.com/authorize?provider=" + s.name, "state", nil
}

func (s *managerLoginService) Callback(context.Context, string, string) (*domainauth.TokenPair, error) {
	return &domainauth.TokenPair{AccessToken: s.name}, nil
}

type managerFactory struct {
	err     error
	configs []config.OIDCConfig
}

func (f *managerFactory) Build(_ context.Context, cfg config.OIDCConfig) (LoginService, error) {
	f.configs = append(f.configs, cfg)
	if f.err != nil {
		return nil, f.err
	}
	return &managerLoginService{name: cfg.DisplayName}, nil
}

type managerAdminGuard struct {
	allowed bool
	issuer  string
}

func (g *managerAdminGuard) HasSSOUserWithRole(_ context.Context, issuer string, _ []string) (bool, error) {
	g.issuer = issuer
	return g.allowed, nil
}

func managerEncryptor(t *testing.T) *secret.Encryptor {
	t.Helper()
	enc, err := secret.NewEncryptor([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	return enc
}

func validManagerConfig(name string) config.OIDCConfig {
	return config.OIDCConfig{
		Enabled:           true,
		IssuerURL:         "https://id.example.com/realm/",
		ClientID:          "paca",
		ClientSecret:      "secret",
		Scopes:            []string{"openid", "profile", "email"},
		RedirectURL:       "https://paca.example.com/api/v1/auth/oidc/callback",
		DisplayName:       name,
		UsernameClaim:     "preferred_username",
		LocalLoginEnabled: true,
	}
}

func newTestManager(t *testing.T, env config.OIDCConfig, repo *managerSettingsRepo, factory *managerFactory, enc *secret.Encryptor, guard *managerAdminGuard) *Manager {
	t.Helper()
	m, err := NewManager(context.Background(), ManagerDeps{
		EnvironmentConfig: env,
		EnvironmentName:   "production",
		PublicURL:         "https://paca.example.com",
		Settings:          repo,
		Encryptor:         enc,
		Factory:           factory.Build,
		AdminGuard:        guard,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestManagerUsesEnvironmentUntilDatabaseConfigured(t *testing.T) {
	repo := &managerSettingsRepo{row: &settingsdom.WorkspaceSettings{LocalLoginEnabled: true}}
	factory := &managerFactory{}
	m := newTestManager(t, validManagerConfig("Environment SSO"), repo, factory, managerEncryptor(t), &managerAdminGuard{allowed: true})

	got := m.AdminConfig()
	if got.Source != ConfigSourceEnvironment || got.DisplayName != "Environment SSO" || !got.ClientSecretConfigured {
		t.Fatalf("unexpected effective config: %+v", got)
	}
	if len(factory.configs) != 1 {
		t.Fatalf("expected environment provider construction, got %d", len(factory.configs))
	}
}

func TestManagerUpdateValidatesPersistsAndActivates(t *testing.T) {
	repo := &managerSettingsRepo{row: &settingsdom.WorkspaceSettings{LocalLoginEnabled: true}}
	factory := &managerFactory{}
	m := newTestManager(t, config.OIDCConfig{LocalLoginEnabled: true}, repo, factory, managerEncryptor(t), &managerAdminGuard{allowed: true})

	in := updateFromConfig(validManagerConfig("Database SSO"))
	got, err := m.Update(context.Background(), in, uuid.New())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Source != ConfigSourceDatabase || got.DisplayName != "Database SSO" || repo.writes != 1 {
		t.Fatalf("unexpected update result: %+v writes=%d", got, repo.writes)
	}
	options := m.LoginOptions()
	if !options.OIDCEnabled || options.DisplayName != "Database SSO" || !options.LocalLoginEnabled {
		t.Fatalf("runtime options not activated: %+v", options)
	}
	callback, err := m.Callback(context.Background(), "code", "state")
	if err != nil || callback.AccessToken != "Database SSO" {
		t.Fatalf("active service not switched: pair=%+v err=%v", callback, err)
	}
}

func TestManagerDiscoveryFailurePreservesPriorState(t *testing.T) {
	repo := &managerSettingsRepo{row: &settingsdom.WorkspaceSettings{LocalLoginEnabled: true}}
	factory := &managerFactory{}
	m := newTestManager(t, validManagerConfig("Old SSO"), repo, factory, managerEncryptor(t), &managerAdminGuard{allowed: true})
	factory.err = errors.New("discovery down")

	_, err := m.Update(context.Background(), updateFromConfig(validManagerConfig("Broken SSO")), uuid.New())
	if !errors.Is(err, ErrProviderValidation) {
		t.Fatalf("expected ErrProviderValidation, got %v", err)
	}
	if repo.writes != 0 || m.AdminConfig().DisplayName != "Old SSO" {
		t.Fatalf("failed discovery changed state: writes=%d active=%+v", repo.writes, m.AdminConfig())
	}
}

func TestManagerInvalidConfigPreservesPriorState(t *testing.T) {
	repo := &managerSettingsRepo{row: &settingsdom.WorkspaceSettings{LocalLoginEnabled: true}}
	factory := &managerFactory{}
	m := newTestManager(t, validManagerConfig("Old SSO"), repo, factory, managerEncryptor(t), &managerAdminGuard{allowed: true})

	in := updateFromConfig(validManagerConfig("Broken SSO"))
	in.IssuerURL = ""
	_, err := m.Update(context.Background(), in, uuid.New())
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
	if repo.writes != 0 || m.AdminConfig().DisplayName != "Old SSO" {
		t.Fatalf("invalid config changed state: writes=%d active=%+v", repo.writes, m.AdminConfig())
	}
}

func TestManagerPersistenceFailurePreservesPriorState(t *testing.T) {
	repo := &managerSettingsRepo{row: &settingsdom.WorkspaceSettings{LocalLoginEnabled: true}}
	factory := &managerFactory{}
	m := newTestManager(t, validManagerConfig("Old SSO"), repo, factory, managerEncryptor(t), &managerAdminGuard{allowed: true})
	repo.withLockErr = errors.New("database unavailable")

	_, err := m.Update(context.Background(), updateFromConfig(validManagerConfig("New SSO")), uuid.New())
	if err == nil {
		t.Fatal("expected persistence failure")
	}
	if m.AdminConfig().DisplayName != "Old SSO" {
		t.Fatalf("failed persistence changed active config: %+v", m.AdminConfig())
	}
}

func TestManagerEncryptsAndPreservesClientSecret(t *testing.T) {
	repo := &managerSettingsRepo{row: &settingsdom.WorkspaceSettings{LocalLoginEnabled: true}}
	factory := &managerFactory{}
	enc := managerEncryptor(t)
	m := newTestManager(t, validManagerConfig("Environment SSO"), repo, factory, enc, &managerAdminGuard{allowed: true})

	in := updateFromConfig(validManagerConfig("Database SSO"))
	in.ClientSecret = "replacement-secret"
	if _, err := m.Update(context.Background(), in, uuid.New()); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	if repo.row.OIDCClientSecretEnc == nil || *repo.row.OIDCClientSecretEnc == "replacement-secret" {
		t.Fatalf("secret was not encrypted: %v", repo.row.OIDCClientSecretEnc)
	}
	plain, err := enc.Decrypt(*repo.row.OIDCClientSecretEnc)
	if err != nil || plain != "replacement-secret" {
		t.Fatalf("stored ciphertext decrypts to %q, err=%v", plain, err)
	}

	in.DisplayName = "Renamed SSO"
	in.ClientSecret = ""
	if _, err := m.Update(context.Background(), in, uuid.New()); err != nil {
		t.Fatalf("preserving Update: %v", err)
	}
	if got := factory.configs[len(factory.configs)-1].ClientSecret; got != "replacement-secret" {
		t.Fatalf("blank replacement did not preserve secret, got %q", got)
	}
}

func TestManagerRejectsSaveWithoutEncryptor(t *testing.T) {
	repo := &managerSettingsRepo{row: &settingsdom.WorkspaceSettings{LocalLoginEnabled: true}}
	m := newTestManager(t, config.OIDCConfig{LocalLoginEnabled: true}, repo, &managerFactory{}, nil, &managerAdminGuard{allowed: true})

	_, err := m.Update(context.Background(), updateFromConfig(validManagerConfig("SSO")), uuid.New())
	if !errors.Is(err, ErrEncryptionUnavailable) || repo.writes != 0 {
		t.Fatalf("expected encryption rejection without write, err=%v writes=%d", err, repo.writes)
	}
}

func TestManagerRejectsSSOOnlyWithoutIssuerAdmin(t *testing.T) {
	repo := &managerSettingsRepo{row: &settingsdom.WorkspaceSettings{LocalLoginEnabled: true}}
	guard := &managerAdminGuard{allowed: false}
	m := newTestManager(t, config.OIDCConfig{LocalLoginEnabled: true}, repo, &managerFactory{}, managerEncryptor(t), guard)
	cfg := validManagerConfig("SSO")
	cfg.LocalLoginEnabled = false

	_, err := m.Update(context.Background(), updateFromConfig(cfg), uuid.New())
	if !errors.Is(err, ErrSSOAdminRequired) || guard.issuer != cfg.IssuerURL || repo.writes != 0 {
		t.Fatalf("expected issuer-scoped lockout guard, issuer=%q err=%v writes=%d", guard.issuer, err, repo.writes)
	}
}

func TestManagerDisabledOIDCForcesLocalLogin(t *testing.T) {
	repo := &managerSettingsRepo{row: &settingsdom.WorkspaceSettings{LocalLoginEnabled: true}}
	m := newTestManager(t, validManagerConfig("Old SSO"), repo, &managerFactory{}, managerEncryptor(t), &managerAdminGuard{allowed: true})
	in := updateFromConfig(validManagerConfig("Prepared SSO"))
	in.Enabled = false
	in.LocalLoginEnabled = false

	got, err := m.Update(context.Background(), in, uuid.New())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Enabled || !got.LocalLoginEnabled || !m.LocalLoginEnabled() || repo.row.OIDCEnabled || !repo.row.LocalLoginEnabled {
		t.Fatalf("disabled config did not force local login: got=%+v row=%+v", got, repo.row)
	}
	if _, _, err := m.BeginLogin(context.Background()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled manager BeginLogin error = %v", err)
	}
}

func TestManagerConcurrentUpdatesExposeOnlyCompleteSnapshots(t *testing.T) {
	repo := &managerSettingsRepo{row: &settingsdom.WorkspaceSettings{LocalLoginEnabled: true}}
	factory := &managerFactory{}
	initial := validManagerConfig("sso-initial")
	initial.ClientID = "client-initial"
	m := newTestManager(t, initial, repo, factory, managerEncryptor(t), &managerAdminGuard{allowed: true})

	const writerCount = 12
	const readerCount = 8
	start := make(chan struct{})
	errCh := make(chan error, writerCount+readerCount)
	var wg sync.WaitGroup

	for i := range writerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			cfg := validManagerConfig(fmt.Sprintf("sso-%d", i))
			cfg.ClientID = fmt.Sprintf("client-%d", i)
			if _, err := m.Update(context.Background(), updateFromConfig(cfg), uuid.New()); err != nil {
				errCh <- fmt.Errorf("update %d: %w", i, err)
			}
		}()
	}

	for range readerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 500 {
				cfg := m.AdminConfig()
				if strings.TrimPrefix(cfg.DisplayName, "sso-") != strings.TrimPrefix(cfg.ClientID, "client-") {
					errCh <- fmt.Errorf("mixed snapshot: display_name=%q client_id=%q", cfg.DisplayName, cfg.ClientID)
					return
				}
				options := m.LoginOptions()
				if !options.OIDCEnabled || !strings.HasPrefix(options.DisplayName, "sso-") {
					errCh <- fmt.Errorf("invalid login options: %+v", options)
					return
				}
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if repo.writes != writerCount {
		t.Fatalf("expected %d serialized writes, got %d", writerCount, repo.writes)
	}
}

func updateFromConfig(cfg config.OIDCConfig) UpdateConfig {
	return UpdateConfig{
		Enabled:           cfg.Enabled,
		IssuerURL:         cfg.IssuerURL,
		ClientID:          cfg.ClientID,
		ClientSecret:      cfg.ClientSecret,
		Scopes:            cfg.Scopes,
		RedirectURL:       cfg.RedirectURL,
		DisplayName:       cfg.DisplayName,
		UsernameClaim:     cfg.UsernameClaim,
		LocalLoginEnabled: cfg.LocalLoginEnabled,
	}
}
